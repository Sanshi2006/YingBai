package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const connectedMessage = "底座已连通"

type chatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"sessionId"`
	Role      string `json:"role"`
}

type chatResponse struct {
	Answer    string        `json:"answer"`
	Type      string        `json:"type"`
	SessionID string        `json:"sessionId,omitempty"`
	Role      string        `json:"role,omitempty"`
	Citations []interface{} `json:"citations"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func Chat(c *gin.Context) {
	var request chatRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeInvalidRequest(c)
		return
	}

	request.Message = strings.TrimSpace(request.Message)
	if request.Message == "" {
		writeInvalidRequest(c)
		return
	}

	c.JSON(http.StatusOK, chatResponse{
		Answer:    connectedMessage,
		Type:      "fixed",
		SessionID: strings.TrimSpace(request.SessionID),
		Role:      strings.TrimSpace(request.Role),
		Citations: make([]interface{}, 0),
	})
}

func writeInvalidRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, errorResponse{
		Error: errorBody{
			Code:    "INVALID_REQUEST",
			Message: "message 不能为空",
		},
	})
}
