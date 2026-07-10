package protocoltest

// Regression tests over the Duo two-instance environment (see duo.go).
// The memory phase guards against the #1255 class of leak: before the fix
// this measured 823 KB/request of permanent post-GC retention; after, 0.5.
//
// Heap profiles are written when OOM_PROFILE_DIR is set:
//
//	OOM_PROFILE_DIR=/tmp go test ./internal/protocoltest/ -run TestDuo -v

import (
	"os"
	"testing"
)

func TestDuoFunctional(t *testing.T) {
	if testing.Short() {
		t.Skip("duo e2e is not a -short test")
	}
	env, err := NewDuoEnv()
	if err != nil {
		t.Fatalf("boot duo env: %v", err)
	}
	defer env.Close()

	checks := env.RunFunctionalChecks(256 * 1024)
	if len(checks) == 0 {
		t.Fatal("no functional checks ran")
	}
	for _, c := range checks {
		if !c.Pass {
			t.Errorf("check %s failed: %s", c.Name, c.Detail)
		} else {
			t.Logf("check %s ok %s", c.Name, c.Detail)
		}
	}
}

func TestDuoMemoryRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("duo e2e is not a -short test")
	}
	env, err := NewDuoEnv()
	if err != nil {
		t.Fatalf("boot duo env: %v", err)
	}
	defer env.Close()

	report, err := env.RunMemoryPhase(DuoMemoryConfig{
		ProfileDir: os.Getenv("OOM_PROFILE_DIR"),
		Progress:   t.Logf,
	})
	if err != nil {
		t.Fatalf("memory phase: %v", err)
	}

	t.Logf("body %.2f MB | slope %.1f KB/request | churn %.2f MB/request | burst peak %.2f MB (post-GC %+.2f MB)",
		float64(report.BodyBytes)/1024/1024, report.SlopeKBPerRequest,
		report.ChurnMBPerRequest, report.PeakHeapMB, report.PostBurstDeltaMB)
	if report.BaselineProfile != "" {
		t.Logf("profiles: %s %s", report.BaselineProfile, report.FinalProfile)
	}

	// The #1255 leak measured 823 KB/request here. Healthy builds measure
	// ~0.5 KB/request; 32 KB leaves generous headroom against GC noise while
	// still catching any per-request pin of a request-body-sized buffer.
	const maxSlopeKB = 32.0
	if report.SlopeKBPerRequest > maxSlopeKB {
		t.Errorf("post-GC retention slope %.1f KB/request exceeds %.0f KB/request — a per-request memory pin (see #1255)",
			report.SlopeKBPerRequest, maxSlopeKB)
	}
}
