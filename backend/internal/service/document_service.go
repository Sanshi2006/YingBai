package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"project-for-yingbai/backend/internal/chunker"
	"project-for-yingbai/backend/internal/extractor"
	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/model"
)

const (
	MaxDocumentSize         int64 = 20 << 20
	DefaultDocumentPage           = 1
	DefaultDocumentPageSize       = 20
	MaxDocumentPageSize           = 100
)

var (
	ErrDocumentRequired        = errors.New("document is required")
	ErrDocumentTooLarge        = errors.New("document is too large")
	ErrUnsupportedDocumentType = errors.New("unsupported document type")
	ErrInvalidCategory         = errors.New("invalid document category")
	ErrInvalidType             = errors.New("invalid document type")
	ErrInvalidPermission       = errors.New("invalid document permission")
	ErrInvalidDocumentFilter   = errors.New("invalid document list filter")
	ErrTextExtractionFailed    = errors.New("text extraction failed")
	ErrExtractedTextEmpty      = errors.New("extracted text is empty")
	ErrExtractedTextTooLarge   = errors.New("extracted text is too large")
	ErrTextChunkingFailed      = errors.New("text chunking failed")
	ErrEmbeddingFailed         = errors.New("document embedding failed")
	ErrDocumentStorageFailed   = errors.New("document storage failed")
	ErrDuplicateDocument       = errors.New("duplicate document content")
	ErrDocumentNotFound        = errors.New("document not found")
	ErrDocumentNotRetryable    = errors.New("document is not retryable")
)

type DocumentRepository interface {
	Save(ctx context.Context, document model.DocumentUpload, storedName, contentHash string, chunks []model.DocumentChunk) error
	SaveFailure(ctx context.Context, document model.DocumentUpload, storedName, contentHash string) error
	ReplaceProcessed(ctx context.Context, document model.DocumentUpload, chunks []model.DocumentChunk) error
	UpdateProcessingError(ctx context.Context, documentID, status, extractionStatus, processingError string, updatedAt time.Time) error
	FindByID(ctx context.Context, documentID string) (model.DocumentUpload, bool, error)
	FindByContentHash(ctx context.Context, contentHash string) (model.DocumentUpload, bool, error)
	Delete(ctx context.Context, documentID string) (bool, error)
	List(ctx context.Context, filter model.DocumentListFilter) (model.DocumentListResult, error)
}

type DocumentService struct {
	uploadDir         string
	textExtractor     *extractor.TextExtractor
	textChunker       *chunker.SemanticChunker
	embeddingProvider llm.LLMProvider
	repository        DocumentRepository
}

func NewDocumentService(uploadDir string, repository DocumentRepository, embeddingProvider llm.LLMProvider) *DocumentService {
	return &DocumentService{
		uploadDir:         uploadDir,
		textExtractor:     extractor.NewTextExtractor(),
		textChunker:       chunker.NewSemanticChunker(),
		embeddingProvider: embeddingProvider,
		repository:        repository,
	}
}

