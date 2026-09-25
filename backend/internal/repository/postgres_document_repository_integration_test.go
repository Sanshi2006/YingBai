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
	if err := repository.Save(ctx, document, documentID+".md", documentID+"-hash", chunks); err != nil {
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
	if err := repository.Save(ctx, rollbackDocument, rollbackID+".md", rollbackID+"-hash", duplicateIndexes); err == nil {
		t.Fatal("expected duplicate chunk indexes to fail")
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM documents WHERE id = $1", rollbackID).Scan(&metadataCount); err != nil {
		t.Fatalf("query rolled back document: %v", err)
	}
	if metadataCount != 0 {
		t.Fatalf("transaction was not rolled back, found %d metadata rows", metadataCount)
	}
}

func TestPostgresDocumentRepositorySearchesTopKWithPermissionFilter(t *testing.T) {
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
	publicID := fmt.Sprintf("sp%d", now.UnixNano())
	internalID := fmt.Sprintf("si%d", now.UnixNano())
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM documents WHERE id = ANY($1::text[])", []string{publicID, internalID})
	}()

	baseDocument := model.DocumentUpload{
		Format:              "md",
		Size:                32,
		Category:            "纺织",
		Type:                "FAQ",
		Status:              "vectorized",
		ExtractionStatus:    "completed",
		EmbeddingModel:      "fake-embedding",
		EmbeddingDimensions: model.DocumentEmbeddingDimensions,
		UploadedAt:          now,
	}
	publicDocument := baseDocument
	publicDocument.ID = publicID
	publicDocument.OriginalName = "public.md"
	publicDocument.Permission = "公开"
	publicDocument.TextLength = 12
	publicDocument.ChunkCount = 2
	if err := repository.Save(ctx, publicDocument, publicID+".md", publicID+"-hash", []model.DocumentChunk{
		{Index: 0, Content: "公开的相似知识", CharCount: 7, Boundary: "semantic", Embedding: directionVector(0.8, 0.6)},
		{Index: 1, Content: "公开的较远知识", CharCount: 7, Boundary: "semantic", Embedding: directionVector(0, 1)},
	}); err != nil {
		t.Fatalf("save public document: %v", err)
	}

	internalDocument := baseDocument
	internalDocument.ID = internalID
	internalDocument.OriginalName = "internal.md"
	internalDocument.Permission = "内部"
	internalDocument.TextLength = 8
	internalDocument.ChunkCount = 1
	if err := repository.Save(ctx, internalDocument, internalID+".md", internalID+"-hash", []model.DocumentChunk{
		{Index: 0, Content: "内部最相似知识", CharCount: 7, Boundary: "semantic", Embedding: directionVector(1, 0)},
	}); err != nil {
		t.Fatalf("save internal document: %v", err)
	}

	queryVector := directionVector(1, 0)
	publicResults, err := repository.SearchSimilar(ctx, queryVector, "fake-embedding", []string{"公开"}, 5)
	if err != nil {
		t.Fatalf("search public chunks: %v", err)
	}
	if len(publicResults) != 2 {
		t.Fatalf("public result count = %d, want 2", len(publicResults))
	}
	for _, result := range publicResults {
		if result.Permission != "公开" || result.DocumentID != publicID {
			t.Fatalf("public search leaked unauthorized result: %#v", result)
		}
	}
	if publicResults[0].ChunkIndex != 0 || publicResults[0].Similarity <= publicResults[1].Similarity {
		t.Fatalf("public results are not ordered by cosine similarity: %#v", publicResults)
	}
	modelMismatchResults, err := repository.SearchSimilar(ctx, queryVector, "different-model", []string{"公开", "内部"}, 5)
	if err != nil {
		t.Fatalf("search with mismatched model: %v", err)
	}
	if len(modelMismatchResults) != 0 {
		t.Fatalf("model-mismatched vectors were returned: %#v", modelMismatchResults)
	}

	privilegedResults, err := repository.SearchSimilar(ctx, queryVector, "fake-embedding", []string{"公开", "内部"}, 2)
	if err != nil {
		t.Fatalf("search privileged chunks: %v", err)
	}
	if len(privilegedResults) != 2 {
		t.Fatalf("privileged result count = %d, want Top-K limit 2", len(privilegedResults))
	}
	if privilegedResults[0].DocumentID != internalID || privilegedResults[0].Similarity < 0.999999 {
		t.Fatalf("nearest internal result was not ranked first: %#v", privilegedResults)
	}
	if privilegedResults[1].DocumentID != publicID {
		t.Fatalf("second result = %#v, want public document", privilegedResults[1])
	}

	if _, err := pool.Exec(ctx, "UPDATE documents SET status = 'failed' WHERE id = $1", internalID); err != nil {
		t.Fatalf("mark internal document non-vectorized: %v", err)
	}
	statusFilteredResults, err := repository.SearchSimilar(ctx, queryVector, "fake-embedding", []string{"公开", "内部"}, 5)
	if err != nil {
		t.Fatalf("search after status change: %v", err)
	}
	for _, result := range statusFilteredResults {
		if result.DocumentID == internalID {
			t.Fatalf("non-vectorized document was returned: %#v", result)
		}
	}
}

