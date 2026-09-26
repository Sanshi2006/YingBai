package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"project-for-yingbai/backend/internal/model"
)

const (
	AnswerTypeOrder = "order"
	MockOrderNumber = "ORD2026001"
)

var orderNumberPattern = regexp.MustCompile(`(?i)\bORD[0-9]{4,20}\b`)

var ErrLIMSQueryFailed = errors.New("LIMS query failed")

type OrderQueryResult struct {
	Found        bool   `json:"found"`
	Mock         bool   `json:"mock"`
	OrderNumber  string `json:"orderNumber,omitempty"`
	Status       string `json:"status,omitempty"`
	Progress     int    `json:"progress,omitempty"`
	ReportStatus string `json:"reportStatus,omitempty"`
}

type ChatAnswer struct {
	Answer    string            `json:"answer"`
	Type      string            `json:"type"`
	Citations []RAGCitation     `json:"citations"`
	Order     *OrderQueryResult `json:"order,omitempty"`
}

type KnowledgeAnswerer interface {
	Answer(ctx context.Context, question, role string, history []model.ConversationLog) (RAGAnswer, error)
}

type LIMSClient interface {
	FindOrder(ctx context.Context, orderNumber string) (OrderQueryResult, bool, error)
}

type ChatService struct {
	knowledge KnowledgeAnswerer
	lims      LIMSClient
}

func NewChatService(knowledge KnowledgeAnswerer, lims LIMSClient) (*ChatService, error) {
	if knowledge == nil {
		return nil, errors.New("knowledge answerer is not configured")
	}
	if lims == nil {
		return nil, errors.New("LIMS client is not configured")
	}
	return &ChatService{knowledge: knowledge, lims: lims}, nil
}

func (s *ChatService) Answer(
	ctx context.Context,
	question, role string,
	history []model.ConversationLog,
) (ChatAnswer, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return ChatAnswer{}, ErrRAGQuestionRequired
	}
	if utf8.RuneCountInString(question) > maxRAGQuestionCharacters {
		return ChatAnswer{}, ErrRAGQuestionTooLong
	}
	if _, err := retrievalPermissions(role); err != nil {
		return ChatAnswer{}, err
	}
	orderNumber, isOrderQuery := recognizeOrderQuery(question)
	if isOrderQuery {
		return s.answerOrderQuery(ctx, orderNumber)
	}

	answer, err := s.knowledge.Answer(ctx, question, role, history)
	if err != nil {
		return ChatAnswer{}, err
	}
	return ChatAnswer{
		Answer: answer.Answer, Type: answer.Type, Citations: answer.Citations,
	}, nil
}

func (s *ChatService) answerOrderQuery(ctx context.Context, orderNumber string) (ChatAnswer, error) {
	if orderNumber == "" {
		return ChatAnswer{
			Answer: "请提供需要查询的订单号，例如 ORD2026001。",
			Type:   AnswerTypeOrder, Citations: make([]RAGCitation, 0),
		}, nil
	}
	order, found, err := s.lims.FindOrder(ctx, orderNumber)
	if err != nil {
		return ChatAnswer{}, fmt.Errorf("%w: %v", ErrLIMSQueryFailed, err)
	}
	if !found {
		return ChatAnswer{
			Answer: fmt.Sprintf("未查询到订单 %s，请核对订单号后重试。", orderNumber),
			Type:   AnswerTypeOrder, Citations: make([]RAGCitation, 0),
			Order: &OrderQueryResult{Found: false, Mock: true, OrderNumber: orderNumber},
		}, nil
	}
	return ChatAnswer{
		Answer: fmt.Sprintf("已查询到订单 %s 的 Mock 进度信息。", order.OrderNumber),
		Type:   AnswerTypeOrder, Citations: make([]RAGCitation, 0), Order: &order,
	}, nil
}

func recognizeOrderQuery(question string) (string, bool) {
	question = strings.TrimSpace(question)
	match := orderNumberPattern.FindString(question)
	if match != "" {
		return strings.ToUpper(match), true
	}
	if strings.Contains(question, "订单") && containsAny(question, "查", "查询", "进度", "状态", "报告") {
		return "", true
	}
	return "", false
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

type MockLIMSClient struct {
	orders map[string]OrderQueryResult
}

func NewMockLIMSClient() *MockLIMSClient {
	return &MockLIMSClient{orders: map[string]OrderQueryResult{
		MockOrderNumber: {
			Found: true, Mock: true, OrderNumber: MockOrderNumber,
			Status: "检测中", Progress: 68, ReportStatus: "未生成",
		},
	}}
}

func (c *MockLIMSClient) FindOrder(ctx context.Context, orderNumber string) (OrderQueryResult, bool, error) {
	if err := ctx.Err(); err != nil {
		return OrderQueryResult{}, false, err
	}
	order, found := c.orders[strings.ToUpper(strings.TrimSpace(orderNumber))]
	return order, found, nil
}

var _ LIMSClient = (*MockLIMSClient)(nil)
