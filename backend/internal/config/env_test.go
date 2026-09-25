package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDotEnvFileLoadsValuesWithoutOverwritingProcessEnvironment(t *testing.T) {
	const loadedKey = "YINGBAI_DOTENV_TEST_LOADED"
	previous, existed := os.LookupEnv(loadedKey)
	if err := os.Unsetenv(loadedKey); err != nil {
		t.Fatalf("unset test environment: %v", err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(loadedKey, previous)
		} else {
			_ = os.Unsetenv(loadedKey)
		}
	})
	t.Setenv("YINGBAI_DOTENV_TEST_EXISTING", "process-value")

	path := filepath.Join(t.TempDir(), ".env")
	content := "YINGBAI_DOTENV_TEST_LOADED=loaded-value\nYINGBAI_DOTENV_TEST_EXISTING=file-value\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test .env: %v", err)
	}
	if err := loadDotEnvFile(path); err != nil {
		t.Fatalf("loadDotEnvFile() error = %v", err)
	}
	if got := os.Getenv(loadedKey); got != "loaded-value" {
		t.Fatalf("loaded value = %q, want loaded-value", got)
	}
	if got := os.Getenv("YINGBAI_DOTENV_TEST_EXISTING"); got != "process-value" {
		t.Fatalf("process environment was overwritten: %q", got)
	}
}

func TestLoadDotEnvFindsProjectFileFromNestedDirectory(t *testing.T) {
	const loadedKey = "YINGBAI_DOTENV_NESTED_TEST"
	previousValue, valueExisted := os.LookupEnv(loadedKey)
	if err := os.Unsetenv(loadedKey); err != nil {
		t.Fatalf("unset nested test environment: %v", err)
	}
	t.Cleanup(func() {
		if valueExisted {
			_ = os.Setenv(loadedKey, previousValue)
		} else {
			_ = os.Unsetenv(loadedKey)
		}
	})

	root := t.TempDir()
	nested := filepath.Join(root, "backend", "internal", "llm")
	if err := os.MkdirAll(nested, 0o750); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(loadedKey+"=nested-value\n"), 0o600); err != nil {
		t.Fatalf("write root .env: %v", err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	if err := os.Chdir(nested); err != nil {
		t.Fatalf("change to nested directory: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	loadedPath, err := LoadDotEnv()
	if err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}
	if loadedPath != filepath.Join(root, ".env") || os.Getenv(loadedKey) != "nested-value" {
		t.Fatalf("loaded path=%q value=%q", loadedPath, os.Getenv(loadedKey))
	}
}

func TestLoadLLMProviderConfigFromEnv(t *testing.T) {
	t.Setenv("LLM_API_KEY", " test-key ")
	t.Setenv("LLM_BASE_URL", " https://example.com/v1/ ")
	t.Setenv("LLM_EMBEDDING_MODEL", " embedding-model ")

	configuration, err := LoadLLMProviderConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadLLMProviderConfigFromEnv() error = %v", err)
	}
	if configuration.APIKey != "test-key" || configuration.BaseURL != "https://example.com/v1/" || configuration.EmbeddingModel != "embedding-model" {
		t.Fatalf("unexpected configuration: %#v", configuration)
	}
}

func TestLoadLLMProviderConfigFromEnvReportsAllMissingVariables(t *testing.T) {
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_EMBEDDING_MODEL", "")

	_, err := LoadLLMProviderConfigFromEnv()
	if !errors.Is(err, ErrMissingLLMConfiguration) {
		t.Fatalf("error = %v, want ErrMissingLLMConfiguration", err)
	}
}

func TestLoadRetrievalConfigFromEnv(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		t.Setenv("RAG_TOP_K", "")
		t.Setenv("RAG_SIMILARITY_THRESHOLD", "")
		configuration, err := LoadRetrievalConfigFromEnv()
		if err != nil {
			t.Fatalf("LoadRetrievalConfigFromEnv() error = %v", err)
		}
		if configuration.TopK != DefaultRetrievalTopK {
			t.Fatalf("TopK = %d, want %d", configuration.TopK, DefaultRetrievalTopK)
		}
		if configuration.SimilarityThreshold != DefaultRAGSimilarityThreshold {
			t.Fatalf("SimilarityThreshold = %f, want %f", configuration.SimilarityThreshold, DefaultRAGSimilarityThreshold)
		}
	})

	t.Run("configured", func(t *testing.T) {
		t.Setenv("RAG_TOP_K", " 8 ")
		t.Setenv("RAG_SIMILARITY_THRESHOLD", " 0.72 ")
		configuration, err := LoadRetrievalConfigFromEnv()
		if err != nil {
			t.Fatalf("LoadRetrievalConfigFromEnv() error = %v", err)
		}
		if configuration.TopK != 8 {
			t.Fatalf("TopK = %d, want 8", configuration.TopK)
		}
		if configuration.SimilarityThreshold != 0.72 {
			t.Fatalf("SimilarityThreshold = %f, want 0.72", configuration.SimilarityThreshold)
		}
	})

	for _, value := range []string{"zero", "0", "21"} {
		t.Run("invalid_"+value, func(t *testing.T) {
			t.Setenv("RAG_TOP_K", value)
			if _, err := LoadRetrievalConfigFromEnv(); err == nil {
				t.Fatalf("expected RAG_TOP_K=%q to fail", value)
			}
		})
	}

	for _, value := range []string{"invalid", "-0.1", "1.1"} {
		t.Run("invalid_threshold_"+value, func(t *testing.T) {
			t.Setenv("RAG_TOP_K", "5")
			t.Setenv("RAG_SIMILARITY_THRESHOLD", value)
			if _, err := LoadRetrievalConfigFromEnv(); err == nil {
				t.Fatalf("expected RAG_SIMILARITY_THRESHOLD=%q to fail", value)
			}
		})
	}
}

func TestLoadChatLLMProviderConfigFromEnv(t *testing.T) {
	t.Setenv("LLM_CHAT_API_KEY", " chat-key ")
	t.Setenv("LLM_CHAT_BASE_URL", " https://api.deepseek.com ")
	t.Setenv("LLM_CHAT_MODEL", " deepseek-flash ")

	configuration, err := LoadChatLLMProviderConfigFromEnv()
	if err != nil {
		t.Fatalf("LoadChatLLMProviderConfigFromEnv() error = %v", err)
	}
	if configuration.APIKey != "chat-key" || configuration.BaseURL != "https://api.deepseek.com" || configuration.Model != "deepseek-flash" {
		t.Fatalf("unexpected configuration: %#v", configuration)
	}
}

func TestLoadChatLLMProviderConfigFromEnvReportsAllMissingVariables(t *testing.T) {
	t.Setenv("LLM_CHAT_API_KEY", "")
	t.Setenv("LLM_CHAT_BASE_URL", "")
	t.Setenv("LLM_CHAT_MODEL", "")

	_, err := LoadChatLLMProviderConfigFromEnv()
	if !errors.Is(err, ErrMissingChatLLMConfiguration) {
		t.Fatalf("error = %v, want ErrMissingChatLLMConfiguration", err)
	}
	for _, variable := range []string{"LLM_CHAT_API_KEY", "LLM_CHAT_BASE_URL", "LLM_CHAT_MODEL"} {
		if !strings.Contains(err.Error(), variable) {
			t.Fatalf("error %q does not report %s", err, variable)
		}
	}
}
