/*
Copyright 2023 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/server/v3/embed"
	"google.golang.org/grpc"

	etcdcron "github.com/diagridio/go-etcd-cron"
	etcdcron_partitioning "github.com/diagridio/go-etcd-cron/partitioning"

	"github.com/dapr/dapr/pkg/actors"
	"github.com/dapr/dapr/pkg/actors/config"
	"github.com/dapr/dapr/pkg/api/grpc/manager"
	globalconfig "github.com/dapr/dapr/pkg/config"
	"github.com/dapr/dapr/pkg/modes"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	"github.com/dapr/dapr/pkg/resiliency"
	"github.com/dapr/dapr/pkg/security"
	"github.com/dapr/kit/concurrency"
	"github.com/dapr/kit/logger"
)

var log = logger.NewLogger("dapr.scheduler.server")

const (
	// This is the namespace used to prefix all entries in etcdCron.
	// Namespace is useful to separate multiple clusters of dist scheduler.
	// We use "_" in the name to make the sure default namespace will not conflict in K8s.
	// We try to use NAMESPACE env here if set, otherwise use this default.
	// This is not the namespace of the appIds scheduling jobs, it is the ns of the scheduler.
	defaultEtcdCronNamespace = "_dapr"

	// Virtual partitions are used to determine ownership of a given job.
	// It is the upper limit of how many scheduler instances can be provisioned.
	// Not configurable by design because changing it can cause problems:
	//   - Decreasing this can cause persisted jobs with upper partitions to be ignored.
	//   - Increasing this is OK as long as all the jobs eventually run with the same version.
	//     Also, increasing is a one way operation.
	//
	// If user deploys more than this number of schedulers, it means that additional instances
	// will crash because cron will fail at start up with param validation.
	cronVirtualPartitions = 23
)

type Options struct {
	AppID            string
	HostAddress      string
	ListenAddress    string
	DataDir          string
	EtcdID           string
	EtcdInitialPeers []string
	EtcdClientPorts  []string
	Mode             modes.DaprMode
	Port             int
	ReplicaCount     int

	Security security.Handler

	PlacementAddress string
}

// Server is the gRPC server for the Scheduler service.
type Server struct {
	port          int
	srv           *grpc.Server
	listenAddress string
	mode          modes.DaprMode

	dataDir          string
	replicaCount     int
	etcdID           string
	etcdInitialPeers []string
	etcdClientPorts  map[string]string
	cron             *etcdcron.Cron
	readyCh          chan struct{}

	grpcManager  *manager.Manager
	actorRuntime actors.ActorRuntime
}

func New(opts Options) *Server {
	clientPorts := make(map[string]string)
	for _, input := range opts.EtcdClientPorts {
		idAndPort := strings.Split(input, "=")
		if len(idAndPort) != 2 {
			log.Warnf("Incorrect format for client ports: %s. Should contain <id>=<client-port>", input)
			continue
		}
		schedulerID := strings.TrimSpace(idAndPort[0])
		port := strings.TrimSpace(idAndPort[1])
		clientPorts[schedulerID] = port
	}

	s := &Server{
		port:          opts.Port,
		listenAddress: opts.ListenAddress,
		mode:          opts.Mode,

		replicaCount:     opts.ReplicaCount,
		etcdID:           opts.EtcdID,
		etcdInitialPeers: opts.EtcdInitialPeers,
		etcdClientPorts:  clientPorts,
		dataDir:          opts.DataDir,
		readyCh:          make(chan struct{}),
	}

	s.srv = grpc.NewServer(opts.Security.GRPCServerOptionMTLS())
	schedulerv1pb.RegisterSchedulerServer(s.srv, s)

	apiLevel := &atomic.Uint32{}
	apiLevel.Store(config.ActorAPILevel)

	if opts.PlacementAddress != "" {
		// Create gRPC manager
		grpcAppChannelConfig := &manager.AppChannelConfig{}
		s.grpcManager = manager.NewManager(opts.Security, opts.Mode, grpcAppChannelConfig)
		s.grpcManager.StartCollector()

		act, _ := actors.NewActors(actors.ActorsOpts{
			AppChannel:       nil,
			GRPCConnectionFn: s.grpcManager.GetGRPCConnection,
			Config: actors.Config{
				Config: config.Config{
					ActorsService:                 "placement:" + opts.PlacementAddress,
					AppID:                         opts.AppID,
					HostAddress:                   opts.HostAddress,
					Port:                          s.port,
					PodName:                       os.Getenv("POD_NAME"),
					HostedActorTypes:              config.NewHostedActors([]string{}),
					ActorDeactivationScanInterval: time.Hour, // TODO: disable this feature since we just need to invoke actors
				},
			},
			TracingSpec:     globalconfig.TracingSpec{},
			Resiliency:      resiliency.New(log),
			StateStoreName:  "",
			CompStore:       nil,
			StateTTLEnabled: false, // artursouza: this should not be relevant to invoke actors.
			Security:        opts.Security,
		})

		s.actorRuntime = act
	}
	return s
}

func (s *Server) Run(ctx context.Context) error {
	log.Info("Dapr Scheduler is starting...")

	if s.actorRuntime != nil {
		log.Info("Initializing actor runtime")
		err := s.actorRuntime.Init(ctx)
		if err != nil {
			return err
		}
	}
	return concurrency.NewRunnerManager(
		s.runServer,
		s.runEtcdCron,
	).Run(ctx)
}

func (s *Server) runServer(ctx context.Context) error {
	var listener net.Listener
	var err error

	if s.listenAddress != "" {
		listener, err = net.Listen("tcp", fmt.Sprintf("%s:%d", s.listenAddress, s.port))
		if err != nil {
			return fmt.Errorf("could not listen on port %d: %w", s.port, err)
		}
		log.Infof("Dapr Scheduler listening on: %s:%d", s.listenAddress, s.port)
	} else {
		listener, err = net.Listen("tcp", fmt.Sprintf(":%d", s.port))
		if err != nil {
			return fmt.Errorf("could not listen on port %d: %w", s.port, err)
		}
		log.Infof("Dapr Scheduler listening on port :%d", s.port)
	}

	errCh := make(chan error)
	go func() {
		log.Infof("Running gRPC server on port %d", s.port)
		if nerr := s.srv.Serve(listener); nerr != nil {
			errCh <- fmt.Errorf("failed to serve: %w", nerr)
			return
		}
	}()

	select {
	case err = <-errCh:
		return err
	case <-ctx.Done():
		s.srv.GracefulStop()
		log.Info("Scheduler GRPC server stopped")
		return nil
	}
}

func (s *Server) runEtcdCron(ctx context.Context) error {
	log.Info("Starting etcd")

	etcd, err := embed.StartEtcd(s.conf())
	if err != nil {
		return err
	}
	defer etcd.Close()

	select {
	case <-etcd.Server.ReadyNotify():
		log.Info("Etcd server is ready!")
	case <-ctx.Done():
		return ctx.Err()
	}

	log.Info("Starting EtcdCron")

	etcdEndpoints := clientEndpoints(s.etcdInitialPeers, s.etcdClientPorts)

	c, err := clientv3.New(clientv3.Config{Endpoints: etcdEndpoints})
	if err != nil {
		return err
	}

	cronInstanceId, err := extractNumberIdFromName(s.etcdID)
	if err != nil {
		return err
	}

	if cronInstanceId < 0 {
		return fmt.Errorf("invalid cron instance id: %d", cronInstanceId)
	}

	partitioning, err := etcdcron_partitioning.NewPartitioning(cronVirtualPartitions, s.replicaCount, cronInstanceId)
	if err != nil {
		return err
	}

	namespace := os.Getenv("NAMESPACE")
	if namespace == "" {
		namespace = defaultEtcdCronNamespace
	}

	cron, err := etcdcron.New(
		etcdcron.WithNamespace(namespace),
		etcdcron.WithPartitioning(partitioning),
		etcdcron.WithEtcdClient(c),
		etcdcron.WithTriggerFunc(s.triggerJob),
		etcdcron.WithErrorsHandler(func(ctx context.Context, j etcdcron.Job, err error) {
			log.Errorf("error processing job %s: %v", j.Name, err)
		}),
	)
	if err != nil {
		return fmt.Errorf("fail to create etcd-cron: %s", err)
	}

	cron.Start(ctx)

	s.cron = cron
	close(s.readyCh)

	select {
	case err := <-etcd.Err():
		return err
	case <-ctx.Done():
		log.Info("Embedded Etcd shutting down")
		cron.Wait()
		// Don't close etcd here because it has a defer close() already.
		return nil
	}
}

func clientEndpoints(initialPeersListIP []string, idToPort map[string]string) []string {
	clientEndpoints := make([]string, 0)
	for _, scheduler := range initialPeersListIP {
		idAndAddress := strings.Split(scheduler, "=")
		if len(idAndAddress) != 2 {
			log.Warnf("Incorrect format for initialPeerList: %s. Should contain <id>=http://<ip>:<peer-port>", initialPeersListIP)
			continue
		}

		id := strings.TrimSpace(idAndAddress[0])
		clientPort, ok := idToPort[id]
		if !ok {
			log.Warnf("Unable to find port from initialPeerList: %s. Should contain <id>=http://<ip>:<peer-port>", initialPeersListIP)
			continue
		}

		address := strings.TrimSpace(idAndAddress[1])
		u, err := url.Parse(address)
		if err != nil {
			log.Warnf("Unable to parse url from initialPeerList: %s. Should contain <id>=http://<ip>:<peer-port>", initialPeersListIP)
			continue
		}

		updatedURL := fmt.Sprintf("%s:%s", u.Hostname(), clientPort)

		clientEndpoints = append(clientEndpoints, updatedURL)
	}
	return clientEndpoints
}

func extractNumberIdFromName(input string) (int, error) {
	re := regexp.MustCompile(`\d+`)
	matches := re.FindAllString(input, -1)

	if len(matches) == 0 {
		return 0, fmt.Errorf("no integer found in the string")
	}

	num, err := strconv.Atoi(matches[0])
	if err != nil {
		return 0, err
	}

	return num, nil
}
