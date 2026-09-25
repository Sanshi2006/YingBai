package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"project-for-yingbai/backend/internal/config"
)

func TestOpenAICompatibleChatProviderUsesChatCompletionsContract(t *testing.T) {
	type capturedRequest struct {
		method        string
		path          string
		authorization string
		body          chatCompletionRequest
		decodeError   error
	}
	received := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body chatCompletionRequest
		decodeError := json.NewDecoder(request.Body).Decode(&body)
		received <- capturedRequest{
			method: request.Method, path: request.URL.Path,
			authorization: request.Header.Get("Authorization"), body: body, decodeError: decodeError,
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"润色后的问题"}}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleChatProvider(config.ChatLLMProviderConfig{
		APIKey: "test-key", BaseURL: server.URL + "/v1", Model: "deepseek-flash",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAICompatibleChatProvider() error = %v", err)
	}
	messages := []ChatMessage{{Role: ChatRoleSystem, Content: "改写问题"}, {Role: ChatRoleUser, Content: "原问题"}}
	answer, err := provider.Complete(context.Background(), messages, ChatCompletionOptions{Temperature: 0, MaxTokens: 256})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if answer != "润色后的问题" {
		t.Fatalf("answer = %q", answer)
	}
	captured := <-received
	if captured.decodeError != nil {
		t.Fatalf("decode request: %v", captured.decodeError)
	}
	if captured.method != http.MethodPost || captured.path != "/v1/chat/completions" {
		t.Fatalf("unexpected request: %s %s", captured.method, captured.path)
	}
	if captured.authorization != "Bearer test-key" {
		t.Fatalf("unexpected authorization header: %q", captured.authorization)
	}
	if captured.body.Model != "deepseek-flash" || captured.body.MaxTokens != 256 || captured.body.Temperature != 0 || captured.body.Stream {
		t.Fatalf("unexpected request body: %#v", captured.body)
	}
	if len(captured.body.Messages) != 2 || captured.body.Messages[1].Content != "原问题" {
		t.Fatalf("unexpected messages: %#v", captured.body.Messages)
	}
}

func TestOpenAICompatibleChatProviderRejectsInvalidResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	provider, err := NewOpenAICompatibleChatProvider(config.ChatLLMProviderConfig{
		APIKey: "test-key", BaseURL: server.URL, Model: "test-chat",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAICompatibleChatProvider() error = %v", err)
	}
	_, err = provider.Complete(context.Background(), []ChatMessage{{Role: ChatRoleUser, Content: "问题"}}, ChatCompletionOptions{MaxTokens: 32})
	if !errors.Is(err, ErrInvalidChatResponse) {
		t.Fatalf("error = %v, want ErrInvalidChatResponse", err)
	}
}

func TestOpenAICompatibleChatProviderFromEnv(t *testing.T) {
	t.Setenv("LLM_CHAT_API_KEY", "chat-key-from-env")
	t.Setenv("LLM_CHAT_BASE_URL", "https://api.deepseek.com")
	t.Setenv("LLM_CHAT_MODEL", "deepseek-flash")
	provider, err := NewOpenAICompatibleChatProviderFromEnv(nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleChatProviderFromEnv() error = %v", err)
	}
	if provider.apiKey != "chat-key-from-env" || provider.endpoint != "https://api.deepseek.com/chat/completions" || provider.model != "deepseek-flash" {
		t.Fatalf("provider did not use environment configuration: %#v", provider)
	}
}

func TestChatProviderRejectsInvalidMessagesAndOptions(t *testing.T) {
	provider := NewFakeChatLLMProvider("unused")
	tests := []struct {
		name     string
		messages []ChatMessage
		options  ChatCompletionOptions
	}{
		{name: "empty messages", options: ChatCompletionOptions{MaxTokens: 1}},
		{name: "invalid role", messages: []ChatMessage{{Role: "tool", Content: "x"}}, options: ChatCompletionOptions{MaxTokens: 1}},
		{name: "empty content", messages: []ChatMessage{{Role: ChatRoleUser, Content: " "}}, options: ChatCompletionOptions{MaxTokens: 1}},
		{name: "invalid temperature", messages: []ChatMessage{{Role: ChatRoleUser, Content: "x"}}, options: ChatCompletionOptions{Temperature: 3, MaxTokens: 1}},
		{name: "invalid max tokens", messages: []ChatMessage{{Role: ChatRoleUser, Content: "x"}}, options: ChatCompletionOptions{}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := provider.Complete(context.Background(), testCase.messages, testCase.options)
			if !errors.Is(err, ErrInvalidChatRequest) {
				t.Fatalf("error = %v, want ErrInvalidChatRequest", err)
			}
		})
	}
}
