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
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"project-for-yingbai/backend/internal/config"
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

func TestChatReturnsFixedRefusalWithoutKnowledge(t *testing.T) {
	engine, conversationLogger := newTestEngineWithLogger("", t.TempDir())
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
	if body.Answer != "知识库暂无依据，请转人工" {
		t.Fatalf("unexpected answer %q", body.Answer)
	}
	if body.Type != "refusal" {
		t.Fatalf("unexpected response type %q", body.Type)
	}
	if body.SessionID != "session-1" || body.Role != "customer" {
		t.Fatalf("request context was not preserved: session=%q role=%q", body.SessionID, body.Role)
	}
	if body.Citations == nil || len(body.Citations) != 0 {
		t.Fatalf("expected an empty citations array, got %#v", body.Citations)
	}
	logs := conversationLogger.entries()
	if len(logs) != 1 {
		t.Fatalf("expected one conversation log, got %d", len(logs))
	}
	if logs[0].SessionID != "session-1" || logs[0].Question != "你好" ||
		logs[0].Answer != "知识库暂无依据，请转人工" || logs[0].AnswerType != "refusal" {
		t.Fatalf("unexpected refusal log: %#v", logs[0])
	}
	if logs[0].CitationDocuments == nil || len(logs[0].CitationDocuments) != 0 || logs[0].CreatedAt.IsZero() {
		t.Fatalf("refusal log is incomplete: %#v", logs[0])
	}
}

func TestChatReturnsKnowledgeAnswerWithCitations(t *testing.T) {
	repository := newMemoryDocumentRepository()
	repository.searchResults = []model.RetrievedChunk{{
		DocumentID: "doc-1", OriginalName: "样品接收规范.md", Permission: "公开",
		ChunkIndex: 0, Content: "包装破损时应暂停流转并记录异常。", Similarity: 0.91,
	}}
	chatProvider := llm.NewFakeChatLLMProvider("样品包装破损时如何处理？", "应暂停流转并记录异常。[S1]")
	conversationLogger := newMemoryConversationLogger()
	engine := router.New("", t.TempDir(), repository, conversationLogger, mustFakeProvider(), chatProvider, testRetrievalConfig())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(`{"message":"包装破了咋办？","sessionId":"session-2","role":"customer"}`))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var body struct {
		Answer    string `json:"answer"`
		Type      string `json:"type"`
		Citations []struct {
			SourceID     string `json:"sourceId"`
			OriginalName string `json:"originalName"`
		} `json:"citations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Type != "knowledge" || body.Answer != "应暂停流转并记录异常。[S1]" {
		t.Fatalf("unexpected knowledge response: %#v", body)
	}
	if len(body.Citations) != 1 || body.Citations[0].SourceID != "S1" || body.Citations[0].OriginalName != "样品接收规范.md" {
		t.Fatalf("unexpected citations: %#v", body.Citations)
	}
	logs := conversationLogger.entries()
	if len(logs) != 1 || logs[0].AnswerType != "knowledge" ||
		len(logs[0].CitationDocuments) != 1 || logs[0].CitationDocuments[0] != "样品接收规范.md" {
		t.Fatalf("unexpected knowledge answer log: %#v", logs)
	}
}

func TestChatReturnsStructuredMockOrderWithoutCallingLLM(t *testing.T) {
	chatProvider := llm.NewFakeChatLLMProvider()
	conversationLogger := newMemoryConversationLogger()
	engine := router.New(
		"", t.TempDir(), newMemoryDocumentRepository(), conversationLogger,
		mustFakeProvider(), chatProvider, testRetrievalConfig(),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(
		`{"message":"麻烦查一下订单 ORD2026001","sessionId":"order-session","role":"customer"}`,
	))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var body struct {
		Type      string        `json:"type"`
		Citations []interface{} `json:"citations"`
		Order     *struct {
			Found        bool   `json:"found"`
			Mock         bool   `json:"mock"`
			OrderNumber  string `json:"orderNumber"`
			Status       string `json:"status"`
			Progress     int    `json:"progress"`
			ReportStatus string `json:"reportStatus"`
		} `json:"order"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode order response: %v", err)
	}
	if body.Type != "order" || body.Order == nil || !body.Order.Found || !body.Order.Mock ||
		body.Order.OrderNumber != "ORD2026001" || body.Order.Status != "检测中" ||
		body.Order.Progress != 68 || body.Order.ReportStatus != "未生成" {
		t.Fatalf("unexpected Mock order response: %#v", body)
	}
	if body.Citations == nil || len(body.Citations) != 0 {
		t.Fatalf("order response should have empty citations: %#v", body.Citations)
	}
	if len(chatProvider.Requests()) != 0 {
		t.Fatal("Mock order query should not call the Chat LLM")
	}
	logs := conversationLogger.entries()
	if len(logs) != 1 || logs[0].AnswerType != "order" || len(logs[0].CitationDocuments) != 0 {
		t.Fatalf("unexpected order conversation log: %#v", logs)
	}
}

