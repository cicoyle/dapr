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
	etcdcron "github.com/Scalingo/go-etcd-cron"
	"github.com/dapr/dapr/pkg/api/grpc/manager"
	"github.com/dapr/dapr/pkg/messaging"
	invokev1 "github.com/dapr/dapr/pkg/messaging/v1"
	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	grpcRetry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"google.golang.org/grpc"
	"time"
)

var (
	appPort      = 3000
	daprHttpPort = 3500
	daprGrpcPort = 50001
	dialTimeout  = 1 * time.Second
)

func (s *Server) ConnectHost(context.Context, *schedulerv1pb.ConnectHostRequest) (*schedulerv1pb.ConnectHostResponse, error) {
	return nil, fmt.Errorf("not implemented")
}

// ScheduleJob is a placeholder method that needs to be implemented
func (s *Server) ScheduleJob(ctx context.Context, req *schedulerv1pb.ScheduleJobRequest) (*schedulerv1pb.ScheduleJobResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	//TODO: figure out if we need/want namespace in job name
	err := s.cron.AddJob(etcdcron.Job{ //save but not execute.
		Name:     req.Job.Name,
		Rhythm:   req.Job.Schedule,
		Repeats:  req.Job.Repeats,
		DueTime:  req.Job.DueTime, //TODO: figure out dueTime
		TTL:      req.Job.Ttl,
		Data:     req.Job.Data,
		Metadata: req.Metadata, //TODO: do I need this here? storing appID
		Func: func(context.Context) error {
			//do logic here
			//conn here

			//grpc client dapr internal call trigger job internal
			invokeReq := invokev1.NewInvokeMethodRequest("triggerJob/"). // TODO: Confirm this
											WithMetadata(map[string][]string{invokev1.DestinationIDHeader: {appID}}).
											WithDataObject(daprTriggerJobReq)
			_, err = s.directMessaging.Invoke(ctx, appID, invokeReq)
			//innerErr := s.triggerJob(req.Job, req.Namespace, req.Metadata)
			//if innerErr != nil {
			//	return innerErr
			//}
			//return nil
		},
	})
	if err != nil {
		log.Errorf("error scheduling job %s: %s", req.Job.Name, err)
		return nil, err
	}

	return &schedulerv1pb.ScheduleJobResponse{}, nil
}

func (s *Server) triggerJob(job *runtimev1pb.Job, namespace string, metadata map[string]string) error {
	//confirm spin off go routine to do this triggering
	go func() {
		_, err := s.TriggerJob(context.Background(), &schedulerv1pb.TriggerJobRequest{
			Job:       job,
			Namespace: namespace,
			Metadata:  metadata,
		})
		if err != nil {
			log.Errorf("error triggering job %s: %s", job.Name, err)
		}
	}()
	return nil

	//_, err := s.TriggerJob(context.Background(), &schedulerv1pb.TriggerJobRequest{
	//	Job:       job,
	//	Namespace: namespace,
	//	Metadata:  metadata,
	//})
	//if err != nil {
	//	log.Errorf("error triggering job %s: %s", job.Name, err)
	//	return err
	//}
	//return nil
}

// ListJobs is a placeholder method that needs to be implemented
func (s *Server) ListJobs(ctx context.Context, req *schedulerv1pb.ListJobsRequest) (*schedulerv1pb.ListJobsResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := s.cron.ListJobsByAppID(req.AppId)

	var jobs []*runtimev1pb.Job
	for _, entry := range entries {
		job := &runtimev1pb.Job{
			Name:     entry.Name,
			Schedule: entry.Rhythm,
			Repeats:  entry.Repeats,
			DueTime:  entry.DueTime,
			Ttl:      entry.TTL,
			Data:     entry.Data,
		}

		jobs = append(jobs, job)
	}

	resp := &schedulerv1pb.ListJobsResponse{Jobs: jobs}

	return resp, nil
}

// GetJob is a placeholder method that needs to be implemented
func (s *Server) GetJob(ctx context.Context, req *schedulerv1pb.JobRequest) (*schedulerv1pb.GetJobResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	//jobName := fmt.Sprintf("%s_%s", req.Metadata["app_id"], req.Job.Name)

	job := s.cron.GetJob(req.JobName)
	//job.Metadata
	if job != nil {
		resp := &schedulerv1pb.GetJobResponse{
			Job: &runtimev1pb.Job{
				Name:     job.Name,
				Schedule: job.Rhythm,
				Repeats:  job.Repeats,
				DueTime:  job.DueTime,
				Ttl:      job.TTL,
				Data:     job.Data,
			},
		}
		return resp, nil
	}

	return nil, fmt.Errorf("job not found")
}

