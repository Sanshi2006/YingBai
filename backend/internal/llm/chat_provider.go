package llm

import "context"

const (
	ChatRoleSystem    = "system"
	ChatRoleUser      = "user"
	ChatRoleAssistant = "assistant"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionOptions struct {
	Temperature float64
	MaxTokens   int
}

// ChatLLMProvider is the model-vendor boundary for question rewriting and grounded answer generation.
type ChatLLMProvider interface {
	Complete(ctx context.Context, messages []ChatMessage, options ChatCompletionOptions) (string, error)
	ChatModel() string
}
