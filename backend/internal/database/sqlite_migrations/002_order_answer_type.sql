CREATE TABLE conversation_logs_v2 (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('customer', 'service', 'admin')),
    question TEXT NOT NULL,
    answer TEXT NOT NULL,
    answer_type TEXT NOT NULL CHECK (answer_type IN ('knowledge', 'refusal', 'order')),
    citation_documents TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL
);

INSERT INTO conversation_logs_v2 (
    id, session_id, role, question, answer, answer_type, citation_documents, created_at
)
SELECT id, session_id, role, question, answer, answer_type, citation_documents, created_at
FROM conversation_logs;

DROP TABLE conversation_logs;
ALTER TABLE conversation_logs_v2 RENAME TO conversation_logs;

CREATE INDEX idx_conversation_logs_session_created_at
    ON conversation_logs (session_id, role, created_at DESC, id DESC);
