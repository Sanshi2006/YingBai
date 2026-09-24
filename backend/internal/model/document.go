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
	UploadedAt          time.Time `json:"uploadedAt"`
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
