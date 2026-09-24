package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

var ErrMissingLLMConfiguration = errors.New("missing LLM provider configuration")

type LLMProviderConfig struct {
	APIKey         string
	BaseURL        string
	EmbeddingModel string
}

// LoadDotEnv loads the first local .env file that exists without overwriting
// environment variables already supplied by the process environment.
func LoadDotEnv() (string, error) {
	candidates := []string{".env", "../.env"}
	for _, candidate := range candidates {
		_, err := os.Stat(candidate)
		switch {
		case err == nil:
			if err := loadDotEnvFile(candidate); err != nil {
				return "", fmt.Errorf("load %s: %w", candidate, err)
			}
			return candidate, nil
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return "", fmt.Errorf("inspect %s: %w", candidate, err)
		}
	}
	return "", nil
}

func loadDotEnvFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, rawValue, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !validEnvironmentKey(key) {
			return fmt.Errorf("line %d has an invalid environment assignment", lineNumber)
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value, err := parseEnvironmentValue(strings.TrimSpace(rawValue))
		if err != nil {
			return fmt.Errorf("line %d has an invalid value: %w", lineNumber, err)
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read file: %w", err)
	}
	return nil
}

func parseEnvironmentValue(value string) (string, error) {
	if len(value) >= 2 && value[0] == '\'' && value[len(value)-1] == '\'' {
		return value[1 : len(value)-1], nil
	}
	if strings.HasPrefix(value, `"`) {
		return strconv.Unquote(value)
	}
	return value, nil
}

func validEnvironmentKey(key string) bool {
	if key == "" || !isEnvironmentKeyStart(key[0]) {
		return false
	}
	for index := 1; index < len(key); index++ {
		character := key[index]
		if !isEnvironmentKeyStart(character) && (character < '0' || character > '9') {
			return false
		}
	}
	return true
}

func isEnvironmentKeyStart(character byte) bool {
	return character == '_' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z'
}

func LoadLLMProviderConfigFromEnv() (LLMProviderConfig, error) {
	configuration := LLMProviderConfig{
		APIKey:         strings.TrimSpace(os.Getenv("LLM_API_KEY")),
		BaseURL:        strings.TrimSpace(os.Getenv("LLM_BASE_URL")),
		EmbeddingModel: strings.TrimSpace(os.Getenv("LLM_EMBEDDING_MODEL")),
	}

	missing := make([]string, 0, 3)
	if configuration.APIKey == "" {
		missing = append(missing, "LLM_API_KEY")
	}
	if configuration.BaseURL == "" {
		missing = append(missing, "LLM_BASE_URL")
	}
	if configuration.EmbeddingModel == "" {
		missing = append(missing, "LLM_EMBEDDING_MODEL")
	}
	if len(missing) > 0 {
		return LLMProviderConfig{}, fmt.Errorf("%w: %s", ErrMissingLLMConfiguration, strings.Join(missing, ", "))
	}
	return configuration, nil
}
