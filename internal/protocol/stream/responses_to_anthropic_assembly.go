package stream

import (
	"encoding/json"
	"net/http"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/shared/constant"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/protocol"
)

// The "assemble then respond" path used by Codex-style providers: the client
// sent a non-streaming Anthropic request, the upstream only speaks streaming
// Responses API, so the stream is folded into a single Anthropic message.
//
// Both the v1 and beta assembly handlers run the same
// responsesToAnthropicConverter as the true-streaming path and fold its
// events; only the final SDK message type differs. This replaces a fully
// duplicated ~450-line map-based event dispatcher that existed solely for
// this path.

// assembledAnthropicBlock accumulates one content block from converter events.
type assembledAnthropicBlock struct {
	blockType string
	id        string
	name      string
	text      []byte
	thinking  []byte
	input     []byte
}

// responsesToAnthropicAssembly folds converter events into message parts.
type responsesToAnthropicAssembly struct {
	id         string
	model      string
	stopReason string
	open       map[int]*assembledAnthropicBlock
	content    []*assembledAnthropicBlock
}

func newResponsesToAnthropicAssembly() *responsesToAnthropicAssembly {
	return &responsesToAnthropicAssembly{open: make(map[int]*assembledAnthropicBlock)}
}

// consume folds a single converter event. Signature deltas are framing-only
// and skipped; message_stop carries no data.
func (a *responsesToAnthropicAssembly) consume(evt anthropicStreamEvent) {
	switch data := evt.data.(type) {
	case anthropicMessageStartEvent:
		a.id = data.Message.ID
		a.model = data.Message.Model

	case anthropicContentBlockStartEvent:
		block := &assembledAnthropicBlock{blockType: data.ContentBlock.Type}
		if data.ContentBlock.ID != nil {
			block.id = *data.ContentBlock.ID
		}
		if data.ContentBlock.Name != nil {
			block.name = *data.ContentBlock.Name
		}
		a.open[data.Index] = block

	case anthropicContentBlockDeltaEvent:
		block, ok := a.open[data.Index]
		if !ok {
			return
		}
		switch data.Delta.Type {
		case deltaTypeTextDelta:
			if data.Delta.Text != nil {
				block.text = append(block.text, *data.Delta.Text...)
			}
		case deltaTypeThinkingDelta:
			if data.Delta.Thinking != nil {
				block.thinking = append(block.thinking, *data.Delta.Thinking...)
			}
		case deltaTypeInputJSONDelta:
			if data.Delta.PartialJSON != nil {
				block.input = append(block.input, *data.Delta.PartialJSON...)
			}
		}

	case anthropicContentBlockStopEvent:
		if block, ok := a.open[data.Index]; ok {
			a.content = append(a.content, block)
			delete(a.open, data.Index)
		}

	case map[string]interface{}:
		if evt.eventType == eventTypeMessageDelta {
			if delta, ok := data["delta"].(map[string]interface{}); ok {
				if stopReason, ok := delta["stop_reason"].(string); ok {
					a.stopReason = stopReason
				}
			}
		}
	}
}

// run drives the converter to completion, folding every event. It returns the
// converter's usage plus any protocol or transport error; the partially
// assembled message stays available for a best-effort response either way.
func (a *responsesToAnthropicAssembly) run(c *gin.Context, stream ResponsesStreamIter, responseModel string) (*protocol.TokenUsage, error) {
	conv := newResponsesToAnthropicConverter(c.Request.Context(), stream, responseModel)
	for {
		evt, done, err := conv.Next()
		if err != nil {
			return conv.Usage(), err
		}
		if done {
			break
		}
		if e, ok := evt.(anthropicStreamEvent); ok {
			a.consume(e)
		}
	}
	if hookErr := conv.HookErr(); hookErr != nil {
		return conv.Usage(), hookErr
	}
	return conv.Usage(), stream.Err()
}

// setAssemblyHeadersAndRecover mirrors the header and panic behavior of the
// previous assembly implementation: SSE headers are set up-front (gin's JSON
// render preserves an already-set Content-Type, so responses keep the
// historical text/event-stream header), and a panic surfaces as an SSE error
// frame.
func setAssemblyHeaders(c *gin.Context) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Headers", "Cache-Control")
}

