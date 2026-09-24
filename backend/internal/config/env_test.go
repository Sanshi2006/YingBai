package config

import (
	"errors"
	"os"
	"path/filepath"
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
