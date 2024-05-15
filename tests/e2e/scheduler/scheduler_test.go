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

package scheduler_e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapr/dapr/tests/e2e/utils"
	kube "github.com/dapr/dapr/tests/platforms/kubernetes"
	"github.com/dapr/dapr/tests/runner"
)

const (
	appName                = "schedulerapp"
	schedulerlogsURLFormat = "%s/test/logs" // URL to fetch logs from test app.
	numHealthChecks        = 1              // Number of get calls before starting tests.
	//numIterations          = 7              // Number of times each test should run.
	numIterations             = 1                                   // Number of times each test should run.
	jobName                   = "testjob"                           // Job name.
	scheduleJobURLFormat      = "%s/scheduleJob/" + jobName + "-%s" // Schedule Job URL
	getJobURLFormat           = "%s/job/" + jobName
	getTriggeredJobsURLFormat = "%s/getTriggeredJobs"
	jobNameForGet             = "GetTestJob" // Job name for getting tests
	//numJobsPerThread             = 10           // Number of get calls before starting tests.
	numJobsPerThread = 1 // Number of get calls before starting tests.
)

type jobData struct {
	DataType   string `json:"@type"`
	Expression string `json:"expression"`
}

// TODO: unexport the fields
type job struct {
	Data     jobData `json:"data,omitempty"`
	Schedule string  `json:"schedule,omitempty"`
	Repeats  int     `json:"repeats,omitempty"`
	DueTime  string  `json:"dueTime,omitempty"`
	TTL      string  `json:"ttl,omitempty"`
}

var tr *runner.TestRunner

func TestMain(m *testing.M) {
	utils.SetupLogs("scheduler")
	utils.InitHTTPClient(false)

	testApps := []kube.AppDescription{
		{
			AppName:             appName,
			DaprEnabled:         true,
			DebugLoggingEnabled: true,
			ImageName:           "e2e-schedulerapp",
			Replicas:            1,
			IngressEnabled:      true,
			MetricsEnabled:      true,
		},
	}

	tr = runner.NewTestRunner(appName, testApps, nil, nil)
	os.Exit(tr.Start(m))
}

type triggeredJob struct {
	TypeURL string `json:"type_url"`
	Value   string `json:"value"`
}

func TestCRUD(t *testing.T) {
	//externalURL := tr.Platform.AcquireAppExternalURL(appName)
	externalURL := "localhost:3000"
	require.NotEmpty(t, externalURL, "external URL must not be empty!")

	time.Sleep(6 * time.Second) // TODO rm

	t.Logf("Checking if app is healthy ...")
	_, err := utils.HTTPGetNTimes(externalURL, numHealthChecks)
	require.NoError(t, err)

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

	time.Sleep(6 * time.Second) // TODO rm

	t.Run("Schedule job and get should succeed.", func(t *testing.T) {
		var wg sync.WaitGroup
		for iteration := 1; iteration <= numIterations; iteration++ {
			wg.Add(1)
			go func(iteration int) {
				defer wg.Done()
				t.Logf("Running iteration %d out of %d ...", iteration, numIterations)

				for i := 0; i < numJobsPerThread; i++ {
					// Call app to schedule job, send job to app
					log.Printf("Scheduling job: testjob-%s", strconv.Itoa(iteration))
					_, err = utils.HTTPPost(fmt.Sprintf(scheduleJobURLFormat, externalURL, strconv.Itoa(iteration)), jobBody)
					require.NoError(t, err)
				}
			}(iteration)
		}
		wg.Wait()

		// Call the app endpoint to get triggered jobs
		log.Println("Checking the count of stored triggered jobs equals the scheduled count of jobs")
		resp, err := utils.HTTPGet(fmt.Sprintf(getTriggeredJobsURLFormat, externalURL))
		require.NoError(t, err)

		var triggeredJobs []triggeredJob
		err = json.Unmarshal([]byte(resp), &triggeredJobs)
		require.NoError(t, err)
		assert.Len(t, triggeredJobs, numIterations)
		t.Log("Done.")
	})
}
