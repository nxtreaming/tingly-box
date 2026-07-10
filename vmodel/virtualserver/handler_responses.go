package virtualserver

// OpenAI Responses API surface for the virtual server.
//
// The Responses endpoint serves the SAME openai registry as /chat/completions:
// a vmodel implements HandleOpenAIChat(+Stream) once and this handler renders
// its output in Responses wire format (response.created → output deltas →
// response.completed), mirroring how the chat handler renders chunk SSE.
// Event shapes follow the fixtures proven against the gateway converters in
// vmodel/benchmark/scenario/builtins.go.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"

	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/token"
	"github.com/tingly-dev/tingly-box/vmodel"
	openaivm "github.com/tingly-dev/tingly-box/vmodel/openai"
)

// ResponsesRequest is the subset of the OpenAI Responses API request the
// virtual server needs: model routing, the stream flag, and the input text
// (echo models and token estimation read it; static mocks ignore it).
type ResponsesRequest struct {
	Model  string          `json:"model"`
	Stream bool            `json:"stream"`
	Input  json.RawMessage `json:"input"`
}

// Responses handles POST /virtual/openai/v1/responses (OpenAI Responses API).
func (h *Handler) Responses(c *gin.Context) {
	var req ResponsesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "Invalid request body: " + err.Error(),
			"type":    "invalid_request_error",
		}})
		return
	}
	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{
			"message": "Model is required",
			"type":    "invalid_request_error",
		}})
		return
	}

	vm := h.openaiReg.Get(req.Model)
	if vm == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{
			"message": fmt.Sprintf("Model not found: %s", req.Model),
			"type":    "invalid_request_error",
		}})
		return
	}

	if e := vmodel.ExtractErrorInjection(vm); e != nil && e.Stage == vmodel.ErrorStagePreContent {
		writePreContentErrorOpenAI(c, e)
		return
	}

	inputText := responsesInputText(req.Input)
	chatReq := &protocol.OpenAIChatCompletionRequest{
		ChatCompletionNewParams: &openai.ChatCompletionNewParams{
			Model:    openai.ChatModel(req.Model),
			Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage(inputText)},
		},
		Stream: req.Stream,
	}

	if req.Stream {
		h.handleResponsesStreaming(c, req.Model, inputText, chatReq, vm)
	} else {
		h.handleResponsesNonStreaming(c, req.Model, inputText, chatReq, vm)
	}
}

// responsesInputText extracts the user text from a Responses `input`, which
// is either a plain string or an array of message items with content parts.
func responsesInputText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var items []struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return string(raw)
	}
	var sb strings.Builder
	for _, item := range items {
		var cs string
		if err := json.Unmarshal(item.Content, &cs); err == nil {
			sb.WriteString(cs)
			continue
		}
		var parts []struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(item.Content, &parts); err == nil {
			for _, p := range parts {
				sb.WriteString(p.Text)
			}
		}
	}
	return sb.String()
}

// responsesOutputItems renders a VModelResponse as Responses output items.
func responsesOutputItems(itemID string, resp *openaivm.VModelResponse) []map[string]interface{} {
	var output []map[string]interface{}
	if resp.Content != "" || len(resp.ToolCalls) == 0 {
		output = append(output, map[string]interface{}{
			"id":     itemID,
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"content": []map[string]interface{}{
				{"type": "output_text", "text": resp.Content, "annotations": []interface{}{}},
			},
		})
	}
	for i, tc := range resp.ToolCalls {
		output = append(output, map[string]interface{}{
			"id":        fmt.Sprintf("fc-%s-%d", itemID, i),
			"type":      "function_call",
			"call_id":   tc.ID,
			"name":      tc.Name,
			"arguments": tc.Arguments,
		})
	}
	return output
}

func responsesUsageMap(inputText string, outputTokens int64) map[string]interface{} {
	in := token.EstimateTokensString(inputText)
	return map[string]interface{}{
		"input_tokens":  in,
		"output_tokens": outputTokens,
		"total_tokens":  in + outputTokens,
	}
}