func TestChatReturnsExplicitMockOrderNotFoundState(t *testing.T) {
	chatProvider := llm.NewFakeChatLLMProvider()
	engine := router.New(
		"", t.TempDir(), newMemoryDocumentRepository(), newMemoryConversationLogger(),
		mustFakeProvider(), chatProvider, testRetrievalConfig(),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(
		`{"message":"查询订单 ORD9999999","sessionId":"missing-order-session","role":"service"}`,
	))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	var body struct {
		Answer string `json:"answer"`
		Order  *struct {
			Found        bool   `json:"found"`
			OrderNumber  string `json:"orderNumber"`
			Status       string `json:"status"`
			ReportStatus string `json:"reportStatus"`
		} `json:"order"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode missing order response: %v", err)
	}
	if body.Order == nil || body.Order.Found || body.Order.OrderNumber != "ORD9999999" ||
		body.Order.Status != "" || body.Order.ReportStatus != "" || !strings.Contains(body.Answer, "未查询到") {
		t.Fatalf("unexpected not-found response: %#v", body)
	}
	if len(chatProvider.Requests()) != 0 {
		t.Fatal("missing Mock order query should not call the Chat LLM")
	}
}

func TestChatEnforcesKnowledgePermissionsForCustomerAndInternalRoles(t *testing.T) {
	tests := []struct {
		role          string
		responses     []string
		wantType      string
		wantCitations int
	}{
		{role: "customer", responses: []string{"内部处理规则是什么？"}, wantType: "refusal", wantCitations: 0},
		{role: "service", responses: []string{"内部处理规则是什么？", "客服可查看内部规则。[S1]"}, wantType: "knowledge", wantCitations: 1},
		{role: "admin", responses: []string{"内部处理规则是什么？", "管理员可查看内部规则。[S1]"}, wantType: "knowledge", wantCitations: 1},
	}
	for _, testCase := range tests {
		t.Run(testCase.role, func(t *testing.T) {
			repository := newMemoryDocumentRepository()
			repository.searchResults = []model.RetrievedChunk{{
				DocumentID: "internal-only", OriginalName: "内部业务规范.md", Permission: "内部",
				Content: "仅供内部人员使用的处理规则。", Similarity: 0.95,
			}}
			chatProvider := llm.NewFakeChatLLMProvider(testCase.responses...)
			engine := router.New(
				"", t.TempDir(), repository, newMemoryConversationLogger(),
				mustFakeProvider(), chatProvider, testRetrievalConfig(),
			)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(fmt.Sprintf(
				`{"message":"内部规则是什么？","sessionId":"permission-%s","role":"%s"}`,
				testCase.role, testCase.role,
			)))
			request.Header.Set("Content-Type", "application/json")

			engine.ServeHTTP(recorder, request)

			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
			}
			var body struct {
				Type      string        `json:"type"`
				Citations []interface{} `json:"citations"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode permission response: %v", err)
			}
			if body.Type != testCase.wantType || len(body.Citations) != testCase.wantCitations {
				t.Fatalf("role %s received unexpected response: %#v", testCase.role, body)
			}
		})
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

func TestChatRejectsMissingSessionID(t *testing.T) {
	engine := newTestEngine("", t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(`{"message":"你好","role":"customer"}`))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d: %s", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "INVALID_REQUEST")
}

