package protocoltest

// Duo is the two-instance end-to-end verification environment born from the
// #1255 OOM investigation:
//
//	client ──(Anthropic v1/beta, streaming, large conversation)──▶ tb2 gateway
//	   tb2 ──(converted provider request over real HTTP)──────────▶ tb1 /virtual/...
//	   tb1 ──(vmodel SSE stream)───────────────────────────────────▶ tb2 ──▶ client
//
// Both instances are full production servers (server.NewServer) running in
// one process on real HTTP listeners, so the whole gateway stack is
// exercised — routing, transform pipeline, client pool, transports, usage
// tracking — and a post-GC heap profile attributes retained bytes to real
// call stacks.
//
// Every anthropic-source conversion route the production vmodel endpoint can
// back is wired (see AllDuoRoutes): {v1, beta} × {anthropic passthrough,
// OpenAI Chat, OpenAI Responses}. The Google target is not covered — the
// vmodel surface deliberately skips it for now.
//
// Two verification phases are provided:
//
//   - RunFunctionalChecks: protocol correctness through a conversion route
//     (streaming SSE shape + assembled content + usage; non-streaming body).
//   - RunMemoryPhase: allocation churn, post-GC retention slope across
//     request batches (a leak shows up as a positive slope; transient spikes
//     do not), concurrent-burst peak heap, and optional pprof heap profiles.
//
// Consumed by `harness duo` (cli/harness) and by duo_test.go as functional +
// memory regression tests.

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
	"time"

	"github.com/tingly-dev/tingly-box/ai"
	"github.com/tingly-dev/tingly-box/internal/config"
	"github.com/tingly-dev/tingly-box/internal/constant"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/sse"
	"github.com/tingly-dev/tingly-box/internal/server"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// DuoRoute is one anthropic-source conversion route through tb2.
type DuoRoute struct {
	// Name identifies the route in flags, check names, and reports,
	// e.g. "beta-chat", "v1-responses", "beta-anthropic".
	Name string
	// Beta selects the Anthropic beta source surface (?beta=true) over v1.
	Beta bool
	// Target is the provider protocol tb2 converts to: "chat", "responses",
	// or "anthropic" (passthrough).
	Target string
}

// RequestModel returns the tb2 request model wired for this route.
func (r DuoRoute) RequestModel() string { return "duo-e2e-" + r.Name }

// AllDuoRoutes lists every anthropic-source route the production vmodel
// endpoint can back: {v1, beta} × {anthropic, openai chat, openai responses}.
func AllDuoRoutes() []DuoRoute {
	var routes []DuoRoute
	for _, src := range []struct {
		prefix string
		beta   bool
	}{{"beta", true}, {"v1", false}} {
		for _, target := range []string{"chat", "responses", "anthropic"} {
			routes = append(routes, DuoRoute{Name: src.prefix + "-" + target, Beta: src.beta, Target: target})
		}
	}
	return routes
}

// FindDuoRoute resolves a route by name.
func FindDuoRoute(name string) (DuoRoute, bool) {
	for _, r := range AllDuoRoutes() {
		if r.Name == name {
			return r, true
		}
	}
	return DuoRoute{}, false
}

// DuoDefaultRoute is the memory-phase default: the Claude Code hot path
// (Anthropic beta client → OpenAI Chat provider) where #1255 was reported.
var DuoDefaultRoute = DuoRoute{Name: "beta-chat", Beta: true, Target: "chat"}

// duoTargetVModel maps a route target to the tb1 vmodel that serves it.
func duoTargetVModel(target string) string {
	if target == "anthropic" {
		return "virtual-claude-3" // anthropic registry
	}
	return "virtual-gpt-4" // openai registry (chat + responses surfaces)
}

// DuoEnv holds the two running instances and the wiring between them.
type DuoEnv struct {
	tb1Cfg *config.AppConfig
	tb2Cfg *config.AppConfig
	tb1    *httptest.Server
	tb2    *httptest.Server
	client *http.Client

	tb2Token string
	dirs     []string
}

