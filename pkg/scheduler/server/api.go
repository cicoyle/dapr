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
	"strings"
	"time"

	etcdcron "github.com/diagridio/go-etcd-cron"
	"google.golang.org/grpc/codes"

	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
	"github.com/dapr/dapr/pkg/proto/runtime/v1"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	"github.com/dapr/dapr/pkg/scheduler/server/internal"
	timeutils "github.com/dapr/kit/time"
)

func (s *Server) ScheduleJob(ctx context.Context, req *schedulerv1pb.ScheduleJobRequest) (*schedulerv1pb.ScheduleJobResponse, error) {
	// TODO(artursouza): Add authorization check between caller and request.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	startTime, err := parseStartTime(req.GetJob().GetDueTime())
	if err != nil {
		return nil, fmt.Errorf("error parsing due time: %w", err)
	}

	metadata := req.GetMetadata()
	if metadata == nil {
		metadata = map[string]string{}
	}

	ttl, err := parseTTL(req.GetJob().GetTtl())
	if err != nil {
		return nil, fmt.Errorf("error parsing TTL: %v", err)
	}
	expiration := time.Time{}
	if ttl > 0 {
		expiration = time.Now().Add(ttl)
	}

	fqn, err := (&jobFQN{
		metadata:  req.GetMetadata(),
		namespace: req.GetNamespace(),
		appID:     req.GetAppId(),
		jobName:   req.GetJob().GetName(),
	}).toString()
	if err != nil {
		return nil, err
	}

	// This is required for trigger function to have full context.
	// The job object does not have Dapr-specific attributes.
	metadata["namespace"] = req.Namespace
	metadata["appID"] = req.AppId

	job := etcdcron.Job{
		Name:       fqn,
		Rhythm:     req.GetJob().GetSchedule(),
		Repeats:    req.GetJob().GetRepeats(),
		StartTime:  startTime,
		Expiration: expiration,
		Payload:    req.GetJob().GetData(),
		Metadata:   metadata,
	}

	log.Infof("Adding job %s", fqn)
	err = s.cron.AddJob(ctx, job)
	if err != nil {
		log.Errorf("error scheduling job %s: %s", fqn, err)
		return nil, err
	}
	log.Infof("Added job %s", fqn)

	return &schedulerv1pb.ScheduleJobResponse{}, nil
}

func (s *Server) triggerJob(ctx context.Context, req etcdcron.TriggerRequest) (etcdcron.TriggerResult, error) {
	fmt.Printf("Triggering job: %s\n", req.JobName)
	metadata := req.Metadata
	actorType := metadata["actorType"]
	actorID := metadata["actorID"]
	reminderName := metadata["reminder"]
	triggerMetadata := map[string][]string{}
	if req.Metadata["namespace"] != "" {
		triggerMetadata["namespace"] = []string{req.Metadata["namespace"]}
	}
	if actorType != "" && actorID != "" && reminderName != "" {
		if s.actorRuntime == nil {
			return etcdcron.Failure, fmt.Errorf("actor runtime is not configured")
		}

		invokeMethod := "remind/" + reminderName
		contentType := metadata["content-type"]
		invokeReq := internalv1pb.NewInternalInvokeRequest(invokeMethod).
			WithMetadata(triggerMetadata).
			WithActor(actorType, actorID).
			WithData(req.Payload.GetValue()).
			WithContentType(contentType)

		res, err := s.actorRuntime.Call(ctx, invokeReq)
		if err != nil {
			return etcdcron.Failure, err
		}

		retCode := int(res.GetStatus().GetCode())
		// The return code varies based on the actor being HTTP or gRPC
		if (retCode != int(codes.OK)) && (retCode != 200) {
			return etcdcron.Failure, nil
		}

		return etcdcron.OK, err
	} else {
		// Normal job type to trigger
		triggeredJob := &schedulerv1pb.WatchJobsResponse{
			Data:     req.Payload,
			Metadata: metadata,
		}

		s.jobTriggerChan <- triggeredJob // send job to be consumed and sent to sidecar from WatchJobs()

		return etcdcron.OK, nil
	}
}