func (s *DocumentService) Save(
	ctx context.Context,
	file multipart.File,
	header *multipart.FileHeader,
	metadata model.DocumentMetadataInput,
) (model.DocumentUpload, error) {
	if file == nil || header == nil || header.Size <= 0 {
		return model.DocumentUpload{}, ErrDocumentRequired
	}
	if header.Size > MaxDocumentSize {
		return model.DocumentUpload{}, ErrDocumentTooLarge
	}
	metadata, err := validateMetadata(metadata)
	if err != nil {
		return model.DocumentUpload{}, err
	}
	originalName := filepath.Base(strings.TrimSpace(header.Filename))
	if originalName == "" || originalName == "." {
		return model.DocumentUpload{}, ErrDocumentRequired
	}
	extension := strings.ToLower(filepath.Ext(originalName))
	format, allowed := allowedDocumentFormats[extension]
	if !allowed {
		return model.DocumentUpload{}, ErrUnsupportedDocumentType
	}
	if err := s.validateConfiguration(); err != nil {
		return model.DocumentUpload{}, err
	}
	if err := os.MkdirAll(s.uploadDir, 0o750); err != nil {
		return model.DocumentUpload{}, fmt.Errorf("create upload directory: %w", err)
	}

	id, err := newDocumentID()
	if err != nil {
		return model.DocumentUpload{}, fmt.Errorf("generate document id: %w", err)
	}
	storedName := id + extension
	storedPath := filepath.Join(s.uploadDir, storedName)
	destination, err := os.OpenFile(storedPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return model.DocumentUpload{}, fmt.Errorf("create uploaded document: %w", err)
	}
	removePartial := true
	defer func() {
		_ = destination.Close()
		if removePartial {
			_ = os.Remove(storedPath)
		}
	}()

	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(destination, hasher), io.LimitReader(file, MaxDocumentSize+1))
	if err != nil {
		return model.DocumentUpload{}, fmt.Errorf("save uploaded document: %w", err)
	}
	if written > MaxDocumentSize {
		return model.DocumentUpload{}, ErrDocumentTooLarge
	}
	if written == 0 {
		return model.DocumentUpload{}, ErrDocumentRequired
	}
	if err := destination.Close(); err != nil {
		return model.DocumentUpload{}, fmt.Errorf("close uploaded document: %w", err)
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))
	if duplicate, exists, err := s.repository.FindByContentHash(ctx, contentHash); err != nil {
		return model.DocumentUpload{}, fmt.Errorf("%w: check duplicate document: %v", ErrDocumentStorageFailed, err)
	} else if exists {
		return duplicate, ErrDuplicateDocument
	}

	now := time.Now().UTC()
	document := model.DocumentUpload{
		ID:               id,
		OriginalName:     originalName,
		Format:           format,
		Size:             written,
		Category:         metadata.Category,
		Type:             metadata.Type,
		Permission:       metadata.Permission,
		Status:           "processing",
		ExtractionStatus: "pending",
		UploadedAt:       now,
		UpdatedAt:        now,
		StoredName:       storedName,
		ContentHash:      contentHash,
	}
	processed, chunks, processErr := s.processDocument(ctx, document, storedPath)
	if processErr != nil {
		failed := document
		failed.Status = "failed"
		failed.ExtractionStatus = "failed"
		failed.ProcessingError = processingFailureReason(processErr)
		if err := s.repository.SaveFailure(ctx, failed, storedName, contentHash); err != nil {
			if duplicate, exists, lookupErr := s.repository.FindByContentHash(ctx, contentHash); lookupErr == nil && exists {
				return duplicate, ErrDuplicateDocument
			}
			return model.DocumentUpload{}, fmt.Errorf("%w: save failed document state: %v", ErrDocumentStorageFailed, err)
		}
		removePartial = false
		return failed, processErr
	}
	if err := s.repository.Save(ctx, processed, storedName, contentHash, chunks); err != nil {
		if duplicate, exists, lookupErr := s.repository.FindByContentHash(ctx, contentHash); lookupErr == nil && exists {
			return duplicate, ErrDuplicateDocument
		}
		return model.DocumentUpload{}, fmt.Errorf("%w: %v", ErrDocumentStorageFailed, err)
	}
	removePartial = false
	return processed, nil
}

func (s *DocumentService) List(ctx context.Context, filter model.DocumentListFilter) (model.DocumentListResult, error) {
	if s.repository == nil {
		return model.DocumentListResult{}, errors.New("document repository is not configured")
	}
	filter, err := validateDocumentFilter(filter)
	if err != nil {
		return model.DocumentListResult{}, err
	}
	result, err := s.repository.List(ctx, filter)
	if err != nil {
		return model.DocumentListResult{}, err
	}
	if result.Documents == nil {
		result.Documents = make([]model.DocumentUpload, 0)
	}
	return result, nil
}

func (s *DocumentService) Delete(ctx context.Context, documentID string) error {
	document, exists, err := s.findDocument(ctx, documentID)
	if err != nil {
		return err
	}
	if !exists {
		return ErrDocumentNotFound
	}
	storedPath, err := s.storedDocumentPath(document.StoredName)
	if err != nil {
		return err
	}
	stagedPath := storedPath + ".deleting"
	fileStaged := false
	if _, statErr := os.Stat(storedPath); statErr == nil {
		if err := os.Rename(storedPath, stagedPath); err != nil {
			return fmt.Errorf("%w: stage original file for deletion: %v", ErrDocumentStorageFailed, err)
		}
		fileStaged = true
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("%w: inspect original file: %v", ErrDocumentStorageFailed, statErr)
	}
	restoreFile := func() {
		if fileStaged {
			_ = os.Rename(stagedPath, storedPath)
		}
	}
	deleted, err := s.repository.Delete(ctx, document.ID)
	if err != nil {
		restoreFile()
		return fmt.Errorf("%w: %v", ErrDocumentStorageFailed, err)
	}
	if !deleted {
		restoreFile()
		return ErrDocumentNotFound
	}
	if fileStaged {
		if err := os.Remove(stagedPath); err != nil {
			return fmt.Errorf("%w: remove staged original file: %v", ErrDocumentStorageFailed, err)
		}
	}
	return nil
}

