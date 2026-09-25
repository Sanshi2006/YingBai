package llm

import (
	"context"
	"errors"
	"strings"
	"sync"
)

var ErrFakeChatResponsesExhausted = errors.New("fake chat responses exhausted")

type FakeChatLLMProvider struct {
	mu        sync.Mutex
	responses []string
	requests  [][]ChatMessage
}

func NewFakeChatLLMProvider(responses ...string) *FakeChatLLMProvider {
	return &FakeChatLLMProvider{responses: append([]string(nil), responses...)}
}

func (p *FakeChatLLMProvider) Complete(ctx context.Context, messages []ChatMessage, options ChatCompletionOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := validateChatCompletionRequest(messages, options); err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, append([]ChatMessage(nil), messages...))
	if len(p.responses) == 0 {
		return "", ErrFakeChatResponsesExhausted
	}
	response := strings.TrimSpace(p.responses[0])
	p.responses = p.responses[1:]
	if response == "" {
		return "", ErrInvalidChatResponse
	}
	return response, nil
}

func (p *FakeChatLLMProvider) ChatModel() string {
	return "fake-chat"
}

func (p *FakeChatLLMProvider) Requests() [][]ChatMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	requests := make([][]ChatMessage, len(p.requests))
	for index := range p.requests {
		requests[index] = append([]ChatMessage(nil), p.requests[index]...)
	}
	return requests
}

var _ ChatLLMProvider = (*FakeChatLLMProvider)(nil)
