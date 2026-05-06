package agentboot_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/agentboot"
	"github.com/tingly-dev/tingly-box/agentboot/claude"
	"github.com/tingly-dev/tingly-box/agentboot/claude/fixture"
)

// recordingStore captures the order of session-state transitions the runner
// emits. The runner is the single owner of the state machine post-Phase 3;
// these tests fail if a future change drops or reorders the calls.
type recordingStore struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingStore) SetRunning(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "running:"+id)
	return true
}

func (r *recordingStore) SetCompleted(id, _ string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "completed:"+id)
	return true
}

func (r *recordingStore) SetFailed(id, _ string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, "failed:"+id)
	return true
}

func (r *recordingStore) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.calls))
	copy(out, r.calls)
	return out
}

// drainHandle reads all events to terminal so Wait() returns. The runner
// only calls Store.SetCompleted/SetFailed inside Wait's once-guarded path,
// so a test that doesn't drain events would never observe the terminal
// state write. Wait may return a non-nil error (e.g. agent reported a
// failure result) — that's a valid terminal too, so we don't require nil.
func drainHandle(t *testing.T, h agentboot.ExecutionHandle) {
	t.Helper()
	for range h.Events() {
		// Drop everything; we only care about terminal Store calls here.
	}
	_, _ = h.Wait()
}

// TestRunner_StoreLifecycle_Success: a successful fixture run must emit
// exactly SetRunning then SetCompleted, in order.
func TestRunner_StoreLifecycle_Success(t *testing.T) {
	store := &recordingStore{}

	agent := claude.NewAgentWithFactory(claude.Config{}, fixture.Factory(fixture.Script{
		fixture.AssistantText("ok"),
		fixture.Result(true),
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handle, err := agent.Execute(ctx, "hi", agentboot.ExecutionOptions{
		SessionID: "sess-success",
		Store:     store,
	})
	require.NoError(t, err)
	drainHandle(t, handle)

	assert.Equal(t,
		[]string{"running:sess-success", "completed:sess-success"},
		store.snapshot(),
		"successful run must transition Running → Completed in that order")
}

// TestRunner_StoreLifecycle_Failure: a fixture script ending in
// Result(false) must emit SetRunning then SetFailed.
func TestRunner_StoreLifecycle_Failure(t *testing.T) {
	store := &recordingStore{}

	agent := claude.NewAgentWithFactory(claude.Config{}, fixture.Factory(fixture.Script{
		fixture.Result(false),
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handle, err := agent.Execute(ctx, "go", agentboot.ExecutionOptions{
		SessionID: "sess-fail",
		Store:     store,
	})
	require.NoError(t, err)
	drainHandle(t, handle)

	calls := store.snapshot()
	require.Len(t, calls, 2, "expected exactly Running + Failed, got %v", calls)
	assert.Equal(t, "running:sess-fail", calls[0])
	assert.Equal(t, "failed:sess-fail", calls[1])
}

// TestRunner_StoreLifecycle_OptionalStore: omitting opts.Store must not
// panic. The runner has nil-guards on opts.Store; this test locks them.
func TestRunner_StoreLifecycle_OptionalStore(t *testing.T) {
	agent := claude.NewAgentWithFactory(claude.Config{}, fixture.Factory(fixture.Script{
		fixture.Result(true),
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handle, err := agent.Execute(ctx, "hi", agentboot.ExecutionOptions{
		SessionID: "no-store",
		// Store deliberately nil.
	})
	require.NoError(t, err)
	drainHandle(t, handle)
}

// TestRunner_StoreLifecycle_NoSessionID: an empty SessionID must skip
// store calls entirely (the runner's guard is `opts.Store != nil &&
// opts.SessionID != ""`).
func TestRunner_StoreLifecycle_NoSessionID(t *testing.T) {
	store := &recordingStore{}

	agent := claude.NewAgentWithFactory(claude.Config{}, fixture.Factory(fixture.Script{
		fixture.Result(true),
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	handle, err := agent.Execute(ctx, "hi", agentboot.ExecutionOptions{
		SessionID: "", // empty
		Store:     store,
	})
	require.NoError(t, err)
	drainHandle(t, handle)

	assert.Empty(t, store.snapshot(),
		"runner must skip store calls when SessionID is empty")
}