func (s *DocumentService) Retry(ctx context.Context, documentID string) (model.DocumentUpload, error) {
	document, exists, err := s.findDocument(ctx, documentID)
	if err != nil {
		return model.DocumentUpload{}, err
	}
	if !exists {
		return model.DocumentUpload{}, ErrDocumentNotFound
	}
	if document.Status != "failed" {
		return document, ErrDocumentNotRetryable
	}
	return s.reprocess(ctx, document)
}

func (s *DocumentService) Revectorize(ctx context.Context, documentID string) (model.DocumentUpload, error) {
	document, exists, err := s.findDocument(ctx, documentID)
	if err != nil {
		return model.DocumentUpload{}, err
	}
	if !exists {
		return model.DocumentUpload{}, ErrDocumentNotFound
	}
	return s.reprocess(ctx, document)
}

func (s *DocumentService) reprocess(ctx context.Context, document model.DocumentUpload) (model.DocumentUpload, error) {
	storedPath, err := s.storedDocumentPath(document.StoredName)
	if err != nil {
		return document, err
	}
	processed, chunks, processErr := s.processDocument(ctx, document, storedPath)
	if processErr != nil {
		now := time.Now().UTC()
		status := document.Status
		extractionStatus := document.ExtractionStatus
		if status == "failed" {
			extractionStatus = "failed"
		}
		reason := processingFailureReason(processErr)
		if err := s.repository.UpdateProcessingError(
			ctx, document.ID, status, extractionStatus, reason, now,
		); err != nil {
			return model.DocumentUpload{}, fmt.Errorf("%w: record retry failure: %v", ErrDocumentStorageFailed, err)
		}
		document.ProcessingError = reason
		document.UpdatedAt = now
		return document, processErr
	}
	if err := s.repository.ReplaceProcessed(ctx, processed, chunks); err != nil {
		return model.DocumentUpload{}, fmt.Errorf("%w: replace document vectors: %v", ErrDocumentStorageFailed, err)
	}
	return processed, nil
}

func (s *DocumentService) processDocument(
	ctx context.Context,
	document model.DocumentUpload,
	storedPath string,
) (model.DocumentUpload, []model.DocumentChunk, error) {
	extractedText, err := s.textExtractor.Extract(storedPath, document.Format)
	if err != nil {
		switch {
		case errors.Is(err, extractor.ErrEmptyText):
			return document, nil, ErrExtractedTextEmpty
		case errors.Is(err, extractor.ErrExtractedTooLarge):
			return document, nil, ErrExtractedTextTooLarge
		default:
			return document, nil, fmt.Errorf("%w: %v", ErrTextExtractionFailed, err)
		}
	}
	chunks, err := s.textChunker.Chunk(extractedText)
	if err != nil {
		return document, nil, fmt.Errorf("%w: %v", ErrTextChunkingFailed, err)
	}
	chunkTexts := make([]string, len(chunks))
	for index := range chunks {
		chunkTexts[index] = chunks[index].Content
	}
	vectors, err := s.embeddingProvider.Embed(ctx, chunkTexts)
	if err != nil {
		return document, nil, fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
	}
	if len(vectors) != len(chunks) {
		return document, nil, fmt.Errorf("%w: received %d vectors for %d chunks", ErrEmbeddingFailed, len(vectors), len(chunks))
	}
	for index := range chunks {
		if len(vectors[index]) != model.DocumentEmbeddingDimensions {
			return document, nil, fmt.Errorf("%w: chunk %d has %d dimensions, expected %d", ErrEmbeddingFailed, index, len(vectors[index]), model.DocumentEmbeddingDimensions)
		}
		chunks[index].Embedding = vectors[index]
	}
	document.Status = "vectorized"
	document.ExtractionStatus = "completed"
	document.TextLength = utf8.RuneCountInString(extractedText)
	document.ChunkCount = len(chunks)
	document.EmbeddingModel = s.embeddingProvider.EmbeddingModel()
	document.EmbeddingDimensions = model.DocumentEmbeddingDimensions
	document.ProcessingError = ""
	document.UpdatedAt = time.Now().UTC()
	return document, chunks, nil
}

func (s *DocumentService) findDocument(ctx context.Context, documentID string) (model.DocumentUpload, bool, error) {
	documentID = strings.TrimSpace(documentID)
	if documentID == "" {
		return model.DocumentUpload{}, false, ErrDocumentNotFound
	}
	document, exists, err := s.repository.FindByID(ctx, documentID)
	if err != nil {
		return model.DocumentUpload{}, false, fmt.Errorf("%w: query document: %v", ErrDocumentStorageFailed, err)
	}
	return document, exists, nil
}

