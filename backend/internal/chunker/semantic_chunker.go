package chunker

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"project-for-yingbai/backend/internal/model"
)

const (
	defaultTargetRunes = 700
	defaultMaxRunes    = 1000
)

var (
	paragraphBoundary = regexp.MustCompile(`\n\s*\n+`)
	markdownHeading   = regexp.MustCompile(`^#{1,6}\s+`)
	numberedHeading   = regexp.MustCompile(`^(第[一二三四五六七八九十百零〇0-9]+[章节篇部]|[一二三四五六七八九十]+、|[0-9]+(?:\.[0-9]+)*[.、]\s*)`)
)

var ErrEmptyText = errors.New("cannot chunk empty text")

// SemanticChunker keeps headings, paragraphs and sentences intact whenever possible.
// It only falls back to a rune-safe hard split when a single sentence exceeds MaxRunes.
type SemanticChunker struct {
	TargetRunes int
	MaxRunes    int
}

func NewSemanticChunker() *SemanticChunker {
	return &SemanticChunker{TargetRunes: defaultTargetRunes, MaxRunes: defaultMaxRunes}
}

func (c *SemanticChunker) Chunk(text string) ([]model.DocumentChunk, error) {
	normalized := strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if normalized == "" {
		return nil, ErrEmptyText
	}

	target, maximum := c.limits()
	units := c.semanticUnits(normalized, maximum)
	contents := packUnits(units, target, maximum)
	chunks := make([]model.DocumentChunk, 0, len(contents))
	for index, content := range contents {
		chunks = append(chunks, model.DocumentChunk{
			Index:     index,
			Content:   content,
			CharCount: utf8.RuneCountInString(content),
			Boundary:  "semantic",
		})
	}
	return chunks, nil
}

func (c *SemanticChunker) limits() (int, int) {
	target, maximum := c.TargetRunes, c.MaxRunes
	if target <= 0 {
		target = defaultTargetRunes
	}
	if maximum <= 0 {
		maximum = defaultMaxRunes
	}
	if target > maximum {
		target = maximum
	}
	return target, maximum
}

func (c *SemanticChunker) semanticUnits(text string, maximum int) []string {
	paragraphs := paragraphBoundary.Split(text, -1)
	units := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			continue
		}
		if utf8.RuneCountInString(paragraph) <= maximum {
			units = append(units, paragraph)
			continue
		}
		units = append(units, splitLongUnit(paragraph, maximum)...)
	}
	return units
}

func packUnits(units []string, target, maximum int) []string {
	chunks := make([]string, 0, len(units))
	current := ""
	flush := func() {
		if current != "" {
			chunks = append(chunks, current)
			current = ""
		}
	}

	for _, unit := range units {
		if isHeading(unit) && current != "" {
			flush()
		}
		if current == "" {
			current = unit
			continue
		}
		candidate := current + "\n\n" + unit
		candidateRunes := utf8.RuneCountInString(candidate)
		currentRunes := utf8.RuneCountInString(current)
		if candidateRunes > maximum || (currentRunes >= target/2 && candidateRunes > target) {
			flush()
			current = unit
			continue
		}
		current = candidate
	}
	flush()
	return chunks
}

func splitLongUnit(unit string, maximum int) []string {
	sentences := splitSentences(unit)
	result := make([]string, 0, len(sentences))
	current := ""
	for _, sentence := range sentences {
		if utf8.RuneCountInString(sentence) > maximum {
			if current != "" {
				result = append(result, current)
				current = ""
			}
			result = append(result, hardSplit(sentence, maximum)...)
			continue
		}
		candidate := current + sentence
		if current != "" && utf8.RuneCountInString(candidate) > maximum {
			result = append(result, current)
			current = sentence
			continue
		}
		current = candidate
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

func splitSentences(text string) []string {
	var sentences []string
	var builder strings.Builder
	for _, r := range text {
		builder.WriteRune(r)
		if strings.ContainsRune("。！？!?；;\n", r) {
			if sentence := strings.TrimSpace(builder.String()); sentence != "" {
				sentences = append(sentences, sentence)
			}
			builder.Reset()
		}
	}
	if sentence := strings.TrimSpace(builder.String()); sentence != "" {
		sentences = append(sentences, sentence)
	}
	return sentences
}

func hardSplit(text string, maximum int) []string {
	runes := []rune(text)
	parts := make([]string, 0, (len(runes)+maximum-1)/maximum)
	for start := 0; start < len(runes); start += maximum {
		end := start + maximum
		if end > len(runes) {
			end = len(runes)
		}
		parts = append(parts, string(runes[start:end]))
	}
	return parts
}

func isHeading(unit string) bool {
	firstLine := unit
	if index := strings.IndexRune(unit, '\n'); index >= 0 {
		firstLine = unit[:index]
	}
	firstLine = strings.TrimSpace(firstLine)
	return markdownHeading.MatchString(firstLine) || numberedHeading.MatchString(firstLine)
}
