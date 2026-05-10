package processor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/sirupsen/logrus"

	"github.com/tingly-dev/tingly-box/internal/client"
	"github.com/tingly-dev/tingly-box/internal/loadbalance"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// poolVisionClient is the production visionClient. It dispatches each
// describe call to the appropriate per-service client obtained from the
// shared ClientPool. First cut supports Anthropic-style providers
// (APIStyle == "anthropic"); other styles return an error and the
// processor falls back to the fail-strip marker.
type poolVisionClient struct {
	pool     *client.ClientPool
	resolver providerResolver
	logger   *logrus.Logger
	prompt   string
}

const defaultVisionPrompt = "Describe this image concisely; output plain text only."
const defaultVisionMaxTokens = 256

// NewPoolVisionClient builds the production vision client backed by the
// shared SDK pool. resolver is typically the routing.ProviderResolver
// implementation (server config). logger may be nil.
func NewPoolVisionClient(pool *client.ClientPool, resolver providerResolver, logger *logrus.Logger) visionClient {
	return &poolVisionClient{
		pool:     pool,
		resolver: resolver,
		logger:   logger,
		prompt:   defaultVisionPrompt,
	}
}

func (a *poolVisionClient) Describe(ctx context.Context, service *loadbalance.Service, mediaType, b64Data, remoteURL string) (string, error) {
	if service == nil {
		return "", errors.New("vision adapter: nil service")
	}
	if a.pool == nil || a.resolver == nil {
		return "", errors.New("vision adapter: pool or resolver not configured")
	}
	provider, err := a.resolver.GetProviderByUUID(service.Provider)
	if err != nil || provider == nil {
		return "", fmt.Errorf("vision adapter: resolve provider %q: %w", service.Provider, err)
	}

	switch strings.ToLower(string(provider.APIStyle)) {
	case "", "anthropic":
		return a.describeViaAnthropic(ctx, provider, service.Model, mediaType, b64Data, remoteURL)
	default:
		// OpenAI-style vision is not yet wired through this adapter; the
		// processor's fail-strip marker will be used instead.
		return "", fmt.Errorf("vision adapter: api_style %q not supported", provider.APIStyle)
	}
}

func (a *poolVisionClient) describeViaAnthropic(ctx context.Context, provider *typ.Provider, model, mediaType, b64Data, remoteURL string) (string, error) {
	var imageBlock anthropic.BetaContentBlockParamUnion
	switch {
	case b64Data != "":
		imageBlock = anthropic.NewBetaImageBlock(anthropic.BetaBase64ImageSourceParam{
			Data:      b64Data,
			MediaType: anthropic.BetaBase64ImageSourceMediaType(mediaType),
		})
	case remoteURL != "":
		imageBlock = anthropic.NewBetaImageBlock(anthropic.BetaURLImageSourceParam{URL: remoteURL})
	default:
		return "", errors.New("vision adapter: no image source")
	}

	c := a.pool.GetAnthropicClient(ctx, provider, model)
	if c == nil {
		return "", errors.New("vision adapter: pool returned nil anthropic client")
	}
	resp, err := c.BetaMessagesNew(ctx, &anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: defaultVisionMaxTokens,
		Messages: []anthropic.BetaMessageParam{
			{
				Role: anthropic.BetaMessageParamRoleUser,
				Content: []anthropic.BetaContentBlockParamUnion{
					imageBlock,
					{OfText: &anthropic.BetaTextBlockParam{Text: a.prompt}},
				},
			},
		},
	})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, b := range resp.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return strings.TrimSpace(sb.String()), nil
}

