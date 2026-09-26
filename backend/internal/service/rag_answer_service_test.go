package service

import (
	"context"
	"errors"
	"testing"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/model"
)

func TestRAGAnswerServiceRunsRewriteRetrievalPromptAndAnswer(t *testing.T) {
	chatLLM := llm.NewFakeChatLLMProvider(
		"样品包装破损时应如何处理？",
		"应暂停样品流转并记录异常。[S1]",
	)
	retriever := &capturingKnowledgeRetriever{results: []model.RetrievedChunk{{
		DocumentID: "doc-1", OriginalName: "样品规范.md", ChunkIndex: 0,
		Content: "包装破损时应暂停流转并记录异常。", Similarity: 0.91,
	}}}
	configuration := config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55}
	preparation, err := NewRAGPreparationService(chatLLM, retriever, configuration)
	if err != nil {
		t.Fatalf("NewRAGPreparationService() error = %v", err)
	}
	answerService, err := NewRAGAnswerService(preparation, chatLLM)
	if err != nil {
		t.Fatalf("NewRAGAnswerService() error = %v", err)
	}

	answer, err := answerService.Answer(context.Background(), "包装破了咋办？", "customer", nil)
	if err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if answer.Type != "knowledge" || answer.Answer != "应暂停样品流转并记录异常。[S1]" {
		t.Fatalf("unexpected answer: %#v", answer)
	}
	if len(answer.Citations) != 1 || answer.Citations[0].SourceID != "S1" || answer.Citations[0].OriginalName != "样品规范.md" {
		t.Fatalf("unexpected citations: %#v", answer.Citations)
	}
	if len(chatLLM.Requests()) != 2 {
		t.Fatalf("chat LLM calls = %d, want rewrite + answer", len(chatLLM.Requests()))
	}
}

func TestRAGAnswerServiceRefusesBelowThresholdWithoutAnswerCall(t *testing.T) {
	chatLLM := llm.NewFakeChatLLMProvider("无关问题的改写")
	retriever := &capturingKnowledgeRetriever{results: []model.RetrievedChunk{{
		DocumentID: "doc-low", OriginalName: "低相关.md", ChunkIndex: 0,
		Content: "低相关内容", Similarity: 0.3,
	}}}
	configuration := config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55}
	preparation, err := NewRAGPreparationService(chatLLM, retriever, configuration)
	if err != nil {
		t.Fatalf("NewRAGPreparationService() error = %v", err)
	}
	answerService, err := NewRAGAnswerService(preparation, chatLLM)
	if err != nil {
		t.Fatalf("NewRAGAnswerService() error = %v", err)
	}

	answer, err := answerService.Answer(context.Background(), "无关问题", "customer", nil)
	if err != nil {
		t.Fatalf("Answer() error = %v", err)
	}
	if answer.Type != "refusal" || answer.Answer != RAGRefusalAnswer || len(answer.Citations) != 0 {
		t.Fatalf("unexpected refusal: %#v", answer)
	}
	if len(chatLLM.Requests()) != 1 {
		t.Fatalf("chat LLM calls = %d, want rewrite only", len(chatLLM.Requests()))
	}
}

func TestRAGAnswerServicePropagatesAnswerFailure(t *testing.T) {
	chatLLM := llm.NewFakeChatLLMProvider("改写后问题")
	retriever := &capturingKnowledgeRetriever{results: []model.RetrievedChunk{{
		DocumentID: "doc-1", OriginalName: "知识.md", Content: "知识内容", Similarity: 0.9,
	}}}
	configuration := config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55}
	preparation, err := NewRAGPreparationService(chatLLM, retriever, configuration)
	if err != nil {
		t.Fatalf("NewRAGPreparationService() error = %v", err)
	}
	answerService, err := NewRAGAnswerService(preparation, chatLLM)
	if err != nil {
		t.Fatalf("NewRAGAnswerService() error = %v", err)
	}

	_, err = answerService.Answer(context.Background(), "问题", "customer", nil)
	if !errors.Is(err, ErrRAGAnswerGeneration) {
		t.Fatalf("error = %v, want ErrRAGAnswerGeneration", err)
	}
}