func TestPostgresDocumentRepositoryManagesFailureRetryFilteringAndDeletion(t *testing.T) {
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
	documentID := fmt.Sprintf("manage%d", now.UnixNano())
	duplicateID := fmt.Sprintf("managedup%d", now.UnixNano())
	contentHash := fmt.Sprintf("%064x", now.UnixNano())
	defer func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupContext, "DELETE FROM documents WHERE id = ANY($1::text[])", []string{documentID, duplicateID})
	}()

	failed := model.DocumentUpload{
		ID: documentID, OriginalName: "failed.md", Format: "md", Size: 25,
		Category: "杂货", Type: "FAQ", Permission: "内部",
		Status: "failed", ExtractionStatus: "failed",
		ProcessingError: "EMBEDDING_FAILED: 文档向量化失败",
		UploadedAt:      now, UpdatedAt: now,
	}
	if err := repository.SaveFailure(ctx, failed, documentID+".md", contentHash); err != nil {
		t.Fatalf("save failed document: %v", err)
	}
	found, exists, err := repository.FindByContentHash(ctx, contentHash)
	if err != nil || !exists || found.ID != documentID || found.ProcessingError == "" {
		t.Fatalf("failed document was not queryable by hash: found=%#v exists=%v err=%v", found, exists, err)
	}
	result, err := repository.List(ctx, model.DocumentListFilter{
		Page: 1, PageSize: 10, Category: "杂货", Permission: "内部", Status: "failed", Query: "failed",
	})
	if err != nil {
		t.Fatalf("filter failed document: %v", err)
	}
	if result.Total != 1 || len(result.Documents) != 1 || result.Documents[0].ID != documentID {
		t.Fatalf("unexpected filtered failed documents: %#v", result)
	}

	duplicate := failed
	duplicate.ID = duplicateID
	duplicate.OriginalName = "duplicate.md"
	if err := repository.SaveFailure(ctx, duplicate, duplicateID+".md", contentHash); err == nil {
		t.Fatal("expected duplicate content hash to violate uniqueness")
	}

	processed := failed
	processed.Status = "vectorized"
	processed.ExtractionStatus = "completed"
	processed.TextLength = 6
	processed.ChunkCount = 1
	processed.EmbeddingModel = "fake-embedding"
	processed.EmbeddingDimensions = model.DocumentEmbeddingDimensions
	processed.ProcessingError = ""
	processed.UpdatedAt = now.Add(time.Minute)
	chunks := []model.DocumentChunk{{
		Index: 0, Content: "重试成功。", CharCount: 5, Boundary: "semantic", Embedding: testVector(0.5),
	}}
	if err := repository.ReplaceProcessed(ctx, processed, chunks); err != nil {
		t.Fatalf("replace failed document with processed chunks: %v", err)
	}
	retried, exists, err := repository.FindByID(ctx, documentID)
	if err != nil || !exists || retried.Status != "vectorized" || retried.ProcessingError != "" || retried.ChunkCount != 1 {
		t.Fatalf("unexpected retried document: %#v exists=%v err=%v", retried, exists, err)
	}

	deleted, err := repository.Delete(ctx, documentID)
	if err != nil || !deleted {
		t.Fatalf("delete managed document: deleted=%v err=%v", deleted, err)
	}
	var remainingChunks int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM document_chunks WHERE document_id = $1", documentID).Scan(&remainingChunks); err != nil {
		t.Fatalf("count chunks after delete: %v", err)
	}
	if remainingChunks != 0 {
		t.Fatalf("expected cascading chunk delete, found %d", remainingChunks)
	}
}

func testVector(value float32) []float32 {
	vector := make([]float32, model.DocumentEmbeddingDimensions)
	for index := range vector {
		vector[index] = value
	}
	return vector
}

func directionVector(first, second float32) []float32 {
	vector := make([]float32, model.DocumentEmbeddingDimensions)
	vector[0] = first
	vector[1] = second
	return vector
}
