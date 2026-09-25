package service

import (
	"context"
	"errors"
	"fmt"

	"project-for-yingbai/backend/internal/llm"
)

const RAGRefusalAnswer = "知识库暂无依据，请转人工"

var ErrRAGAnswerGeneration = errors.New("RAG answer generation failed")

type RAGCitation struct {
	SourceID     string  `json:"sourceId"`
	DocumentID   string  `json:"documentId"`
	OriginalName string  `json:"originalName"`
	ChunkIndex   int     `json:"chunkIndex"`
	Similarity   float64 `json:"similarity"`
}

type RAGAnswer struct {
	Answer    string        `json:"answer"`
	Type      string        `json:"type"`
	Citations []RAGCitation `json:"citations"`
}

type RAGAnswerService struct {
	preparation *RAGPreparationService
	chatLLM     llm.ChatLLMProvider
}

func NewRAGAnswerService(preparation *RAGPreparationService, chatLLM llm.ChatLLMProvider) (*RAGAnswerService, error) {
	if preparation == nil {
		return nil, errors.New("RAG preparation service is not configured")
	}
	if chatLLM == nil {
		return nil, errors.New("chat LLM provider is not configured")
	}
	return &RAGAnswerService{preparation: preparation, chatLLM: chatLLM}, nil
}

func (s *RAGAnswerService) Answer(ctx context.Context, question, role string) (RAGAnswer, error) {
	preparation, err := s.preparation.Prepare(ctx, question, role)
	if err != nil {
		return RAGAnswer{}, err
	}
	if len(preparation.Chunks) == 0 {
		return RAGAnswer{
			Answer:    RAGRefusalAnswer,
			Type:      "refusal",
			Citations: make([]RAGCitation, 0),
		}, nil
	}

	answer, err := s.chatLLM.Complete(ctx, preparation.AnswerMessages, llm.ChatCompletionOptions{
		Temperature: 0.2,
		MaxTokens:   4096,
	})
	if err != nil {
		return RAGAnswer{}, fmt.Errorf("%w: %v", ErrRAGAnswerGeneration, err)
	}
	citations := make([]RAGCitation, len(preparation.Chunks))
	for index, chunk := range preparation.Chunks {
		citations[index] = RAGCitation{
			SourceID:     fmt.Sprintf("S%d", index+1),
			DocumentID:   chunk.DocumentID,
			OriginalName: chunk.OriginalName,
			ChunkIndex:   chunk.ChunkIndex,
			Similarity:   chunk.Similarity,
		}
	}
	return RAGAnswer{Answer: answer, Type: "knowledge", Citations: citations}, nil
}