func TestChatReturnsStableErrorWhenConversationLogFails(t *testing.T) {
	repository := newMemoryDocumentRepository()
	conversationLogger := newMemoryConversationLogger()
	conversationLogger.err = errors.New("simulated SQLite write failure")
	engine := router.New("", t.TempDir(), repository, conversationLogger, mustFakeProvider(), mustFakeChatProvider(), testRetrievalConfig())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(
		`{"message":"你好","sessionId":"session-log-failure","role":"customer"}`,
	))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d: %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "CHAT_LOG_FAILED")
}

func TestChatUsesLatestFiveTurnsFromSameSessionAndRole(t *testing.T) {
	repository := newMemoryDocumentRepository()
	repository.searchResults = []model.RetrievedChunk{{
		DocumentID: "doc-follow-up", OriginalName: "检测时效.md", Permission: "公开",
		Content: "该项目检测时效为五个工作日。", Similarity: 0.93,
	}}
	conversationLogger := newMemoryConversationLogger()
	for index := 1; index <= 6; index++ {
		if err := conversationLogger.Log(context.Background(), model.ConversationLog{
			SessionID: "persisted-session", Role: "customer",
			Question: fmt.Sprintf("customer-history-%d", index),
			Answer:   fmt.Sprintf("customer-answer-%d", index), AnswerType: "knowledge",
		}); err != nil {
			t.Fatalf("seed customer history: %v", err)
		}
	}
	if err := conversationLogger.Log(context.Background(), model.ConversationLog{
		SessionID: "persisted-session", Role: "admin", Question: "admin-secret-question",
		Answer: "admin-secret-answer", AnswerType: "knowledge",
	}); err != nil {
		t.Fatalf("seed admin history: %v", err)
	}
	chatProvider := llm.NewFakeChatLLMProvider("该项目的检测时效是多少？", "检测时效为五个工作日。[S1]")
	engine := router.New("", t.TempDir(), repository, conversationLogger, mustFakeProvider(), chatProvider, testRetrievalConfig())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(
		`{"message":"那它要多久？","sessionId":"persisted-session","role":"customer"}`,
	))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	requests := chatProvider.Requests()
	if len(requests) != 2 {
		t.Fatalf("chat LLM calls = %d, want rewrite + answer", len(requests))
	}
	for _, llmRequest := range requests {
		prompt := llmRequest[len(llmRequest)-1].Content
		for index := 2; index <= 6; index++ {
			if !strings.Contains(prompt, fmt.Sprintf("customer-history-%d", index)) {
				t.Fatalf("prompt is missing recent customer turn %d: %s", index, prompt)
			}
		}
		if strings.Contains(prompt, "customer-history-1") || strings.Contains(prompt, "admin-secret") {
			t.Fatalf("prompt contains expired or cross-role history: %s", prompt)
		}
	}
}

func TestChatReturnsStableErrorWhenConversationHistoryFails(t *testing.T) {
	conversationLogger := newMemoryConversationLogger()
	conversationLogger.historyErr = errors.New("simulated SQLite history failure")
	chatProvider := mustFakeChatProvider()
	engine := router.New(
		"", t.TempDir(), newMemoryDocumentRepository(), conversationLogger,
		mustFakeProvider(), chatProvider, testRetrievalConfig(),
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(
		`{"message":"继续上一个问题","sessionId":"session-history-failure","role":"customer"}`,
	))
	request.Header.Set("Content-Type", "application/json")

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d: %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}
	assertErrorCode(t, recorder, "CHAT_HISTORY_FAILED")
	if len(chatProvider.(*llm.FakeChatLLMProvider).Requests()) != 0 {
		t.Fatal("history failure should stop before calling the chat LLM")
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

func TestMobileChatPersistsSessionIDInLocalStorage(t *testing.T) {
	appPath, err := filepath.Abs(filepath.Join("..", "..", "..", "mobile", "assets", "app.js"))
	if err != nil {
		t.Fatalf("resolve mobile app script: %v", err)
	}
	appSource, err := os.ReadFile(appPath)
	if err != nil {
		t.Fatalf("read mobile app script: %v", err)
	}
	source := string(appSource)
	for _, expected := range []string{
		`sessionId: getOrCreateSessionId()`,
		`localStorage.getItem("lab-chat-session-id")`,
		`localStorage.setItem("lab-chat-session-id", sessionId)`,
		`sessionId: state.sessionId`,
	} {
		if !strings.Contains(source, expected) {
			t.Fatalf("mobile session persistence is missing %q", expected)
		}
	}
}

func TestMobileIncludesOrderCardAndDayFourQuickPrompts(t *testing.T) {
	mobileDir, err := filepath.Abs(filepath.Join("..", "..", "..", "mobile"))
	if err != nil {
		t.Fatalf("resolve mobile directory: %v", err)
	}
	files := map[string][]string{
		filepath.Join(mobileDir, "index.html"): {
			`data-prompt="查一下订单 ORD2026001">查订单`,
			`data-prompt="常见检测标准有哪些？">常见标准`,
			`data-prompt="请帮我转人工客服">转人工`,
		},
		filepath.Join(mobileDir, "assets", "app.js"): {
			"order: payload.order || null", "function createOrderCard(order)", "Mock LIMS",
		},
		filepath.Join(mobileDir, "assets", "styles.css"): {
			".order-card", ".order-progress", "min-height: 44px",
		},
	}
	for path, expectedValues := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read mobile asset %s: %v", path, err)
		}
		for _, expected := range expectedValues {
			if !strings.Contains(string(content), expected) {
				t.Fatalf("mobile asset %s is missing %q", path, expected)
			}
		}
	}
}

