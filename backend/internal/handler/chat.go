package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"project-for-yingbai/backend/internal/model"
	"project-for-yingbai/backend/internal/service"
)

type chatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
}

type chatResponse struct {
	Answer    string                    `json:"answer"`
	Type      string                    `json:"type"`
	SessionID string                    `json:"sessionId,omitempty"`
	Role      string                    `json:"role,omitempty"`
	Citations []service.RAGCitation     `json:"citations"`
	Order     *service.OrderQueryResult `json:"order,omitempty"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type ChatAnswerer interface {
	Answer(ctx context.Context, question, role string, history []model.ConversationLog) (service.ChatAnswer, error)
}

type ConversationLogger interface {
	Log(ctx context.Context, entry model.ConversationLog) error
	ListRecentBySessionAndRole(ctx context.Context, sessionID, role string, limit int) ([]model.ConversationLog, error)
}

type ChatHandler struct {
	answerer ChatAnswerer
	logger   ConversationLogger
}

func NewChatHandler(answerer ChatAnswerer, logger ConversationLogger) *ChatHandler {
	if answerer == nil {
		panic("RAG answerer is not configured")
	}
	if logger == nil {
		panic("conversation logger is not configured")
	}
	return &ChatHandler{answerer: answerer, logger: logger}
}

func (h *ChatHandler) Chat(c *gin.Context) {
	var request chatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeInvalidRequest(c)
		return
	}

	request.Message = strings.TrimSpace(request.Message)
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.Role = strings.TrimSpace(request.Role)
	if request.Message == "" || request.SessionID == "" {
		writeInvalidRequest(c)
		return
	}
	history, err := h.logger.ListRecentBySessionAndRole(
		c.Request.Context(), request.SessionID, request.Role, service.MaxConversationHistoryTurns,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: errorBody{
			Code:    "CHAT_HISTORY_FAILED",
			Message: "会话历史读取失败，请稍后重试",
		}})
		return
	}
	answer, err := h.answerer.Answer(c.Request.Context(), request.Message, request.Role, history)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrRAGQuestionRequired), errors.Is(err, service.ErrRAGQuestionTooLong):
			writeInvalidRequest(c)
		case errors.Is(err, service.ErrInvalidRetrievalRole):
			c.JSON(http.StatusBadRequest, errorResponse{Error: errorBody{Code: "INVALID_ROLE", Message: "role 必须是 customer、service 或 admin"}})
		case errors.Is(err, service.ErrLIMSQueryFailed):
			c.JSON(http.StatusBadGateway, errorResponse{Error: errorBody{Code: "LIMS_QUERY_FAILED", Message: "订单查询失败，请稍后重试"}})
		default:
			c.JSON(http.StatusBadGateway, errorResponse{Error: errorBody{Code: "RAG_PROCESSING_FAILED", Message: "知识问答处理失败，请稍后重试"}})
		}
		return
	}
	citationDocuments := make([]string, 0, len(answer.Citations))
	seenCitations := make(map[string]struct{}, len(answer.Citations))
	for _, citation := range answer.Citations {
		name := strings.TrimSpace(citation.OriginalName)
		if name == "" {
			continue
		}
		if _, exists := seenCitations[name]; exists {
			continue
		}
		seenCitations[name] = struct{}{}
		citationDocuments = append(citationDocuments, name)
	}
	if err := h.logger.Log(c.Request.Context(), model.ConversationLog{
		SessionID:         request.SessionID,
		Role:              request.Role,
		Question:          request.Message,
		Answer:            answer.Answer,
		AnswerType:        answer.Type,
		CitationDocuments: citationDocuments,
		CreatedAt:         time.Now().UTC(),
	}); err != nil {
		c.JSON(http.StatusInternalServerError, errorResponse{Error: errorBody{
			Code:    "CHAT_LOG_FAILED",
			Message: "对话日志保存失败，请稍后重试",
		}})
		return
	}

	c.JSON(http.StatusOK, chatResponse{
		Answer:    answer.Answer,
		Type:      answer.Type,
		SessionID: request.SessionID,
		Role:      request.Role,
		Citations: answer.Citations,
		Order:     answer.Order,
	})
}

func writeInvalidRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, errorResponse{
		Error: errorBody{
			Code:    "INVALID_REQUEST",
			Message: "message 和 sessionId 不能为空，message 不能超过 2000 个字符",
		},
	})
}
