package multiapp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dapr/dapr/tests/integration/framework/process/http/app"
	"github.com/dapr/dapr/tests/integration/framework/process/sqlite"
	"github.com/dapr/dapr/tests/integration/framework/process/workflow"
	"github.com/dapr/durabletask-go/backend"
	"github.com/dapr/durabletask-go/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dapr/dapr/tests/integration/framework"
	"github.com/dapr/dapr/tests/integration/framework/process/daprd"
	"github.com/dapr/dapr/tests/integration/framework/process/placement"
	"github.com/dapr/dapr/tests/integration/framework/process/scheduler"
	"github.com/dapr/dapr/tests/integration/suite"
	"github.com/dapr/durabletask-go/api"
	"github.com/dapr/durabletask-go/task"
)

func init() {
	suite.Register(new(oldcallactivity))
}

// oldcallactivity demonstrates calling activities across different Dapr applications.
type oldcallactivity struct {
	daprd1 *daprd.Daprd
	daprd2 *daprd.Daprd
	place  *placement.Placement
	sched  *scheduler.Scheduler

	workflow1 *workflow.Workflow
	workflow2 *workflow.Workflow

	registry1 *task.TaskRegistry
	registry2 *task.TaskRegistry
}

func (c *oldcallactivity) Setup(t *testing.T) []framework.Option {
	c.place = placement.New(t)
	c.sched = scheduler.New(t)
	db := sqlite.New(t,
		sqlite.WithActorStateStore(true),
		sqlite.WithMetadata("busyTimeout", "10s"),
		sqlite.WithMetadata("disableWAL", "true"),
	)

	app1 := app.New(t)
	app2 := app.New(t)

	// Create registries for each app and register orchestrator/activity
	c.registry1 = task.NewTaskRegistry()
	c.registry2 = task.NewTaskRegistry()

	fmt.Println("[DEBUG] Registering activity ProcessData on app2's registry (Setup)")
	c.registry2.AddActivityN("ProcessData", func(ctx task.ActivityContext) (any, error) {
		fmt.Println("[DEBUG] >>> Entered app2 ProcessData activity handler <<<")
		var input string
		if err := ctx.GetInput(&input); err != nil {
			fmt.Printf("[ERROR] Failed to get input in app2 activity: %v\n", err)
			return nil, fmt.Errorf("failed to get input in app2 activity: %w", err)
		}
		fmt.Printf("[DEBUG] app2 activity received input: %s\n", input)
		result := fmt.Sprintf("[App2] Processed: %s", input)
		fmt.Printf("[DEBUG] app2 activity returning: %s\n", result)
		return result, nil
	})

	fmt.Println("[DEBUG] Registering orchestrator CrossAppWorkflow on app1's registry (Setup)")
	c.registry1.AddOrchestratorN("CrossAppWorkflow", func(ctx *task.OrchestrationContext) (any, error) {
		fmt.Printf("[DEBUG] Starting CrossAppWorkflow in app1 with instance ID: %s\n", ctx.ID)
		var input string
		if err := ctx.GetInput(&input); err != nil {
			fmt.Printf("[ERROR] Failed to get input in orchestrator: %v\n", err)
			return nil, fmt.Errorf("failed to get input in app1: %w", err)
		}
		fmt.Printf("[DEBUG] App1 received input: %s\n", input)
		fmt.Println("[DEBUG] Orchestrator about to call activity on app2")
		fmt.Println("[DEBUG] Calling ProcessData activity in app2")
		app2ID := "app2"
		fmt.Printf("[DEBUG] Using app2 ID: %s\n", app2ID)
		var output string
		err := ctx.CallActivity("ProcessData",
			task.WithActivityInput(input),
			task.WithAppID(app2ID)).
			Await(&output)
		if err != nil {
			fmt.Printf("[ERROR] failed to execute activity in app2: %v\n", err)
			return nil, fmt.Errorf("failed to execute activity in app2: %w", err)
		}
		fmt.Printf("[DEBUG] Received response from app2: %s\n", output)
		return output, nil
	})

	c.daprd1 = daprd.New(t,
		daprd.WithInMemoryActorStateStore("mystore"),
		daprd.WithPlacementAddresses(c.place.Address()),
		daprd.WithScheduler(c.sched),
		daprd.WithAppID("app1"),
		daprd.WithAppPort(app1.Port()),
		daprd.WithLogLevel("debug"),
	)
	c.daprd2 = daprd.New(t,
		daprd.WithInMemoryActorStateStore("mystore"),
		daprd.WithPlacementAddresses(c.place.Address()),
		daprd.WithScheduler(c.sched),
		daprd.WithAppID("app2"),
		daprd.WithAppPort(app2.Port()),
		daprd.WithLogLevel("debug"),
	)

	return []framework.Option{
		framework.WithProcesses(c.place, c.sched, db, app1, app2, c.daprd1, c.daprd2),
	}
}

func (c *oldcallactivity) Run(t *testing.T, ctx context.Context) {
	c.sched.WaitUntilRunning(t, ctx)
	c.place.WaitUntilRunning(t, ctx)
	c.daprd1.WaitUntilRunning(t, ctx)
	c.daprd2.WaitUntilRunning(t, ctx)

	// Start workflow listeners for each app
	fmt.Println("[DEBUG] Initializing workflow clients")
	client1 := client.NewTaskHubGrpcClient(c.daprd1.GRPCConn(t, ctx), backend.DefaultLogger())
	client2 := client.NewTaskHubGrpcClient(c.daprd2.GRPCConn(t, ctx), backend.DefaultLogger())

	fmt.Println("[DEBUG] Starting WorkItemListener on app1 (orchestrator)")
	err := client1.StartWorkItemListener(ctx, c.registry1)
	if err != nil {
		fmt.Printf("[ERROR] WorkItemListener on app1 exited: %v\n", err)
	}
	fmt.Println("[DEBUG] Started WorkItemListener on app1")
	fmt.Println("[DEBUG] Starting WorkItemListener on app2 (activity)")
	err = client2.StartWorkItemListener(ctx, c.registry2)
	if err != nil {
		fmt.Printf("[ERROR] WorkItemListener on app2 exited: %v\n", err)
	}
	fmt.Println("[DEBUG] Started WorkItemListener on app2")

	fmt.Println("[DEBUG] Starting the workflow")
	id, err := client1.ScheduleNewOrchestration(ctx, "CrossAppWorkflow", api.WithInput("Hello from app1"))
	if err != nil {
		fmt.Printf("[ERROR] Failed to schedule orchestration: %v\n", err)
	}
	require.NoError(t, err)
	fmt.Printf("[DEBUG] Started workflow with ID: %s\n", id)

	// Wait for completion with timeout
	fmt.Println("[DEBUG] Waiting for workflow completion")
	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	metadata, err := client1.WaitForOrchestrationCompletion(waitCtx, id, api.WithFetchPayloads(true))
	if err != nil {
		fmt.Printf("[ERROR] WaitForOrchestrationCompletion returned error: %v\n", err)
	}
	require.NoError(t, err)

	assert.True(t, api.OrchestrationMetadataIsComplete(metadata))
	fmt.Printf("[DEBUG] Workflow completed with status: %s\n", metadata.RuntimeStatus)

	assert.Equal(t, `"Processed by app2: Hello from app1"`, metadata.GetOutput().GetValue())
	fmt.Println("[DEBUG] Test completed successfully")
}