func (s *Server) DeleteJob(ctx context.Context, req *schedulerv1pb.DeleteJobRequest) (*schedulerv1pb.DeleteJobResponse, error) {
	// TODO(artursouza): Add authorization check between caller and request.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	fqn, err := (&jobFQN{
		metadata:  req.GetMetadata(),
		namespace: req.GetNamespace(),
		appID:     req.GetAppId(),
		jobName:   req.GetJobName(),
	}).toString()
	if err != nil {
		return nil, err
	}

	err = s.cron.DeleteJob(ctx, fqn)
	if err != nil {
		log.Errorf("error deleting job %s: %s", fqn, err)
		return nil, err
	}

	return &schedulerv1pb.DeleteJobResponse{}, nil
}

func (s *Server) GetJob(ctx context.Context, req *schedulerv1pb.GetJobRequest) (*schedulerv1pb.GetJobResponse, error) {
	// TODO(artursouza): Add authorization check between caller and request.
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	fqn, err := (&jobFQN{
		metadata:  req.GetMetadata(),
		namespace: req.GetNamespace(),
		appID:     req.GetAppId(),
		jobName:   req.GetJobName(),
	}).toString()
	if err != nil {
		return nil, err
	}

	job, err := s.cron.FetchJob(ctx, fqn)
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, fmt.Errorf("job not found: %s", fqn)
	}

	ttl := ""
	if !job.Expiration.IsZero() {
		ttl = time.Until(job.Expiration).String()
	}

	dueTime := ""
	if !job.StartTime.IsZero() {
		dueTime = job.StartTime.Format(time.RFC3339)
	}

	return &schedulerv1pb.GetJobResponse{
		Job: &runtime.Job{
			Name:     req.GetJobName(),
			Schedule: job.Rhythm,
			Repeats:  job.Repeats,
			Ttl:      ttl,
			DueTime:  dueTime,
			Data:     job.Payload,
		},
	}, nil
}

// parseStartTime is a wrapper around timeutils.ParseTime that truncates the time to seconds.
func parseStartTime(dueTime string) (time.Time, error) {
	if dueTime == "" {
		return time.Time{}, nil
	}

	now := time.Now()
	t, err := timeutils.ParseTime(dueTime, &now)
	if err != nil {
		return t, err
	}
	t = t.Truncate(time.Second)
	return t, nil
}

func parseTTL(ttl string) (time.Duration, error) {
	if ttl == "" {
		return time.Duration(0), nil
	}

	years, months, days, period, _, err := timeutils.ParseDuration(ttl)
	if err != nil {
		return time.Duration(0), fmt.Errorf("parse error: %w", err)
	}
	if (years == 0) && (months == 0) && (days == 0) {
		// Optimization to avoid the complex calculation below
		return period, nil
	}

	return time.Until(time.Now().AddDate(years, months, days).Add(period)), nil
}

// WatchJobs sends jobs to Dapr sidecars upon component changes.
func (s *Server) WatchJobs(req *schedulerv1pb.WatchJobsRequest, stream schedulerv1pb.Scheduler_WatchJobsServer) error {
	conn := &internal.Connection{
		Namespace: req.GetNamespace(),
		AppID:     req.GetAppId(),
		Stream:    stream,
	}

	s.sidecarConnChan <- conn

	select {
	case <-s.closeCh:
	case <-stream.Context().Done():
	}

	log.Infof("Removing a Sidecar connection from Scheduler for appID: %s.", req.GetAppId())
	s.connectionPool.Remove(req.GetNamespace(), req.GetAppId(), conn)
	return nil
}

type jobFQN struct {
	metadata  map[string]string
	namespace string
	appID     string
	jobName   string
}

// Composes the job's FQN that is used in the job store.
func (j *jobFQN) toString() (string, error) {
	scope := ""
	actorType := ""
	actorID := ""
	reminderName := ""
	if j.metadata != nil {
		scope = j.metadata["scope"]
		actorType = j.metadata["actorType"]
		actorID = j.metadata["actorID"]
		reminderName = j.metadata["reminder"]
	}

	appID := j.appID
	if scope == "namespace" {
		appID = "*"
	}

	if actorType != "" {
		if actorID == "" {
			return "", fmt.Errorf("actorID expected but not present in reminder")
		}
		if reminderName == "" {
			return "", fmt.Errorf("reminder's name expected but not present in reminder")
		}

		return strings.Join([]string{j.namespace, appID, "reminder", actorType, actorID, reminderName}, "||"), nil
	}

	return strings.Join([]string{j.namespace, appID, "job", j.jobName}, "||"), nil
}
