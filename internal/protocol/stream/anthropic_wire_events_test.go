package stream

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestTypedWireEventsMatchLegacyMapShapes verifies that the typed wire event
// structs marshal to exactly the same JSON documents the pre-refactor
// map-based emitters produced (field presence and values; key order is
// irrelevant to JSON semantics).
func TestTypedWireEventsMatchLegacyMapShapes(t *testing.T) {
	cases := []struct {
		name   string
		typed  any
		legacy map[string]interface{}
	}{
		{
			name:  "message_start",
			typed: newAnthropicMessageStartEvent("msg_123", "model-x", 42),
			legacy: map[string]interface{}{
				"type": "message_start",
				"message": map[string]interface{}{
					"id":            "msg_123",
					"type":          "message",
					"role":          "assistant",
					"content":       []interface{}{},
					"model":         "model-x",
					"stop_reason":   nil,
					"stop_sequence": nil,
					"usage": map[string]interface{}{
						"input_tokens":  42,
						"output_tokens": 0,
					},
				},
			},
		},
		{
			name: "content_block_start text",
			typed: anthropicContentBlockStartEvent{
				Type: eventTypeContentBlockStart, Index: 0,
				ContentBlock: anthropicTextBlockStart(),
			},
			legacy: map[string]interface{}{
				"type":          "content_block_start",
				"index":         0,
				"content_block": map[string]interface{}{"type": "text", "text": ""},
			},
		},
		{
			name: "content_block_start thinking",
			typed: anthropicContentBlockStartEvent{
				Type: eventTypeContentBlockStart, Index: 2,
				ContentBlock: anthropicThinkingBlockStart(),
			},
			legacy: map[string]interface{}{
				"type":          "content_block_start",
				"index":         2,
				"content_block": map[string]interface{}{"type": "thinking", "thinking": ""},
			},
		},
		{
			name: "content_block_start tool_use empty input",
			typed: anthropicContentBlockStartEvent{
				Type: eventTypeContentBlockStart, Index: 1,
				ContentBlock: anthropicToolUseBlockStart("toolu_1", "get_weather"),
			},
			legacy: map[string]interface{}{
				"type":  "content_block_start",
				"index": 1,
				"content_block": map[string]interface{}{
					"type": "tool_use", "id": "toolu_1", "name": "get_weather",
					"input": map[string]interface{}{},
				},
			},
		},
		{
			name: "content_block_start tool_use google args",
			typed: anthropicContentBlockStartEvent{
				Type: eventTypeContentBlockStart, Index: 1,
				ContentBlock: anthropicToolUseBlockStartWithInput("id1", "fn", map[string]any{"city": "Paris"}),
			},
			legacy: map[string]interface{}{
				"type":  "content_block_start",
				"index": 1,
				"content_block": map[string]interface{}{
					"type": "tool_use", "id": "id1", "name": "fn",
					"input": map[string]interface{}{"city": "Paris"},
				},
			},
		},
		{
			name: "content_block_start tool_use nil google args",
			typed: anthropicContentBlockStartEvent{
				Type: eventTypeContentBlockStart, Index: 1,
				ContentBlock: anthropicToolUseBlockStartWithInput("id1", "fn", map[string]any(nil)),
			},
			legacy: map[string]interface{}{
				"type":  "content_block_start",
				"index": 1,
				"content_block": map[string]interface{}{
					"type": "tool_use", "id": "id1", "name": "fn",
					"input": nil,
				},
			},
		},
		{
			name: "text delta",
			typed: anthropicContentBlockDeltaEvent{
				Type: eventTypeContentBlockDelta, Index: 0,
				Delta: anthropicTextDelta("hello"),
			},
			legacy: map[string]interface{}{
				"type": "content_block_delta", "index": 0,
				"delta": map[string]interface{}{"type": "text_delta", "text": "hello"},
			},
		},
		{
			name: "thinking delta",
			typed: anthropicContentBlockDeltaEvent{
				Type: eventTypeContentBlockDelta, Index: 3,
				Delta: anthropicThinkingDelta("hmm"),
			},
			legacy: map[string]interface{}{
				"type": "content_block_delta", "index": 3,
				"delta": map[string]interface{}{"type": "thinking_delta", "thinking": "hmm"},
			},
		},
		{
			name: "input_json delta",
			typed: anthropicContentBlockDeltaEvent{
				Type: eventTypeContentBlockDelta, Index: 1,
				Delta: anthropicInputJSONDelta(`{"a":`),
			},
			legacy: map[string]interface{}{
				"type": "content_block_delta", "index": 1,
				"delta": map[string]interface{}{"type": "input_json_delta", "partial_json": `{"a":`},
			},
		},
		{
			name: "signature delta",
			typed: anthropicContentBlockDeltaEvent{
				Type: eventTypeContentBlockDelta, Index: 3,
				Delta: anthropicSignatureDelta("c2ln"),
			},
			legacy: map[string]interface{}{
				"type": "content_block_delta", "index": 3,
				"delta": map[string]interface{}{"type": "signature_delta", "signature": "c2ln"},
			},
		},
		{
			name: "content_block_stop",
			typed: anthropicContentBlockStopEvent{
				Type: eventTypeContentBlockStop, Index: 5,
			},
			legacy: map[string]interface{}{"type": "content_block_stop", "index": 5},
		},
		{
			name:   "message_stop",
			typed:  anthropicMessageStopEvent{Type: eventTypeMessageStop},
			legacy: map[string]interface{}{"type": "message_stop"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			typedJSON, err := json.Marshal(tc.typed)
			if err != nil {
				t.Fatalf("marshal typed: %v", err)
			}
			legacyJSON, err := json.Marshal(tc.legacy)
			if err != nil {
				t.Fatalf("marshal legacy: %v", err)
			}
			var typedNorm, legacyNorm interface{}
			if err := json.Unmarshal(typedJSON, &typedNorm); err != nil {
				t.Fatalf("unmarshal typed: %v", err)
			}
			if err := json.Unmarshal(legacyJSON, &legacyNorm); err != nil {
				t.Fatalf("unmarshal legacy: %v", err)
			}
			if !reflect.DeepEqual(typedNorm, legacyNorm) {
				t.Errorf("wire mismatch:\n typed:  %s\n legacy: %s", typedJSON, legacyJSON)
			}
		})
	}
}

// TestToolUseBlockStartWithUntypedNilInput pins the omitempty coercion: an
// untyped nil input must still emit "input": null, not drop the key.
func TestToolUseBlockStartWithUntypedNilInput(t *testing.T) {
	b, err := json.Marshal(anthropicToolUseBlockStartWithInput("id1", "fn", nil))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["input"]; !ok || v != nil {
		t.Errorf("expected \"input\": null, got %s", b)
	}
}
