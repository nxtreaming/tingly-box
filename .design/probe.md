# Probe Subsystem

## Overview

The probe subsystem performs SDK-level end-to-end connectivity tests for providers and rules. There are two probe strategies:

- **Lightweight** (`internal/probe/lightweight.go`): HTTP-level checks (OPTIONS, `/models`, `/chat/completions`) with no SDK. Used during provider onboarding to validate credentials quickly.
- **E2E** (`internal/probe/e2e.go`): Full SDK round-trip using the same client methods as production traffic (ChatCompletionsNew, ResponsesNew, MessagesNew, GenerateContent). This catches provider quirks that only show up under the real code path.

## E2E Target Types

An `E2ERequest` has three `target_type` values:

| `target_type`     | What it tests                                                                |
|-------------------|------------------------------------------------------------------------------|
| `provider`        | A saved provider record by UUID, pinned to a specific model                  |
| `rule`            | A rule by UUID — exercises all TB middleware for that rule's scenario         |
| `provider_config` | An inline provider config (name, api_base, api_style, token) — used during onboarding before the provider is saved |

### Test Modes

`test_mode` controls the shape of the probe request:

| Mode        | Description                                         |
|-------------|-----------------------------------------------------|
| `simple`    | Single non-streaming completion                     |
| `streaming` | Streaming completion (SSE)                          |
| `tool`      | Completion with a tool definition + auto tool choice |

## TB Loopback Pattern

Provider and rule probes route through TB's own HTTP endpoint (`http://localhost:{port}/tingly/{scenario}[/v1]`) rather than going directly to the upstream API. This ensures that rule flags (`openai_endpoint_override`, thinking effort, etc.), smart routing, and load balancing all execute exactly as they would for production traffic.

```
Probe code
  → SDK client (with probeHeaderRoundTripper)
    → TB loopback /tingly/{scenario}/chat/completions (or /messages)
      → determineRuleWithScenario (reads X-Tingly-Probe-* headers)
        → SimpleSelector.SelectService (pins service if header present)
          → upstream provider
```

### URL conventions

`loopbackAPIBase(port, scenario)` delegates to `ScenarioEndpoint(scenario)` for the canonical `/tingly/{scenario}` path — no `/v1` suffix. TB registers both `/tingly/:scenario` and `/tingly/:scenario/v1` with identical handlers, so each SDK appends its own operation path (`/chat/completions`, `/messages`) without needing the prefix to carry a version segment.

`resolveRuleTarget` passes `rule.Scenario` directly to `loopbackAPIBase`. If `ServerPort == 0` (unknown), it returns an error rather than falling back to direct (rule probes have no meaningful fallback).

`resolveProviderTarget` calls `defaultScenarioForAPIStyle(provider.APIStyle)` to get the canonical scenario for the provider, then passes it to `loopbackAPIBase`. Google providers and the `port == 0` case fall back to direct SDK calls.

Virtual model providers (`provider.IsVirtual()`) are also resolved to the TB loopback via `resolveVModelLoopbackTarget`, sharing the same `loopbackAPIBase` helper.

## Probe Headers

Two request headers let the probe subsystem control TB routing without modifying the stored rule or provider configuration.

### `X-Tingly-Probe-Service: {provider_uuid}:{model}`

Injected by `resolveProviderTarget` on the SDK client transport. Two TB layers consume it:

1. **`determineRuleWithScenario`** (handlers.go): If no `X-Tingly-Probe-Rule` header is present, builds a minimal synthetic `typ.Rule` wrapping the pinned service so the handler has a rule to work with.
2. **`SimpleSelector.SelectService`** (routing/simple.go): Bypasses the affinity → smart routing → load balancer pipeline and returns the pinned provider+model directly.

### `X-Tingly-Probe-Rule: {rule_uuid}`

Optionally injected by callers that want to apply a specific rule's flags while overriding service selection via `X-Tingly-Probe-Service`. `determineRuleWithScenario` loads the named rule and returns it; the `SelectService` probe pin still applies.

### Transport wiring

Headers are stored in the `context.Context` via `client.WithProbeHeaders(ctx, headers)`. `probeHeaderRoundTripper` (in `internal/client/http.go`) reads the context on every `RoundTrip` and injects the headers into the outgoing request.

This round tripper is **not** installed on production clients. `ProbeProviderWithSDK` calls `client.ApplyProbeHeadersToClient(c)` only when `client.GetProbeHeaders(ctx)` returns true — i.e., only for probe-path clients.

## Code layout

```
internal/probe/
  types.go        — E2ERequest/E2EData/E2EMode/E2ETarget types, ScenarioEndpoint()
  e2e.go          — E2EService: resolveTargetToProviderModel, loopbackAPIBase, ProbeProviderWithSDK
  sdkprobe.go     — SDK dispatch helpers: probeOpenAIChat, probeAnthropicMessages, probeGoogleGenerate, …
  lightweight.go  — LightweightProbeService (HTTP-level, no SDK)
  probetools.go   — Tool definitions used by E2EModeTool
  result.go       — ProbeResult type, cache

internal/client/
  http.go         — probeHeadersKey, WithProbeHeaders, GetProbeHeaders,
                    probeHeaderRoundTripper, wrapWithProbeHeaders, ApplyProbeHeadersToClient

internal/server/
  handlers.go     — determineRuleWithScenario: X-Tingly-Probe-Rule / X-Tingly-Probe-Service handling
  routing/
    simple.go     — SimpleSelector.SelectService: X-Tingly-Probe-Service service pin
```

## Trade-offs and constraints

- **Google probes go direct**: The TB loopback only exposes `/tingly/openai` and `/tingly/anthropic` endpoints. Google uses its own SDK and has no matching loopback route, so `resolveProviderTarget` returns the original provider record for Google.
- **Rule probe requires a running server**: `resolveRuleTarget` fails fast if `ServerPort == 0`. There is no direct fallback for rule probes because the whole point is to exercise TB middleware.
- **Probe headers are not authenticated**: Any caller that can reach the TB HTTP port can send `X-Tingly-Probe-Service` and bypass load balancing. This is intentional — probe endpoints are admin-only behind TB's own auth layer.
- **`probe-synthetic` rule UUID**: The synthetic rule created from `X-Tingly-Probe-Service` (when no probe rule header is present) carries `UUID: "probe-synthetic"`. This is a sentinel value, not a persisted rule; it exists only for the duration of the request.
