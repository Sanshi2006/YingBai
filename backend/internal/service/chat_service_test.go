package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"project-for-yingbai/backend/internal/model"
)

func TestChatServiceRecognizesNaturalLanguageOrderQueries(t *testing.T) {
	knowledge := &stubKnowledgeAnswerer{}
	chatService, err := NewChatService(knowledge, NewMockLIMSClient())
	if err != nil {
		t.Fatalf("NewChatService() error = %v", err)
	}
	questions := []string{
		"查一下订单 ORD2026001",
		"麻烦帮我查询一下 ord2026001 的进度，谢谢",
		"ORD2026001 报告出来了吗？",
	}
	for _, question := range questions {
		answer, err := chatService.Answer(context.Background(), question, "customer", nil)
		if err != nil {
			t.Fatalf("Answer(%q) error = %v", question, err)
		}
		if answer.Type != AnswerTypeOrder || answer.Order == nil || !answer.Order.Found || !answer.Order.Mock {
			t.Fatalf("unexpected order answer for %q: %#v", question, answer)
		}
		if answer.Order.OrderNumber != MockOrderNumber || answer.Order.Status != "检测中" ||
			answer.Order.Progress != 68 || answer.Order.ReportStatus != "未生成" {
			t.Fatalf("unstable Mock order data: %#v", answer.Order)
		}
	}
	if knowledge.calls != 0 {
		t.Fatalf("order query reached knowledge RAG %d time(s)", knowledge.calls)
	}
}

func TestChatServiceReturnsExplicitOrderInputAndNotFoundStates(t *testing.T) {
	chatService, err := NewChatService(&stubKnowledgeAnswerer{}, NewMockLIMSClient())
	if err != nil {
		t.Fatalf("NewChatService() error = %v", err)
	}

	missingNumber, err := chatService.Answer(context.Background(), "请帮我查订单", "service", nil)
	if err != nil {
		t.Fatalf("missing-number query error = %v", err)
	}
	if missingNumber.Type != AnswerTypeOrder || missingNumber.Order != nil ||
		missingNumber.Answer != "请提供需要查询的订单号，例如 ORD2026001。" {
		t.Fatalf("unexpected missing-number answer: %#v", missingNumber)
	}

	notFound, err := chatService.Answer(context.Background(), "查订单 ORD9999999", "admin", nil)
	if err != nil {
		t.Fatalf("not-found query error = %v", err)
	}
	if notFound.Order == nil || notFound.Order.Found || notFound.Order.OrderNumber != "ORD9999999" ||
		notFound.Order.Status != "" || notFound.Order.Progress != 0 || notFound.Order.ReportStatus != "" {
		t.Fatalf("not-found response fabricated order data: %#v", notFound)
	}
}

func TestChatServiceFallsBackToKnowledgeAndRejectsInvalidRole(t *testing.T) {
	knowledge := &stubKnowledgeAnswerer{answer: RAGAnswer{
		Answer: "知识回答", Type: "knowledge", Citations: make([]RAGCitation, 0),
	}}
	chatService, err := NewChatService(knowledge, NewMockLIMSClient())
	if err != nil {
		t.Fatalf("NewChatService() error = %v", err)
	}
	answer, err := chatService.Answer(context.Background(), "常见标准有哪些？", "customer", nil)
	if err != nil {
		t.Fatalf("knowledge fallback error = %v", err)
	}
	if answer.Type != "knowledge" || knowledge.calls != 1 {
		t.Fatalf("unexpected knowledge fallback: answer=%#v calls=%d", answer, knowledge.calls)
	}
	if _, err := chatService.Answer(context.Background(), "查订单 ORD2026001", "unknown", nil); !errors.Is(err, ErrInvalidRetrievalRole) {
		t.Fatalf("invalid role error = %v, want ErrInvalidRetrievalRole", err)
	}
}

func TestChatServiceRejectsOversizedOrderQueryAndWrapsLIMSError(t *testing.T) {
	chatService, err := NewChatService(&stubKnowledgeAnswerer{}, failingLIMSClient{})
	if err != nil {
		t.Fatalf("NewChatService() error = %v", err)
	}
	if _, err := chatService.Answer(
		context.Background(), strings.Repeat("查", maxRAGQuestionCharacters+1)+" ORD2026001", "customer", nil,
	); !errors.Is(err, ErrRAGQuestionTooLong) {
		t.Fatalf("oversized order query error = %v, want ErrRAGQuestionTooLong", err)
	}
	if _, err := chatService.Answer(
		context.Background(), "查订单 ORD2026001", "customer", nil,
	); !errors.Is(err, ErrLIMSQueryFailed) {
		t.Fatalf("LIMS error = %v, want ErrLIMSQueryFailed", err)
	}
}

type stubKnowledgeAnswerer struct {
	answer RAGAnswer
	err    error
	calls  int
}

type failingLIMSClient struct{}

func (failingLIMSClient) FindOrder(context.Context, string) (OrderQueryResult, bool, error) {
	return OrderQueryResult{}, false, errors.New("simulated LIMS failure")
}

func (s *stubKnowledgeAnswerer) Answer(
	_ context.Context,
	_, _ string,
	_ []model.ConversationLog,
) (RAGAnswer, error) {
	s.calls++
	return s.answer, s.err
}
