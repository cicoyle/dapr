/*
Copyright 2024 The Dapr Authors
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or impliei.
See the License for the specific language governing permissions and
limitations under the License.
*/

package scheduler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	rtv1 "github.com/dapr/dapr/pkg/proto/runtime/v1"
	"github.com/dapr/dapr/tests/integration/framework"
	"github.com/dapr/dapr/tests/integration/framework/process/daprd"
	prochttp "github.com/dapr/dapr/tests/integration/framework/process/http"
	"github.com/dapr/dapr/tests/integration/framework/process/placement"
	procscheduler "github.com/dapr/dapr/tests/integration/framework/process/scheduler"
	"github.com/dapr/dapr/tests/integration/framework/util"
	"github.com/dapr/dapr/tests/integration/suite"
)

func init() {
	suite.Register(new(remote))
}

type remote struct {
	daprd1    *daprd.Daprd
	daprd2    *daprd.Daprd
	place     *placement.Placement
	scheduler *procscheduler.Scheduler

	actorIDsNum int
	actorIDs    []string

	lock         sync.Mutex
	methodcalled []string
}

func (r *remote) Setup(t *testing.T) []framework.Option {
	r.actorIDsNum = 500
	r.methodcalled = make([]string, 0, r.actorIDsNum)
	r.actorIDs = make([]string, r.actorIDsNum)
	for i := 0; i < r.actorIDsNum; i++ {
		uid, err := uuid.NewUUID()
		require.NoError(t, err)
		r.actorIDs[i] = uid.String()
	}

	handler := http.NewServeMux()
	handler.HandleFunc("/dapr/config", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"entities": ["myactortype"]}`))
	})
	handler.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	for _, id := range r.actorIDs {
		id := id
		handler.HandleFunc(fmt.Sprintf("/actors/myactortype/%s/method/remind/remindermethod", id), func(http.ResponseWriter, *http.Request) {
			r.lock.Lock()
			defer r.lock.Unlock()
			r.methodcalled = append(r.methodcalled, id)
		})
		handler.HandleFunc(fmt.Sprintf("/actors/myactortype/%s/method/foo", id), func(http.ResponseWriter, *http.Request) {})
	}

	r.scheduler = procscheduler.New(t)
	srv := prochttp.New(t, prochttp.WithHandler(handler))
	r.place = placement.New(t)

	r.daprd1 = daprd.New(t,
		daprd.WithInMemoryActorStateStore("mystore"),
		daprd.WithPlacementAddresses(r.place.Address()),
		daprd.WithSchedulerAddresses(r.scheduler.Address()),
		daprd.WithAppPort(srv.Port()),
	)
	r.daprd2 = daprd.New(t,
		daprd.WithInMemoryActorStateStore("mystore"),
		daprd.WithPlacementAddresses(r.place.Address()),
		daprd.WithSchedulerAddresses(r.scheduler.Address()),
		daprd.WithAppPort(srv.Port()),
	)

	return []framework.Option{
		framework.WithProcesses(r.scheduler, r.place, srv, r.daprd1, r.daprd2),
	}
}

func (r *remote) Run(t *testing.T, ctx context.Context) {
	r.scheduler.WaitUntilRunning(t, ctx)
	r.place.WaitUntilRunning(t, ctx)
	r.daprd1.WaitUntilRunning(t, ctx)
	r.daprd2.WaitUntilRunning(t, ctx)

	daprdURL := "http://localhost:" + strconv.Itoa(r.daprd1.HTTPPort()) + "/v1.0/actors/myactortype/"
	client := util.HTTPClient(t)
	for i := 0; i < r.actorIDsNum; i++ {
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, daprdURL+r.actorIDs[i]+"/method/foo", nil)
			require.NoError(t, err)
			resp, rErr := client.Do(req)
			//nolint:testifylint
			if assert.NoError(c, rErr) {
				assert.NoError(c, resp.Body.Close())
				assert.Equal(c, http.StatusOK, resp.StatusCode)
			}
		}, time.Second*10, time.Millisecond*10, "actor not ready in time")
	}

	gclient := r.daprd1.GRPCClient(t, ctx)
	for _, id := range r.actorIDs {
		_, err := gclient.RegisterActorReminder(ctx, &rtv1.RegisterActorReminderRequest{
			ActorType: "myactortype",
			ActorId:   id,
			Name:      "remindermethod",
			DueTime:   "1s",
			Data:      []byte("reminderdata"),
		})
		require.NoError(t, err)
	}

	assert.Eventually(t, func() bool {
		r.lock.Lock()
		defer r.lock.Unlock()
		return len(r.methodcalled) == r.actorIDsNum
	}, time.Second*3, time.Millisecond*10)
	assert.ElementsMatch(t, r.actorIDs, r.methodcalled)
}
