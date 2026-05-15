package server

import (
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

func TestResolveRuleFlags_NilRule(t *testing.T) {
	got := resolveRuleFlags(nil)
	// Zero value: all fields default
	want := typ.RuleFlags{}
	if got != want {
		t.Errorf("resolveRuleFlags(nil) = %#v, want zero value %#v", got, want)
	}
}

func TestResolveRuleFlags_CopiesFlags(t *testing.T) {
	rule := &typ.Rule{
		Flags: typ.RuleFlags{
			CursorCompat:           true,
			SkipUsage:              true,
			CustomUserAgent:        "MyApp/1.0",
			UseMaxCompletionTokens: true,
		},
	}
	got := resolveRuleFlags(rule)
	if !got.CursorCompat || !got.SkipUsage || !got.UseMaxCompletionTokens {
		t.Errorf("bool flags lost: %#v", got)
	}
	if got.CustomUserAgent != "MyApp/1.0" {
		t.Errorf("CustomUserAgent = %q, want %q", got.CustomUserAgent, "MyApp/1.0")
	}
}

func TestApplyMaxCompletionTokensRewrite_NilSafe(t *testing.T) {
	// Must not panic on nil
	applyMaxCompletionTokensRewrite(nil)
}

func TestApplyMaxCompletionTokensRewrite_MovesMaxTokens(t *testing.T) {
	req := &openai.ChatCompletionNewParams{
		MaxTokens: param.NewOpt(int64(1024)),
	}
	applyMaxCompletionTokensRewrite(req)

	if req.MaxTokens.Valid() {
		t.Errorf("expected MaxTokens cleared after rewrite, got %v", req.MaxTokens.Value)
	}
	if !req.MaxCompletionTokens.Valid() {
		t.Fatalf("expected MaxCompletionTokens populated after rewrite")
	}
	if req.MaxCompletionTokens.Value != 1024 {
		t.Errorf("MaxCompletionTokens = %d, want 1024", req.MaxCompletionTokens.Value)
	}
}

func TestApplyMaxCompletionTokensRewrite_NoMaxTokensNoOp(t *testing.T) {
	// When neither field is set the rewrite is a no-op (avoid emitting an
	// explicit max_completion_tokens=0 which most providers reject).
	req := &openai.ChatCompletionNewParams{}
	applyMaxCompletionTokensRewrite(req)
	if req.MaxTokens.Valid() {
		t.Errorf("MaxTokens unexpectedly valid: %#v", req.MaxTokens)
	}
	if req.MaxCompletionTokens.Valid() {
		t.Errorf("MaxCompletionTokens unexpectedly valid: %#v", req.MaxCompletionTokens)
	}
}

func TestShouldStripUsage_NilExtra(t *testing.T) {
	if shouldStripUsage(nil) {
		t.Errorf("nil extra map should not strip usage")
	}
}

func TestShouldStripUsage_EmptyExtra(t *testing.T) {
	if shouldStripUsage(map[string]interface{}{}) {
		t.Errorf("empty extra map should not strip usage")
	}
}

func TestShouldStripUsage_CursorCompatTrue(t *testing.T) {
	if !shouldStripUsage(map[string]interface{}{"cursor_compat": true}) {
		t.Errorf("cursor_compat=true should strip usage")
	}
}

func TestShouldStripUsage_SkipUsageTrue(t *testing.T) {
	if !shouldStripUsage(map[string]interface{}{"skip_usage": true}) {
		t.Errorf("skip_usage=true should strip usage")
	}
}

func TestShouldStripUsage_BothTrue(t *testing.T) {
	if !shouldStripUsage(map[string]interface{}{
		"cursor_compat": true,
		"skip_usage":    true,
	}) {
		t.Errorf("both flags true should strip usage")
	}
}

func TestShouldStripUsage_BothFalse(t *testing.T) {
	if shouldStripUsage(map[string]interface{}{
		"cursor_compat": false,
		"skip_usage":    false,
	}) {
		t.Errorf("both flags false should not strip usage")
	}
}

func TestShouldStripUsage_NonBoolValueIgnored(t *testing.T) {
	// Defensive: a non-bool sneaks past the type assertion as false.
	if shouldStripUsage(map[string]interface{}{
		"cursor_compat": "yes",
		"skip_usage":    1,
	}) {
		t.Errorf("non-bool values should be treated as false, not strip")
	}
}

func TestApplyMaxCompletionTokensRewrite_PreservesExistingMaxCompletionTokens(t *testing.T) {
	// If both fields are already present (caller supplied them), prefer the
	// MaxTokens migration but document the surprising case.
	req := &openai.ChatCompletionNewParams{
		MaxTokens:           param.NewOpt(int64(512)),
		MaxCompletionTokens: param.NewOpt(int64(2048)),
	}
	applyMaxCompletionTokensRewrite(req)

	if req.MaxTokens.Valid() {
		t.Errorf("expected MaxTokens cleared, got %v", req.MaxTokens.Value)
	}
	// Current behavior: MaxTokens overwrites MaxCompletionTokens. Track this
	// in tests so future refactors notice.
	if req.MaxCompletionTokens.Value != 512 {
		t.Errorf("MaxCompletionTokens = %d, want 512 (rewrite overrides existing)", req.MaxCompletionTokens.Value)
	}
}
