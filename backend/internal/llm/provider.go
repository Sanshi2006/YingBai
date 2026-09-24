package llm

import "context"

// LLMProvider is the model-vendor boundary used by vectorization workflows.
type LLMProvider interface {
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
	EmbeddingModel() string
}
