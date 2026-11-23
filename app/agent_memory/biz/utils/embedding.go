package utils

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/MoScenix/agent-memory/app/agent_memory/conf"
	openaiembedding "github.com/cloudwego/eino-ext/components/embedding/openai"
)

// NewQwenEmbedder configures Eino's embedder for DashScope's compatible API.
func NewQwenEmbedder(ctx context.Context, cfg conf.Embedding) (*openaiembedding.Embedder, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	endpoint, err := url.Parse(baseURL)
	if err != nil || endpoint.Hostname() == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") ||
		endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("a valid Qwen embedding base URL is required")
	}
	if cfg.Model == "" || strings.TrimSpace(cfg.APIKey) == "" || cfg.Dimensions <= 0 {
		return nil, errors.New("Qwen embedding requires a model, API key and positive dimensions")
	}
	return openaiembedding.NewEmbedder(ctx, &openaiembedding.EmbeddingConfig{
		Model:      cfg.Model,
		APIKey:     cfg.APIKey,
		BaseURL:    baseURL,
		Dimensions: &cfg.Dimensions,
		HTTPClient: &http.Client{
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	})
}
