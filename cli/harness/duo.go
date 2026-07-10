package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/protocoltest"
)

// DuoCmd is Tier "Duo" — two full tingly-box instances in one process:
// tb2 (the gateway under test) routes to tb1's vmodel endpoint over real
// HTTP, and the harness drives Claude-Code-shaped conversations through
// tb2's protocol-conversion path. It verifies both FUNCTION (SSE shape,
// assembled content, usage propagation) and MEMORY (post-GC retention slope,
// allocation churn, concurrent-burst peak) — the setup that pinned down the
// #1255 OOM (823 KB/request retained before the fix vs 0.5 KB after).
//
//	client ──(Anthropic beta stream)──▶ tb2 ──(OpenAI Chat, real HTTP)──▶ tb1 /virtual/openai/v1
type DuoCmd struct {
	BodyMB     float64 `kong:"name='body-mb',default='2',help='Conversation size per request in MB (mimics agentic full-context turns)'"`
	Batch      int     `kong:"name='batch',default='15',help='Requests per sequential batch (two batches measure the retention slope)'"`
	Workers    int     `kong:"name='workers',default='4',help='Concurrent workers in the burst phase'"`
	PerWorker  int     `kong:"name='per-worker',default='5',help='Requests per worker in the burst phase'"`
	MaxSlopeKB float64 `kong:"name='max-slope-kb',default='32',help='Fail if post-GC retention exceeds this many KB/request'"`
	SkipMemory bool    `kong:"name='skip-memory',help='Run only the functional checks'"`
	SkipFunc   bool    `kong:"name='skip-func',help='Run only the memory phase'"`
	ProfileDir string  `kong:"name='profile-dir',help='Write pprof heap profiles (duo-baseline/duo-final.pb.gz) to this directory'"`
	JSON       bool    `kong:"name='json',help='Emit results as JSON'"`
	Verbose    bool    `kong:"name='verbose',short='v',help='Show gateway logs (default: quiet)'"`
}

// duoResult is the JSON output shape.
type duoResult struct {
	TB1URL     string                        `json:"tb1_url"`
	TB2URL     string                        `json:"tb2_url"`
	Functional []protocoltest.DuoCheck       `json:"functional,omitempty"`
	Memory     *protocoltest.DuoMemoryReport `json:"memory,omitempty"`
	Pass       bool                          `json:"pass"`
}

func (cmd *DuoCmd) Run() error {
	if !cmd.Verbose {
		// TestMode (not ReleaseMode): without a MultiLogger the access-log
		// middleware falls back to a plain logrus logger and only discards
		// its output under gin.TestMode.
		gin.SetMode(gin.TestMode)
		logrus.SetLevel(logrus.ErrorLevel)
	}

	env, err := protocoltest.NewDuoEnv()
	if err != nil {
		return fmt.Errorf("boot duo environment: %w", err)
	}
	defer env.Close()

	bodyBytes := int(cmd.BodyMB * 1024 * 1024)
	result := duoResult{TB1URL: env.TB1URL(), TB2URL: env.TB2URL(), Pass: true}

	progress := func(format string, args ...any) {
		if !cmd.JSON {
			fmt.Printf("  ▸ "+format+"\n", args...)
		}
	}

	if !cmd.SkipFunc {
		if !cmd.JSON {
			fmt.Printf("duo: functional checks (tb2 %s → tb1 %s, body %.1f MB)\n", env.TB2URL(), env.TB1URL(), cmd.BodyMB)
		}
		result.Functional = env.RunFunctionalChecks(bodyBytes)
		for _, c := range result.Functional {
			if !c.Pass {
				result.Pass = false
			}
			if !cmd.JSON {
				mark := "✔"
				if !c.Pass {
					mark = "✘"
				}
				if c.Detail != "" {
					fmt.Printf("  %s %-28s %s\n", mark, c.Name, c.Detail)
				} else {
					fmt.Printf("  %s %s\n", mark, c.Name)
				}
			}
		}
	}

	if !cmd.SkipMemory {
		if !cmd.JSON {
			fmt.Println("duo: memory phase")
		}
		report, err := env.RunMemoryPhase(protocoltest.DuoMemoryConfig{
			BodyBytes:  bodyBytes,
			Batch:      cmd.Batch,
			Workers:    cmd.Workers,
			PerWorker:  cmd.PerWorker,
			ProfileDir: cmd.ProfileDir,
			Progress:   progress,
		})
		if err != nil {
			return fmt.Errorf("memory phase: %w", err)
		}
		result.Memory = report

		slopeOK := report.SlopeKBPerRequest <= cmd.MaxSlopeKB
		if !slopeOK {
			result.Pass = false
		}
		if !cmd.JSON {
			fmt.Printf("  baseline post-GC heap      %8.2f MB\n", report.BaselineHeapMB)
			fmt.Printf("  retained after %3d reqs    %+8.2f MB\n", report.SequentialCount/2, report.AfterBatch1MB)
			fmt.Printf("  retained after %3d reqs    %+8.2f MB\n", report.SequentialCount, report.AfterBatch2MB)
			verdict := "OK"
			if !slopeOK {
				verdict = fmt.Sprintf("LEAK (limit %.1f)", cmd.MaxSlopeKB)
			}
			fmt.Printf("  retention slope            %8.1f KB/request   %s\n", report.SlopeKBPerRequest, verdict)
			fmt.Printf("  allocation churn           %8.2f MB/request\n", report.ChurnMBPerRequest)
			fmt.Printf("  burst peak heap (%d×%d)     %8.2f MB (post-GC %+.2f MB)\n",
				report.ConcurrentWorkers, report.ConcurrentTotal/report.ConcurrentWorkers, report.PeakHeapMB, report.PostBurstDeltaMB)
			if report.FinalProfile != "" {
				fmt.Printf("  heap profiles: %s , %s\n", report.BaselineProfile, report.FinalProfile)
				fmt.Printf("  inspect: go tool pprof -top -inuse_space %s\n", report.FinalProfile)
			}
		}
	}

	if cmd.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	}
	if !result.Pass {
		return fmt.Errorf("duo verification failed")
	}
	if !cmd.JSON {
		fmt.Println("duo: PASS")
	}
	return nil
}
