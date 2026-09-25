package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/model"
)

var (
	ErrRetrievalQueryRequired = errors.New("retrieval query is required")
	ErrInvalidRetrievalRole   = errors.New("invalid retrieval role")
	ErrQueryEmbeddingFailed   = errors.New("query embedding failed")
)

type VectorSearchRepository interface {
	SearchSimilar(ctx context.Context, queryVector []float32, embeddingModel string, permissions []string, limit int) ([]model.RetrievedChunk, error)
}

type RetrievalService struct {
	repository VectorSearchRepository
	provider   llm.LLMProvider
	topK       int
}

func NewRetrievalService(repository VectorSearchRepository, provider llm.LLMProvider, configuration config.RetrievalConfig) (*RetrievalService, error) {
	if repository == nil {
		return nil, errors.New("retrieval repository is not configured")
	}
	if provider == nil {
		return nil, errors.New("embedding provider is not configured")
	}
	if configuration.TopK < 1 || configuration.TopK > config.MaxRetrievalTopK {
		return nil, fmt.Errorf("retrieval top-k must be between 1 and %d", config.MaxRetrievalTopK)
	}
	return &RetrievalService{repository: repository, provider: provider, topK: configuration.TopK}, nil
}

func (s *RetrievalService) Search(ctx context.Context, query, role string) ([]model.RetrievedChunk, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, ErrRetrievalQueryRequired
	}
	permissions, err := retrievalPermissions(role)
	if err != nil {
		return nil, err
	}

	vectors, err := s.provider.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQueryEmbeddingFailed, err)
	}
	if len(vectors) != 1 || len(vectors[0]) != model.DocumentEmbeddingDimensions {
		return nil, fmt.Errorf("%w: expected one %d-dimensional vector", ErrQueryEmbeddingFailed, model.DocumentEmbeddingDimensions)
	}

	results, err := s.repository.SearchSimilar(ctx, vectors[0], s.provider.EmbeddingModel(), permissions, s.topK)
	if err != nil {
		return nil, fmt.Errorf("retrieve knowledge chunks: %w", err)
	}
	if results == nil {
		results = make([]model.RetrievedChunk, 0)
	}
	return results, nil
}

func retrievalPermissions(role string) ([]string, error) {
	switch strings.TrimSpace(role) {
	case "customer":
		return []string{"公开"}, nil
	case "service", "admin":
		return []string{"公开", "内部"}, nil
	default:
		return nil, ErrInvalidRetrievalRole
	}
}
