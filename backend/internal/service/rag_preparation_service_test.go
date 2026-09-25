package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/model"
)

func TestRAGPreparationServiceRewritesRetrievesAndBuildsGroundedPrompt(t *testing.T) {
	chatLLM := llm.NewFakeChatLLMProvider("样品接收异常时应如何处理？")
	retriever := &capturingKnowledgeRetriever{results: []model.RetrievedChunk{
		{
			DocumentID: "doc-public", OriginalName: "样品规范.md", Permission: "公开",
			ChunkIndex: 2, Content: "包装破损时应暂停流转并记录异常。", CharCount: 18, Similarity: 0.92,
		},
	}}
	service, err := NewRAGPreparationService(chatLLM, retriever, config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55})
	if err != nil {
		t.Fatalf("NewRAGPreparationService() error = %v", err)
	}

	preparation, err := service.Prepare(context.Background(), " 包装破了咋办？ ", "customer")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if preparation.OriginalQuestion != "包装破了咋办？" || preparation.RewrittenQuestion != "样品接收异常时应如何处理？" {
		t.Fatalf("unexpected questions: %#v", preparation)
	}
	if retriever.query != preparation.RewrittenQuestion || retriever.role != "customer" || retriever.calls != 1 {
		t.Fatalf("retriever received query=%q role=%q calls=%d", retriever.query, retriever.role, retriever.calls)
	}
	if len(preparation.AnswerMessages) != 2 {
		t.Fatalf("answer messages = %#v, want 2", preparation.AnswerMessages)
	}
	if !strings.Contains(preparation.AnswerMessages[0].Content, "只能依据") || !strings.Contains(preparation.AnswerMessages[0].Content, "知识库暂无依据，请转人工") {
		t.Fatalf("grounding rules are missing: %q", preparation.AnswerMessages[0].Content)
	}
	for _, expected := range []string{"样品规范.md", "包装破损时应暂停流转并记录异常。", `"sourceId": "S1"`} {
		if !strings.Contains(preparation.AnswerMessages[1].Content, expected) {
			t.Fatalf("answer payload does not contain %q: %s", expected, preparation.AnswerMessages[1].Content)
		}
	}
	requests := chatLLM.Requests()
	if len(requests) != 1 || !strings.Contains(requests[0][0].Content, "不得添加") || !strings.Contains(requests[0][1].Content, "包装破了咋办？") {
		t.Fatalf("unexpected rewrite request: %#v", requests)
	}
}

func TestRAGPreparationServiceDoesNotBuildAnswerPromptWithoutKnowledge(t *testing.T) {
	chatLLM := llm.NewFakeChatLLMProvider("一个没有命中的问题")
	service, err := NewRAGPreparationService(chatLLM, &capturingKnowledgeRetriever{}, config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55})
	if err != nil {
		t.Fatalf("NewRAGPreparationService() error = %v", err)
	}

	preparation, err := service.Prepare(context.Background(), "没有命中的问题", "customer")
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if len(preparation.Chunks) != 0 || len(preparation.AnswerMessages) != 0 {
		t.Fatalf("empty retrieval should not build an answer prompt: %#v", preparation)
	}
}

func TestRAGPreparationServiceRejectsInvalidQuestionBeforeCallingLLM(t *testing.T) {
	chatLLM := llm.NewFakeChatLLMProvider("unused")
	retriever := &capturingKnowledgeRetriever{}
	service, err := NewRAGPreparationService(chatLLM, retriever, config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55})
	if err != nil {
		t.Fatalf("NewRAGPreparationService() error = %v", err)
	}

	if _, err := service.Prepare(context.Background(), "   ", "customer"); !errors.Is(err, ErrRAGQuestionRequired) {
		t.Fatalf("error = %v, want ErrRAGQuestionRequired", err)
	}
	if len(chatLLM.Requests()) != 0 || retriever.calls != 0 {
		t.Fatal("invalid question reached the LLM or retriever")
	}
}

func TestBuildGroundedAnswerMessagesRejectsIncompleteChunk(t *testing.T) {
	_, err := BuildGroundedAnswerMessages("问题", "改写问题", []model.RetrievedChunk{{DocumentID: "doc-1"}})
	if !errors.Is(err, ErrGroundedPrompt) {
		t.Fatalf("error = %v, want ErrGroundedPrompt", err)
	}
}

type capturingKnowledgeRetriever struct {
	calls   int
	query   string
	role    string
	results []model.RetrievedChunk
	err     error
}

func (r *capturingKnowledgeRetriever) Search(_ context.Context, query, role string) ([]model.RetrievedChunk, error) {
	r.calls++
	r.query = query
	r.role = role
	return append([]model.RetrievedChunk(nil), r.results...), r.err
}
