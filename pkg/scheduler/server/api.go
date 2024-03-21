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
	"time"

	etcdcron "github.com/diagridio/go-etcd-cron"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/anypb"

	internalv1pb "github.com/dapr/dapr/pkg/proto/internals/v1"
	schedulerv1pb "github.com/dapr/dapr/pkg/proto/scheduler/v1"
	timeutils "github.com/dapr/kit/time"
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

	startTime, err := parseStartTime(req.GetJob().GetDueTime())
	if err != nil {
		return nil, fmt.Errorf("error parsing due time: %w", err)
	}
	ttl, err := parseTTL(req.GetJob().GetTtl())
	if err != nil {
		return nil, fmt.Errorf("error parsing TTL: %w", err)
	}

	job := etcdcron.Job{
		Name:      composeJobName(req.GetNamespace(), req.GetJob().GetName()),
		Rhythm:    req.GetJob().GetSchedule(),
		Repeats:   req.GetJob().GetRepeats(),
		StartTime: startTime,
		TTL:       ttl,
		Payload:   req.GetJob().GetData(),
		Metadata:  req.GetMetadata(),
	}

	err = s.cron.AddJob(ctx, job)
	if err != nil {
		log.Errorf("error scheduling job %s: %s", req.GetJob().GetName(), err)
		return nil, err
	}

	return &schedulerv1pb.ScheduleJobResponse{}, nil
}

func (s *Server) triggerJob(ctx context.Context, metadata map[string]string, payload *anypb.Any) (etcdcron.TriggerResult, error) {
	log.Debug("Triggering job")
	actorType := metadata["actorType"]
	actorID := metadata["actorId"]
	reminderName := metadata["reminder"]
	if actorType != "" && actorID != "" && reminderName != "" {
		if s.actorRuntime == nil {
			return etcdcron.Failure, fmt.Errorf("actor runtime is not configured")
		}

		invokeMethod := "remind/" + reminderName
		contentType := metadata["content-type"]
		invokeReq := internalv1pb.NewInternalInvokeRequest(invokeMethod).
			WithActor(actorType, actorID).
			WithData(payload.GetValue()).
			WithContentType(contentType)

		res, err := s.actorRuntime.Call(ctx, invokeReq)
		if err != nil {
			return etcdcron.Failure, err
		}

		if res.GetStatus().GetCode() != int32(codes.OK) {
			return etcdcron.Failure, nil
		}

		return etcdcron.OK, err
	}

	// Trigger not supported
	log.Warn("Cannot trigger job: not supported.")
	return etcdcron.Failure, nil
}

// DeleteJob is a placeholder method that needs to be implemented
func (s *Server) DeleteJob(ctx context.Context, req *schedulerv1pb.JobRequest) (*schedulerv1pb.DeleteJobResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.readyCh:
	}

	err := s.cron.DeleteJob(ctx, req.GetJobName())
	if err != nil {
		log.Errorf("error deleting job %s: %s", req.GetJobName(), err)
		return nil, err
	}

	return &schedulerv1pb.DeleteJobResponse{}, nil
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

func composeJobName(namespace, jobName string) string {
	if namespace == "" {
		return "default||" + jobName
	}

	return namespace + "||" + jobName
}
