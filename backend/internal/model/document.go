package model

import "time"

const DocumentEmbeddingDimensions = 1024

type DocumentUpload struct {
	ID                  string    `json:"id"`
	OriginalName        string    `json:"originalName"`
	Format              string    `json:"format"`
	Size                int64     `json:"size"`
	Category            string    `json:"category"`
	Type                string    `json:"type"`
	Permission          string    `json:"permission"`
	Status              string    `json:"status"`
	ExtractionStatus    string    `json:"extractionStatus"`
	TextLength          int       `json:"textLength"`
	ChunkCount          int       `json:"chunkCount"`
	EmbeddingModel      string    `json:"embeddingModel"`
	EmbeddingDimensions int       `json:"embeddingDimensions"`
	ProcessingError     string    `json:"processingError,omitempty"`
	UploadedAt          time.Time `json:"uploadedAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
	StoredName          string    `json:"-"`
	ContentHash         string    `json:"-"`
}

// DocumentChunk is an ordered semantic text block extracted from a document.
type DocumentChunk struct {
	Index     int       `json:"index"`
	Content   string    `json:"content"`
	CharCount int       `json:"charCount"`
	Boundary  string    `json:"boundary"`
	Embedding []float32 `json:"-"`
}

type DocumentMetadataInput struct {
	Category   string
	Type       string
	Permission string
}

type DocumentListFilter struct {
	Page       int
	PageSize   int
	Query      string
	Category   string
	Type       string
	Permission string
	Status     string
}

type DocumentListResult struct {
	Documents  []DocumentUpload `json:"documents"`
	Total      int              `json:"total"`
	Page       int              `json:"page"`
	PageSize   int              `json:"pageSize"`
	TotalPages int              `json:"totalPages"`
}

// RetrievedChunk is a permission-filtered knowledge chunk ranked by cosine similarity.
type RetrievedChunk struct {
	DocumentID   string  `json:"documentId"`
	OriginalName string  `json:"originalName"`
	Category     string  `json:"category"`
	Type         string  `json:"type"`
	Permission   string  `json:"permission"`
	ChunkIndex   int     `json:"chunkIndex"`
	Content      string  `json:"content"`
	CharCount    int     `json:"charCount"`
	Similarity   float64 `json:"similarity"`
}
