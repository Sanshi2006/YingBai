package repository_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"project-for-yingbai/backend/internal/database"
	"project-for-yingbai/backend/internal/model"
	"project-for-yingbai/backend/internal/repository"
)

func TestPostgresDocumentRepositoryPersistsMetadataAndChunksAtomically(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer pool.Close()

	repository := repository.NewPostgresDocumentRepository(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	documentID := fmt.Sprintf("itest%d", now.UnixNano())
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM documents WHERE id = $1", documentID)
	}()

	document := model.DocumentUpload{
		ID:                  documentID,
		OriginalName:        "integration.md",
		Format:              "md",
		Size:                42,
		Category:            "纺织",
		Type:                "标准",
		Permission:          "内部",
		Status:              "vectorized",
		ExtractionStatus:    "completed",
		TextLength:          18,
		ChunkCount:          2,
		EmbeddingModel:      "fake-embedding",
		EmbeddingDimensions: model.DocumentEmbeddingDimensions,
		UploadedAt:          now,
	}
	chunks := []model.DocumentChunk{
		{Index: 0, Content: "# 标题\n\n第一段。", CharCount: 10, Boundary: "semantic", Embedding: testVector(0.1)},
		{Index: 1, Content: "第二段。", CharCount: 4, Boundary: "semantic", Embedding: testVector(0.2)},
	}
	if err := repository.Save(ctx, document, documentID+".md", chunks); err != nil {
		t.Fatalf("save document: %v", err)
	}

	var metadataCount, chunkCount, vectorDimensions int
	var allModelsMatch bool
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM documents WHERE id = $1", documentID).Scan(&metadataCount); err != nil {
		t.Fatalf("query document metadata: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM document_chunks WHERE document_id = $1", documentID).Scan(&chunkCount); err != nil {
		t.Fatalf("query document chunks: %v", err)
	}
	if metadataCount != 1 || chunkCount != 2 {
		t.Fatalf("stored metadata=%d chunks=%d, want metadata=1 chunks=2", metadataCount, chunkCount)
	}
	if err := pool.QueryRow(ctx, `
		SELECT min(vector_dims(embedding)), bool_and(embedding_model = $2)
		FROM document_chunks WHERE document_id = $1
	`, documentID, document.EmbeddingModel).Scan(&vectorDimensions, &allModelsMatch); err != nil {
		t.Fatalf("query stored vectors: %v", err)
	}
	if vectorDimensions != model.DocumentEmbeddingDimensions || !allModelsMatch {
		t.Fatalf("stored vector dimensions=%d models_match=%v", vectorDimensions, allModelsMatch)
	}

	rollbackID := fmt.Sprintf("rollback%d", now.UnixNano())
	rollbackDocument := document
	rollbackDocument.ID = rollbackID
	duplicateIndexes := []model.DocumentChunk{
		{Index: 0, Content: "第一块", CharCount: 3, Boundary: "semantic", Embedding: testVector(0.3)},
		{Index: 0, Content: "重复索引", CharCount: 4, Boundary: "semantic", Embedding: testVector(0.4)},
	}
	if err := repository.Save(ctx, rollbackDocument, rollbackID+".md", duplicateIndexes); err == nil {
		t.Fatal("expected duplicate chunk indexes to fail")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM documents WHERE id = $1", rollbackID).Scan(&metadataCount); err != nil {
		t.Fatalf("query rolled back document: %v", err)
	}
	if metadataCount != 0 {
		t.Fatalf("transaction was not rolled back, found %d metadata rows", metadataCount)
	}
}

func testVector(value float32) []float32 {
	vector := make([]float32, model.DocumentEmbeddingDimensions)
	for index := range vector {
		vector[index] = value
	}
	return vector
}