// DeleteJob is a placeholder method that needs to be implemented
func (s *Server) DeleteJob(ctx context.Context, req *schedulerv1pb.JobRequest) (*schedulerv1pb.DeleteJobResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	err := s.cron.DeleteJob(req.JobName)
	if err != nil {
		log.Errorf("error deleting job %s: %s", req.JobName, err)
		return nil, err
	}

	return &schedulerv1pb.DeleteJobResponse{}, nil
}

//type gRPCConnectionFn func(ctx context.Context, address string, id string, namespace string, customOpts ...grpc.DialOption) (*grpc.ClientConn, func(destroy bool), error)

func (s *Server) TriggerJob(ctx context.Context, req *schedulerv1pb.TriggerJobRequest) (*schedulerv1pb.TriggerJobResponse, error) {

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}
	log.Info("Triggering job")

	appID, ok := req.Metadata["appID"]
	if !ok {
		log.Errorf("Error getting appID for job: %s", req.Job.GetName())
		return nil, fmt.Errorf("appID not found for job")
	}

	s.mu.Lock()
	connPool, ok := s.connPools[appID]
	if !ok {
		// If connection pool doesn't exist for the app ID, create one
		connPool = manager.NewConnectionPool(maxConnIdle, 0)
		s.connPools[appID] = connPool
	}
	s.mu.Unlock()

	// Connect to Dapr Sidecar with mTLS
	namespace, ok := req.Metadata["namespace"]
	connInterface, err := s.createDaprSidecarConnection(ctx, appID, namespace) //todo: confirm
	if err != nil {
		return nil, fmt.Errorf("failed to establish connection to Dapr sidecar: %v", err)
	}

	conn, ok := connInterface.(*grpc.ClientConn)
	if !ok {
		return nil, fmt.Errorf("unexpected type for connection, expected *grpc.ClientConn, got %T", connInterface)
	}

	defer conn.Close()
	defer s.connPools[appID].Release(conn)
	//defer connPool.Release(conn)

	// Create Direct Messaging Client
	//do once
	//give sechandler somewhere
	directMessaging := messaging.NewDirectMessaging(messaging.NewDirectMessagingOpts{
		ClientConnFn: func(ctx context.Context, address string, id string, namespace string, customOpts ...grpc.DialOption) (*grpc.ClientConn, func(destroy bool), error) {
			return conn, func(_ bool) {}, nil
		},
	})

	daprTriggerJobReq := &internalv1pb.TriggerJobRequest{
		Job:       req.GetJob(),
		Namespace: req.GetNamespace(),
		Metadata:  req.GetMetadata(),
	}

	//Invoke the internal daprd sidecar TriggerJob()
	//internalv1pb triggerJob is what to call
	//mv to func in addJob
	//mtls
	//invokeReq := invokev1.NewInvokeMethodRequest("triggerJob/"). // TODO: Confirm this
	//								WithMetadata(map[string][]string{invokev1.DestinationIDHeader: {appID}}).
	//								WithDataObject(daprTriggerJobReq)
	//_, err = directMessaging.Invoke(ctx, appID, invokeReq) // creates a new client
	// new client connection per trigger
	if err != nil {
		return nil, fmt.Errorf("failed to trigger job: %v", err)
	}

	return nil, nil
}

// Create a gRPC connection to Dapr sidecar with mTLS
func (s *Server) createDaprSidecarConnection(ctx context.Context, appID string, namespace string) (grpc.ClientConnInterface, error) {
	//how do I know what to put for the RequireTrustDomainFromString field???
	myAppID, err := spiffeid.FromSegments(spiffeid.RequireTrustDomainFromString("dapr"), namespace, "default", appID) //or cluster.local???
	if err != nil {
		return nil, fmt.Errorf("failed to create SpiffeID: %v", err)
	}

	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(grpcRetry.UnaryClientInterceptor()),
		s.sec.GRPCDialOptionMTLS(myAppID), grpc.WithReturnConnectionError(), //this feels wrong: confirm
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()
	// Use the connection pool to get a connection
	conn, err := s.connPools[appID].Get(func() (grpc.ClientConnInterface, error) {
		return grpc.DialContext(
			ctx,
			"localhost", // Don't hardcode: Set this to the address of your Dapr sidecar I think
			opts...,
		)
	})

	//conn, teardown, err := gRPCConnectionFn(context.TODO(), "localhost", appID, namespace)
	//if err != nil {
	//	return nil, err
	//}

	//conn, err := grpc.DialContext(ctx, "localhost", opts...) // TODO: Don't hardcode: Set this to the address of your Dapr sidecar
	//conn, err := grpc.DialContext(
	//	ctx,
	//	"localhost", // Don't hardcode: Set this to the address of your Dapr sidecar
	//	grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)),
	//	grpc.WithBlock(),
	//	grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
	//		return net.Dial("unix", addr)
	//	}),
	//)
	if err != nil {
		return nil, err
	}

	return conn, nil
}

