//go:build perf
// +build perf

/*
Copyright 2021 The Dapr Authors
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

package actor_reminder_perf

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dapr/dapr/tests/perf"
	"github.com/dapr/dapr/tests/perf/utils"
	kube "github.com/dapr/dapr/tests/platforms/kubernetes"
	"github.com/dapr/dapr/tests/runner"
	"github.com/dapr/dapr/tests/runner/summary"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	numHealthChecks = 60 // Number of times to check for endpoint health per app.
	actorType       = "PerfTestActorReminder"
	appName         = "perf-actor-reminder-service"

	// Number between 0 and 1 representing the actual QPS ratio to the target QPS.
	minRelativeQPS = float64(0.98)

	// Target for the QPS.
	// Calibrated with 3 and 5 replicas of scheduler service.
	// If you run with only 1 replica, the QPS is much higher.
	targetRegisterQPS = 1500

	// Trigger QPS expected in aggregate of reminders firing ev 1s.
	// Calibrated with 3 replicas of scheduler service.
	targetTriggerQPS = 520
)

var tr *runner.TestRunner

func TestMain(m *testing.M) {
	utils.SetupLogs("actor_reminder")

	testApps := []kube.AppDescription{
		{
			AppName:           appName,
			DaprEnabled:       true,
			ImageName:         "perf-actorfeatures",
			Replicas:          1,
			IngressEnabled:    true,
			AppPort:           3000,
			DaprCPULimit:      "4.0",
			DaprCPURequest:    "0.1",
			DaprMemoryLimit:   "512Mi",
			DaprMemoryRequest: "250Mi",
			AppCPULimit:       "4.0",
			AppCPURequest:     "0.1",
			AppMemoryLimit:    "800Mi",
			AppMemoryRequest:  "2500Mi",
			AppEnv: map[string]string{
				"TEST_APP_ACTOR_TYPE": actorType,
			},
		},
	}

	tr = runner.NewTestRunner("actorreminder", testApps, nil, nil)
	os.Exit(tr.Start(m))
}

func TestActorReminderRegistrationPerformance(t *testing.T) {
	p := perf.Params(
		perf.WithQPS(targetRegisterQPS),
		perf.WithConnections(8),
		perf.WithDuration("1m"),
		perf.WithPayload("{}"),
	)

	// Get the ingress external url of test app
	testAppURL := tr.Platform.AcquireAppExternalURL(appName)
	require.NotEmpty(t, testAppURL, "test app external URL must not be empty")

	// Check if test app endpoint is available
	t.Logf("test app url: %s", testAppURL+"/health")
	_, err := utils.HTTPGetNTimes(testAppURL+"/health", numHealthChecks)
	require.NoError(t, err)

	// Perform dapr test
	endpoint := fmt.Sprintf("http://127.0.0.1:3500/v1.0/actors/%v/{uuid}/reminders/myreminder", actorType)
	p.TargetEndpoint = endpoint
	p.Payload = `{"dueTime":"24h","period":"24h","ttl":"90s"}` // So reminders don't fire and test only registration.
	body, err := json.Marshal(&p)
	require.NoError(t, err)

	t.Logf("running dapr test with params: %s", body)
	daprResp, err := utils.HTTPPost(fmt.Sprintf("%s/test", testAppURL), body)
	t.Log("checking err...")
	require.NoError(t, err)
	require.NotEmpty(t, daprResp)
	// fast fail if daprResp starts with error
	require.False(t, strings.HasPrefix(string(daprResp), "error"))

	// Wait for reminder to expire.
	time.Sleep(90 * time.Second)

	appUsage, err := tr.Platform.GetAppUsage(appName)
	require.NoError(t, err)

	sidecarUsage, err := tr.Platform.GetSidecarUsage(appName)
	require.NoError(t, err)

	restarts, err := tr.Platform.GetTotalRestarts(appName)
	require.NoError(t, err)

	t.Logf("dapr test results: %s", string(daprResp))
	t.Logf("target dapr app consumed %vm CPU and %vMb of Memory", appUsage.CPUm, appUsage.MemoryMb)
	t.Logf("target dapr sidecar consumed %vm CPU and %vMb of Memory", sidecarUsage.CPUm, sidecarUsage.MemoryMb)
	t.Logf("target dapr app or sidecar restarted %v times", restarts)

	var daprResult perf.TestResult
	err = json.Unmarshal(daprResp, &daprResult)
	require.NoErrorf(t, err, "Failed to unmarshal: %s", string(daprResp))

	percentiles := map[int]string{2: "90th", 3: "99th"}

	for k, v := range percentiles {
		daprValue := daprResult.DurationHistogram.Percentiles[k].Value
		t.Logf("%s percentile: %sms", v, fmt.Sprintf("%.2f", daprValue*1000))
	}
	t.Logf("Actual QPS: %.2f, expected QPS: %d", daprResult.ActualQPS, p.QPS)

	report := perf.NewTestReport(
		[]perf.TestResult{daprResult},
		"Actor Reminder",
		sidecarUsage,
		appUsage)
	report.SetTotalRestartCount(restarts)
	err = utils.UploadAzureBlob(report)

	if err != nil {
		t.Error(err)
	}

	summary.ForTest(t).
		Service(appName).
		Client(appName).
		CPU(appUsage.CPUm).
		Memory(appUsage.MemoryMb).
		SidecarCPU(sidecarUsage.CPUm).
		SidecarMemory(sidecarUsage.MemoryMb).
		Restarts(restarts).
		ActualQPS(daprResult.ActualQPS).
		Params(p).
		OutputFortio(daprResult).
		Flush()

	assert.Equal(t, 0, daprResult.RetCodes.Num400)
	assert.Equal(t, 0, daprResult.RetCodes.Num500)
	assert.Equal(t, 0, restarts)
	assert.GreaterOrEqual(t, daprResult.ActualQPS, targetRegisterQPS*minRelativeQPS)
}

func TestActorReminderTriggerPerformance(t *testing.T) {
	// Get the ingress external url of test app
	testAppURL := tr.Platform.AcquireAppExternalURL(appName)
	require.NotEmpty(t, testAppURL, "test app external URL must not be empty")

	// Check if test app endpoint is available
	t.Logf("test app url: %s", testAppURL+"/health")
	_, err := utils.HTTPGetNTimes(testAppURL+"/health", numHealthChecks)
	require.NoError(t, err)

	t.Logf("Registering reminders ...")
	// Each reminder should fire at 1 qps
	numReminders := targetTriggerQPS
	reminderBody := []byte("{\"data\":\"reminderdata\",\"period\": \"1s\",\"ttl\":\"90s\"}")
	for numReminders > 0 {
		numReminders--
		daprResp, err := utils.HTTPPost(fmt.Sprintf("%s/call/%v/%v/reminders/myreminder", testAppURL, actorType, uuid.New().String()), reminderBody)

		t.Log("checking err...")
		require.NoError(t, err)
		require.Empty(t, daprResp)
	}

	// Reset counters to start counting all together
	{
		resp, err := utils.HTTPDelete(fmt.Sprintf("%s/counter", testAppURL))

		t.Log("checking err...")
		require.NoError(t, err)
		require.Equal(t, "0", string(resp))
	}

	// Wait for reminders to trigger.
	time.Sleep(60 * time.Second)

	// Now see how many times they triggered in aggregate.
	{
		resp, err := utils.HTTPGet(fmt.Sprintf("%s/counter", testAppURL))

		t.Log("checking err...")
		require.NoError(t, err)
		require.NotEmpty(t, string(resp))

		count, err := strconv.Atoi(string(resp))
		require.NoError(t, err)

		assert.GreaterOrEqual(t, float64(count/60.0), targetTriggerQPS*minRelativeQPS)
	}
}