func TestMobileIncludesDayFiveInteractionStates(t *testing.T) {
	mobileDir, err := filepath.Abs(filepath.Join("..", "..", "..", "mobile", "assets"))
	if err != nil {
		t.Fatalf("resolve mobile assets: %v", err)
	}
	files := map[string][]string{
		filepath.Join(mobileDir, "app.js"): {
			`window.visualViewport?.addEventListener("resize", syncVisualViewport)`,
			`document.documentElement.style.setProperty("--app-height"`,
			"function renderEmptyState()",
			`label.textContent = "正在查询，请稍候"`,
			`button.textContent = "正在重试…"`,
			`if (!button || state.sending) return`,
		},
		filepath.Join(mobileDir, "styles.css"): {
			"body.chat-active", ".chat-empty-state", ".chat-view.keyboard-open .quick-prompts",
			"height: var(--app-height)",
		},
	}
	for path, expectedValues := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read mobile asset %s: %v", path, err)
		}
		for _, expected := range expectedValues {
			if !strings.Contains(string(content), expected) {
				t.Fatalf("mobile Day 5 state handling in %s is missing %q", path, expected)
			}
		}
	}
}

func TestDocumentManagementPage(t *testing.T) {
	mobileDir, err := filepath.Abs(filepath.Join("..", "..", "..", "mobile"))
	if err != nil {
		t.Fatalf("resolve mobile directory: %v", err)
	}
	engine := newTestEngine(mobileDir, t.TempDir())
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/documents", nil)

	engine.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "文档管理") ||
		!strings.Contains(recorder.Body.String(), "/assets/documents.js") {
		t.Fatal("document management page did not include expected content")
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
			engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), mustFakeProvider(), mustFakeChatProvider(), testRetrievalConfig())
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

func TestDocumentUploadPreservesFailedExtractionForRetry(t *testing.T) {
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
			if len(entries) != 1 {
				t.Fatalf("failed extraction should preserve one original file, found %d", len(entries))
			}
		})
	}
}

func TestDocumentUploadRecordsEmbeddingFailureForRetry(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), failingLLMProvider{}, mustFakeChatProvider(), testRetrievalConfig())
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
	if len(entries) != 1 {
		t.Fatalf("failed embedding should preserve one original file, found %d", len(entries))
	}
	result, err := repository.List(context.Background(), model.DocumentListFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list repository documents: %v", err)
	}
	if len(result.Documents) != 1 || result.Documents[0].Status != "failed" ||
		!strings.Contains(result.Documents[0].ProcessingError, "EMBEDDING_FAILED") {
		t.Fatalf("failed embedding state was not persisted: %#v", result.Documents)
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
	engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), mustFakeProvider(), mustFakeChatProvider(), testRetrievalConfig())
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
			[]byte("sample content "+upload.filename),
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

	restartedEngine := router.New("", uploadDir, repository, newMemoryConversationLogger(), mustFakeProvider(), mustFakeChatProvider(), testRetrievalConfig())
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
	var body model.DocumentListResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode empty list response: %v", err)
	}
	if body.Documents == nil || len(body.Documents) != 0 || body.Total != 0 ||
		body.Page != 1 || body.PageSize != 20 || body.TotalPages != 0 {
		t.Fatalf("unexpected empty list response: %#v", body)
	}
}

