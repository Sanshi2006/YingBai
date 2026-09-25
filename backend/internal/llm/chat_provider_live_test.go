package llm

import (
	"context"
	"os"
	"testing"
	"time"

	"project-for-yingbai/backend/internal/config"
)

func TestLiveOpenAICompatibleChatCompletion(t *testing.T) {
	if os.Getenv("TEST_LIVE_CHAT_LLM") != "1" {
		t.Skip("TEST_LIVE_CHAT_LLM is not enabled")
	}
	if _, err := config.LoadDotEnv(); err != nil {
		t.Fatalf("load local environment: %v", err)
	}
	provider, err := NewOpenAICompatibleChatProviderFromEnv(nil)
	if err != nil {
		t.Fatalf("create live chat provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	answer, err := provider.Complete(ctx, []ChatMessage{
		{Role: ChatRoleSystem, Content: "把用户问题改写成一句适合检索的中文，只输出改写结果。"},
		{Role: ChatRoleUser, Content: "包装破了咋办？"},
	}, ChatCompletionOptions{Temperature: 0, MaxTokens: 1024})
	if err != nil {
		t.Fatalf("live chat completion request: %v", err)
	}
	if answer == "" {
		t.Fatal("live chat completion returned empty content")
	}
	t.Logf("live chat completion succeeded: model=%s output_characters=%d", provider.ChatModel(), len([]rune(answer)))
}
