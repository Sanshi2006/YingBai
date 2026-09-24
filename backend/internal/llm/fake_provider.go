package llm

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
)

var ErrInvalidFakeDimension = errors.New("fake embedding dimension must be positive")

type FakeLLMProvider struct {
	dimension int
}

func NewFakeLLMProvider(dimension int) (*FakeLLMProvider, error) {
	if dimension <= 0 {
		return nil, ErrInvalidFakeDimension
	}
	return &FakeLLMProvider{dimension: dimension}, nil
}

func (p *FakeLLMProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if err := validateEmbeddingInputs(inputs); err != nil {
		return nil, err
	}
	vectors := make([][]float32, len(inputs))
	for inputIndex, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		vector := make([]float32, p.dimension)
		var squaredNorm float64
		for dimensionIndex := range vector {
			hashInput := make([]byte, 4+len(input))
			binary.BigEndian.PutUint32(hashInput[:4], uint32(dimensionIndex))
			copy(hashInput[4:], input)
			sum := sha256.Sum256(hashInput)
			unsigned := binary.BigEndian.Uint32(sum[:4])
			value := float32(float64(unsigned)/float64(^uint32(0))*2 - 1)
			vector[dimensionIndex] = value
			squaredNorm += float64(value * value)
		}
		norm := float32(math.Sqrt(squaredNorm))
		for dimensionIndex := range vector {
			vector[dimensionIndex] /= norm
		}
		vectors[inputIndex] = vector
	}
	return vectors, nil
}

func (p *FakeLLMProvider) EmbeddingModel() string {
	return "fake-embedding"
}

var _ LLMProvider = (*FakeLLMProvider)(nil)
