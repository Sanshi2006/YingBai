CREATE TABLE IF NOT EXISTS documents (
    id VARCHAR(32) PRIMARY KEY,
    original_name TEXT NOT NULL,
    stored_name TEXT NOT NULL,
    format VARCHAR(8) NOT NULL CHECK (format IN ('pdf', 'txt', 'md')),
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    category TEXT NOT NULL CHECK (category IN ('纺织', '鞋类', '杂货')),
    document_type TEXT NOT NULL CHECK (document_type IN ('标准', '业务规范', 'FAQ')),
    permission TEXT NOT NULL CHECK (permission IN ('公开', '内部')),
    status TEXT NOT NULL,
    extraction_status TEXT NOT NULL,
    text_length INTEGER NOT NULL CHECK (text_length >= 0),
    chunk_count INTEGER NOT NULL CHECK (chunk_count >= 0),
    uploaded_at TIMESTAMPTZ NOT NULL
);

CREATE TABLE IF NOT EXISTS document_chunks (
    id BIGSERIAL PRIMARY KEY,
    document_id VARCHAR(32) NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL CHECK (chunk_index >= 0),
    content TEXT NOT NULL CHECK (char_length(content) > 0),
    char_count INTEGER NOT NULL CHECK (char_count > 0),
    boundary TEXT NOT NULL DEFAULT 'semantic',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (document_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_documents_uploaded_at ON documents (uploaded_at DESC);
CREATE INDEX IF NOT EXISTS idx_documents_category ON documents (category);
CREATE INDEX IF NOT EXISTS idx_documents_permission ON documents (permission);
CREATE INDEX IF NOT EXISTS idx_document_chunks_document_id ON document_chunks (document_id, chunk_index);
