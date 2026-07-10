package stream

// Empirical memory measurement for issue #1255. Not a regression test —
// it prints numbers separating per-request transient churn, per-request
// live retention during a stream, and cross-request (post-GC) retention
// on the exact path the OOM was reported on: Anthropic beta request →
// OpenAI Chat params → OpenAI SSE stream → Anthropic beta SSE out.
//
// Run with: go test ./internal/protocol/stream/ -run TestOOMMeasure -v

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	openaistream "github.com/openai/openai-go/v3/packages/ssestream"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/request"
	"github.com/tingly-dev/tingly-box/internal/protocol/token"
)

// seqDecoder replays pre-built SSE events.
type seqDecoder struct {
	events []openaistream.Event
	i      int
}

func (d *seqDecoder) Next() bool {
	if d.i >= len(d.events) {
		return false
	}
	d.i++
	return true
}
func (d *seqDecoder) Event() openaistream.Event { return d.events[d.i-1] }
func (d *seqDecoder) Close() error              { return nil }
func (d *seqDecoder) Err() error                { return nil }

// buildLargeBetaBody builds an Anthropic beta request carrying `msgs` messages
// of `msgBytes` each — mimicking an agentic client shipping full context.
func buildLargeBetaBody(msgs, msgBytes int) []byte {
	filler := strings.Repeat("The quick brown fox jumps over the lazy dog. ", msgBytes/45+1)[:msgBytes]
	var sb strings.Builder
	sb.WriteString(`{"model":"gpt-x","max_tokens":4096,"stream":true,"messages":[`)
	for i := 0; i < msgs; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		b, _ := json.Marshal(filler)
		fmt.Fprintf(&sb, `{"role":%q,"content":%s}`, role, string(b))
	}
	sb.WriteString(`]}`)
	return []byte(sb.String())
}

// buildChunkEvents builds n content chunks plus finish + usage-only chunks.
func buildChunkEvents(n int) []openaistream.Event {
	events := make([]openaistream.Event, 0, n+2)
	delta := strings.Repeat("word ", 50) // 250 bytes per chunk
	for i := 0; i < n; i++ {
		data := fmt.Sprintf(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{"content":%q}}]}`, delta)
		events = append(events, openaistream.Event{Type: "", Data: []byte(data)})
	}
	events = append(events, openaistream.Event{Data: []byte(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)})
	events = append(events, openaistream.Event{Data: []byte(`{"id":"c","object":"chat.completion.chunk","created":1,"model":"m","choices":[],"usage":{"prompt_tokens":600000,"completion_tokens":5000}}`)})
	return events
}

func heapAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

func TestOOMMeasureFullPath(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement only")
	}
	gin.SetMode(gin.TestMode)

	const iterations = 30
	body := buildLargeBetaBody(60, 40*1024) // ~2.4MB body ≈ 600k-token session
	t.Logf("request body size: %.2f MB", float64(len(body))/1024/1024)

	baseline := heapAfterGC()
	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)
	allocBefore := m0.TotalAlloc

	var liveDuring uint64
	for i := 0; i < iterations; i++ {
		var parsed protocol.AnthropicBetaMessagesRequest
		if err := json.Unmarshal(body, &parsed); err != nil {
			t.Fatal(err)
		}
		openaiReq, _ := request.ConvertAnthropicBetaToOpenAIRequest(parsed.BetaMessageNewParams, true, true, false)

		// live retention while the stream is "in flight": parsed request +
		// converted params are both still referenced here.
		if i == 0 {
			liveDuring = heapAfterGC() - baseline
		}

		stream := openaistream.NewStream[openai.ChatCompletionChunk](&seqDecoder{events: buildChunkEvents(400)}, nil)
		w := &closeNotifyRecorder{httptest.NewRecorder()}
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

		if _, err := HandleOpenAIToAnthropicBetaStream(protocol.NewHandleContext(c, "m"), openaiReq, stream, "m"); err != nil {
			t.Fatal(err)
		}
	}

	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	churnPerIter := float64(m1.TotalAlloc-allocBefore) / float64(iterations) / 1024 / 1024
	retained := int64(heapAfterGC()) - int64(baseline)

	t.Logf("live heap while one request in flight (parsed+converted): %.2f MB", float64(liveDuring)/1024/1024)
	t.Logf("allocation churn per request: %.2f MB", churnPerIter)
	t.Logf("retained after %d requests (post-GC delta): %.2f MB", iterations, float64(retained)/1024/1024)
}

// TestOOMMeasureExactBPE quantifies the tiktoken estimate this branch removed
// from the stream hot path.
func TestOOMMeasureExactBPE(t *testing.T) {
	if testing.Short() {
		t.Skip("measurement only")
	}
	body := buildLargeBetaBody(60, 40*1024)
	var parsed protocol.AnthropicBetaMessagesRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatal(err)
	}
	openaiReq, _ := request.ConvertAnthropicBetaToOpenAIRequest(parsed.BetaMessageNewParams, true, true, false)

	// warm the codec cache so we measure per-request cost, not compile cost
	if _, err := token.EstimateInputTokens(openaiReq); err != nil {
		t.Fatal(err)
	}

	var m0 runtime.MemStats
	runtime.ReadMemStats(&m0)
	tok, err := token.EstimateInputTokens(openaiReq)
	if err != nil {
		t.Fatal(err)
	}
	var m1 runtime.MemStats
	runtime.ReadMemStats(&m1)
	t.Logf("exact BPE estimate: %d tokens, allocation churn: %.2f MB per call", tok, float64(m1.TotalAlloc-m0.TotalAlloc)/1024/1024)

	runtime.ReadMemStats(&m0)
	tok2 := token.EstimateInputTokensSimple(openaiReq)
	runtime.ReadMemStats(&m1)
	t.Logf("len/4 estimate: %d tokens, allocation churn: %.4f MB per call", tok2, float64(m1.TotalAlloc-m0.TotalAlloc)/1024/1024)
}
