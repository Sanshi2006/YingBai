package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"project-for-yingbai/backend/internal/model"
)

type PostgresDocumentRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresDocumentRepository(pool *pgxpool.Pool) *PostgresDocumentRepository {
	return &PostgresDocumentRepository{pool: pool}
}

func (r *PostgresDocumentRepository) Save(ctx context.Context, document model.DocumentUpload, storedName string, chunks []model.DocumentChunk) error {
	if document.EmbeddingModel == "" || document.EmbeddingDimensions != model.DocumentEmbeddingDimensions {
		return fmt.Errorf("invalid document embedding metadata: model=%q dimensions=%d", document.EmbeddingModel, document.EmbeddingDimensions)
	}
	for _, chunk := range chunks {
		if len(chunk.Embedding) != model.DocumentEmbeddingDimensions {
			return fmt.Errorf("chunk %d embedding has %d dimensions, expected %d", chunk.Index, len(chunk.Embedding), model.DocumentEmbeddingDimensions)
		}
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin document transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx, `
		INSERT INTO documents (
			id, original_name, stored_name, format, size_bytes, category,
			document_type, permission, status, extraction_status,
			text_length, chunk_count, embedding_model, embedding_dimensions, uploaded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`, document.ID, document.OriginalName, storedName, document.Format, document.Size,
		document.Category, document.Type, document.Permission, document.Status,
		document.ExtractionStatus, document.TextLength, document.ChunkCount,
		document.EmbeddingModel, document.EmbeddingDimensions, document.UploadedAt)
	if err != nil {
		return fmt.Errorf("insert document metadata: %w", err)
	}

	for _, chunk := range chunks {
		_, err = tx.Exec(ctx, `
			INSERT INTO document_chunks (
				document_id, chunk_index, content, char_count, boundary,
				embedding, embedding_model
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, document.ID, chunk.Index, chunk.Content, chunk.CharCount, chunk.Boundary,
			pgvector.NewVector(chunk.Embedding), document.EmbeddingModel)
		if err != nil {
			return fmt.Errorf("insert document chunk %d: %w", chunk.Index, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit document transaction: %w", err)
	}
	return nil
}

func (r *PostgresDocumentRepository) List(ctx context.Context) ([]model.DocumentUpload, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, original_name, format, size_bytes, category, document_type,
			permission, status, extraction_status, text_length, chunk_count,
			COALESCE(embedding_model, ''), COALESCE(embedding_dimensions, 0), uploaded_at
		FROM documents
		ORDER BY uploaded_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	documents := make([]model.DocumentUpload, 0)
	for rows.Next() {
		var document model.DocumentUpload
		if err := rows.Scan(
			&document.ID, &document.OriginalName, &document.Format, &document.Size,
			&document.Category, &document.Type, &document.Permission, &document.Status,
			&document.ExtractionStatus, &document.TextLength, &document.ChunkCount,
			&document.EmbeddingModel, &document.EmbeddingDimensions, &document.UploadedAt,
		); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}
	return documents, nil
}
