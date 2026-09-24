package extractor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	pdf "github.com/ledongthuc/pdf"
)

const MaxExtractedTextBytes int64 = 10 << 20

var (
	ErrEmptyText         = errors.New("document contains no extractable text")
	ErrInvalidUTF8       = errors.New("document text is not valid UTF-8")
	ErrExtractedTooLarge = errors.New("extracted text is too large")
)

type TextExtractor struct{}

func NewTextExtractor() *TextExtractor {
	return &TextExtractor{}
}

func (e *TextExtractor) Extract(path, format string) (string, error) {
	var (
		content []byte
		err     error
	)

	switch format {
	case "txt", "md":
		content, err = readLimitedFile(path)
	case "pdf":
		content, err = readPDFText(path)
	default:
		return "", fmt.Errorf("unsupported extraction format %q", format)
	}
	if err != nil {
		return "", err
	}
	if !utf8.Valid(content) {
		return "", ErrInvalidUTF8
	}

	text := strings.TrimPrefix(string(content), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimSpace(text)
	if text == "" {
		return "", ErrEmptyText
	}
	return text, nil
}

func readLimitedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return readWithLimit(file)
}

func readPDFText(path string) ([]byte, error) {
	file, reader, err := pdf.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	plainText, err := reader.GetPlainText()
	if err != nil {
		return nil, err
	}
	return readWithLimit(plainText)
}

func readWithLimit(reader io.Reader) ([]byte, error) {
	content, err := io.ReadAll(io.LimitReader(reader, MaxExtractedTextBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > MaxExtractedTextBytes {
		return nil, ErrExtractedTooLarge
	}
	return content, nil
}
