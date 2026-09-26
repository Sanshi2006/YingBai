package router

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/handler"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/service"
)

type KnowledgeRepository interface {
	service.DocumentRepository
	service.VectorSearchRepository
}

func New(
	mobileDir, uploadDir string,
	documentRepository KnowledgeRepository,
	conversationLogger handler.ConversationLogger,
	embeddingProvider llm.LLMProvider,
	chatProvider llm.ChatLLMProvider,
	retrievalConfiguration config.RetrievalConfig,
) *gin.Engine {
	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		panic("configure trusted proxies: " + err.Error())
	}
	engine.Use(gin.Logger(), gin.Recovery(), securityHeaders())

	engine.GET("/health", handler.Health)
	retrievalService, err := service.NewRetrievalService(documentRepository, embeddingProvider, retrievalConfiguration)
	if err != nil {
		panic("configure retrieval service: " + err.Error())
	}
	preparationService, err := service.NewRAGPreparationService(chatProvider, retrievalService, retrievalConfiguration)
	if err != nil {
		panic("configure RAG preparation service: " + err.Error())
	}
	answerService, err := service.NewRAGAnswerService(preparationService, chatProvider)
	if err != nil {
		panic("configure RAG answer service: " + err.Error())
	}
	chatService, err := service.NewChatService(answerService, service.NewMockLIMSClient())
	if err != nil {
		panic("configure chat service: " + err.Error())
	}
	chatHandler := handler.NewChatHandler(chatService, conversationLogger)
	engine.POST("/chat", chatHandler.Chat)
	documentHandler := handler.NewDocumentHandler(service.NewDocumentService(uploadDir, documentRepository, embeddingProvider))
	engine.POST("/api/v1/documents", documentHandler.Upload)
	engine.GET("/api/v1/documents", documentHandler.List)
	engine.DELETE("/api/v1/documents/:id", documentHandler.Delete)
	engine.POST("/api/v1/documents/:id/retry", documentHandler.Retry)
	engine.POST("/api/v1/documents/:id/revectorize", documentHandler.Revectorize)

	if mobileDir != "" {
		engine.Static("/assets", filepath.Join(mobileDir, "assets"))
		engine.GET("/", func(c *gin.Context) {
			c.File(filepath.Join(mobileDir, "index.html"))
		})
		engine.GET("/documents", func(c *gin.Context) {
			c.File(filepath.Join(mobileDir, "documents.html"))
		})
		engine.GET("/favicon.ico", func(c *gin.Context) {
			c.Status(http.StatusNoContent)
		})
	}

	return engine
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "no-referrer")
		c.Header("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'")
		c.Next()
	}
}
