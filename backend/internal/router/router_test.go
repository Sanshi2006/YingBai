package router_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"project-for-yingbai/backend/internal/router"
)

func TestHealth(t *testing.T) {
	engine := router.New("")
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
	engine := router.New("")
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
	engine := router.New("")
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

	engine := router.New(mobileDir)
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
