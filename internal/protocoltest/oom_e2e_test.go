package protocoltest

// Two-instance e2e memory measurement for issue #1255.
//
// Topology (the "tb2 → tb1(vmodel)" setup):
//
//	client ──(Anthropic beta, streaming, ~2.4MB conversation)──▶ tb2 gateway
//	   tb2 ──(converted OpenAI Chat request over real HTTP)────▶ tb1 /virtual/openai/v1
//	   tb1 ──(vmodel virtual-gpt-4 SSE stream)──────────────────▶ tb2 ──▶ client
//
// Both instances are full production servers (server.NewServer) in one
// process, so a post-GC heap profile attributes retained bytes to real
// gateway call stacks. The test prints:
//
//   - per-request allocation churn
//   - post-GC retention slope across request batches (a leak shows up as a
//     positive slope; transient spikes do not)
//   - peak heap during a concurrent burst
//
// and writes pprof heap profiles (baseline / final) to OOM_PROFILE_DIR (or
// os.TempDir()) for `go tool pprof -top` inspection of where memory pins.
//
// Run with:
//
//	OOM_PROFILE_DIR=/tmp go test ./internal/protocoltest/ -run TestOOME2E -v

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/config"
	"github.com/tingly-dev/tingly-box/internal/constant"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/server"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

const oomE2ERequestModel = "oom-e2e-model"

// startFullServer boots a production server.NewServer on a real HTTP listener.
func startFullServer(t *testing.T, name string) (*config.AppConfig, *httptest.Server) {
	t.Helper()
	dir, err := os.MkdirTemp("", "oom-e2e-"+name+"-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	appCfg, err := config.NewAppConfig(config.WithConfigDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	srv := server.NewServer(appCfg.GetGlobalConfig(), server.WithAdaptor(false))
	ts := httptest.NewServer(srv.GetRouter())
	t.Cleanup(ts.Close)
	return appCfg, ts
}

// wireTB2ToTB1 registers tb1's virtual OpenAI endpoint as tb2's provider and
// routes an anthropic-scenario model to it.
func wireTB2ToTB1(t *testing.T, tb2Cfg *config.AppConfig, tb1URL, tb1Token string) {
	t.Helper()
	provider := &typ.Provider{
		UUID:               "tb1-vmodel",
		Name:               "tb1-vmodel",
		APIBase:            tb1URL + "/virtual/openai/v1",
		APIStyle:           protocol.APIStyleOpenAI,
		OpenAIEndpointMode: ai.EndpointModeChat,
		Token:              tb1Token,
		Enabled:            true,
		Timeout:            int64(constant.DefaultRequestTimeout),
	}
	if err := tb2Cfg.AddProvider(provider); err != nil {
		t.Fatal(err)
	}
	rule := typ.Rule{
		UUID:          oomE2ERequestModel,
		Scenario:      typ.ScenarioAnthropic,
		RequestModel:  oomE2ERequestModel,
		ResponseModel: "virtual-gpt-4",
		Services: []*loadbalance.Service{{
			Provider:   "tb1-vmodel",
			Model:      "virtual-gpt-4",
			Weight:     1,
			Active:     true,
			TimeWindow: 300,
		}},
		LBTactic: typ.Tactic{Type: loadbalance.TacticAdaptive, Params: typ.DefaultAdaptiveParams()},
		Active:   true,
	}
	if err := tb2Cfg.GetGlobalConfig().AddRequestConfig(rule); err != nil {
		t.Fatal(err)
	}
}

// buildBigBetaBody builds a Claude-Code-shaped conversation of ~totalBytes.
func buildBigBetaBody(totalBytes int) []byte {
	const msgBytes = 40 * 1024
	msgs := totalBytes / msgBytes
	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", msgBytes/45+1)[:msgBytes]
	fb, _ := json.Marshal(filler)

	var sb strings.Builder
	fmt.Fprintf(&sb, `{"model":%q,"max_tokens":1024,"stream":true,"messages":[`, oomE2ERequestModel)
	for i := 0; i < msgs; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		fmt.Fprintf(&sb, `{"role":%q,"content":[{"type":"text","text":%s}]}`, role, string(fb))
	}
	sb.WriteString(`]}`)
	return []byte(sb.String())
}

