package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"project-for-yingbai/backend/internal/llm"
	"project-for-yingbai/backend/internal/model"
	"project-for-yingbai/backend/internal/router"
)

func TestHealth(t *testing.T) {
	engine := newTestEngine("", t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}

	var body struct {
		Status  string `json:"status"`
		Service string `json:"service"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("expected status ok, got %q", body.Status)
	}
	if body.Service != "customer-service-backend" {
		t.Fatalf("unexpected service %q", body.Service)
	}
}

func TestChatReturnsFixedMessage(t *testing.T) {
	engine := newTestEngine("", t.TempDir())
	recorder := httptest.NewRecorder()
	payload := []byte(`{"message":"你好","sessionId":"session-1","role":"customer"}`)
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var body struct {
		Answer    string        `json:"answer"`
		Type      string        `json:"type"`
		SessionID string        `json:"sessionId"`
		Role      string        `json:"role"`
		Citations []interface{} `json:"citations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Answer != "底座已连通" {
		t.Fatalf("unexpected answer %q", body.Answer)
	}
	if body.Type != "fixed" {
		t.Fatalf("unexpected response type %q", body.Type)
	}
	if body.SessionID != "session-1" || body.Role != "customer" {
		t.Fatalf("request context was not preserved: session=%q role=%q", body.SessionID, body.Role)
	}
	if body.Citations == nil || len(body.Citations) != 0 {
		t.Fatalf("expected an empty citations array, got %#v", body.Citations)
	}
}

func TestChatRejectsMissingMessage(t *testing.T) {
	engine := newTestEngine("", t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(`{"role":"customer"}`))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, recorder.Code)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != "INVALID_REQUEST" {
		t.Fatalf("unexpected error code %q", body.Error.Code)
	}
}

func TestMobileHome(t *testing.T) {
	mobileDir, err := filepath.Abs(filepath.Join("..", "..", "..", "mobile"))
	if err != nil {
		t.Fatalf("resolve mobile directory: %v", err)
	}

	engine := newTestEngine(mobileDir, t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "智能客服") {
		t.Fatal("mobile home did not contain the expected title")
	}
	if recorder.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("mobile home did not include security headers")
	}
}