func TestDocumentUploadRejectsDuplicateContent(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), mustFakeProvider(), mustFakeChatProvider(), testRetrievalConfig())
	content := []byte("完全相同的知识内容")

	firstRecorder := httptest.NewRecorder()
	engine.ServeHTTP(firstRecorder, newDocumentUploadRequest(t, "original.txt", content))
	if firstRecorder.Code != http.StatusCreated {
		t.Fatalf("first upload failed: %d %s", firstRecorder.Code, firstRecorder.Body.String())
	}
	var firstBody struct {
		Document model.DocumentUpload `json:"document"`
	}
	if err := json.Unmarshal(firstRecorder.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first upload: %v", err)
	}

	duplicateRecorder := httptest.NewRecorder()
	engine.ServeHTTP(duplicateRecorder, newDocumentUploadRequest(t, "renamed.md", content))
	if duplicateRecorder.Code != http.StatusConflict {
		t.Fatalf("expected duplicate status %d, got %d: %s", http.StatusConflict, duplicateRecorder.Code, duplicateRecorder.Body.String())
	}
	var duplicateBody struct {
		Error struct {
			Code       string `json:"code"`
			DocumentID string `json:"documentId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(duplicateRecorder.Body.Bytes(), &duplicateBody); err != nil {
		t.Fatalf("decode duplicate response: %v", err)
	}
	if duplicateBody.Error.Code != "DUPLICATE_DOCUMENT" || duplicateBody.Error.DocumentID != firstBody.Document.ID {
		t.Fatalf("unexpected duplicate response: %#v", duplicateBody)
	}
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("read upload directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("duplicate upload should keep exactly one file, got %d", len(entries))
	}
}

func TestDocumentListSupportsPaginationAndFilters(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), mustFakeProvider(), mustFakeChatProvider(), testRetrievalConfig())
	uploads := []struct {
		name, content, category, docType, permission string
	}{
		{"alpha-public.md", "alpha 公开知识", "纺织", "FAQ", "公开"},
		{"alpha-internal.txt", "alpha 内部知识", "纺织", "业务规范", "内部"},
		{"beta-public.txt", "beta 鞋类知识", "鞋类", "标准", "公开"},
	}
	for _, upload := range uploads {
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, newDocumentUploadRequestWithMetadata(
			t, upload.name, []byte(upload.content), upload.category, upload.docType, upload.permission,
		))
		if recorder.Code != http.StatusCreated {
			t.Fatalf("upload %s failed: %d %s", upload.name, recorder.Code, recorder.Body.String())
		}
	}

	query := url.Values{
		"page":       {"1"},
		"pageSize":   {"1"},
		"q":          {"alpha"},
		"category":   {"纺织"},
		"permission": {"公开"},
		"status":     {"vectorized"},
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/documents?"+query.Encode(), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("filtered list failed: %d %s", recorder.Code, recorder.Body.String())
	}
	var result model.DocumentListResult
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode filtered list: %v", err)
	}
	if result.Total != 1 || result.Page != 1 || result.PageSize != 1 ||
		result.TotalPages != 1 || len(result.Documents) != 1 ||
		result.Documents[0].OriginalName != "alpha-public.md" {
		t.Fatalf("unexpected filtered page: %#v", result)
	}

	invalidRecorder := httptest.NewRecorder()
	engine.ServeHTTP(invalidRecorder, httptest.NewRequest(http.MethodGet, "/api/v1/documents?pageSize=101", nil))
	if invalidRecorder.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid filter status %d, got %d", http.StatusBadRequest, invalidRecorder.Code)
	}
	assertErrorCode(t, invalidRecorder, "INVALID_DOCUMENT_FILTER")
}

func TestFailedDocumentCanBeRetried(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	provider := newRecoverableLLMProvider(true)
	engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), provider, mustFakeChatProvider(), testRetrievalConfig())
	uploadRecorder := httptest.NewRecorder()
	engine.ServeHTTP(uploadRecorder, newDocumentUploadRequest(t, "retry.txt", []byte("可重试的有效文本")))
	if uploadRecorder.Code != http.StatusBadGateway {
		t.Fatalf("expected failed upload status %d, got %d: %s", http.StatusBadGateway, uploadRecorder.Code, uploadRecorder.Body.String())
	}
	var failedBody struct {
		Error struct {
			DocumentID string `json:"documentId"`
		} `json:"error"`
	}
	if err := json.Unmarshal(uploadRecorder.Body.Bytes(), &failedBody); err != nil {
		t.Fatalf("decode failed upload: %v", err)
	}
	if failedBody.Error.DocumentID == "" {
		t.Fatal("failed upload did not return a retryable document id")
	}

	provider.setFail(false)
	retryRecorder := httptest.NewRecorder()
	retryRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/documents/"+failedBody.Error.DocumentID+"/retry",
		nil,
	)
	engine.ServeHTTP(retryRecorder, retryRequest)
	if retryRecorder.Code != http.StatusOK {
		t.Fatalf("retry failed: %d %s", retryRecorder.Code, retryRecorder.Body.String())
	}
	var retryBody struct {
		Document model.DocumentUpload `json:"document"`
	}
	if err := json.Unmarshal(retryRecorder.Body.Bytes(), &retryBody); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if retryBody.Document.Status != "vectorized" || retryBody.Document.ProcessingError != "" ||
		retryBody.Document.ChunkCount == 0 {
		t.Fatalf("unexpected retried document: %#v", retryBody.Document)
	}
}

func TestDocumentCanBeRevectorizedAndDeleted(t *testing.T) {
	uploadDir := t.TempDir()
	repository := newMemoryDocumentRepository()
	provider := newRecoverableLLMProvider(false)
	engine := router.New("", uploadDir, repository, newMemoryConversationLogger(), provider, mustFakeChatProvider(), testRetrievalConfig())
	uploadRecorder := httptest.NewRecorder()
	engine.ServeHTTP(uploadRecorder, newDocumentUploadRequest(t, "managed.md", []byte("# 可维护知识\n\n正文内容。")))
	if uploadRecorder.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", uploadRecorder.Code, uploadRecorder.Body.String())
	}
	var uploadBody struct {
		Document model.DocumentUpload `json:"document"`
	}
	if err := json.Unmarshal(uploadRecorder.Body.Bytes(), &uploadBody); err != nil {
		t.Fatalf("decode upload response: %v", err)
	}

	revectorizeRecorder := httptest.NewRecorder()
	engine.ServeHTTP(revectorizeRecorder, httptest.NewRequest(
		http.MethodPost,
		"/api/v1/documents/"+uploadBody.Document.ID+"/revectorize",
		nil,
	))
	if revectorizeRecorder.Code != http.StatusOK {
		t.Fatalf("revectorize failed: %d %s", revectorizeRecorder.Code, revectorizeRecorder.Body.String())
	}
	if provider.callCount() != 2 {
		t.Fatalf("expected embedding provider to be called twice, got %d", provider.callCount())
	}

	deleteRecorder := httptest.NewRecorder()
	engine.ServeHTTP(deleteRecorder, httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/documents/"+uploadBody.Document.ID,
		nil,
	))
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("delete failed: %d %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	if _, exists, err := repository.FindByID(context.Background(), uploadBody.Document.ID); err != nil || exists {
		t.Fatalf("deleted document still exists: exists=%v err=%v", exists, err)
	}
	entries, err := os.ReadDir(uploadDir)
	if err != nil {
		t.Fatalf("read upload directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("delete should remove the original file, found %d entries", len(entries))
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
	mu            sync.Mutex
	documents     []model.DocumentUpload
	chunks        map[string][]model.DocumentChunk
	hashes        map[string]string
	searchResults []model.RetrievedChunk
}

func newMemoryDocumentRepository() *memoryDocumentRepository {
	return &memoryDocumentRepository{
		chunks: make(map[string][]model.DocumentChunk),
		hashes: make(map[string]string),
	}
}

func newTestEngine(mobileDir, uploadDir string) http.Handler {
	engine, _ := newTestEngineWithLogger(mobileDir, uploadDir)
	return engine
}

func newTestEngineWithLogger(mobileDir, uploadDir string) (http.Handler, *memoryConversationLogger) {
	conversationLogger := newMemoryConversationLogger()
	return router.New(
		mobileDir,
		uploadDir,
		newMemoryDocumentRepository(),
		conversationLogger,
		mustFakeProvider(),
		mustFakeChatProvider(),
		testRetrievalConfig(),
	), conversationLogger
}

func mustFakeProvider() llm.LLMProvider {
	provider, err := llm.NewFakeLLMProvider(model.DocumentEmbeddingDimensions)
	if err != nil {
		panic(err)
	}
	return provider
}

func mustFakeChatProvider() llm.ChatLLMProvider {
	return llm.NewFakeChatLLMProvider("改写后的测试问题")
}

func testRetrievalConfig() config.RetrievalConfig {
	return config.RetrievalConfig{TopK: 5, SimilarityThreshold: 0.55}
}

type failingLLMProvider struct{}

func (failingLLMProvider) Embed(context.Context, []string) ([][]float32, error) {
	return nil, errors.New("simulated embedding failure")
}

func (failingLLMProvider) EmbeddingModel() string {
	return "failing-embedding"
}

type recoverableLLMProvider struct {
	mu       sync.Mutex
	fail     bool
	calls    int
	delegate llm.LLMProvider
}

func newRecoverableLLMProvider(fail bool) *recoverableLLMProvider {
	return &recoverableLLMProvider{fail: fail, delegate: mustFakeProvider()}
}

func (p *recoverableLLMProvider) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	p.mu.Lock()
	p.calls++
	fail := p.fail
	p.mu.Unlock()
	if fail {
		return nil, errors.New("simulated recoverable embedding failure")
	}
	return p.delegate.Embed(ctx, inputs)
}

func (p *recoverableLLMProvider) EmbeddingModel() string {
	return "recoverable-embedding"
}

func (p *recoverableLLMProvider) setFail(fail bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fail = fail
}

func (p *recoverableLLMProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type memoryConversationLogger struct {
	mu         sync.Mutex
	logs       []model.ConversationLog
	err        error
	historyErr error
}

func newMemoryConversationLogger() *memoryConversationLogger {
	return &memoryConversationLogger{}
}

func (l *memoryConversationLogger) Log(_ context.Context, entry model.ConversationLog) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	citations := make([]string, len(entry.CitationDocuments))
	copy(citations, entry.CitationDocuments)
	entry.CitationDocuments = citations
	l.logs = append(l.logs, entry)
	return nil
}

func (l *memoryConversationLogger) entries() []model.ConversationLog {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := append([]model.ConversationLog(nil), l.logs...)
	for index := range entries {
		citations := make([]string, len(entries[index].CitationDocuments))
		copy(citations, entries[index].CitationDocuments)
		entries[index].CitationDocuments = citations
	}
	return entries
}

func (l *memoryConversationLogger) ListRecentBySessionAndRole(
	_ context.Context,
	sessionID, role string,
	limit int,
) ([]model.ConversationLog, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.historyErr != nil {
		return nil, l.historyErr
	}
	matches := make([]model.ConversationLog, 0, limit)
	for _, entry := range l.logs {
		if entry.SessionID == sessionID && entry.Role == role {
			matches = append(matches, entry)
		}
	}
	if len(matches) > limit {
		matches = matches[len(matches)-limit:]
	}
	return append([]model.ConversationLog(nil), matches...), nil
}

func (r *memoryDocumentRepository) Save(
	_ context.Context,
	document model.DocumentUpload,
	storedName, contentHash string,
	chunks []model.DocumentChunk,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	document.StoredName = storedName
	document.ContentHash = contentHash
	r.documents = append(r.documents, document)
	r.chunks[document.ID] = append([]model.DocumentChunk(nil), chunks...)
	r.hashes[contentHash] = document.ID
	return nil
}

func (r *memoryDocumentRepository) SaveFailure(
	_ context.Context,
	document model.DocumentUpload,
	storedName, contentHash string,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	document.StoredName = storedName
	document.ContentHash = contentHash
	r.documents = append(r.documents, document)
	r.hashes[contentHash] = document.ID
	return nil
}

func (r *memoryDocumentRepository) ReplaceProcessed(
	_ context.Context,
	document model.DocumentUpload,
	chunks []model.DocumentChunk,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.documentIndex(document.ID)
	if index < 0 {
		return errors.New("document not found")
	}
	document.StoredName = r.documents[index].StoredName
	document.ContentHash = r.documents[index].ContentHash
	r.documents[index] = document
	r.chunks[document.ID] = append([]model.DocumentChunk(nil), chunks...)
	return nil
}

func (r *memoryDocumentRepository) UpdateProcessingError(
	_ context.Context,
	documentID, status, extractionStatus, processingError string,
	updatedAt time.Time,
) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.documentIndex(documentID)
	if index < 0 {
		return errors.New("document not found")
	}
	r.documents[index].Status = status
	r.documents[index].ExtractionStatus = extractionStatus
	r.documents[index].ProcessingError = processingError
	r.documents[index].UpdatedAt = updatedAt
	return nil
}

func (r *memoryDocumentRepository) FindByID(_ context.Context, documentID string) (model.DocumentUpload, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.documentIndex(documentID)
	if index < 0 {
		return model.DocumentUpload{}, false, nil
	}
	return r.documents[index], true, nil
}

func (r *memoryDocumentRepository) FindByContentHash(_ context.Context, contentHash string) (model.DocumentUpload, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	documentID, exists := r.hashes[contentHash]
	if !exists {
		return model.DocumentUpload{}, false, nil
	}
	index := r.documentIndex(documentID)
	if index < 0 {
		return model.DocumentUpload{}, false, nil
	}
	return r.documents[index], true, nil
}

func (r *memoryDocumentRepository) Delete(_ context.Context, documentID string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	index := r.documentIndex(documentID)
	if index < 0 {
		return false, nil
	}
	delete(r.hashes, r.documents[index].ContentHash)
	delete(r.chunks, documentID)
	r.documents = append(r.documents[:index], r.documents[index+1:]...)
	return true, nil
}

func (r *memoryDocumentRepository) List(_ context.Context, filter model.DocumentListFilter) (model.DocumentListResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	documents := make([]model.DocumentUpload, 0, len(r.documents))
	for _, document := range r.documents {
		if filter.Category != "" && document.Category != filter.Category {
			continue
		}
		if filter.Type != "" && document.Type != filter.Type {
			continue
		}
		if filter.Permission != "" && document.Permission != filter.Permission {
			continue
		}
		if filter.Status != "" && document.Status != filter.Status {
			continue
		}
		if filter.Query != "" && !strings.Contains(strings.ToLower(document.OriginalName), strings.ToLower(filter.Query)) {
			continue
		}
		documents = append(documents, document)
	}
	sort.Slice(documents, func(left, right int) bool {
		if documents[left].UploadedAt.Equal(documents[right].UploadedAt) {
			return documents[left].ID > documents[right].ID
		}
		return documents[left].UploadedAt.After(documents[right].UploadedAt)
	})
	total := len(documents)
	start := (filter.Page - 1) * filter.PageSize
	if start > total {
		start = total
	}
	end := start + filter.PageSize
	if end > total {
		end = total
	}
	pageDocuments := append([]model.DocumentUpload(nil), documents[start:end]...)
	totalPages := 0
	if total > 0 {
		totalPages = (total + filter.PageSize - 1) / filter.PageSize
	}
	return model.DocumentListResult{
		Documents:  pageDocuments,
		Total:      total,
		Page:       filter.Page,
		PageSize:   filter.PageSize,
		TotalPages: totalPages,
	}, nil
}

func (r *memoryDocumentRepository) SearchSimilar(_ context.Context, _ []float32, _ string, permissions []string, limit int) ([]model.RetrievedChunk, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	allowed := make(map[string]struct{}, len(permissions))
	for _, permission := range permissions {
		allowed[permission] = struct{}{}
	}
	results := make([]model.RetrievedChunk, 0, limit)
	for _, result := range r.searchResults {
		if _, ok := allowed[result.Permission]; !ok {
			continue
		}
		results = append(results, result)
		if len(results) == limit {
			break
		}
	}
	return results, nil
}

func (r *memoryDocumentRepository) chunksFor(documentID string) []model.DocumentChunk {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]model.DocumentChunk(nil), r.chunks[documentID]...)
}

func (r *memoryDocumentRepository) documentIndex(documentID string) int {
	for index := range r.documents {
		if r.documents[index].ID == documentID {
			return index
		}
	}
	return -1
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
