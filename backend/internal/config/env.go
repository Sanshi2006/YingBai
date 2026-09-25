package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	ErrMissingLLMConfiguration     = errors.New("missing LLM provider configuration")
	ErrMissingChatLLMConfiguration = errors.New("missing chat LLM provider configuration")
)

const (
	DefaultRetrievalTopK          = 5
	MaxRetrievalTopK              = 20
	DefaultRAGSimilarityThreshold = 0.55
	MinRAGSimilarityThreshold     = 0.0
	MaxRAGSimilarityThreshold     = 1.0
)

type LLMProviderConfig struct {
	APIKey         string
	BaseURL        string
	EmbeddingModel string
}

type ChatLLMProviderConfig struct {
	APIKey  string
	BaseURL string
	Model   string
}

type RetrievalConfig struct {
	TopK                int
	SimilarityThreshold float64
}

// LoadDotEnv loads the first local .env file that exists without overwriting
// environment variables already supplied by the process environment.
func LoadDotEnv() (string, error) {
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}

	directory := workingDirectory
	for depth := 0; depth <= 5; depth++ {
		candidate := filepath.Join(directory, ".env")
		_, err := os.Stat(candidate)
		switch {
		case err == nil:
			if err := loadDotEnvFile(candidate); err != nil {
				return "", fmt.Errorf("load %s: %w", candidate, err)
			}
			return candidate, nil
		case errors.Is(err, os.ErrNotExist):
		default:
			return "", fmt.Errorf("inspect %s: %w", candidate, err)
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
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

func LoadRetrievalConfigFromEnv() (RetrievalConfig, error) {
	rawTopK := strings.TrimSpace(os.Getenv("RAG_TOP_K"))
	rawThreshold := strings.TrimSpace(os.Getenv("RAG_SIMILARITY_THRESHOLD"))
	configuration := RetrievalConfig{
		TopK:                DefaultRetrievalTopK,
		SimilarityThreshold: DefaultRAGSimilarityThreshold,
	}
	if rawTopK == "" {
		rawTopK = strconv.Itoa(DefaultRetrievalTopK)
	}

	topK, err := strconv.Atoi(rawTopK)
	if err != nil || topK < 1 || topK > MaxRetrievalTopK {
		return RetrievalConfig{}, fmt.Errorf("RAG_TOP_K must be an integer between 1 and %d", MaxRetrievalTopK)
	}
	configuration.TopK = topK
	if rawThreshold == "" {
		return configuration, nil
	}
	threshold, err := strconv.ParseFloat(rawThreshold, 64)
	if err != nil || threshold < MinRAGSimilarityThreshold || threshold > MaxRAGSimilarityThreshold {
		return RetrievalConfig{}, fmt.Errorf("RAG_SIMILARITY_THRESHOLD must be a number between %.1f and %.1f", MinRAGSimilarityThreshold, MaxRAGSimilarityThreshold)
	}
	configuration.SimilarityThreshold = threshold
	return configuration, nil
}

func LoadChatLLMProviderConfigFromEnv() (ChatLLMProviderConfig, error) {
	configuration := ChatLLMProviderConfig{
		APIKey:  strings.TrimSpace(os.Getenv("LLM_CHAT_API_KEY")),
		BaseURL: strings.TrimSpace(os.Getenv("LLM_CHAT_BASE_URL")),
		Model:   strings.TrimSpace(os.Getenv("LLM_CHAT_MODEL")),
	}

	missing := make([]string, 0, 3)
	if configuration.APIKey == "" {
		missing = append(missing, "LLM_CHAT_API_KEY")
	}
	if configuration.BaseURL == "" {
		missing = append(missing, "LLM_CHAT_BASE_URL")
	}
	if configuration.Model == "" {
		missing = append(missing, "LLM_CHAT_MODEL")
	}
	if len(missing) > 0 {
		return ChatLLMProviderConfig{}, fmt.Errorf("%w: %s", ErrMissingChatLLMConfiguration, strings.Join(missing, ", "))
	}
	return configuration, nil
}
