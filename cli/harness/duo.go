package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/protocoltest"
)

// DuoCmd is Tier "Duo": two-instance e2e verification (function + memory)
// over every anthropic-source conversion route. The topology, route matrix,
// and #1255 background live on the engine — see internal/protocoltest/duo.go.
type DuoCmd struct {
	Routes     string  `kong:"name='routes',default='all',help='Comma-separated route names for the functional phase, or \"all\" (routes: beta-chat, beta-responses, beta-anthropic, v1-chat, v1-responses, v1-anthropic)'"`
	MemRoutes  string  `kong:"name='mem-routes',default='beta-chat',help='Comma-separated route names for the memory phase, or \"all\" (default: the Claude Code hot path)'"`
	BodyMB     float64 `kong:"name='body-mb',default='2',help='Conversation size per request in MB (mimics agentic full-context turns)'"`
	Batch      int     `kong:"name='batch',default='15',help='Requests per sequential batch (two batches measure the retention slope)'"`
	Workers    int     `kong:"name='workers',default='4',help='Concurrent workers in the burst phase'"`
	PerWorker  int     `kong:"name='per-worker',default='5',help='Requests per worker in the burst phase'"`
	MaxSlopeKB float64 `kong:"name='max-slope-kb',default='32',help='Fail if post-GC retention exceeds this many KB/request'"`
	SkipMemory bool    `kong:"name='skip-memory',help='Run only the functional checks'"`
	SkipFunc   bool    `kong:"name='skip-func',help='Run only the memory phase'"`
	ProfileDir string  `kong:"name='profile-dir',help='Write pprof heap profiles (duo-<route>-{baseline,final}.pb.gz) to this directory'"`
	JSON       bool    `kong:"name='json',help='Emit results as JSON'"`
	Verbose    bool    `kong:"name='verbose',short='v',help='Show gateway logs (default: quiet)'"`
}

// duoResult is the JSON output shape.
type duoResult struct {
	TB1URL     string                          `json:"tb1_url"`
	TB2URL     string                          `json:"tb2_url"`
	Functional []protocoltest.DuoCheck         `json:"functional,omitempty"`
	Memory     []*protocoltest.DuoMemoryReport `json:"memory,omitempty"`
	Pass       bool                            `json:"pass"`
}

// resolveRoutes parses a comma-separated route list ("all" = every route).
func resolveRoutes(spec string) ([]protocoltest.DuoRoute, error) {
	if strings.TrimSpace(spec) == "all" {
		return protocoltest.AllDuoRoutes(), nil
	}
	var routes []protocoltest.DuoRoute
	for _, name := range strings.Split(spec, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		r, ok := protocoltest.FindDuoRoute(name)
		if !ok {
			return nil, fmt.Errorf("unknown route %q (known: %s)", name, duoRouteNames())
		}
		routes = append(routes, r)
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("no routes selected")
	}
	return routes, nil
}

func duoRouteNames() string {
	var names []string
	for _, r := range protocoltest.AllDuoRoutes() {
		names = append(names, r.Name)
	}
	return strings.Join(names, ", ")
}

func (cmd *DuoCmd) Run() error {
	if !cmd.Verbose {
		// TestMode (not ReleaseMode): without a MultiLogger the access-log
		// middleware falls back to a plain logrus logger and only discards
		// its output under gin.TestMode.
		gin.SetMode(gin.TestMode)
		logrus.SetLevel(logrus.ErrorLevel)
	}

	funcRoutes, err := resolveRoutes(cmd.Routes)
	if err != nil {
		return err
	}
	memRoutes, err := resolveRoutes(cmd.MemRoutes)
	if err != nil {
		return err
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
		for _, route := range funcRoutes {
			checks := env.RunFunctionalChecks(route, bodyBytes)
			result.Functional = append(result.Functional, checks...)
			failed := 0
			for _, c := range checks {
				if !c.Pass {
					failed++
					result.Pass = false
				}
			}
			if cmd.JSON {
				continue
			}
			if failed == 0 {
				fmt.Printf("  ✔ %-16s %d checks\n", route.Name, len(checks))
				continue
			}
			fmt.Printf("  ✘ %-16s %d/%d checks failed\n", route.Name, failed, len(checks))
			for _, c := range checks {
				if !c.Pass {
					fmt.Printf("      ✘ %-28s %s\n", c.Name, c.Detail)
				}
			}
		}
	}

	if !cmd.SkipMemory {
		if !cmd.JSON {
			fmt.Println("duo: memory phase")
		}
		for i := range memRoutes {
			route := memRoutes[i]
			report, err := env.RunMemoryPhase(protocoltest.DuoMemoryConfig{
				Route:      &route,
				BodyBytes:  bodyBytes,
				Batch:      cmd.Batch,
				Workers:    cmd.Workers,
				PerWorker:  cmd.PerWorker,
				ProfileDir: cmd.ProfileDir,
				Progress:   progress,
			})
			if err != nil {
				return fmt.Errorf("memory phase (%s): %w", route.Name, err)
			}
			result.Memory = append(result.Memory, report)

			slopeOK := report.SlopeKBPerRequest <= cmd.MaxSlopeKB
			if !slopeOK {
				result.Pass = false
			}
			if !cmd.JSON {
				verdict := "OK"
				if !slopeOK {
					verdict = fmt.Sprintf("LEAK (limit %.1f)", cmd.MaxSlopeKB)
				}
				fmt.Printf("  %s:\n", route.Name)
				fmt.Printf("    baseline post-GC heap      %8.2f MB\n", report.BaselineHeapMB)
				fmt.Printf("    retained after %3d reqs    %+8.2f MB\n", report.Batch, report.AfterBatch1MB)
				fmt.Printf("    retained after %3d reqs    %+8.2f MB\n", 2*report.Batch, report.AfterBatch2MB)
				fmt.Printf("    retention slope            %8.1f KB/request   %s\n", report.SlopeKBPerRequest, verdict)
				fmt.Printf("    allocation churn           %8.2f MB/request\n", report.ChurnMBPerRequest)
				fmt.Printf("    burst peak heap (%d×%d)     %8.2f MB (post-GC %+.2f MB)\n",
					report.ConcurrentWorkers, report.ConcurrentTotal/report.ConcurrentWorkers, report.PeakHeapMB, report.PostBurstDeltaMB)
				if report.FinalProfile != "" {
					fmt.Printf("    heap profiles: %s , %s\n", report.BaselineProfile, report.FinalProfile)
					fmt.Printf("    inspect: go tool pprof -top -inuse_space %s\n", report.FinalProfile)
				}
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
