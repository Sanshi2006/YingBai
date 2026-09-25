package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/model"
)

func TestRetrievalServiceAppliesRolePermissionsAndConfiguredTopK(t *testing.T) {
	provider, err := llm.NewFakeLLMProvider(model.DocumentEmbeddingDimensions)
	if err != nil {
		t.Fatalf("NewFakeLLMProvider() error = %v", err)
	}

	tests := []struct {
		role        string
		permissions []string
	}{
		{role: "customer", permissions: []string{"公开"}},
		{role: "service", permissions: []string{"公开", "内部"}},
		{role: "admin", permissions: []string{"公开", "内部"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.role, func(t *testing.T) {
			repository := &capturingRetrievalRepository{results: []model.RetrievedChunk{{DocumentID: "doc-1", Similarity: 0.91}}}
			service, err := NewRetrievalService(repository, provider, config.RetrievalConfig{TopK: 7})
			if err != nil {
				t.Fatalf("NewRetrievalService() error = %v", err)
			}

			results, err := service.Search(context.Background(), " 样品如何接收？ ", testCase.role)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if len(results) != 1 || results[0].DocumentID != "doc-1" {
				t.Fatalf("unexpected results: %#v", results)
			}
			if repository.calls != 1 || repository.limit != 7 {
				t.Fatalf("repository calls=%d limit=%d, want calls=1 limit=7", repository.calls, repository.limit)
			}
			if repository.embeddingModel != "fake-embedding" {
				t.Fatalf("embedding model = %q, want fake-embedding", repository.embeddingModel)
			}
			if len(repository.queryVector) != model.DocumentEmbeddingDimensions {
				t.Fatalf("query dimensions = %d, want %d", len(repository.queryVector), model.DocumentEmbeddingDimensions)
			}
			if !reflect.DeepEqual(repository.permissions, testCase.permissions) {
				t.Fatalf("permissions = %#v, want %#v", repository.permissions, testCase.permissions)
			}
		})
	}
}

func TestRetrievalServiceRejectsInvalidInputBeforeDatabaseSearch(t *testing.T) {
	provider, err := llm.NewFakeLLMProvider(model.DocumentEmbeddingDimensions)
	if err != nil {
		t.Fatalf("NewFakeLLMProvider() error = %v", err)
	}
	repository := &capturingRetrievalRepository{}
	service, err := NewRetrievalService(repository, provider, config.RetrievalConfig{TopK: config.DefaultRetrievalTopK})
	if err != nil {
		t.Fatalf("NewRetrievalService() error = %v", err)
	}

	if _, err := service.Search(context.Background(), "   ", "customer"); !errors.Is(err, ErrRetrievalQueryRequired) {
		t.Fatalf("empty query error = %v, want ErrRetrievalQueryRequired", err)
	}
	if _, err := service.Search(context.Background(), "问题", "unknown"); !errors.Is(err, ErrInvalidRetrievalRole) {
		t.Fatalf("invalid role error = %v, want ErrInvalidRetrievalRole", err)
	}
	if repository.calls != 0 {
		t.Fatalf("invalid requests reached repository %d times", repository.calls)
	}
}

func TestNewRetrievalServiceRejectsUnsafeTopK(t *testing.T) {
	provider, err := llm.NewFakeLLMProvider(model.DocumentEmbeddingDimensions)
	if err != nil {
		t.Fatalf("NewFakeLLMProvider() error = %v", err)
	}
	for _, topK := range []int{0, config.MaxRetrievalTopK + 1} {
		if _, err := NewRetrievalService(&capturingRetrievalRepository{}, provider, config.RetrievalConfig{TopK: topK}); err == nil {
			t.Fatalf("expected TopK=%d to fail", topK)
		}
	}
}

type capturingRetrievalRepository struct {
	calls          int
	queryVector    []float32
	embeddingModel string
	permissions    []string
	limit          int
	results        []model.RetrievedChunk
}

func (r *capturingRetrievalRepository) SearchSimilar(_ context.Context, queryVector []float32, embeddingModel string, permissions []string, limit int) ([]model.RetrievedChunk, error) {
	r.calls++
	r.queryVector = append([]float32(nil), queryVector...)
	r.embeddingModel = embeddingModel
	r.permissions = append([]string(nil), permissions...)
	r.limit = limit
	return append([]model.RetrievedChunk(nil), r.results...), nil
}
