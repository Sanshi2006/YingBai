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

var (
	ErrInvalidChatProviderConfig = errors.New("invalid chat LLM provider configuration")
	ErrInvalidChatRequest        = errors.New("invalid chat completion request")
	ErrInvalidChatResponse       = errors.New("invalid chat completion response")
)

type OpenAICompatibleChatProvider struct {
	apiKey   string
	model    string
	endpoint string
	client   *http.Client
}

func NewOpenAICompatibleChatProvider(configuration config.ChatLLMProviderConfig, client *http.Client) (*OpenAICompatibleChatProvider, error) {
	if strings.TrimSpace(configuration.APIKey) == "" || strings.TrimSpace(configuration.Model) == "" {
		return nil, ErrInvalidChatProviderConfig
	}
	baseURL, err := url.Parse(strings.TrimSpace(configuration.BaseURL))
	if err != nil || (baseURL.Scheme != "http" && baseURL.Scheme != "https") || baseURL.Host == "" || baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return nil, fmt.Errorf("%w: LLM_CHAT_BASE_URL must be an absolute HTTP(S) URL without query or fragment", ErrInvalidChatProviderConfig)
	}
	endpoint, err := url.JoinPath(baseURL.String(), "chat", "completions")
	if err != nil {
		return nil, fmt.Errorf("%w: build chat completions endpoint: %v", ErrInvalidChatProviderConfig, err)
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	return &OpenAICompatibleChatProvider{
		apiKey:   strings.TrimSpace(configuration.APIKey),
		model:    strings.TrimSpace(configuration.Model),
		endpoint: endpoint,
		client:   client,
	}, nil
}

func NewOpenAICompatibleChatProviderFromEnv(client *http.Client) (*OpenAICompatibleChatProvider, error) {
	configuration, err := config.LoadChatLLMProviderConfigFromEnv()
	if err != nil {
		return nil, err
	}
	return NewOpenAICompatibleChatProvider(configuration, client)
}

func (p *OpenAICompatibleChatProvider) Complete(ctx context.Context, messages []ChatMessage, options ChatCompletionOptions) (string, error) {
	if err := validateChatCompletionRequest(messages, options); err != nil {
		return "", err
	}
	payload, err := json.Marshal(chatCompletionRequest{
		Model:       p.model,
		Messages:    messages,
		Temperature: options.Temperature,
		MaxTokens:   options.MaxTokens,
		Stream:      false,
	})
	if err != nil {
		return "", fmt.Errorf("encode chat completion request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create chat completion request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+p.apiKey)
	request.Header.Set("Content-Type", "application/json")

	response, err := p.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("send chat completion request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	if err != nil {
		return "", fmt.Errorf("read chat completion response: %w", err)
	}
	if int64(len(body)) > maxProviderResponseBytes {
		return "", fmt.Errorf("%w: response exceeds %d bytes", ErrInvalidChatResponse, maxProviderResponseBytes)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", chatProviderHTTPError(response.StatusCode, body)
	}

	var decoded chatCompletionResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", fmt.Errorf("%w: decode response: %v", ErrInvalidChatResponse, err)
	}
	if len(decoded.Choices) == 0 {
		return "", fmt.Errorf("%w: response has no choices", ErrInvalidChatResponse)
	}
	content := strings.TrimSpace(decoded.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf(
			"%w: first choice has empty content (finish_reason=%q reasoning_content_present=%t)",
			ErrInvalidChatResponse,
			decoded.Choices[0].FinishReason,
			strings.TrimSpace(decoded.Choices[0].Message.ReasoningContent) != "",
		)
	}
	return content, nil
}

func (p *OpenAICompatibleChatProvider) ChatModel() string {
	return p.model
}

func validateChatCompletionRequest(messages []ChatMessage, options ChatCompletionOptions) error {
	if len(messages) == 0 {
		return fmt.Errorf("%w: messages are empty", ErrInvalidChatRequest)
	}
	for index, message := range messages {
		if message.Role != ChatRoleSystem && message.Role != ChatRoleUser && message.Role != ChatRoleAssistant {
			return fmt.Errorf("%w: unsupported role %q at index %d", ErrInvalidChatRequest, message.Role, index)
		}
		if strings.TrimSpace(message.Content) == "" {
			return fmt.Errorf("%w: empty content at index %d", ErrInvalidChatRequest, index)
		}
	}
	if options.Temperature < 0 || options.Temperature > 2 {
		return fmt.Errorf("%w: temperature must be between 0 and 2", ErrInvalidChatRequest)
	}
	if options.MaxTokens < 1 || options.MaxTokens > 8192 {
		return fmt.Errorf("%w: max tokens must be between 1 and 8192", ErrInvalidChatRequest)
	}
	return nil
}

func chatProviderHTTPError(status int, body []byte) error {
	var decoded providerErrorResponse
	message := strings.TrimSpace(http.StatusText(status))
	if json.Unmarshal(body, &decoded) == nil && strings.TrimSpace(decoded.Error.Message) != "" {
		message = strings.TrimSpace(decoded.Error.Message)
	}
	if len(message) > 512 {
		message = message[:512] + "..."
	}
	return fmt.Errorf("chat provider returned HTTP %d: %s", status, message)
}

type chatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
}

type chatCompletionResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role             string `json:"role"`
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

var _ ChatLLMProvider = (*OpenAICompatibleChatProvider)(nil)
