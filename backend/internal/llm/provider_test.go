package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"project-for-yingbai/backend/internal/config"
)

func TestFakeLLMProviderReturnsDeterministicVectors(t *testing.T) {
	provider, err := NewFakeLLMProvider(8)
	if err != nil {
		t.Fatalf("NewFakeLLMProvider() error = %v", err)
	}
	first, err := provider.Embed(context.Background(), []string{"第一段", "第二段"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	second, err := provider.Embed(context.Background(), []string{"第一段", "第二段"})
	if err != nil {
		t.Fatalf("Embed() second call error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fake vectors are not deterministic: first=%v second=%v", first, second)
	}
	if len(first) != 2 || len(first[0]) != 8 || len(first[1]) != 8 {
		t.Fatalf("unexpected fake vector dimensions: %#v", first)
	}
	if reflect.DeepEqual(first[0], first[1]) {
		t.Fatal("different inputs produced identical fake vectors")
	}
}

func TestOpenAICompatibleProviderUsesEmbeddingContract(t *testing.T) {
	type capturedRequest struct {
		method        string
		path          string
		authorization string
		body          embeddingRequest
		decodeError   error
	}
	received := make(chan capturedRequest, 1)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body embeddingRequest
		decodeError := json.NewDecoder(request.Body).Decode(&body)
		received <- capturedRequest{
			method: request.Method, path: request.URL.Path,
			authorization: request.Header.Get("Authorization"), body: body, decodeError: decodeError,
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"index":1,"embedding":[0.3,0.4]},{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	defer server.Close()

	provider, err := NewOpenAICompatibleProvider(config.LLMProviderConfig{
		APIKey:         "test-key",
		BaseURL:        server.URL + "/v1",
		EmbeddingModel: "test-embedding",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}
	vectors, err := provider.Embed(context.Background(), []string{"A", "B"})
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	captured := <-received
	if captured.decodeError != nil {
		t.Fatalf("decode request: %v", captured.decodeError)
	}
	if captured.method != http.MethodPost || captured.path != "/v1/embeddings" {
		t.Fatalf("unexpected request: %s %s", captured.method, captured.path)
	}
	if captured.authorization != "Bearer test-key" {
		t.Fatalf("unexpected authorization header: %q", captured.authorization)
	}
	if captured.body.Model != "test-embedding" || !reflect.DeepEqual(captured.body.Input, []string{"A", "B"}) {
		t.Fatalf("unexpected request body: %#v", captured.body)
	}
	want := [][]float32{{0.1, 0.2}, {0.3, 0.4}}
	if !reflect.DeepEqual(vectors, want) {
		t.Fatalf("vectors = %v, want %v", vectors, want)
	}
}

func TestOpenAICompatibleProviderRejectsIncompleteResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"index":0,"embedding":[]}]}`))
	}))
	defer server.Close()
	provider, err := NewOpenAICompatibleProvider(config.LLMProviderConfig{
		APIKey: "test-key", BaseURL: server.URL, EmbeddingModel: "test-embedding",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}
	_, err = provider.Embed(context.Background(), []string{"A"})
	if !errors.Is(err, ErrInvalidEmbeddingResponse) {
		t.Fatalf("error = %v, want ErrInvalidEmbeddingResponse", err)
	}
}

func TestOpenAICompatibleProviderRejectsMixedDimensions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]},{"index":1,"embedding":[0.3]}]}`))
	}))
	defer server.Close()
	provider, err := NewOpenAICompatibleProvider(config.LLMProviderConfig{
		APIKey: "test-key", BaseURL: server.URL, EmbeddingModel: "test-embedding",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}
	_, err = provider.Embed(context.Background(), []string{"A", "B"})
	if !errors.Is(err, ErrInvalidEmbeddingResponse) {
		t.Fatalf("error = %v, want ErrInvalidEmbeddingResponse", err)
	}
}

func TestOpenAICompatibleProviderBatchesInputs(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		var body embeddingRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			http.Error(writer, "invalid request", http.StatusBadRequest)
			return
		}
		if len(body.Input) > defaultEmbeddingBatchSize {
			http.Error(writer, "batch too large", http.StatusBadRequest)
			return
		}
		data := make([]map[string]any, len(body.Input))
		for index := range body.Input {
			data[index] = map[string]any{"index": index, "embedding": []float32{float32(index + 1)}}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"data": data})
	}))
	defer server.Close()
	provider, err := NewOpenAICompatibleProvider(config.LLMProviderConfig{
		APIKey: "test-key", BaseURL: server.URL, EmbeddingModel: "test-embedding",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProvider() error = %v", err)
	}
	inputs := make([]string, defaultEmbeddingBatchSize+1)
	for index := range inputs {
		inputs[index] = "text"
	}
	vectors, err := provider.Embed(context.Background(), inputs)
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}
	if calls.Load() != 2 || len(vectors) != len(inputs) {
		t.Fatalf("calls=%d vectors=%d, want calls=2 vectors=%d", calls.Load(), len(vectors), len(inputs))
	}
}

func TestOpenAICompatibleProviderFromEnv(t *testing.T) {
	t.Setenv("LLM_API_KEY", "key-from-env")
	t.Setenv("LLM_BASE_URL", "https://example.com/v1")
	t.Setenv("LLM_EMBEDDING_MODEL", "model-from-env")
	provider, err := NewOpenAICompatibleProviderFromEnv(nil)
	if err != nil {
		t.Fatalf("NewOpenAICompatibleProviderFromEnv() error = %v", err)
	}
	if provider.apiKey != "key-from-env" || provider.endpoint != "https://example.com/v1/embeddings" || provider.model != "model-from-env" {
		t.Fatalf("provider did not use environment configuration: %#v", provider)
	}
}