func TestDocumentUploadAcceptsAllowedFormats(t *testing.T) {
	testCases := []struct {
		name          string
		filename      string
		format        string
		content       []byte
		extractedText string
	}{
		{name: "PDF", filename: "standard.PDF", format: "pdf", content: minimalTextPDF("Hello PDF"), extractedText: "Hello PDF"},
		{name: "TXT", filename: "faq.txt", format: "txt", content: []byte("sample faq\r\nsecond line"), extractedText: "sample faq\nsecond line"},
		{name: "Markdown", filename: "guide.md", format: "md", content: []byte("# Sample guide"), extractedText: "# Sample guide"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			uploadDir := t.TempDir()
			repository := newMemoryDocumentRepository()
			engine := router.New("", uploadDir, repository, mustFakeProvider())
			request := newDocumentUploadRequest(t, testCase.filename, testCase.content)
			recorder := httptest.NewRecorder()

			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusCreated {
				t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, recorder.Code, recorder.Body.String())
			}

			var body struct {
				Document struct {
					ID                  string `json:"id"`
					OriginalName        string `json:"originalName"`
					Format              string `json:"format"`
					Size                int64  `json:"size"`
					Category            string `json:"category"`
					Type                string `json:"type"`
					Permission          string `json:"permission"`
					Status              string `json:"status"`
					ExtractionStatus    string `json:"extractionStatus"`
					TextLength          int    `json:"textLength"`
					ChunkCount          int    `json:"chunkCount"`
					EmbeddingModel      string `json:"embeddingModel"`
					EmbeddingDimensions int    `json:"embeddingDimensions"`
				} `json:"document"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if body.Document.ID == "" {
				t.Fatal("expected a generated document id")
			}
			if body.Document.OriginalName != testCase.filename {
				t.Fatalf("expected original name %q, got %q", testCase.filename, body.Document.OriginalName)
			}
			if body.Document.Format != testCase.format {
				t.Fatalf("expected format %q, got %q", testCase.format, body.Document.Format)
			}
			if body.Document.Size != int64(len(testCase.content)) || body.Document.Status != "vectorized" {
				t.Fatalf("unexpected document metadata: %#v", body.Document)
			}
			if body.Document.Category != "纺织" || body.Document.Type != "标准" || body.Document.Permission != "公开" {
				t.Fatalf("unexpected business metadata: %#v", body.Document)
			}
			if body.Document.ExtractionStatus != "completed" || body.Document.TextLength != len([]rune(testCase.extractedText)) || body.Document.ChunkCount == 0 {
				t.Fatalf("unexpected extraction metadata: %#v", body.Document)
			}
			if body.Document.EmbeddingModel != "fake-embedding" || body.Document.EmbeddingDimensions != model.DocumentEmbeddingDimensions {
				t.Fatalf("unexpected embedding metadata: %#v", body.Document)
			}

			entries, err := os.ReadDir(uploadDir)
			if err != nil {
				t.Fatalf("read upload directory: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("expected only the original document on disk, got %d files", len(entries))
			}
			storedDocument := entries[0].Name()
			if storedDocument == "" {
				t.Fatal("stored document file was not found")
			}
			storedContent, err := os.ReadFile(filepath.Join(uploadDir, storedDocument))
			if err != nil {
				t.Fatalf("read stored file: %v", err)
			}
			if !bytes.Equal(storedContent, testCase.content) {
				t.Fatal("stored file content did not match uploaded content")
			}
			chunks := repository.chunksFor(body.Document.ID)
			if len(chunks) != body.Document.ChunkCount {
				t.Fatalf("stored chunk count = %d, response chunk count = %d", len(chunks), body.Document.ChunkCount)
			}
			if len(chunks) != 1 || chunks[0].Content != testCase.extractedText || chunks[0].Boundary != "semantic" || len(chunks[0].Embedding) != model.DocumentEmbeddingDimensions {
				t.Fatalf("unexpected stored semantic chunks: %#v", chunks)
			}
		})
	}
}

func TestDocumentUploadRejectsUnextractableContent(t *testing.T) {
	testCases := []struct {
		name      string
		filename  string
		content   []byte
		errorCode string
	}{
		{name: "empty text", filename: "empty.txt", content: []byte(" \r\n\t "), errorCode: "NO_EXTRACTABLE_TEXT"},
		{name: "invalid UTF-8", filename: "invalid.md", content: []byte{0xff, 0xfe, 0xfd}, errorCode: "TEXT_EXTRACTION_FAILED"},
		{name: "malformed PDF", filename: "broken.pdf", content: []byte("%PDF-1.7 broken"), errorCode: "TEXT_EXTRACTION_FAILED"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			uploadDir := t.TempDir()
			engine := newTestEngine("", uploadDir)
			request := newDocumentUploadRequest(t, testCase.filename, testCase.content)
			recorder := httptest.NewRecorder()

			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected status %d, got %d: %s", http.StatusUnprocessableEntity, recorder.Code, recorder.Body.String())
			}
			assertErrorCode(t, recorder, testCase.errorCode)

			entries, err := os.ReadDir(uploadDir)
			if err != nil {
				t.Fatalf("read upload directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("failed extraction should leave no files, found %d", len(entries))
			}
		})
	}
}

func TestDocumentUploadCleansUpWhenEmbeddingFails(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	engine := router.New("", uploadDir, repository, failingLLMProvider{})
	request := newDocumentUploadRequest(t, "embedding-failure.txt", []byte("valid extracted content"))
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadGateway, recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "EMBEDDING_FAILED")
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("read upload directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed embedding should leave no files, found %d", len(entries))
	}
	documents, err := repository.List(context.Background())
	if err != nil {
		t.Fatalf("list repository documents: %v", err)
	}
	if len(documents) != 0 {
		t.Fatalf("failed embedding should leave no metadata, found %d", len(documents))
	}
}

func TestDocumentUploadValidatesBusinessMetadata(t *testing.T) {
	testCases := []struct {
		name       string
		category   string
		docType    string
		permission string
		errorCode  string
	}{
		{name: "invalid category", category: "电子", docType: "标准", permission: "公开", errorCode: "INVALID_CATEGORY"},
		{name: "invalid type", category: "纺织", docType: "手册", permission: "公开", errorCode: "INVALID_DOCUMENT_TYPE"},
		{name: "invalid permission", category: "纺织", docType: "标准", permission: "机密", errorCode: "INVALID_PERMISSION"},
		{name: "missing metadata", errorCode: "INVALID_CATEGORY"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			uploadDir := t.TempDir()
			engine := newTestEngine("", uploadDir)
			request := newDocumentUploadRequestWithMetadata(
				t,
				"standard.pdf",
				[]byte("%PDF-1.7 sample"),
				testCase.category,
				testCase.docType,
				testCase.permission,
			)
			recorder := httptest.NewRecorder()

			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}
			assertErrorCode(t, recorder, testCase.errorCode)

			entries, err := os.ReadDir(uploadDir)
			if err != nil {
				t.Fatalf("read upload directory: %v", err)
			}
			if len(entries) != 0 {
				t.Fatalf("invalid metadata should not create files, found %d", len(entries))
			}
		})
	}
}

func TestDocumentListPersistsAcrossRouterRestart(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	engine := router.New("", uploadDir, repository, mustFakeProvider())
	uploads := []struct {
		filename   string
		category   string
		docType    string
		permission string
	}{
		{filename: "public.md", category: "纺织", docType: "FAQ", permission: "公开"},
		{filename: "internal.txt", category: "鞋类", docType: "业务规范", permission: "内部"},
	}

	for _, upload := range uploads {
		request := newDocumentUploadRequestWithMetadata(
			t,
			upload.filename,
			[]byte("sample content"),
			upload.category,
			upload.docType,
			upload.permission,
		)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusCreated {
			t.Fatalf("upload %q failed with status %d: %s", upload.filename, recorder.Code, recorder.Body.String())
		}
	}

	restartedEngine := router.New("", uploadDir, repository, mustFakeProvider())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	restartedEngine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var body struct {
		Documents []struct {
			OriginalName        string `json:"originalName"`
			Category            string `json:"category"`
			Type                string `json:"type"`
			Permission          string `json:"permission"`
			ExtractionStatus    string `json:"extractionStatus"`
			TextLength          int    `json:"textLength"`
			ChunkCount          int    `json:"chunkCount"`
			EmbeddingModel      string `json:"embeddingModel"`
			EmbeddingDimensions int    `json:"embeddingDimensions"`
		} `json:"documents"`
		Total int `json:"total"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Total != 2 || len(body.Documents) != 2 {
		t.Fatalf("expected two listed documents, got total=%d documents=%d", body.Total, len(body.Documents))
	}

	found := make(map[string]bool)
	for _, document := range body.Documents {
		found[document.OriginalName] = true
		if document.ExtractionStatus != "completed" || document.TextLength == 0 || document.ChunkCount == 0 {
			t.Fatalf("document extraction metadata was not persisted: %#v", document)
		}
		if document.EmbeddingModel != "fake-embedding" || document.EmbeddingDimensions != model.DocumentEmbeddingDimensions {
			t.Fatalf("document embedding metadata was not persisted: %#v", document)
		}
		if document.OriginalName == "public.md" && (document.Category != "纺织" || document.Type != "FAQ" || document.Permission != "公开") {
			t.Fatalf("unexpected public document metadata: %#v", document)
		}
		if document.OriginalName == "internal.txt" && (document.Category != "鞋类" || document.Type != "业务规范" || document.Permission != "内部") {
			t.Fatalf("unexpected internal document metadata: %#v", document)
		}
	}
	if !found["public.md"] || !found["internal.txt"] {
		t.Fatalf("listed documents were incomplete: %#v", found)
	}
}

func TestDocumentListReturnsEmptyCollection(t *testing.T) {
	engine := newTestEngine("", filepath.Join(t.TempDir(), "not-created"))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if recorder.Body.String() != "{\"documents\":[],\"total\":0}" {
		t.Fatalf("unexpected empty list response: %s", recorder.Body.String())
	}
}

func TestDocumentUploadRejectsUnsupportedFormat(t *testing.T) {
	uploadDir := t.TempDir()
	engine := newTestEngine("", uploadDir)
	request := newDocumentUploadRequest(t, "report.docx", []byte("not supported"))
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d: %s", http.StatusUnsupportedMediaType, recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "UNSUPPORTED_FILE_TYPE")

	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("read upload directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("unsupported file should not be stored, found %d file(s)", len(entries))
	}
}

func TestDocumentUploadRequiresFile(t *testing.T) {
	engine := newTestEngine("", t.TempDir())
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &requestBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "FILE_REQUIRED")
}

func newDocumentUploadRequest(t *testing.T, filename string, content []byte) *http.Request {
	return newDocumentUploadRequestWithMetadata(t, filename, content, "纺织", "标准", "公开")
}

func newDocumentUploadRequestWithMetadata(
	t *testing.T,
	filename string,
	content []byte,
	category string,
	docType string,
	permission string,
) *http.Request {
	t.Helper()

	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)
	fields := map[string]string{
		"category":   category,
		"type":       docType,
		"permission": permission,
	}
	for field, value := range fields {
		if err := writer.WriteField(field, value); err != nil {
			t.Fatalf("write multipart field %q: %v", field, err)
		}
	}
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("create multipart file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write multipart file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/documents", &requestBody)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, expected string) {
	t.Helper()

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error.Code != expected {
		t.Fatalf("expected error code %q, got %q", expected, body.Error.Code)
	}
}