func (s *DocumentService) storedDocumentPath(storedName string) (string, error) {
	storedName = strings.TrimSpace(storedName)
	if storedName == "" || filepath.Base(storedName) != storedName {
		return "", fmt.Errorf("%w: invalid stored document name", ErrDocumentStorageFailed)
	}
	return filepath.Join(s.uploadDir, storedName), nil
}

func (s *DocumentService) validateConfiguration() error {
	if s.uploadDir == "" {
		return errors.New("upload directory is not configured")
	}
	if s.repository == nil {
		return errors.New("document repository is not configured")
	}
	if s.embeddingProvider == nil {
		return errors.New("embedding provider is not configured")
	}
	return nil
}

func processingFailureReason(err error) string {
	switch {
	case errors.Is(err, ErrExtractedTextEmpty):
		return "NO_EXTRACTABLE_TEXT: 文档中未提取到可用文本"
	case errors.Is(err, ErrExtractedTextTooLarge):
		return "EXTRACTED_TEXT_TOO_LARGE: 提取后的文本超过 10 MB"
	case errors.Is(err, ErrTextExtractionFailed):
		return "TEXT_EXTRACTION_FAILED: 文档文本提取失败"
	case errors.Is(err, ErrTextChunkingFailed):
		return "TEXT_CHUNKING_FAILED: 文档文本切割失败"
	case errors.Is(err, ErrEmbeddingFailed):
		return "EMBEDDING_FAILED: 文档向量化失败"
	default:
		return "PROCESSING_FAILED: 文档处理失败"
	}
}

func validateDocumentFilter(filter model.DocumentListFilter) (model.DocumentListFilter, error) {
	if filter.Page == 0 {
		filter.Page = DefaultDocumentPage
	}
	if filter.PageSize == 0 {
		filter.PageSize = DefaultDocumentPageSize
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > MaxDocumentPageSize {
		return model.DocumentListFilter{}, ErrInvalidDocumentFilter
	}
	filter.Query = strings.TrimSpace(filter.Query)
	filter.Category = strings.TrimSpace(filter.Category)
	filter.Type = strings.TrimSpace(filter.Type)
	filter.Permission = strings.TrimSpace(filter.Permission)
	filter.Status = strings.TrimSpace(filter.Status)
	if filter.Query != "" && utf8.RuneCountInString(filter.Query) > 100 {
		return model.DocumentListFilter{}, ErrInvalidDocumentFilter
	}
	if filter.Category != "" {
		if _, ok := allowedCategories[filter.Category]; !ok {
			return model.DocumentListFilter{}, ErrInvalidDocumentFilter
		}
	}
	if filter.Type != "" {
		if strings.EqualFold(filter.Type, "faq") {
			filter.Type = "FAQ"
		}
		if _, ok := allowedTypes[filter.Type]; !ok {
			return model.DocumentListFilter{}, ErrInvalidDocumentFilter
		}
	}
	if filter.Permission != "" {
		if _, ok := allowedPermissions[filter.Permission]; !ok {
			return model.DocumentListFilter{}, ErrInvalidDocumentFilter
		}
	}
	if filter.Status != "" && filter.Status != "vectorized" && filter.Status != "failed" {
		return model.DocumentListFilter{}, ErrInvalidDocumentFilter
	}
	return filter, nil
}

var allowedDocumentFormats = map[string]string{
	".pdf": "pdf",
	".txt": "txt",
	".md":  "md",
}

var allowedCategories = map[string]struct{}{
	"纺织": {},
	"鞋类": {},
	"杂货": {},
}

var allowedTypes = map[string]struct{}{
	"标准":   {},
	"业务规范": {},
	"FAQ":  {},
}

var allowedPermissions = map[string]struct{}{
	"公开": {},
	"内部": {},
}

func validateMetadata(metadata model.DocumentMetadataInput) (model.DocumentMetadataInput, error) {
	metadata.Category = strings.TrimSpace(metadata.Category)
	metadata.Type = strings.TrimSpace(metadata.Type)
	metadata.Permission = strings.TrimSpace(metadata.Permission)
	if strings.EqualFold(metadata.Type, "faq") {
		metadata.Type = "FAQ"
	}
	if _, allowed := allowedCategories[metadata.Category]; !allowed {
		return model.DocumentMetadataInput{}, ErrInvalidCategory
	}
	if _, allowed := allowedTypes[metadata.Type]; !allowed {
		return model.DocumentMetadataInput{}, ErrInvalidType
	}
	if _, allowed := allowedPermissions[metadata.Permission]; !allowed {
		return model.DocumentMetadataInput{}, ErrInvalidPermission
	}
	return metadata, nil
}

func newDocumentID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
