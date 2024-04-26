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
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	commonv1pb "github.com/dapr/dapr/pkg/proto/common/v1"
	runtimev1pb "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/dapr/tests/integration/framework"
	"github.com/dapr/dapr/tests/integration/framework/process/daprd"
	"github.com/dapr/dapr/tests/integration/framework/process/http/app"
	"github.com/dapr/dapr/tests/integration/framework/process/scheduler"
	"github.com/dapr/dapr/tests/integration/suite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
)

func init() {
	suite.Register(new(httpcallback))
}

type httpcallback struct {
	daprd     *daprd.Daprd
	scheduler *scheduler.Scheduler
	//jobChan   chan *runtimev1pb.JobEventRequest

	jobChan chan *commonv1pb.InvokeRequest
}

func (h *httpcallback) Setup(t *testing.T) []framework.Option {
	h.scheduler = scheduler.New(t)

	//h.jobChan = make(chan *runtimev1pb.JobEventRequest, 1)
	h.jobChan = make(chan *commonv1pb.InvokeRequest, 1)
	srv := app.New(t,
		app.WithHandlerFunc("/dapr/receiveJobs/test", func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			//_, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "Error reading request body", http.StatusInternalServerError)
				return
			}

			fmt.Printf("CASSIE in handler func. Body: %+v", r.Body)
			invokeRequest := &commonv1pb.InvokeRequest{
				Method: "/dapr/receiveJobs/test",
				Data: &anypb.Any{
					TypeUrl: "type.googleapis.com/google.type.Expr",
					Value:   body,
				},
			}
			
			h.jobChan <- invokeRequest
			//h.jobChan <- &jobEventRequest
			w.WriteHeader(http.StatusOK)
		}),
	)

	h.daprd = daprd.New(t,
		daprd.WithSchedulerAddresses(h.scheduler.Address()),
		daprd.WithAppPort(srv.Port()),
		daprd.WithAppProtocol("http"),
		daprd.WithLogLevel("debug"),
	)

	return []framework.Option{
		framework.WithProcesses(h.scheduler, srv, h.daprd),
	}
}

func (h *httpcallback) Run(t *testing.T, ctx context.Context) {
	h.scheduler.WaitUntilRunning(t, ctx)
	h.daprd.WaitUntilRunning(t, ctx)

	client := h.daprd.GRPCClient(t, ctx)

	t.Run("app receives triggered job", func(t *testing.T) {
		h.receiveJob(t, ctx, client)
	})

}

func (h *httpcallback) receiveJob(t *testing.T, ctx context.Context, client runtimev1pb.DaprClient) {
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
	case job := <-h.jobChan:
		fmt.Printf("\n\nGOT JOB: %+v", job)

		assert.NotNil(t, job)
		assert.Equal(t, job.GetMethod(), "/dapr/receiveJobs/test")

		var data jobData
		dataBytes := job.GetData().GetValue()

		err := json.Unmarshal(dataBytes, &data)
		require.NoError(t, err)

		decodedValue, err := base64.StdEncoding.DecodeString(data.Value)
		require.NoError(t, err)

		expectedVal := strings.TrimSpace(string(decodedValue))
		assert.Equal(t, expectedVal, `{"expression": "val"}`)
		fmt.Printf("Decoded value: %s", strings.TrimSpace(string(decodedValue)))

	case <-time.After(time.Second * 3):
		assert.Fail(t, "timed out waiting for triggered job")
	}
}
