package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"project-for-yingbai/backend/internal/config"
	"project-for-yingbai/backend/internal/model"
)

var (
	ErrInvalidSearchVector      = errors.New("invalid search vector")
	ErrInvalidSearchPermissions = errors.New("invalid search permissions")
	ErrInvalidSearchLimit       = errors.New("invalid search limit")
)

const documentColumns = `
	id, original_name, stored_name, format, size_bytes, category, document_type,
	permission, status, extraction_status, text_length, chunk_count,
	COALESCE(embedding_model, ''), COALESCE(embedding_dimensions, 0),
	processing_error, uploaded_at, updated_at, COALESCE(content_hash, '')
`

type PostgresDocumentRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresDocumentRepository(pool *pgxpool.Pool) *PostgresDocumentRepository {
	return &PostgresDocumentRepository{pool: pool}
}

func (r *PostgresDocumentRepository) Save(
	ctx context.Context,
	document model.DocumentUpload,
	storedName, contentHash string,
	chunks []model.DocumentChunk,
) error {
	if err := validateDocumentVectors(document, chunks); err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin document transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := insertDocument(ctx, tx, document, storedName, contentHash); err != nil {
		return err
	}
	if err := insertDocumentChunks(ctx, tx, document.ID, document.EmbeddingModel, chunks); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit document transaction: %w", err)
	}
	return nil
}

func (r *PostgresDocumentRepository) SaveFailure(
	ctx context.Context,
	document model.DocumentUpload,
	storedName, contentHash string,
) error {
	if document.Status != "failed" || strings.TrimSpace(document.ProcessingError) == "" {
		return errors.New("failed document must include a processing error")
	}
	if document.UpdatedAt.IsZero() {
		document.UpdatedAt = document.UploadedAt
	}
	_, err := r.pool.Exec(ctx, `
		INSERT INTO documents (
			id, original_name, stored_name, format, size_bytes, category,
			document_type, permission, status, extraction_status,
			text_length, chunk_count, embedding_model, embedding_dimensions,
			processing_error, uploaded_at, updated_at, content_hash
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, NULL, NULL, $13, $14, $15, $16
		)
	`, document.ID, document.OriginalName, storedName, document.Format, document.Size,
		document.Category, document.Type, document.Permission, document.Status,
		document.ExtractionStatus, document.TextLength, document.ChunkCount,
		document.ProcessingError, document.UploadedAt, document.UpdatedAt, contentHash)
	if err != nil {
		return fmt.Errorf("insert failed document metadata: %w", err)
	}
	return nil
}

func (r *PostgresDocumentRepository) ReplaceProcessed(
	ctx context.Context,
	document model.DocumentUpload,
	chunks []model.DocumentChunk,
) error {
	if err := validateDocumentVectors(document, chunks); err != nil {
		return err
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin document replacement transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, "DELETE FROM document_chunks WHERE document_id = $1", document.ID); err != nil {
		return fmt.Errorf("delete previous document chunks: %w", err)
	}
	commandTag, err := tx.Exec(ctx, `
		UPDATE documents
		SET status = $2,
			extraction_status = $3,
			text_length = $4,
			chunk_count = $5,
			embedding_model = $6,
			embedding_dimensions = $7,
			processing_error = '',
			updated_at = $8
		WHERE id = $1
	`, document.ID, document.Status, document.ExtractionStatus, document.TextLength,
		document.ChunkCount, document.EmbeddingModel, document.EmbeddingDimensions, document.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update processed document: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("document not found during replacement")
	}
	if err := insertDocumentChunks(ctx, tx, document.ID, document.EmbeddingModel, chunks); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit document replacement: %w", err)
	}
	return nil
}

func (r *PostgresDocumentRepository) UpdateProcessingError(
	ctx context.Context,
	documentID, status, extractionStatus, processingError string,
	updatedAt time.Time,
) error {
	commandTag, err := r.pool.Exec(ctx, `
		UPDATE documents
		SET status = $2,
			extraction_status = $3,
			processing_error = $4,
			updated_at = $5
		WHERE id = $1
	`, documentID, status, extractionStatus, processingError, updatedAt)
	if err != nil {
		return fmt.Errorf("update document processing error: %w", err)
	}
	if commandTag.RowsAffected() != 1 {
		return errors.New("document not found while recording processing error")
	}
	return nil
}

