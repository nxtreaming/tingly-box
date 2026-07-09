package server

import (
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/gin-gonic/gin"
	"github.com/tingly-dev/tingly-box/internal/server/recording"

	"github.com/tingly-dev/tingly-box/internal/obs"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/protocol/transform"
	servertransform "github.com/tingly-dev/tingly-box/internal/server/transform"
	"github.com/tingly-dev/tingly-box/internal/smart_compact"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// Stateless transform singletons shared across requests. BaseTransform and
// ConsistencyTransform are pure functions of the target type, and
// VendorTransform carries no state at all, so rebuilding them per request
// only produced allocation churn on the core forwarding path.
var (
	baseTransformCache        sync.Map // protocol.APIType -> *transform.BaseTransform
	consistencyTransformCache sync.Map // protocol.APIType -> *transform.ConsistencyTransform
	vendorTransformShared     = transform.NewVendorTransform()
)

func baseTransformFor(targetType protocol.APIType) *transform.BaseTransform {
	if v, ok := baseTransformCache.Load(targetType); ok {
		return v.(*transform.BaseTransform)
	}
	v, _ := baseTransformCache.LoadOrStore(targetType, transform.NewBaseTransform(targetType))
	return v.(*transform.BaseTransform)
}

func consistencyTransformFor(targetType protocol.APIType) *transform.ConsistencyTransform {
	if v, ok := consistencyTransformCache.Load(targetType); ok {
		return v.(*transform.ConsistencyTransform)
	}
	v, _ := consistencyTransformCache.LoadOrStore(targetType, transform.NewConsistencyTransform(targetType))
	return v.(*transform.ConsistencyTransform)
}

// mcpTransformCache lazily builds the MCP transforms once per handler. They
// only hold the (construction-time-fixed) MCP runtime pointer plus the strip
// flag, so both guard variants are prebuilt and selected per request.
type mcpTransformCache struct {
	once      sync.Once
	injection transform.Transform
	strip     transform.Transform
	guardOn   transform.Transform
	guardOff  transform.Transform
}

func (ph *ProtocolHandler) mcpChainTransforms(stripEnabled bool) []transform.Transform {
	ph.mcpTC.once.Do(func() {
		rt := ph.deps.MCPRuntime
		ph.mcpTC.injection = servertransform.NewMCPToolInjectionTransform(rt)
		ph.mcpTC.strip = servertransform.NewNativeWebSearchStripTransform(rt)
		ph.mcpTC.guardOn = servertransform.NewMCPToolStripGuardTransform(rt, true)
		ph.mcpTC.guardOff = servertransform.NewMCPToolStripGuardTransform(rt, false)
	})
	guard := ph.mcpTC.guardOff
	if stripEnabled {
		guard = ph.mcpTC.guardOn
	}
	return []transform.Transform{ph.mcpTC.injection, ph.mcpTC.strip, guard}
}

func (ph *ProtocolHandler) TransformAnthropicBeta(c *gin.Context, req *protocol.AnthropicBetaMessagesRequest, target protocol.APIType, provider *typ.Provider, isStreaming bool, protocolRecorder *recording.ProtocolRecorder, scenarioType typ.RuleScenario, preBaseTransforms []transform.Transform, preVendorTransforms []transform.Transform) (*transform.TransformContext, error) {

	// Build transform chain with recording support. The rule-driven pre-Base and
	// preVendor transforms are slotted into their canonical positions by the builder.
	chain, err := ph.buildTransformChain(c, target, scenarioType, protocolRecorder, preBaseTransforms, preVendorTransforms)
	if err != nil {
		return nil, err
	}

	// Create transform context
	var scenarioFlags *typ.ScenarioFlags
	if scenarioConfig := ph.deps.Config.GetScenarioConfig(scenarioType); scenarioConfig != nil {
		flags := scenarioConfig.GetDefaultFlags()
		scenarioFlags = &flags
		if flags.SmartCompact {
			baseTransforms := chain.GetTransforms()
			chain.SetTransforms(append(
				[]transform.Transform{smart_compact.NewCompactTransform(2)},
				baseTransforms...,
			))
		}
	}

	opts := []transform.TransformOption{
		transform.WithContext(c.Request.Context()),
		transform.WithProvider(provider),
		transform.WithScenarioFlags(scenarioFlags),
		transform.WithStreaming(isStreaming),
		transform.WithDevice(ph.deps.Config.ClaudeCodeDeviceID),
	}

	// Advisor loopback requests carry X-Tingly-Advisor-Depth >= 1; skip MCP tool injection for them
	if c.GetHeader("X-Tingly-Advisor-Depth") != "" {
		opts = append(opts, transform.WithIsAdvisorRequest(true))
	}

	transformCtx := transform.NewTransformContext(
		req.BetaMessageNewParams,
		opts...,
	)
	transformCtx.HasNativeAdvisor = HasNativeAdvisorBeta(req)
	transformCtx.SourceAPI = protocol.TypeAnthropicBeta
	transformCtx.TargetAPI = target

	// Execute transform chain
	finalCtx, err := chain.Execute(transformCtx)
	if err != nil {
		return nil, err
	}

	// Store transform steps in V2 recorder
	if protocolRecorder != nil {
		protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
	}

	return finalCtx, nil
}

func (ph *ProtocolHandler) TransformAnthropicV1(c *gin.Context, req *protocol.AnthropicMessagesRequest, target protocol.APIType, provider *typ.Provider, isStreaming bool, protocolRecorder *recording.ProtocolRecorder, scenarioType typ.RuleScenario, preBaseTransforms []transform.Transform, preVendorTransforms []transform.Transform) (*transform.TransformContext, error) {
	// Build transform chain with recording support. The rule-driven pre-Base and
	// preVendor transforms are slotted into their canonical positions by the builder.
	chain, err := ph.buildTransformChain(c, target, scenarioType, protocolRecorder, preBaseTransforms, preVendorTransforms)
	if err != nil {
		return nil, err
	}

	// Create transform context
	var scenarioFlags *typ.ScenarioFlags
	if scenarioConfig := ph.deps.Config.GetScenarioConfig(scenarioType); scenarioConfig != nil {
		flags := scenarioConfig.GetDefaultFlags()
		scenarioFlags = &flags
		if flags.SmartCompact {
			baseTransforms := chain.GetTransforms()
			chain.SetTransforms(append(
				[]transform.Transform{smart_compact.NewCompactTransform(2)},
				baseTransforms...,
			))
		}
	}

	opts := []transform.TransformOption{
		transform.WithProvider(provider),
		transform.WithScenarioFlags(scenarioFlags),
		transform.WithStreaming(isStreaming),
		transform.WithDevice(ph.deps.Config.ClaudeCodeDeviceID),
	}

	// Check if this is an advisor request
	if c.GetHeader("X-Tingly-Advisor-Depth") != "" {
		opts = append(opts, transform.WithIsAdvisorRequest(true))
	}

	transformCtx := transform.NewTransformContext(
		req.MessageNewParams,
		opts...,
	)
	transformCtx.SourceAPI = protocol.TypeAnthropicV1
	transformCtx.TargetAPI = target

	// Execute transform chain
	finalCtx, err := chain.Execute(transformCtx)
	if err != nil {
		if protocolRecorder != nil {
			protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
			protocolRecorder.RecordError(err)
		}
		return nil, err
	}

	// Store transform steps in V2 recorder
	if protocolRecorder != nil {
		protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
	}
	return finalCtx, nil
}

func (ph *ProtocolHandler) TransformOpenAIChat(c *gin.Context, req *protocol.OpenAIChatCompletionRequest, target protocol.APIType, provider *typ.Provider, isStreaming bool, protocolRecorder *recording.ProtocolRecorder, scenarioType typ.RuleScenario, preBaseTransforms []transform.Transform, preVendorTransforms []transform.Transform) (*transform.TransformContext, error) {
	// Build transform chain with recording support. The rule-driven pre-Base and
	// preVendor transforms are slotted into their canonical positions by the builder.
	chain, err := ph.buildTransformChain(c, target, scenarioType, protocolRecorder, preBaseTransforms, preVendorTransforms)
	if err != nil {
		return nil, err
	}

	// Create transform context
	var scenarioFlags *typ.ScenarioFlags
	if scenarioConfig := ph.deps.Config.GetScenarioConfig(scenarioType); scenarioConfig != nil {
		scenarioFlags = &scenarioConfig.Flags
	}

	opts := []transform.TransformOption{
		transform.WithProvider(provider),
		transform.WithScenarioFlags(scenarioFlags),
		transform.WithStreaming(isStreaming),
		transform.WithDevice(ph.deps.Config.ClaudeCodeDeviceID),
	}

	// Check if this is an advisor request
	if c.GetHeader("X-Tingly-Advisor-Depth") != "" {
		opts = append(opts, transform.WithIsAdvisorRequest(true))
	}

	transformCtx := transform.NewTransformContext(
		req.ChatCompletionNewParams,
		opts...,
	)
	transformCtx.SourceAPI = protocol.TypeOpenAIChat
	transformCtx.TargetAPI = target

	// Execute transform chain
	finalCtx, err := chain.Execute(transformCtx)
	if err != nil {
		if protocolRecorder != nil {
			protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
			protocolRecorder.RecordError(err)
		}
		return nil, err
	}

	// Store transform steps in V2 recorder
	if protocolRecorder != nil {
		protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
	}
	return finalCtx, nil
}

func (ph *ProtocolHandler) TransformOpenAIResponses(c *gin.Context, req *protocol.ResponseCreateRequest, target protocol.APIType, provider *typ.Provider, isStreaming bool, protocolRecorder *recording.ProtocolRecorder, scenarioType typ.RuleScenario, maxAllowed int, preBaseTransforms []transform.Transform, preVendorTransforms []transform.Transform) (*transform.TransformContext, error) {
	// Build transform chain with recording support. The rule-driven pre-Base and
	// preVendor transforms are slotted into their canonical positions by the builder.
	chain, err := ph.buildTransformChain(c, target, scenarioType, protocolRecorder, preBaseTransforms, preVendorTransforms)
	if err != nil {
		return nil, err
	}

	// Create transform context
	var scenarioFlags *typ.ScenarioFlags
	if scenarioConfig := ph.deps.Config.GetScenarioConfig(scenarioType); scenarioConfig != nil {
		scenarioFlags = &scenarioConfig.Flags
	}

	opts := []transform.TransformOption{
		transform.WithProvider(provider),
		transform.WithScenarioFlags(scenarioFlags),
		transform.WithStreaming(isStreaming),
		transform.WithDevice(ph.deps.Config.ClaudeCodeDeviceID),
		transform.WithMaxTokens(int64(maxAllowed)),
	}

	transformCtx := transform.NewTransformContext(
		req.ResponseNewParams,
		opts...,
	)
	transformCtx.SourceAPI = protocol.TypeOpenAIResponses
	transformCtx.TargetAPI = target

	// Execute transform chain
	finalCtx, err := chain.Execute(transformCtx)
	if err != nil {
		if protocolRecorder != nil {
			protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
			protocolRecorder.RecordError(err)
		}
		return nil, err
	}

	// Store transform steps in V2 recorder
	if protocolRecorder != nil {
		protocolRecorder.SetTransformSteps(finalCtx.TransformSteps)
	}
	return finalCtx, nil
}

// buildTransformChain assembles the canonical transform chain in a single place,
// slotting the rule-driven transforms into the two named positions — preBase and
// preVendor — that bracket the protocol conversion and the vendor finalize:
//
//	preBase slot   : preBase rule transforms (act on the client's original shape)
//	StagePre-record (if enabled)
//	Base           (protocol conversion)
//	MCP            (inject / native-websearch-strip / strip-guard) [if mcpEnabled]
//	Consistency    (cross-provider normalization, param clamping)
//	preVendor slot : preVendor rule transforms (act on the converted, upstream-bound shape)
//	Vendor         (provider-specific finalize)
//	StagePost-record (if enabled)
//
// Invariant: nothing runs after Vendor except recording. Vendor directly faces
// the provider and must be the last mutation, so the preVendor transforms are
// inserted after Consistency but BEFORE Vendor — this also means the StagePost
// recording captures the truly-final, dispatched request.
func (ph *ProtocolHandler) buildTransformChain(c *gin.Context, targetType protocol.APIType, scenarioType typ.RuleScenario, recorder *recording.ProtocolRecorder, preBase []transform.Transform, preVendor []transform.Transform) (*transform.TransformChain, error) {

	recordMode := ph.getScenarioRecordMode(scenarioType)
	shouldRecord := recorder != nil

	var transforms []transform.Transform

	requestRecordingEnabled := recordMode == obs.RecordModeRequestOnly ||
		recordMode == obs.RecordModeRequestResponse ||
		recordMode == obs.RecordModeStagedRequestResponse

	// preBase slot: rule transforms that act on the inbound request shape, before
	// any protocol conversion (and before recording, so the type-switch in each
	// transform sees what the client actually sent).
	transforms = append(transforms, preBase...)

	// 1. Pre-transform recording (if request recording is enabled)
	if shouldRecord && requestRecordingEnabled {
		transforms = append(transforms, NewTransformRecorder(c, recorder, StagePre))
	}

	// 2. Base transform (protocol conversion)
	transforms = append(transforms, baseTransformFor(targetType))
	if ph.mcpEnabled() {
		transforms = append(transforms, ph.mcpChainTransforms(ph.mcpStripDisabledToolsEnabled())...)
	}
	// 3. Consistency transform (cross-provider normalization including message alignment)
	transforms = append(transforms, consistencyTransformFor(targetType))

	// preVendor slot: rule transforms that act on the converted, upstream-bound
	// shape. Placed after Consistency (so its param clamping still applies) and
	// before Vendor (so Vendor remains the final, immutable step).
	transforms = append(transforms, preVendor...)

	transforms = append(transforms, vendorTransformShared)

	// 4. Post-transform recording (if request recording is enabled). Runs last so
	// it snapshots the truly-final request dispatched to the provider.
	if shouldRecord && requestRecordingEnabled {
		transforms = append(transforms, NewTransformRecorder(c, recorder, StagePost))
	}

	return transform.NewTransformChain(transforms), nil
}

// buildAnthropicPreChain constructs the pre-request transform chain for Anthropic V1 and Beta handlers.
// Currently only applies MaxTokens validation.
// All other scenario-level transforms (ThinkingEffort, CleanHeader) are handled via
// rule flags injection in resolveRuleFlagsWithScenario.
func buildAnthropicPreChain(
	scenarioConfig *typ.ScenarioConfig,
	defaultMaxTokens, maxAllowed int,
) []transform.Transform {
	var chain []transform.Transform
	// Only MaxTokens validation remains at scenario level
	chain = append(chain, servertransform.NewMaxTokensTransform(defaultMaxTokens, maxAllowed))
	return chain
}

// scenarioFlagsOrNil returns the scenario flags or nil.
func scenarioFlagsOrNil(scenarioConfig *typ.ScenarioConfig) *typ.ScenarioFlags {
	if scenarioConfig != nil {
		return &scenarioConfig.Flags
	}
	return nil
}

// ExecuteAnthropicV1PreChain builds and runs the pre-transform chain for Anthropic V1 requests.
// Returns an error that should be mapped to HTTP 400.
func ExecuteAnthropicV1PreChain(
	req *anthropic.MessageNewParams,
	scenarioConfig *typ.ScenarioConfig,
	defaultMaxTokens, maxAllowed int,
	isStreaming bool,
) error {
	transforms := buildAnthropicPreChain(scenarioConfig, defaultMaxTokens, maxAllowed)
	ctx := transform.NewTransformContext(
		req,
		transform.WithScenarioFlags(scenarioFlagsOrNil(scenarioConfig)),
		transform.WithStreaming(isStreaming),
	)
	if len(transforms) == 0 {
		return nil
	}
	_, err := transform.NewTransformChain(transforms).Execute(ctx)
	return err
}

// ExecuteAnthropicBetaPreChain builds and runs the pre-transform chain for Anthropic Beta requests.
// Returns an error that should be mapped to HTTP 400.
func ExecuteAnthropicBetaPreChain(
	req *anthropic.BetaMessageNewParams,
	scenarioConfig *typ.ScenarioConfig,
	defaultMaxTokens, maxAllowed int,
	isStreaming bool,
) error {
	transforms := buildAnthropicPreChain(scenarioConfig, defaultMaxTokens, maxAllowed)
	ctx := transform.NewTransformContext(
		req,
		transform.WithScenarioFlags(scenarioFlagsOrNil(scenarioConfig)),
		transform.WithStreaming(isStreaming),
	)
	if len(transforms) == 0 {
		return nil
	}
	_, err := transform.NewTransformChain(transforms).Execute(ctx)
	return err
}
