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
	"github.com/dapr/dapr/pkg/api/grpc/manager"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"net"
	"sync"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
	"google.golang.org/grpc"

	etcdcron "github.com/Scalingo/go-etcd-cron"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	"github.com/dapr/dapr/pkg/security"
	"github.com/dapr/kit/concurrency"
	"github.com/dapr/kit/logger"
)

var log = logger.NewLogger("dapr.scheduler.server")
var (
	// TODO: adjust this
	maxConnIdle = 20 * time.Minute
)

type Options struct {
	// Port is the port that the server will listen on.
	Port     int
	Security security.Handler
	//InternalSidecarClient internalv1pb.JobCallbackClient
	DaprClient runtimev1pb.DaprClient
}

// Server is the gRPC server for the Scheduler service.
type Server struct {
	port int
	srv  *grpc.Server
	sec  security.Handler
	//internalClient internalv1pb.JobCallbackClient
	daprClient runtimev1pb.DaprClient
	//connPool *manager.ConnectionPool need more than this to be indexed by app id
	connPools map[string]*manager.ConnectionPool // Map of connection pools indexed by app ID
	mu        sync.RWMutex                       // acct for concurrent access to connPools
	cron      *etcdcron.Cron
	readyCh   chan struct{}
}

func New(opts Options) *Server {
	s := &Server{
		daprClient: opts.DaprClient,
		//connPool:       manager.NewConnectionPool(maxConnIdle, 0),
		connPools: make(map[string]*manager.ConnectionPool),
		sec:       opts.Security,
		//internalClient: opts.InternalSidecarClient,
		port:    opts.Port,
		readyCh: make(chan struct{}),
	}

	s.srv = grpc.NewServer(opts.Security.GRPCServerOptionMTLS())
	schedulerv1pb.RegisterSchedulerServer(s.srv, s)

	return s
}

func (s *Server) Run(ctx context.Context) error {
	log.Info("Dapr Scheduler is starting...")
	return concurrency.NewRunnerManager(
		s.runServer,
		s.runEtcd,
	).Run(ctx)
}

func (s *Server) runServer(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("could not listen on port %d: %w", s.port, err)
	}

	errCh := make(chan error)
	go func() {
		log.Infof("Running gRPC server on port %d", s.port)
		if err := s.srv.Serve(lis); err != nil {
			errCh <- fmt.Errorf("failed to serve: %w", err)
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

func (s *Server) runEtcd(ctx context.Context) error {
	log.Info("Starting etcd")

	etcd, err := embed.StartEtcd(conf())
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
	cron, err := etcdcron.New()
	if err != nil {
		return fmt.Errorf("fail to create etcd-cron: %s", err)
	}

	cron.Start(ctx)
	defer cron.Stop()

	s.cron = cron
	close(s.readyCh)

	select {
	case err := <-etcd.Err():
		return err
	case <-ctx.Done():
		log.Info("Embedded Etcd shutting down")
		return nil
	}
}