func recoverAssemblyPanic(c *gin.Context) {
	if r := recover(); r != nil {
		logrus.WithContext(c.Request.Context()).Errorf("Panic in Responses API to Anthropic assembly handler: %v", r)
		if c.Writer != nil {
			c.SSEvent("error", "{\"error\":{\"message\":\"Internal streaming error\",\"type\":\"internal_error\"}}")
			if flusher, ok := c.Writer.(http.Flusher); ok {
				flusher.Flush()
			}
		}
	}
}

func closeResponsesStream(c *gin.Context, stream ResponsesStreamIter) {
	if stream == nil {
		return
	}
	if err := stream.Close(); err != nil {
		logrus.WithContext(c.Request.Context()).Errorf("Error closing Responses API stream: %v", err)
	}
}

// HandleResponsesToAnthropicV1Assembly consumes a Responses API stream and
// responds with a single assembled Anthropic v1 message.
func HandleResponsesToAnthropicV1Assembly(c *gin.Context, stream ResponsesStreamIter, responseModel string) (*protocol.TokenUsage, error) {
	defer recoverAssemblyPanic(c)
	defer closeResponsesStream(c, stream)
	setAssemblyHeaders(c)

	asm := newResponsesToAnthropicAssembly()
	usage, err := asm.run(c, stream, responseModel)

	msg := anthropic.Message{
		ID:         asm.id,
		Type:       constant.Message("message"),
		Role:       constant.Assistant("assistant"),
		Model:      anthropic.Model(asm.model),
		StopReason: anthropic.StopReason(asm.stopReason),
	}
	for _, block := range asm.content {
		union := anthropic.ContentBlockUnion{
			Type:     block.blockType,
			ID:       block.id,
			Name:     block.name,
			Text:     string(block.text),
			Thinking: string(block.thinking),
		}
		if len(block.input) > 0 {
			union.Input = json.RawMessage(block.input)
		}
		msg.Content = append(msg.Content, union)
	}
	if usage != nil {
		msg.Usage.InputTokens = int64(usage.InputTokens)
		msg.Usage.OutputTokens = int64(usage.OutputTokens)
		if usage.CacheInputTokens > 0 {
			msg.Usage.CacheReadInputTokens = int64(usage.CacheInputTokens)
		}
	}

	// Even on error, respond with what was assembled so far (matching the
	// previous implementation's best-effort behavior).
	c.JSON(http.StatusOK, msg)
	return usage, err
}

// HandleResponsesToAnthropicBetaAssembly consumes a Responses API stream and
// responds with a single assembled Anthropic beta message.
func HandleResponsesToAnthropicBetaAssembly(c *gin.Context, stream ResponsesStreamIter, responseModel string) (*protocol.TokenUsage, error) {
	defer recoverAssemblyPanic(c)
	defer closeResponsesStream(c, stream)
	setAssemblyHeaders(c)

	asm := newResponsesToAnthropicAssembly()
	usage, err := asm.run(c, stream, responseModel)

	msg := anthropic.BetaMessage{
		ID:         asm.id,
		Type:       constant.Message("message"),
		Role:       constant.Assistant("assistant"),
		Model:      anthropic.Model(asm.model),
		StopReason: anthropic.BetaStopReason(asm.stopReason),
	}
	for _, block := range asm.content {
		union := anthropic.BetaContentBlockUnion{
			Type:     block.blockType,
			ID:       block.id,
			Name:     block.name,
			Text:     string(block.text),
			Thinking: string(block.thinking),
		}
		if len(block.input) > 0 {
			union.Input = json.RawMessage(block.input)
		}
		msg.Content = append(msg.Content, union)
	}
	if usage != nil {
		msg.Usage.InputTokens = int64(usage.InputTokens)
		msg.Usage.OutputTokens = int64(usage.OutputTokens)
		if usage.CacheInputTokens > 0 {
			msg.Usage.CacheReadInputTokens = int64(usage.CacheInputTokens)
		}
	}

	// Even on error, respond with what was assembled so far (matching the
	// previous implementation's best-effort behavior).
	c.JSON(http.StatusOK, msg)
	return usage, err
}
