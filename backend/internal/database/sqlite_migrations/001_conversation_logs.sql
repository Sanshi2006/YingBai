CREATE TABLE IF NOT EXISTS conversation_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('customer', 'service', 'admin')),
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    answer_type TEXT NOT NULL CHECK (answer_type IN ('knowledge', 'refusal')),
    citation_documents TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_conversation_logs_session_created_at
    ON conversation_logs (session_id, created_at DESC, id DESC);
