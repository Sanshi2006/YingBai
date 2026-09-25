package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"time"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/database"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/repository"
	"project-for-yingbai/backend/internal/router"
)

const defaultPort = "8080"
const defaultDatabaseURL = "postgres://yingbai:yingbai_dev_password@localhost:5432/yingbai?sslmode=disable"

var defaultSQLiteDatabasePath = filepath.Join("..", "data", "runtime", "conversations.db")

func main() {
	if _, err := config.LoadDotEnv(); err != nil {
		log.Fatalf("environment configuration failed: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = defaultPort
	}

	mobileDir := os.Getenv("MOBILE_DIR")
	if mobileDir == "" {
		mobileDir = filepath.Join("..", "mobile")
	}

	uploadDir := os.Getenv("UPLOAD_DIR")
	if uploadDir == "" {
		uploadDir = filepath.Join("..", "data", "uploads")
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = defaultDatabaseURL
	}
	databaseContext, cancelDatabase := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelDatabase()
	pool, err := database.Open(databaseContext, databaseURL)
	if err != nil {
		log.Fatalf("database initialization failed: %v", err)
	}
	defer pool.Close()

	documentRepository := repository.NewPostgresDocumentRepository(pool)
	sqliteDatabasePath := os.Getenv("SQLITE_DATABASE_PATH")
	if sqliteDatabasePath == "" {
		sqliteDatabasePath = defaultSQLiteDatabasePath
	}
	sqliteContext, cancelSQLite := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelSQLite()
	conversationDatabase, err := database.OpenSQLite(sqliteContext, sqliteDatabasePath)
	if err != nil {
		log.Fatalf("conversation database initialization failed: %v", err)
	}
	defer conversationDatabase.Close()
	conversationRepository := repository.NewSQLiteConversationLogRepository(conversationDatabase)

	embeddingProvider, err := llm.NewOpenAICompatibleProviderFromEnv(nil)
	if err != nil {
		log.Fatalf("embedding provider initialization failed: %v", err)
	}
	chatProvider, err := llm.NewOpenAICompatibleChatProviderFromEnv(nil)
	if err != nil {
		log.Fatalf("chat provider initialization failed: %v", err)
	}
	retrievalConfiguration, err := config.LoadRetrievalConfigFromEnv()
	if err != nil {
		log.Fatalf("retrieval configuration failed: %v", err)
	}
	engine := router.New(mobileDir, uploadDir, documentRepository, conversationRepository, embeddingProvider, chatProvider, retrievalConfiguration)
	address := "0.0.0.0:" + port
	log.Printf("backend listening on http://%s", address)

	if err := engine.Run(address); err != nil {
		log.Fatalf("backend stopped: %v", err)
	}
}
