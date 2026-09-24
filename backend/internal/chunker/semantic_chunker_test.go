package chunker

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestChunkKeepsHeadingWithFollowingParagraph(t *testing.T) {
	chunker := &SemanticChunker{TargetRunes: 40, MaxRunes: 60}
	text := "# 第一章\n\n这是第一章的说明内容。\n\n# 第二章\n\n这是第二章的说明内容。"
	chunks, err := chunker.Chunk(text)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("chunk count = %d, want 2", len(chunks))
	}
	if !strings.HasPrefix(chunks[0].Content, "# 第一章") || !strings.Contains(chunks[0].Content, "第一章的说明") {
		t.Fatalf("first chunk did not preserve heading context: %q", chunks[0].Content)
	}
	if !strings.HasPrefix(chunks[1].Content, "# 第二章") || !strings.Contains(chunks[1].Content, "第二章的说明") {
		t.Fatalf("second chunk did not preserve heading context: %q", chunks[1].Content)
	}
}

func TestChunkSplitsLongParagraphOnSentenceBoundaries(t *testing.T) {
	chunker := &SemanticChunker{TargetRunes: 12, MaxRunes: 16}
	chunks, err := chunker.Chunk("第一句话很完整。第二句话也完整。第三句话仍完整。")
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("chunk count = %d, want at least 2", len(chunks))
	}
	for _, chunk := range chunks {
		if utf8.RuneCountInString(chunk.Content) > 16 {
			t.Fatalf("chunk exceeds maximum: %q", chunk.Content)
		}
		if !strings.HasSuffix(chunk.Content, "。") {
			t.Fatalf("chunk did not end at a sentence boundary: %q", chunk.Content)
		}
	}
}

func TestChunkRejectsEmptyText(t *testing.T) {
	_, err := NewSemanticChunker().Chunk(" \n\t ")
	if err != ErrEmptyText {
		t.Fatalf("error = %v, want ErrEmptyText", err)
	}
}
