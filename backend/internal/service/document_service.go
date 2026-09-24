package service

import (
	"context"
	"crypto/rand"
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

const MaxDocumentSize int64 = 20 << 20

var (
	ErrDocumentRequired        = errors.New("document is required")
	ErrDocumentTooLarge        = errors.New("document is too large")
	ErrUnsupportedDocumentType = errors.New("unsupported document type")
	ErrInvalidCategory         = errors.New("invalid document category")
	ErrInvalidType             = errors.New("invalid document type")
	ErrInvalidPermission       = errors.New("invalid document permission")
	ErrTextExtractionFailed    = errors.New("text extraction failed")
	ErrExtractedTextEmpty      = errors.New("extracted text is empty")
	ErrExtractedTextTooLarge   = errors.New("extracted text is too large")
	ErrTextChunkingFailed      = errors.New("text chunking failed")
	ErrEmbeddingFailed         = errors.New("document embedding failed")
	ErrDocumentStorageFailed   = errors.New("document storage failed")
)

type DocumentRepository interface {
	Save(ctx context.Context, document model.DocumentUpload, storedName string, chunks []model.DocumentChunk) error
	List(ctx context.Context) ([]model.DocumentUpload, error)
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

func (s *DocumentService) Save(ctx context.Context, file multipart.File, header *multipart.FileHeader, metadata model.DocumentMetadataInput) (model.DocumentUpload, error) {
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
	if s.uploadDir == "" {
		return model.DocumentUpload{}, errors.New("upload directory is not configured")
	}
	if s.repository == nil {
		return model.DocumentUpload{}, errors.New("document repository is not configured")
	}
	if s.embeddingProvider == nil {
		return model.DocumentUpload{}, errors.New("embedding provider is not configured")
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

	written, err := io.Copy(destination, io.LimitReader(file, MaxDocumentSize+1))
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

	extractedText, err := s.textExtractor.Extract(storedPath, format)
	if err != nil {
		switch {
		case errors.Is(err, extractor.ErrEmptyText):
			return model.DocumentUpload{}, ErrExtractedTextEmpty
		case errors.Is(err, extractor.ErrExtractedTooLarge):
			return model.DocumentUpload{}, ErrExtractedTextTooLarge
		default:
			return model.DocumentUpload{}, fmt.Errorf("%w: %v", ErrTextExtractionFailed, err)
		}
	}
	chunks, err := s.textChunker.Chunk(extractedText)
	if err != nil {
		return model.DocumentUpload{}, fmt.Errorf("%w: %v", ErrTextChunkingFailed, err)
	}
	chunkTexts := make([]string, len(chunks))
	for index := range chunks {
		chunkTexts[index] = chunks[index].Content
	}
	vectors, err := s.embeddingProvider.Embed(ctx, chunkTexts)
	if err != nil {
		return model.DocumentUpload{}, fmt.Errorf("%w: %v", ErrEmbeddingFailed, err)
	}
	if len(vectors) != len(chunks) {
		return model.DocumentUpload{}, fmt.Errorf("%w: received %d vectors for %d chunks", ErrEmbeddingFailed, len(vectors), len(chunks))
	}
	for index := range chunks {
		if len(vectors[index]) != model.DocumentEmbeddingDimensions {
			return model.DocumentUpload{}, fmt.Errorf("%w: chunk %d has %d dimensions, expected %d", ErrEmbeddingFailed, index, len(vectors[index]), model.DocumentEmbeddingDimensions)
		}
		chunks[index].Embedding = vectors[index]
	}

	document := model.DocumentUpload{
		ID:                  id,
		OriginalName:        originalName,
		Format:              format,
		Size:                written,
		Category:            metadata.Category,
		Type:                metadata.Type,
		Permission:          metadata.Permission,
		Status:              "vectorized",
		ExtractionStatus:    "completed",
		TextLength:          utf8.RuneCountInString(extractedText),
		ChunkCount:          len(chunks),
		EmbeddingModel:      s.embeddingProvider.EmbeddingModel(),
		EmbeddingDimensions: model.DocumentEmbeddingDimensions,
		UploadedAt:          time.Now().UTC(),
	}
	if err := s.repository.Save(ctx, document, storedName, chunks); err != nil {
		return model.DocumentUpload{}, fmt.Errorf("%w: %v", ErrDocumentStorageFailed, err)
	}

	removePartial = false
	return document, nil
}

func (s *DocumentService) List(ctx context.Context) ([]model.DocumentUpload, error) {
	if s.repository == nil {
		return nil, errors.New("document repository is not configured")
	}
	documents, err := s.repository.List(ctx)
	if err != nil {
		return nil, err
	}
	if documents == nil {
		documents = make([]model.DocumentUpload, 0)
	}
	return documents, nil
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
