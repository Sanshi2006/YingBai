package router

import (
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"project-for-yingbai/backend/internal/handler"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/service"
)

func New(mobileDir, uploadDir string, documentRepository service.DocumentRepository, embeddingProvider llm.LLMProvider) *gin.Engine {
	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		panic("configure trusted proxies: " + err.Error())
	}
	engine.Use(gin.Logger(), gin.Recovery(), securityHeaders())

	engine.GET("/health", handler.Health)
	engine.POST("/chat", handler.Chat)
	documentHandler := handler.NewDocumentHandler(service.NewDocumentService(uploadDir, documentRepository, embeddingProvider))
	engine.POST("/api/v1/documents", documentHandler.Upload)
	engine.GET("/api/v1/documents", documentHandler.List)

	if mobileDir != "" {
		engine.Static("/assets", filepath.Join(mobileDir, "assets"))
		engine.GET("/", func(c *gin.Context) {
			c.File(filepath.Join(mobileDir, "index.html"))
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
