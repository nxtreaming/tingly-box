# processor

Op-level processors for `smart_routing`. A processor is a side-effect handler
bound to a `(SmartOpPosition, SmartOpOperation)` tuple. When the smart-routing
stage matches a rule whose ops carry a registered processor, the stage runs
the processor(s) and returns `(nil, false)` — letting the rest of the
pipeline (LoadBalancer, etc.) pick the real downstream with the **mutated**
request. This is the *implicit bypass* contract.

The matched rule's `Services` are treated as the **processor's upstream
candidate pool**, not the routing destination.

## Wiring

```
boot (server.go)
  └─► processor.RegisterAll(pool, resolver, logger)
        ├─► VisionProxyProcessor{
        │     Client:   NewPoolVisionClient(pool, resolver, logger),
        │     Resolver: resolver,
        │   }
        └─► smartrouting.RegisterProcessor(
              PositionProxyVision,
              OpProxyVisionEnabled,
              visionProc)
                  │
                  ▼
            registry["proxy_vision|enabled"] = visionProc

per request (internal/server/routing/stage_smart_routing.go)
  for op in matchedRule.Ops:
      if p, ok := smartrouting.LookupProcessor(op.Position, op.Operation); ok:
          collect p
  if len(collected) > 0:
      run each in op-order with a ProcessorContext
      mark ctx.BypassedSmartRules[ruleIndex] = struct{}{}
      return (nil, false)   // pipeline continues
```

## VisionProxyProcessor

Replaces every image content block in the request with a text block whose
body describes the image. Enables **text-only downstream models to accept
image-bearing requests**.

### Process pipeline

```
pctx.Request : *anthropic.BetaMessageNewParams (or v1 / OpenAI)

  messages: [
    { role: user,
      content: [
        { OfText:  "What's in this picture?" },
        { OfImage: <base64 jpeg> }   ◄── target
      ] } ]
       │
       │ pickUsableService(pctx.Services)
       │   skip nil / inactive / unresolvable-provider svcs
       │
       │ extractImageSource → (mediaType, b64Data, remoteURL)
       │   - Beta:   img.Source.OfBase64 | img.Source.OfURL
       │   - V1:     img.Source.OfBase64 | img.Source.OfURL
       │   - OpenAI: ParseImageURLToAnthropicSource(image_url.url)
       │
       │ describe(ctx, service, mediaType, b64, url):
       │   visionClient.Describe(...)  // sequential, one call per image
       │       │
       │       ▼
       │   poolVisionClient (production adapter)
       │     dispatches by provider.APIStyle and ALWAYS uses streaming
       │     (most providers require it for vision); events are folded
       │     back into a non-streaming message via the shared
       │     internal/protocol/assembler package so we never re-implement
       │     accumulation logic:
       │       "anthropic" → BetaMessagesNewStreaming →
       │                     assembler.NewAnthropicBetaSDKAssembler →
       │                     read text blocks from *BetaMessage
       │       "openai"    → ChatCompletionsNewStreaming →
       │                     assembler.NewOpenAIStreamAssembler →
       │                     read Choice.Message.Content from *ChatCompletion
       │       other       → error → fail-strip marker
       ▼
  describe = "a red apple on a white plate"   (success)
           = ""                                (empty   → fail-strip)
           = err                               (error   → fail-strip)
       │
       │ replace OfImage in-place with OfText block
       ▼
  content: [
    { OfText: "What's in this picture?" },
    { OfText: "[image: a red apple on a white plate]" } ]

  smart_routing stage returns (nil, false);
  LoadBalancer picks main service;
  forwarder serializes the now-text-only typed request downstream.
```

### Fail-strip semantics

The image block is removed **regardless of outcome** so the downstream
text-only model never receives unsupported content.

```
                          ┌──────────────────────────────────────────────┐
                          │ describe outcome                  → replacement│
                          ├──────────────────────────────────┬───────────┤
  no usable service       │ usable == nil                    │  unavail   │
  vision client nil       │ p.Client == nil                  │  unavail   │
  Describe() error        │ err != nil                       │  unavail   │
  empty response          │ strings.TrimSpace(desc) == ""    │  unavail   │
  success                 │ desc non-empty                   │  [image: …]│
                          └──────────────────────────────────┴───────────┘
  unavail = "[image: (description unavailable)]"
```

### Protocol coverage

| Request shape                              | Image block source                             | Notes                                  |
|--------------------------------------------|------------------------------------------------|----------------------------------------|
| `*anthropic.BetaMessageNewParams`          | `BetaImageBlockParam.Source` (Base64 \| URL)   | walks every user/assistant message     |
| `*anthropic.MessageNewParams`              | `ImageBlockParam.Source` (Base64 \| URL)       | walks every user/assistant message     |
| `*openai.ChatCompletionNewParams`          | `user.content[].OfImageURL.ImageURL.URL`       | parses `data:` URLs and remote URLs    |

Unknown request shapes are left alone (no-op).

## Adding a new processor

1. Implement `smartrouting.OpProcessor`:
   ```go
   type MyProc struct { /* deps */ }
   func (p *MyProc) Process(pctx *smartrouting.ProcessorContext) error { … }
   ```
2. Register it in `processor.RegisterAll`:
   ```go
   smartrouting.RegisterProcessor(
       smartrouting.PositionXxx,
       smartrouting.OpXxx,
       &MyProc{…})
   ```
3. Add a `SmartOp` entry in `internal/smart_routing/op.go` and handle the op
   in the appropriate `evaluateXxxOp` function so rules can declare it.

The matched rule's `Services` are passed in `pctx.Services` for processors
that need an upstream pool — `pickUsableService`-style selection is the
processor's responsibility.

## Out of scope (today)

- Concurrent image description (sequential, one call per image).
- Caching describe results across requests.