type memoryDocumentRepository struct {
	mu        sync.Mutex
	documents []model.DocumentUpload
	chunks    map[string][]model.DocumentChunk
}

func newMemoryDocumentRepository() *memoryDocumentRepository {
	return &memoryDocumentRepository{chunks: make(map[string][]model.DocumentChunk)}
}

func newTestEngine(mobileDir, uploadDir string) http.Handler {
	return router.New(mobileDir, uploadDir, newMemoryDocumentRepository(), mustFakeProvider())
}

func mustFakeProvider() llm.LLMProvider {
	provider, err := llm.NewFakeLLMProvider(model.DocumentEmbeddingDimensions)
	if err != nil {
		panic(err)
	}
	return provider
}

type failingLLMProvider struct{}

func (failingLLMProvider) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("simulated embedding failure")
}

func (failingLLMProvider) EmbeddingModel() string {
	return "failing-embedding"
}

func (r *memoryDocumentRepository) Save(_ context.Context, document model.DocumentUpload, _ string, chunks []model.DocumentChunk) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.documents = append(r.documents, document)
	r.chunks[document.ID] = append([]model.DocumentChunk(nil), chunks...)
	return nil
}

func (r *memoryDocumentRepository) List(_ context.Context) ([]model.DocumentUpload, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	documents := append([]model.DocumentUpload(nil), r.documents...)
	sort.Slice(documents, func(left, right int) bool {
		if documents[left].UploadedAt.Equal(documents[right].UploadedAt) {
			return documents[left].ID > documents[right].ID
		}
		return documents[left].UploadedAt.After(documents[right].UploadedAt)
	})
	return documents, nil
}

func (r *memoryDocumentRepository) chunksFor(documentID string) []model.DocumentChunk {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.DocumentChunk(nil), r.chunks[documentID]...)
}

func minimalTextPDF(text string) []byte {
	var document bytes.Buffer
	document.WriteString("%PDF-1.4\n")

	offsets := make([]int, 6)
	writeObject := func(number int, content string) {
		offsets[number] = document.Len()
		fmt.Fprintf(&document, "%d 0 obj\n%s\nendobj\n", number, content)
	}

	writeObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	writeObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	writeObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>")
	writeObject(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	writeObject(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))

	xrefOffset := document.Len()
	document.WriteString("xref\n0 6\n")
	document.WriteString("0000000000 65535 f \n")
	for number := 1; number <= 5; number++ {
		fmt.Fprintf(&document, "%010d 00000 n \n", offsets[number])
	}
	fmt.Fprintf(&document, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)
	return document.Bytes()
}
