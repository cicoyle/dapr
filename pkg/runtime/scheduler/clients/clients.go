/*
Copyright 2024 The Dapr Authors
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

package clients

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dapr/dapr/pkg/healthz"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	"github.com/dapr/dapr/pkg/scheduler/client"
	"github.com/dapr/dapr/pkg/security"
	"github.com/dapr/kit/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var log = logger.NewLogger("dapr.runtime.scheduler.clients")

// Options contains the configuration options for the Scheduler clients.
type Options struct {
	Addresses []string
	Security  security.Handler
	Healthz   healthz.Healthz
}

// Clients builds Scheduler clients and provides those clients in a round-robin
// fashion.
type Clients struct {
	clientLock     sync.RWMutex
	addresses      []string
	sec            security.Handler
	clients        *[]schedulerv1pb.SchedulerClient
	htarget        healthz.Target
	lastUsedIdx    atomic.Uint64
	running        atomic.Bool
	readyCh        chan struct{}
	closeCh        chan struct{}
	ChangeNotifier chan struct{}
}

func New(opts Options) *Clients {
	return &Clients{
		clientLock: sync.RWMutex{},
		addresses:  opts.Addresses,
		sec:        opts.Security,
		htarget:    opts.Healthz.AddTarget(),
		readyCh:    make(chan struct{}),
		closeCh:    make(chan struct{}),
		clients:    new([]schedulerv1pb.SchedulerClient),
		//ChangeNotifier: make(chan struct{}, 1), // non-blocking
	}
}

func (c *Clients) Run(ctx context.Context) error {
	if !c.running.CompareAndSwap(false, true) {
		return errors.New("scheduler clients already running")
	}

	// watch for Schedulers scaling up/down
	// todo waitgroup
	go c.watchSchedulerHosts(ctx)
	// TODO: block until we have a list, ready channel & rm manager changes
	for {
		// call when there is a change in clients
		err := c.connectClients(ctx)
		if err == nil {
			break
		}

		log.Errorf("Failed to initialize scheduler clients: %s. Retrying...", err)

		select {
		case <-time.After(time.Second):
		case <-ctx.Done():
			return ctx.Err()
			//TODO: cassie input from channel here
		}
	}

	if len(*c.clients) > 0 {
		log.Info("Scheduler clients initialized")
	}

	close(c.readyCh)
	c.htarget.Ready()
	<-ctx.Done()
	close(c.closeCh)

	return nil
}

// RegisterChangeListener allows clients to register to be notified about updates.
func (c *Clients) RegisterChangeListener() <-chan struct{} {
	return c.ChangeNotifier
}

// Next returns the next client in a round-robin manner.
func (c *Clients) Next(ctx context.Context) (schedulerv1pb.SchedulerClient, error) {
	if len(*c.clients) == 0 {
		return nil, errors.New("no scheduler client initialized")
	}

	select {
	case <-c.readyCh:
	case <-c.closeCh:
		return nil, errors.New("scheduler clients closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	//nolint:gosec
	return (*c.clients)[int(c.lastUsedIdx.Add(1))%len(*c.clients)], nil
}

// All returns all scheduler clients.
func (c *Clients) All(ctx context.Context) ([]schedulerv1pb.SchedulerClient, error) {
	c.clientLock.RLock()
	defer c.clientLock.RUnlock()

	select {
	case <-c.readyCh:
	case <-c.closeCh:
		return nil, errors.New("scheduler clients closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return *c.clients, nil
}

func (c *Clients) connectClients(ctx context.Context) error {
	c.clientLock.Lock()
	defer c.clientLock.Unlock()

	clients := make([]schedulerv1pb.SchedulerClient, len(c.addresses))
	for i, address := range c.addresses {
		log.Debugf("Attempting to connect to Scheduler at address: %s", address)
		client, err := client.New(ctx, address, c.sec)
		if err != nil {
			return fmt.Errorf("scheduler client not initialized for address %s: %s", address, err)
		}

		log.Infof("Scheduler client initialized for address: %s", address)
		clients[i] = client
	}

	// update subscribers of change of Scheduler clients
	select {
	//case c.ChangeNotifier <- struct{}{}:
	default:
	}

	*c.clients = clients
	return nil
}

func (c *Clients) watchSchedulerHosts(ctx context.Context) {
	if len(*c.clients) == 0 {
		log.Errorf("No Scheduler clients available")
		return
	}

	for {
		var stream schedulerv1pb.Scheduler_WatchHostsClient
		var err error

		// Only connect to 1 Scheduler, but round-robin if there is an error
		for _, client := range *c.clients {
			stream, err = client.WatchHosts(ctx, &schedulerv1pb.WatchHostsRequest{})
			if err != nil {
				if status.Code(err) == codes.Unimplemented {
					log.Warnf("Scheduler WatchHosts unimplemented. Consider upgrading such that Dapr can dynamically connect to the Schedulers")
					return
				}
				log.Errorf("Failed to watch Scheduler hosts: %s. Retrying...", err)
				continue
			}
			log.Infof("Successfully established Scheduler WatchHosts stream.")
			break
		}

		if stream == nil {
			log.Errorf("No clients available for WatchHosts. Retrying...")
			time.Sleep(4 * time.Second)
			continue
		}

		// Receive host updates from the established stream
		c.handleHostUpdates(ctx, stream) // sidecar is watching scheduler for host changes
	}
}

func (c *Clients) handleHostUpdates(ctx context.Context, stream schedulerv1pb.Scheduler_WatchHostsClient) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			resp, err := stream.Recv()
			if err != nil {
				log.Errorf("Error receiving from watchHosts stream: %s", err)
				break
			}
			c.updateAddresses(resp.Hosts)
		}
	}
}

// updateAddresses is called when there is a change in Scheduler addresses, meaning the Schedulers
// were scaled up or down and the daprd sidecar needs to be made dynamically aware of the changes to
// connect to all available Schedulers. Compare list of new addresses to old addresses.
func (c *Clients) updateAddresses(updatedSchedulerAddresses []*schedulerv1pb.HostPort) {
	newAddrs := make([]string, len(updatedSchedulerAddresses))
	for i, hostPort := range updatedSchedulerAddresses {
		schedulerConn := hostPort.Host + ":" + strconv.Itoa(int(hostPort.Port))
		newAddrs[i] = schedulerConn
	}

	c.clientLock.Lock()
	defer c.clientLock.Unlock()

	// TODO: hand write this, dont use deep equal
	if !reflect.DeepEqual(c.addresses, updatedSchedulerAddresses) {
		log.Info("Detected change in Scheduler hosts, reconnecting clients.")
		c.addresses = newAddrs
		if err := c.connectClients(context.Background()); err != nil {
			log.Errorf("Failed to reconnect clients: %s", err)
		} else {
			log.Info("Scheduler clients successfully updated.")
		}
	}
}
