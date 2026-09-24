CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS embedding_model TEXT,
    ADD COLUMN IF NOT EXISTS embedding_dimensions INTEGER;

ALTER TABLE document_chunks
    ADD COLUMN IF NOT EXISTS embedding vector(1024),
    ADD COLUMN IF NOT EXISTS embedding_model TEXT;

CREATE INDEX IF NOT EXISTS idx_document_chunks_embedding_hnsw
    ON document_chunks USING hnsw (embedding vector_cosine_ops)
    WHERE embedding IS NOT NULL;