func (r *PostgresDocumentRepository) FindByID(ctx context.Context, documentID string) (model.DocumentUpload, bool, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+documentColumns+" FROM documents WHERE id = $1", documentID)
	document, err := scanDocument(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DocumentUpload{}, false, nil
	}
	if err != nil {
		return model.DocumentUpload{}, false, fmt.Errorf("query document by id: %w", err)
	}
	return document, true, nil
}

func (r *PostgresDocumentRepository) FindByContentHash(ctx context.Context, contentHash string) (model.DocumentUpload, bool, error) {
	row := r.pool.QueryRow(ctx, "SELECT "+documentColumns+" FROM documents WHERE content_hash = $1", contentHash)
	document, err := scanDocument(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.DocumentUpload{}, false, nil
	}
	if err != nil {
		return model.DocumentUpload{}, false, fmt.Errorf("query document by content hash: %w", err)
	}
	return document, true, nil
}

func (r *PostgresDocumentRepository) Delete(ctx context.Context, documentID string) (bool, error) {
	commandTag, err := r.pool.Exec(ctx, "DELETE FROM documents WHERE id = $1", documentID)
	if err != nil {
		return false, fmt.Errorf("delete document: %w", err)
	}
	return commandTag.RowsAffected() == 1, nil
}

func (r *PostgresDocumentRepository) List(ctx context.Context, filter model.DocumentListFilter) (model.DocumentListResult, error) {
	conditions := make([]string, 0, 5)
	arguments := make([]any, 0, 7)
	addCondition := func(column, value string) {
		if value == "" {
			return
		}
		arguments = append(arguments, value)
		conditions = append(conditions, fmt.Sprintf("%s = $%d", column, len(arguments)))
	}
	addCondition("category", filter.Category)
	addCondition("document_type", filter.Type)
	addCondition("permission", filter.Permission)
	addCondition("status", filter.Status)
	if filter.Query != "" {
		arguments = append(arguments, "%"+filter.Query+"%")
		conditions = append(conditions, fmt.Sprintf("original_name ILIKE $%d", len(arguments)))
	}
	whereClause := ""
	if len(conditions) > 0 {
		whereClause = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int
	if err := r.pool.QueryRow(ctx, "SELECT count(*) FROM documents"+whereClause, arguments...).Scan(&total); err != nil {
		return model.DocumentListResult{}, fmt.Errorf("count documents: %w", err)
	}

	queryArguments := append([]any(nil), arguments...)
	queryArguments = append(queryArguments, filter.PageSize, (filter.Page-1)*filter.PageSize)
	query := "SELECT " + documentColumns + " FROM documents" + whereClause +
		fmt.Sprintf(" ORDER BY uploaded_at DESC, id DESC LIMIT $%d OFFSET $%d", len(queryArguments)-1, len(queryArguments))
	rows, err := r.pool.Query(ctx, query, queryArguments...)
	if err != nil {
		return model.DocumentListResult{}, fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	documents := make([]model.DocumentUpload, 0, filter.PageSize)
	for rows.Next() {
		document, err := scanDocument(rows)
		if err != nil {
			return model.DocumentListResult{}, fmt.Errorf("scan document: %w", err)
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return model.DocumentListResult{}, fmt.Errorf("iterate documents: %w", err)
	}
	totalPages := 0
	if total > 0 {
		totalPages = (total + filter.PageSize - 1) / filter.PageSize
	}
	return model.DocumentListResult{
		Documents:  documents,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (r *PostgresDocumentRepository) SearchSimilar(
	ctx context.Context,
	queryVector []float32,
	embeddingModel string,
	permissions []string,
	limit int,
) ([]model.RetrievedChunk, error) {
	if len(queryVector) != model.DocumentEmbeddingDimensions {
		return nil, fmt.Errorf("%w: got %d dimensions, expected %d", ErrInvalidSearchVector, len(queryVector), model.DocumentEmbeddingDimensions)
	}
	embeddingModel = strings.TrimSpace(embeddingModel)
	if embeddingModel == "" {
		return nil, fmt.Errorf("%w: embedding model is empty", ErrInvalidSearchVector)
	}
	if len(permissions) == 0 {
		return nil, ErrInvalidSearchPermissions
	}
	if limit < 1 || limit > config.MaxRetrievalTopK {
		return nil, fmt.Errorf("%w: got %d", ErrInvalidSearchLimit, limit)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.original_name, d.category, d.document_type, d.permission,
			c.chunk_index, c.content, c.char_count,
			1 - (c.embedding <=> $1) AS similarity
		FROM document_chunks c
		JOIN documents d ON d.id = c.document_id
		WHERE d.status = 'vectorized'
			AND c.embedding IS NOT NULL
			AND c.embedding_model = $2
			AND d.permission = ANY($3::text[])
		ORDER BY c.embedding <=> $1, d.id, c.chunk_index
		LIMIT $4
	`, pgvector.NewVector(queryVector), embeddingModel, permissions, limit)
	if err != nil {
		return nil, fmt.Errorf("search similar document chunks: %w", err)
	}
	defer rows.Close()

	results := make([]model.RetrievedChunk, 0, limit)
	for rows.Next() {
		var result model.RetrievedChunk
		if err := rows.Scan(
			&result.DocumentID, &result.OriginalName, &result.Category, &result.Type,
			&result.Permission, &result.ChunkIndex, &result.Content,
			&result.CharCount, &result.Similarity,
		); err != nil {
			return nil, fmt.Errorf("scan similar document chunk: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate similar document chunks: %w", err)
	}
	return results, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDocument(row rowScanner) (model.DocumentUpload, error) {
	var document model.DocumentUpload
	err := row.Scan(
		&document.ID, &document.OriginalName, &document.StoredName, &document.Format,
		&document.Size, &document.Category, &document.Type, &document.Permission,
		&document.Status, &document.ExtractionStatus, &document.TextLength,
		&document.ChunkCount, &document.EmbeddingModel, &document.EmbeddingDimensions,
		&document.ProcessingError, &document.UploadedAt, &document.UpdatedAt,
		&document.ContentHash,
	)
	return document, err
}

func insertDocument(
	ctx context.Context,
	tx pgx.Tx,
	document model.DocumentUpload,
	storedName, contentHash string,
) error {
	if document.UpdatedAt.IsZero() {
		document.UpdatedAt = document.UploadedAt
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO documents (
			id, original_name, stored_name, format, size_bytes, category,
			document_type, permission, status, extraction_status,
			text_length, chunk_count, embedding_model, embedding_dimensions,
			processing_error, uploaded_at, updated_at, content_hash
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18
		)
	`, document.ID, document.OriginalName, storedName, document.Format, document.Size,
		document.Category, document.Type, document.Permission, document.Status,
		document.ExtractionStatus, document.TextLength, document.ChunkCount,
		document.EmbeddingModel, document.EmbeddingDimensions, document.ProcessingError,
		document.UploadedAt, document.UpdatedAt, contentHash)
	if err != nil {
		return fmt.Errorf("insert document metadata: %w", err)
	}
	return nil
}

func insertDocumentChunks(
	ctx context.Context,
	tx pgx.Tx,
	documentID, embeddingModel string,
	chunks []model.DocumentChunk,
) error {
	for _, chunk := range chunks {
		_, err := tx.Exec(ctx, `
			INSERT INTO document_chunks (
				document_id, chunk_index, content, char_count, boundary,
				embedding, embedding_model
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, documentID, chunk.Index, chunk.Content, chunk.CharCount, chunk.Boundary,
			pgvector.NewVector(chunk.Embedding), embeddingModel)
		if err != nil {
			return fmt.Errorf("insert document chunk %d: %w", chunk.Index, err)
		}
	}
	return nil
}

func validateDocumentVectors(document model.DocumentUpload, chunks []model.DocumentChunk) error {
	if document.EmbeddingModel == "" || document.EmbeddingDimensions != model.DocumentEmbeddingDimensions {
		return fmt.Errorf("invalid document embedding metadata: model=%q dimensions=%d", document.EmbeddingModel, document.EmbeddingDimensions)
	}
	for _, chunk := range chunks {
		if len(chunk.Embedding) != model.DocumentEmbeddingDimensions {
			return fmt.Errorf("chunk %d embedding has %d dimensions, expected %d", chunk.Index, len(chunk.Embedding), model.DocumentEmbeddingDimensions)
		}
	}
	return nil
}
