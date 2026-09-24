package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"project-for-yingbai/backend/internal/config"
)

const maxProviderResponseBytes int64 = 16 << 20
const defaultEmbeddingBatchSize = 10

var (
	ErrEmptyEmbeddingInput      = errors.New("embedding input is empty")
	ErrInvalidProviderConfig    = errors.New("invalid LLM provider configuration")
	ErrInvalidEmbeddingResponse = errors.New("invalid embedding response")
)

type OpenAICompatibleProvider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func NewOpenAICompatibleProvider(configuration config.LLMProviderConfig, client *http.Client) (*OpenAICompatibleProvider, error) {
	if strings.TrimSpace(configuration.APIKey) == "" || strings.TrimSpace(configuration.EmbeddingModel) == "" {
		return nil, ErrInvalidProviderConfig
	}
	baseURL, err := url.Parse(strings.TrimSpace(configuration.BaseURL))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: LLM_BASE_URL must be an absolute HTTP(S) URL without query or fragment", ErrInvalidProviderConfig)
	}
	endpoint, err := url.JoinPath(baseURL.String(), "embeddings")
	if err != nil {
		return nil, fmt.Errorf("%w: build embeddings endpoint: %v", ErrInvalidProviderConfig, err)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OpenAICompatibleProvider{
		apiKey:   strings.TrimSpace(configuration.APIKey),
		model:    strings.TrimSpace(configuration.EmbeddingModel),
		endpoint: endpoint,
		client:   client,
	}, nil
}

func NewOpenAICompatibleProviderFromEnv(client *http.Client) (*OpenAICompatibleProvider, error) {
	configuration, err := config.LoadLLMProviderConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewOpenAICompatibleProvider(configuration, client)
}

func (p *OpenAICompatibleProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := validateEmbeddingInputs(inputs); err != nil {
		return nil, err
	}
	vectors := make([][]float32, 0, len(inputs))
	for start := 0; start < len(inputs); start += defaultEmbeddingBatchSize {
		end := start + defaultEmbeddingBatchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch, err := p.embedBatch(ctx, inputs[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed batch starting at input %d: %w", start, err)
		}
		vectors = append(vectors, batch...)
	}
	return vectors, nil
}

func (p *OpenAICompatibleProvider) EmbeddingModel() string {
	return p.model
}

func (p *OpenAICompatibleProvider) embedBatch(ctx context.Context, inputs []string) ([][]float32, error) {
	payload, err := json.Marshal(embeddingRequest{Model: p.model, Input: inputs})
	if err != nil {
		return nil, fmt.Errorf("encode embedding request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create embedding request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("send embedding request: %w", err)
	}
	defer response.Body.Close()

	limitedBody := io.LimitReader(response.Body, maxProviderResponseBytes+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, fmt.Errorf("read embedding response: %w", err)
	}
	if int64(len(body)) > maxProviderResponseBytes {
		return nil, fmt.Errorf("%w: response exceeds %d bytes", ErrInvalidEmbeddingResponse, maxProviderResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, providerHTTPError(response.StatusCode, body)
	}

	var decoded embeddingResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return nil, fmt.Errorf("%w: decode response: %v", ErrInvalidEmbeddingResponse, err)
	}
	if len(decoded.Data) != len(inputs) {
		return nil, fmt.Errorf("%w: received %d vectors for %d inputs", ErrInvalidEmbeddingResponse, len(decoded.Data), len(inputs))
	}

	vectors := make([][]float32, len(inputs))
	seen := make([]bool, len(inputs))
	expectedDimension := 0
	for _, item := range decoded.Data {
		if item.Index < 0 || item.Index >= len(inputs) || seen[item.Index] {
			return nil, fmt.Errorf("%w: invalid or duplicate index %d", ErrInvalidEmbeddingResponse, item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("%w: empty vector at index %d", ErrInvalidEmbeddingResponse, item.Index)
		}
		if expectedDimension == 0 {
			expectedDimension = len(item.Embedding)
		} else if len(item.Embedding) != expectedDimension {
			return nil, fmt.Errorf("%w: vector at index %d has dimension %d, expected %d", ErrInvalidEmbeddingResponse, item.Index, len(item.Embedding), expectedDimension)
		}
		seen[item.Index] = true
		vectors[item.Index] = item.Embedding
	}
	return vectors, nil
}

type embeddingRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingResponse struct {
	Data []struct {
		Index     int       `json:"index"`
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

type providerErrorResponse struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func validateEmbeddingInputs(inputs []string) error {
	if len(inputs) == 0 {
		return ErrEmptyEmbeddingInput
	}
	for index, input := range inputs {
		if strings.TrimSpace(input) == "" {
			return fmt.Errorf("%w at index %d", ErrEmptyEmbeddingInput, index)
		}
	}
	return nil
}

func providerHTTPError(status int, body []byte) error {
	var decoded providerErrorResponse
	message := strings.TrimSpace(http.StatusText(status))
	if json.Unmarshal(body, &decoded) == nil && strings.TrimSpace(decoded.Error.Message) != "" {
		message = strings.TrimSpace(decoded.Error.Message)
	}
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	return fmt.Errorf("embedding provider returned HTTP %d: %s", status, message)
}

var _ LLMProvider = (*OpenAICompatibleProvider)(nil)
