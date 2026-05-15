package server

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// resolveRuleFlags returns a copy of the rule's flags, or the zero value when
// no rule is bound. Callers may always read fields without nil-checking.
func resolveRuleFlags(rule *typ.Rule) typ.RuleFlags {
	if rule == nil {
		return typ.RuleFlags{}
	}
	return rule.Flags
}

// applyMaxCompletionTokensRewrite moves the value of `max_tokens` into the
// newer `max_completion_tokens` field. OpenAI's o1/o3/gpt-5 families reject
// `max_tokens`; this rewrite lets callers opt in per rule.
func applyMaxCompletionTokensRewrite(req *openai.ChatCompletionNewParams) {
	if req == nil {
		return
	}
	if req.MaxTokens.Valid() {
		req.MaxCompletionTokens = param.NewOpt(req.MaxTokens.Value)
		req.MaxTokens = param.Opt[int64]{}
	}
}