// NewDuoEnv boots tb1 (vmodel provider) and tb2 (gateway under test) and
// wires one tb2 rule per route in AllDuoRoutes to tb1's virtual endpoints.
// Callers must Close() the returned env.
func NewDuoEnv() (*DuoEnv, error) {
	env := &DuoEnv{client: &http.Client{Timeout: 120 * time.Second}}

	boot := func(name string) (*config.AppConfig, *httptest.Server, error) {
		dir, err := os.MkdirTemp("", "duo-"+name+"-*")
		if err != nil {
			return nil, nil, err
		}
		env.dirs = append(env.dirs, dir)
		appCfg, err := config.NewAppConfig(config.WithConfigDir(dir))
		if err != nil {
			return nil, nil, err
		}
		srv := server.NewServer(appCfg.GetGlobalConfig(), server.WithAdaptor(false))
		return appCfg, httptest.NewServer(srv.GetRouter()), nil
	}

	var err error
	if env.tb1Cfg, env.tb1, err = boot("tb1"); err != nil {
		env.Close()
		return nil, fmt.Errorf("boot tb1: %w", err)
	}
	if env.tb2Cfg, env.tb2, err = boot("tb2"); err != nil {
		env.Close()
		return nil, fmt.Errorf("boot tb2: %w", err)
	}
	env.tb2Token = env.tb2Cfg.GetGlobalConfig().GetModelToken()
	tb1Token := env.tb1Cfg.GetGlobalConfig().GetModelToken()

	// One provider per target protocol; routes of both source surfaces share it.
	providers := map[string]*typ.Provider{
		"chat": {
			UUID:               "tb1-openai-chat",
			Name:               "tb1-openai-chat",
			APIBase:            env.tb1.URL + "/virtual/openai/v1",
			APIStyle:           protocol.APIStyleOpenAI,
			OpenAIEndpointMode: ai.EndpointModeChat,
		},
		"responses": {
			UUID:               "tb1-openai-responses",
			Name:               "tb1-openai-responses",
			APIBase:            env.tb1.URL + "/virtual/openai/v1",
			APIStyle:           protocol.APIStyleOpenAI,
			OpenAIEndpointMode: ai.EndpointModeResponses,
		},
		"anthropic": {
			UUID:     "tb1-anthropic",
			Name:     "tb1-anthropic",
			APIBase:  env.tb1.URL + "/virtual/anthropic", // SDK appends /v1/messages
			APIStyle: protocol.APIStyleAnthropic,
		},
	}
	for _, p := range providers {
		p.Token = tb1Token
		p.Enabled = true
		p.Timeout = int64(constant.DefaultRequestTimeout)
		if err := env.tb2Cfg.AddProvider(p); err != nil {
			env.Close()
			return nil, fmt.Errorf("add provider %s: %w", p.Name, err)
		}
	}

	for _, route := range AllDuoRoutes() {
		rule := typ.Rule{
			UUID:          route.RequestModel(),
			Scenario:      typ.ScenarioAnthropic,
			RequestModel:  route.RequestModel(),
			ResponseModel: duoTargetVModel(route.Target),
			Services: []*loadbalance.Service{{
				Provider:   providers[route.Target].UUID,
				Model:      duoTargetVModel(route.Target),
				Weight:     1,
				Active:     true,
				TimeWindow: 300,
			}},
			LBTactic: typ.Tactic{Type: loadbalance.TacticRandom, Params: typ.NewRandomParams()},
			Active:   true,
		}
		if err := env.tb2Cfg.GetGlobalConfig().AddRequestConfig(rule); err != nil {
			env.Close()
			return nil, fmt.Errorf("add rule %s: %w", route.Name, err)
		}
	}
	return env, nil
}

// Close shuts down both instances and removes their config dirs.
func (env *DuoEnv) Close() {
	if env.tb1 != nil {
		env.tb1.Close()
	}
	if env.tb2 != nil {
		env.tb2.Close()
	}
	for _, d := range env.dirs {
		os.RemoveAll(d)
	}
}

// TB1URL and TB2URL expose the instance endpoints (diagnostics/logging).
func (env *DuoEnv) TB1URL() string { return env.tb1.URL }
func (env *DuoEnv) TB2URL() string { return env.tb2.URL }

// BuildConversationBody builds a Claude-Code-shaped Anthropic request of
// approximately totalBytes for the given route: alternating user/assistant
// text messages, so the gateway parses and converts a realistically large
// agentic context. The shape is valid for both the v1 and beta surfaces.
func BuildConversationBody(route DuoRoute, totalBytes int, streaming bool) []byte {
	const msgBytes = 40 * 1024
	msgs := totalBytes / msgBytes
	if msgs < 1 {
		msgs = 1
	}
	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", msgBytes/45+1)[:msgBytes]
	fb, _ := json.Marshal(filler)

	var sb strings.Builder
	fmt.Fprintf(&sb, `{"model":%q,"max_tokens":1024,"stream":%v,"messages":[`, route.RequestModel(), streaming)
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

// post sends one request through tb2's anthropic endpoint for the route's
// source surface (v1 or beta).
func (env *DuoEnv) post(route DuoRoute, body []byte) (*http.Response, error) {
	path := "/tingly/anthropic/v1/messages"
	if route.Beta {
		path += "?beta=true"
	}
	req, err := http.NewRequest(http.MethodPost, env.tb2.URL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.tb2Token)
	return env.client.Do(req)
}

// DrainStreaming drives one streaming request over the route and fully
// drains the SSE body, returning the number of `event:` lines seen.
func (env *DuoEnv) DrainStreaming(route DuoRoute, body []byte) (int, error) {
	resp, err := env.post(route, body)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return 0, fmt.Errorf("status %d: %s", resp.StatusCode, b)
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
		return 0, fmt.Errorf("no SSE events received")
	}
	return events, sc.Err()
}

