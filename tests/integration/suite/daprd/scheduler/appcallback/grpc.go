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

package appcallback

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	commonv1pb "github.com/dapr/dapr/pkg/proto/common/v1"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/dapr/tests/integration/framework"
	"github.com/dapr/dapr/tests/integration/framework/process/daprd"
	"github.com/dapr/dapr/tests/integration/framework/process/grpc/app"
	"github.com/dapr/dapr/tests/integration/framework/process/scheduler"
	"github.com/dapr/dapr/tests/integration/suite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	suite.Register(new(grpc))
}

type grpc struct {
	daprd     *daprd.Daprd
	scheduler *scheduler.Scheduler
	jobChan   chan *commonv1pb.InvokeRequest
}

type jobData struct {
	TypeURL string `json:"type_url"`
	Value   string `json:"value"`
}

func (g *grpc) Setup(t *testing.T) []framework.Option {
	g.scheduler = scheduler.New(t,
		scheduler.WithLogLevel("debug"),
	)

	g.jobChan = make(chan *commonv1pb.InvokeRequest, 1)
	srv := app.New(t,
		app.WithOnInvokeFn(func(ctx context.Context, in *commonv1pb.InvokeRequest) (*commonv1pb.InvokeResponse, error) {
			g.jobChan <- in
			return &commonv1pb.InvokeResponse{
				Data: &anypb.Any{
					TypeUrl: "type.googleapis.com/google.type.Expr",
					Value:   []byte(`{"expression": "val"}`),
				},
			}, nil
		}),
	)

	g.daprd = daprd.New(t,
		daprd.WithSchedulerAddresses(g.scheduler.Address()),
		daprd.WithAppPort(srv.Port(t)),
		daprd.WithAppProtocol("grpc"),
	)

	return []framework.Option{
		framework.WithProcesses(g.scheduler, srv, g.daprd),
	}
}

func (g *grpc) Run(t *testing.T, ctx context.Context) {
	g.scheduler.WaitUntilRunning(t, ctx)
	g.daprd.WaitUntilRunning(t, ctx)

	client := g.daprd.GRPCClient(t, ctx)

	t.Run("app receives triggered job", func(t *testing.T) {
		g.receiveJob(t, ctx, client)
	})

}

func (g *grpc) receiveJob(t *testing.T, ctx context.Context, client runtimev1pb.DaprClient) {
	t.Helper()

	req := &runtimev1pb.ScheduleJobRequest{
		Job: &runtimev1pb.Job{
			Name:     "test",
			Schedule: "@every 1s",
			Repeats:  1,
			Data: &anypb.Any{
				TypeUrl: "type.googleapis.com/google.type.Expr",
				Value:   []byte(`{"expression": "val"}`),
			},
		},
	}
	_, err := client.ScheduleJob(ctx, req)
	require.NoError(t, err)

	select {
	case job := <-g.jobChan:
		assert.NotNil(t, job)
		assert.Equal(t, job.GetMethod(), "watchJobs/test")

		var data jobData
		dataBytes := job.GetData().GetValue()

		err := json.Unmarshal(dataBytes, &data)
		require.NoError(t, err)

		decodedValue, err := base64.StdEncoding.DecodeString(data.Value)
		require.NoError(t, err)

		expectedVal := strings.TrimSpace(string(decodedValue))
		assert.Equal(t, expectedVal, `{"expression": "val"}`)

	case <-time.After(time.Second * 3):
		assert.Fail(t, "timed out waiting for triggered job")
	}
}
