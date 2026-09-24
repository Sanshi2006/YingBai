package llm

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveOpenAICompatibleEmbedding(t *testing.T) {
	if os.Getenv("TEST_LIVE_LLM") != "1" {
		t.Skip("TEST_LIVE_LLM is not enabled")
	}
	provider, err := NewOpenAICompatibleProviderFromEnv(nil)
	if err != nil {
		t.Fatalf("create live provider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	vectors, err := provider.Embed(ctx, []string{"实验室智能客服向量链路测试"})
	if err != nil {
		t.Fatalf("live embedding request: %v", err)
	}
	if len(vectors) != 1 || len(vectors[0]) == 0 {
		t.Fatalf("unexpected live embedding shape: vectors=%d", len(vectors))
	}
	t.Logf("live embedding succeeded: vectors=%d dimensions=%d", len(vectors), len(vectors[0]))
}