// sendStreaming drives one streaming request through tb2 and fully drains it.
func sendStreaming(client *http.Client, tb2URL, token string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, tb2URL+"/tingly/anthropic/v1/messages?beta=true", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("status %d: %s", resp.StatusCode, b)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	events := 0
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "event:") {
			events++
		}
	}
	if events == 0 {
		return fmt.Errorf("no SSE events received")
	}
	return sc.Err()
}

func heapAfterGCE2E() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func writeHeapProfile(t *testing.T, dir, name string) string {
	t.Helper()
	runtime.GC()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOOME2EMemoryPin(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement only")
	}

	profileDir := os.Getenv("OOM_PROFILE_DIR")
	if profileDir == "" {
		profileDir = os.TempDir()
	}

	tb1Cfg, tb1 := startFullServer(t, "tb1")
	tb2Cfg, tb2 := startFullServer(t, "tb2")
	wireTB2ToTB1(t, tb2Cfg, tb1.URL, tb1Cfg.GetGlobalConfig().GetModelToken())

	tb2Token := tb2Cfg.GetGlobalConfig().GetModelToken()
	body := buildBigBetaBody(2 * 1024 * 1024) // ~2MB ≈ 500k-token session
	t.Logf("request body: %.2f MB", float64(len(body))/1024/1024)

	client := &http.Client{Timeout: 60 * time.Second}

	// Warmup: routing caches, transports, tokenizer, sqlite, etc.
	for i := 0; i < 3; i++ {
		if err := sendStreaming(client, tb2.URL, tb2Token, body); err != nil {
			t.Fatalf("warmup request %d: %v", i, err)
		}
	}

	baseline := heapAfterGCE2E()
	baseProfile := writeHeapProfile(t, profileDir, "oom-e2e-baseline.pb.gz")
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	// Phase 1: sequential batches — a leak shows as a positive retention slope.
	const batch = 15
	runBatch := func() {
		for i := 0; i < batch; i++ {
			if err := sendStreaming(client, tb2.URL, tb2Token, body); err != nil {
				t.Fatalf("request: %v", err)
			}
		}
	}
	runBatch()
	afterBatch1 := heapAfterGCE2E()
	runBatch()
	afterBatch2 := heapAfterGCE2E()

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	churn := float64(m1.TotalAlloc-m0.TotalAlloc) / float64(2*batch) / 1024 / 1024

	t.Logf("baseline post-GC heap: %.2f MB", float64(baseline)/1024/1024)
	t.Logf("after %d requests: %+.2f MB | after %d requests: %+.2f MB",
		batch, float64(int64(afterBatch1)-int64(baseline))/1024/1024,
		2*batch, float64(int64(afterBatch2)-int64(baseline))/1024/1024)
	slope := (float64(int64(afterBatch2)) - float64(int64(afterBatch1))) / batch / 1024
	t.Logf("retention slope: %.1f KB/request (near zero ⇒ no per-request leak)", slope)
	t.Logf("allocation churn: %.2f MB/request", churn)

	// Phase 2: concurrent burst — Claude Code style parallel requests.
	// Sample the live heap while streams are in flight.
	peakCh := make(chan uint64, 1)
	stop := make(chan struct{})
	go func() {
		var peak uint64
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				peakCh <- peak
				return
			case <-tick.C:
				var m runtime.MemStats
				runtime.ReadMemStats(&m)
				if m.HeapAlloc > peak {
					peak = m.HeapAlloc
				}
			}
		}
	}()
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 5; i++ {
				if err := sendStreaming(client, tb2.URL, tb2Token, body); err != nil {
					t.Errorf("concurrent request: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	peak := <-peakCh
	afterConcurrent := heapAfterGCE2E()

	t.Logf("concurrent burst (4 workers × 5 reqs): peak live heap %.2f MB, post-GC %+.2f MB vs baseline",
		float64(peak)/1024/1024, float64(int64(afterConcurrent)-int64(baseline))/1024/1024)

	finalProfile := writeHeapProfile(t, profileDir, "oom-e2e-final.pb.gz")
	t.Logf("heap profiles: baseline=%s final=%s", baseProfile, finalProfile)
	t.Logf("inspect pins with: go tool pprof -top -inuse_space %s", finalProfile)
}