func (h *Handler) handleResponsesNonStreaming(c *gin.Context, model, inputText string, chatReq *ChatCompletionRequest, vm openaivm.VirtualModel) {
	if d := vm.SimulatedDelay(); d > 0 {
		time.Sleep(d)
	}
	resp, err := vm.HandleOpenAIChat(chatReq)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "api_error",
		}})
		return
	}

	respID := fmt.Sprintf("resp-virtual-%d", time.Now().Unix())
	c.JSON(http.StatusOK, map[string]interface{}{
		"id":         respID,
		"object":     "response",
		"created_at": time.Now().Unix(),
		"model":      model,
		"status":     "completed",
		"output":     responsesOutputItems("item-"+respID, &resp),
		"usage":      responsesUsageMap(inputText, token.EstimateTokensString(resp.Content)),
	})
}

func (h *Handler) handleResponsesStreaming(c *gin.Context, model, inputText string, chatReq *ChatCompletionRequest, vm openaivm.VirtualModel) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	if _, ok := c.Writer.(http.Flusher); !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
			"message": "Streaming not supported by this connection",
			"type":    "api_error",
			"code":    "streaming_unsupported",
		}})
		return
	}

	respID := fmt.Sprintf("resp-virtual-%d", time.Now().Unix())
	itemID := "item-" + respID
	send := func(payload map[string]interface{}) {
		data, _ := json.Marshal(payload)
		c.SSEvent("", string(data))
		c.Writer.Flush()
	}

	c.Stream(func(w io.Writer) bool {
		if d := vm.SimulatedDelay(); d > 0 {
			time.Sleep(d)
		}

		send(map[string]interface{}{
			"type": "response.created",
			"response": map[string]interface{}{
				"id": respID, "object": "response", "created_at": time.Now().Unix(),
				"model": model, "status": "in_progress", "output": []interface{}{},
			},
		})
		send(map[string]interface{}{
			"type": "response.output_item.added", "response_id": respID, "output_index": 0,
			"item": map[string]interface{}{
				"id": itemID, "type": "message", "role": "assistant",
				"status": "in_progress", "content": []interface{}{},
			},
		})

		final := openaivm.VModelResponse{FinishReason: "stop"}
		toolIndex := 0
		err := vm.HandleOpenAIChatStream(c.Request.Context(), chatReq, func(ev any) {
			select {
			case <-c.Request.Context().Done():
				return
			default:
			}
			switch e := ev.(type) {
			case openaivm.DeltaEvent:
				final.Content += e.Content
				send(map[string]interface{}{
					"type": "response.output_text.delta", "response_id": respID,
					"item_id": itemID, "output_index": 0, "content_index": 0,
					"delta": e.Content,
				})
			case openaivm.ToolEvent:
				fcID := fmt.Sprintf("fc-%s-%d", itemID, toolIndex)
				toolIndex++
				final.ToolCalls = append(final.ToolCalls, e.ToolCall)
				send(map[string]interface{}{
					"type": "response.output_item.added", "response_id": respID, "output_index": toolIndex,
					"item": map[string]interface{}{
						"id": fcID, "type": "function_call", "call_id": e.ToolCall.ID,
						"name": e.ToolCall.Name, "status": "in_progress",
					},
				})
				send(map[string]interface{}{
					"type": "response.function_call_arguments.delta", "response_id": respID,
					"item_id": fcID, "output_index": toolIndex, "delta": e.ToolCall.Arguments,
				})
				send(map[string]interface{}{
					"type": "response.function_call_arguments.done", "response_id": respID,
					"item_id": fcID, "output_index": toolIndex, "arguments": e.ToolCall.Arguments,
				})
			case openaivm.DoneEvent:
				final.FinishReason = e.FinishReason
			}
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{
				"message": err.Error(),
				"type":    "api_error",
			}})
			return false
		}

		send(map[string]interface{}{
			"type": "response.output_text.done", "response_id": respID,
			"item_id": itemID, "output_index": 0, "content_index": 0,
			"text": final.Content,
		})
		send(map[string]interface{}{
			"type": "response.completed",
			"response": map[string]interface{}{
				"id": respID, "object": "response", "created_at": time.Now().Unix(),
				"model": model, "status": "completed",
				"output": responsesOutputItems(itemID, &final),
				"usage":  responsesUsageMap(inputText, token.EstimateTokensString(final.Content)),
			},
		})
		c.SSEvent("", "[DONE]")
		c.Writer.Flush()
		return false
	})
}