/*
func (s *Server) TriggerJob(ctx context.Context, req *schedulerv1pb.TriggerJobRequest) (*schedulerv1pb.TriggerJobResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}
	log.Info("Triggering job")

	if appID, ok := req.Metadata["appID"]; !ok {
		log.Errorf("Error getting appID for job: %s", req.Job.GetName())
	}

	//s.daprClient.Sch

	daprTriggerJobReq := &internalv1pb.TriggerJobRequest{
		Job:       req.GetJob(),
		Namespace: req.GetNamespace(),
		Metadata:  req.GetMetadata(),
	}

	//conn, teardown, err := connectionCreatorFn(ctx, )
	//if err != nil {
	//	if teardown == nil {
	//		teardown = nopTeardown
	//	}
	//	return nil, teardown, err
	//}
	var conn grpc.ClientConnInterface
	conn, err := grpc.DialContext(
		ctx,
		daprSidecarAddress, // Set this to the address of your Dapr sidecar
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return net.Dial("unix", addr)
		}),
	)
	internalClient := internalv1pb.JobCallbackClient(conn)

	//internalClient.TriggerJob()
	//invokeReq := invokev1.NewInvokeMethodRequest()

	//directMessaging := messagingv1.DirectMessaging(messagingv1.NewDirectMessagingOpts{}).(*messagingv1.DirectMessaging)
	msg := messaging.NewDirectMessaging(messaging.NewDirectMessagingOpts{
		AppID:              appID,
		Namespace:          "",
		Port:               daprGrpcPort,
		CompStore:          nil,
		Mode:               "",
		Channels:           nil,
		ClientConnFn:       nil,
		Resolver:           nil,
		MultiResolver:      nil,
		MaxRequestBodySize: 0,
		Proxy:              nil,
		ReadBufferSize:     0,
		Resiliency:         nil,
	})
	//callback to sidecar

	msg.Invoke(ctx, appID, internalClient.TriggerJob())

	return nil, fmt.Errorf("not implemented")
}
*/

//
//func (s *Server) TriggerJob(ctx context.Context, req *schedulerv1pb.TriggerJobRequest) (*schedulerv1pb.TriggerJobResponse, error) {
//	select {
//	case <-ctx.Done():
//		return nil, ctx.Err()
//	case <-s.readyCh:
//	}
//	log.Info("Triggering job")
//
//	// Step 1: Extract AppID from Metadata
//	appID, ok := req.Metadata["appID"]
//	if !ok {
//		log.Errorf("Error getting appID for job: %s", req.Job.GetName())
//		return nil, fmt.Errorf("appID not found in metadata")
//	}
//
//	// Step 2: Create Dapr TriggerJob Request
//	daprTriggerJobReq := &internalv1pb.TriggerJobRequest{
//		Job:       req.Job.GetName(),
//		Namespace: req.Job.GetNamespace(),
//		Metadata:  req.Metadata,
//	}
//
//	// Step 3: Establish Connection to Dapr Sidecar
//	conn, err := grpc.DialContext(
//		ctx,
//		daprSidecarAddress, // Set this to the address of your Dapr sidecar
//		grpc.WithTransportCredentials(insecure.NewCredentials()),
//		grpc.WithBlock(),
//		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
//			return net.Dial("unix", addr)
//		}),
//	)
//	if err != nil {
//		return nil, fmt.Errorf("failed to establish connection to Dapr sidecar: %v", err)
//	}
//	defer conn.Close()
//
//	// Step 4: Invoke the TriggerJob Method
//	internalClient := internalv1pb.NewJobCallbackClient(conn)
//	_, err = internalClient.TriggerJob(ctx, daprTriggerJobReq)
//	if err != nil {
//		return nil, fmt.Errorf("failed to trigger job: %v", err)
//	}
//
//	// Step 5: Handle Cleanup and Error Cases
//	// Add appropriate cleanup or error handling code as needed.
//
//	return nil, nil
//}
//
