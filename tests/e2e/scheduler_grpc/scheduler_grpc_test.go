//go:build e2e
// +build e2e

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

package scheduler_grpc_e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dapr/dapr/tests/e2e/utils"
	kube "github.com/dapr/dapr/tests/platforms/kubernetes"
	"github.com/dapr/dapr/tests/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appPortGRPC = 3001
	appName     = "schedulerapp-grpc"
	//numHealthChecks           = 2                                      // Number of get calls before starting tests.
	//numIterations             = 4                                      // Number of times each test should run.
	numIterations             = 1                                      // Number of times each test should run.
	jobName                   = "testjob"                              // Job name.
	scheduleJobURLFormat      = "%s/scheduleJob/" + jobName + "-%s-%s" // App Schedule Job URL.
	getTriggeredJobsURLFormat = "%s/getTriggeredJobs"                  // App Get the Triggered Jobs URL.
	//numJobsPerThread          = 10                                     // Number of get calls before starting tests.
	numJobsPerThread = 1 // Number of get calls before starting tests.
)

type triggeredJob struct {
	TypeURL string `json:"type_url"`
	Value   string `json:"value"`
}

type jobData struct {
	DataType   string `json:"@type"`
	Expression string `json:"expression"`
}

type job struct {
	Data     jobData `json:"data,omitempty"`
	Schedule string  `json:"schedule,omitempty"`
	Repeats  int     `json:"repeats,omitempty"`
	DueTime  string  `json:"dueTime,omitempty"`
}

var tr *runner.TestRunner

func TestMain(m *testing.M) {
	utils.SetupLogs("scheduler_grpc")
	utils.InitHTTPClient(true)

	testApps := []kube.AppDescription{
		{
			AppName:             appName,
			DaprEnabled:         true,
			DebugLoggingEnabled: true,
			ImageName:           "e2e-schedulerapp_grpc",
			Replicas:            1,
			IngressEnabled:      true,
			MetricsEnabled:      true,
			AppProtocol:         "grpc",
			AppPort:             appPortGRPC, //grpc appPort
		},
		//{
		//	AppName:             appName,//http
		//	DaprEnabled:         true,
		//	DebugLoggingEnabled: true,
		//	ImageName:           "e2e-schedulerapp",
		//	Replicas:            1,
		//	IngressEnabled:      true,
		//	MetricsEnabled:      true,
		//	AppProtocol:         "http",
		//},
	}

	tr = runner.NewTestRunner(appName, testApps, nil, nil)
	os.Exit(tr.Start(m))
}

func TestJobTriggered(t *testing.T) {
	externalURL := "127.0.0.1:3000"
	//externalURL := tr.Platform.AcquireAppExternalURL(appName)
	require.NotEmpty(t, externalURL, "external URL must not be empty!")

	time.Sleep(8 * time.Second)

	// Makes the test wait for the apps and load balancers to be ready
	err := utils.HealthCheckApps(externalURL)
	require.NoError(t, err, "Health checks failed")

	data := jobData{
		DataType:   "type.googleapis.com/google.type.Expr",
		Expression: "expression",
	}

	j := job{
		Data:     data,
		Schedule: "@every 1s",
		Repeats:  1,
		DueTime:  "1s",
	}
	jobBody, err := json.Marshal(j)
	require.NoError(t, err)

	time.Sleep(6 * time.Second)

	t.Run("Schedule job and app should receive triggered job.", func(t *testing.T) {
		var wg sync.WaitGroup
		for iteration := 1; iteration <= numIterations; iteration++ {
			wg.Add(1)
			go func(iteration int) {
				defer wg.Done()
				t.Logf("Running iteration %d out of %d ...", iteration, numIterations)

				for i := 0; i < numJobsPerThread; i++ {
					// Call app to schedule job, send job to app
					log.Printf("Scheduling job: testjob-%s-%s", strconv.Itoa(iteration), strconv.Itoa(i))
					log.Printf("Scheduling job to this endpoint: %s", fmt.Sprintf(scheduleJobURLFormat, externalURL, strconv.Itoa(iteration), strconv.Itoa(i)))

					_, code, err := utils.HTTPPostWithStatus(fmt.Sprintf(scheduleJobURLFormat, externalURL, strconv.Itoa(iteration), strconv.Itoa(i)), jobBody)
					log.Printf("Scheduling job err: %s, code: %d", err, code)

					require.NoError(t, err)
					require.Equal(t, http.StatusOK, code)

				}
			}(iteration)
		}
		wg.Wait()

		assert.Eventually(t, func() bool {
			log.Println("Checking the count of stored triggered jobs equals the scheduled count of jobs")
			// Call the app endpoint to get triggered jobs
			log.Printf("Getting job from this endpoint: %s", fmt.Sprintf(getTriggeredJobsURLFormat, externalURL))

			resp, err := utils.HTTPGet(fmt.Sprintf(getTriggeredJobsURLFormat, externalURL))
			require.NoError(t, err)

			var triggeredJobs []triggeredJob
			err = json.Unmarshal([]byte(resp), &triggeredJobs)
			require.NoError(t, err)

			// Check if the length of triggeredJobs matches the expected length of scheduled jobs
			return len(triggeredJobs) == numIterations*numJobsPerThread
			//}, 5*time.Second, 50*time.Millisecond)
		}, 1*time.Second, 50*time.Millisecond)
		t.Log("Done.")
	})
}