// ─── Functional phase ─────────────────────────────────────────────────────────

// DuoCheck is one functional verification result.
type DuoCheck struct {
	Route  string `json:"route"`
	Name   string `json:"name"`
	Pass   bool   `json:"pass"`
	Detail string `json:"detail,omitempty"`
}

// RunFunctionalChecks verifies protocol correctness of one conversion route
// with a bodyBytes-sized conversation: streaming SSE shape, assembled
// content, usage propagation, and the non-streaming response body.
func (env *DuoEnv) RunFunctionalChecks(route DuoRoute, bodyBytes int) []DuoCheck {
	var checks []DuoCheck
	add := func(name string, pass bool, detail string) {
		checks = append(checks, DuoCheck{Route: route.Name, Name: name, Pass: pass, Detail: detail})
	}

	// Streaming: event shape + assembled result.
	resp, err := env.post(route, BuildConversationBody(route, bodyBytes, true))
	if err != nil {
		add("stream/http", false, err.Error())
		return checks
	}
	func() {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
			add("stream/http", false, fmt.Sprintf("status %d: %s", resp.StatusCode, b))
			return
		}
		add("stream/http", true, "200")

		events, _ := sse.ReadSSELines(resp.Body)
		joined := strings.Join(events, "\n")
		for _, evt := range []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"} {
			add("stream/event/"+evt, strings.Contains(joined, evt), "")
		}

		parsed := sse.AssembleAnthropicStream(events)
		if parsed == nil {
			add("stream/assemble", false, "assembler returned nil")
			return
		}
		add("stream/assemble", parsed.Content != "", fmt.Sprintf("content=%dB", len(parsed.Content)))
		add("stream/finish_reason", parsed.FinishReason != "", parsed.FinishReason)
		if parsed.Usage == nil {
			add("stream/usage", false, "no usage in stream")
		} else {
			add("stream/usage", parsed.Usage.InputTokens > 0 && parsed.Usage.OutputTokens > 0,
				fmt.Sprintf("in=%d out=%d", parsed.Usage.InputTokens, parsed.Usage.OutputTokens))
		}
	}()

	// Non-streaming: response body shape.
	resp2, err := env.post(route, BuildConversationBody(route, bodyBytes, false))
	if err != nil {
		add("nonstream/http", false, err.Error())
		return checks
	}
	defer resp2.Body.Close()
	raw, _ := io.ReadAll(resp2.Body)
	if resp2.StatusCode != http.StatusOK {
		add("nonstream/http", false, fmt.Sprintf("status %d: %s", resp2.StatusCode, raw[:min(len(raw), 2048)]))
		return checks
	}
	add("nonstream/http", true, "200")
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		add("nonstream/body", false, "invalid JSON: "+err.Error())
		return checks
	}
	parsed := sse.ParseAnthropicResult(m)
	if parsed == nil {
		add("nonstream/body", false, "unparseable anthropic body")
		return checks
	}
	add("nonstream/content", parsed.Content != "", fmt.Sprintf("content=%dB", len(parsed.Content)))
	add("nonstream/usage", parsed.Usage != nil && parsed.Usage.InputTokens > 0, "")
	return checks
}

// ─── Memory phase ─────────────────────────────────────────────────────────────

// DuoMemoryConfig parameterizes RunMemoryPhase.
type DuoMemoryConfig struct {
	Route      *DuoRoute // conversion route to drive (default DuoDefaultRoute)
	BodyBytes  int       // conversation size per request (default 2MB)
	Warmup     int       // warmup requests before the baseline (default 3)
	Batch      int       // requests per sequential batch, two batches are run (default 15)
	Workers    int       // concurrent workers in the burst phase (default 4)
	PerWorker  int       // requests per worker in the burst phase (default 5)
	ProfileDir string    // write pprof heap profiles here ("" = skip)
	Progress   func(format string, args ...any)
}

func (c *DuoMemoryConfig) withDefaults() {
	if c.Route == nil {
		r := DuoDefaultRoute
		c.Route = &r
	}
	if c.BodyBytes <= 0 {
		c.BodyBytes = 2 * 1024 * 1024
	}
	if c.Warmup <= 0 {
		c.Warmup = 3
	}
	if c.Batch <= 0 {
		c.Batch = 15
	}
	if c.Workers <= 0 {
		c.Workers = 4
	}
	if c.PerWorker <= 0 {
		c.PerWorker = 5
	}
	if c.Progress == nil {
		c.Progress = func(string, ...any) {}
	}
}

// DuoMemoryReport is the outcome of RunMemoryPhase.
type DuoMemoryReport struct {
	Route             string  `json:"route"`
	BodyBytes         int     `json:"body_bytes"`
	SequentialCount   int     `json:"sequential_requests"`
	BaselineHeapMB    float64 `json:"baseline_heap_mb"`
	AfterBatch1MB     float64 `json:"after_batch1_delta_mb"`
	AfterBatch2MB     float64 `json:"after_batch2_delta_mb"`
	SlopeKBPerRequest float64 `json:"retention_slope_kb_per_request"`
	ChurnMBPerRequest float64 `json:"alloc_churn_mb_per_request"`
	ConcurrentWorkers int     `json:"concurrent_workers"`
	ConcurrentTotal   int     `json:"concurrent_requests"`
	PeakHeapMB        float64 `json:"concurrent_peak_heap_mb"`
	PostBurstDeltaMB  float64 `json:"post_burst_delta_mb"`
	BaselineProfile   string  `json:"baseline_profile,omitempty"`
	FinalProfile      string  `json:"final_profile,omitempty"`
}

func duoHeapAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func duoWriteHeapProfile(dir, name string) (string, error) {
	runtime.GC()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		return "", err
	}
	return path, nil
}

// RunMemoryPhase measures allocation churn, post-GC retention slope, and
// concurrent-burst peak heap on one conversion route. A near-zero slope means
// no per-request leak; the #1255 latency-attribute leak measured 823
// KB/request here before the fix, 0.5 KB/request after.
func (env *DuoEnv) RunMemoryPhase(cfg DuoMemoryConfig) (*DuoMemoryReport, error) {
	cfg.withDefaults()
	route := *cfg.Route
	body := BuildConversationBody(route, cfg.BodyBytes, true)
	report := &DuoMemoryReport{
		Route:             route.Name,
		BodyBytes:         len(body),
		SequentialCount:   2 * cfg.Batch,
		ConcurrentWorkers: cfg.Workers,
		ConcurrentTotal:   cfg.Workers * cfg.PerWorker,
	}

	cfg.Progress("route %s: warmup %d requests, body %.2f MB", route.Name, cfg.Warmup, float64(len(body))/1024/1024)
	for i := 0; i < cfg.Warmup; i++ {
		if _, err := env.DrainStreaming(route, body); err != nil {
			return nil, fmt.Errorf("warmup request %d: %w", i, err)
		}
	}

	baseline := duoHeapAfterGC()
	report.BaselineHeapMB = float64(baseline) / 1024 / 1024
	if cfg.ProfileDir != "" {
		p, err := duoWriteHeapProfile(cfg.ProfileDir, "duo-"+route.Name+"-baseline.pb.gz")
		if err != nil {
			return nil, err
		}
		report.BaselineProfile = p
	}
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)

	runBatch := func() error {
		for i := 0; i < cfg.Batch; i++ {
			if _, err := env.DrainStreaming(route, body); err != nil {
				return fmt.Errorf("sequential request: %w", err)
			}
		}
		return nil
	}
	cfg.Progress("route %s: sequential 2 batches × %d requests", route.Name, cfg.Batch)
	if err := runBatch(); err != nil {
		return nil, err
	}
	after1 := duoHeapAfterGC()
	if err := runBatch(); err != nil {
		return nil, err
	}
	after2 := duoHeapAfterGC()

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	report.AfterBatch1MB = float64(int64(after1)-int64(baseline)) / 1024 / 1024
	report.AfterBatch2MB = float64(int64(after2)-int64(baseline)) / 1024 / 1024
	report.SlopeKBPerRequest = (float64(int64(after2)) - float64(int64(after1))) / float64(cfg.Batch) / 1024
	report.ChurnMBPerRequest = float64(m1.TotalAlloc-m0.TotalAlloc) / float64(2*cfg.Batch) / 1024 / 1024

	// Concurrent burst with live-heap sampling.
	cfg.Progress("route %s: concurrent burst %d workers × %d requests", route.Name, cfg.Workers, cfg.PerWorker)
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
	errCh := make(chan error, cfg.Workers)
	for g := 0; g < cfg.Workers; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < cfg.PerWorker; i++ {
				if _, err := env.DrainStreaming(route, body); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	report.PeakHeapMB = float64(<-peakCh) / 1024 / 1024
	select {
	case err := <-errCh:
		return nil, fmt.Errorf("concurrent request: %w", err)
	default:
	}
	report.PostBurstDeltaMB = float64(int64(duoHeapAfterGC())-int64(baseline)) / 1024 / 1024

	if cfg.ProfileDir != "" {
		p, err := duoWriteHeapProfile(cfg.ProfileDir, "duo-"+route.Name+"-final.pb.gz")
		if err != nil {
			return nil, err
		}
		report.FinalProfile = p
	}
	return report, nil
}
