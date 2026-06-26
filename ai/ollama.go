// Package ai provides a thin client for the Ollama local inference API.
// This file is identical across all projects in the "Build 10 AI Projects in 30 Days" series.
// It exposes two functions: Chat (blocking, single response) and ChatStream (streaming via NDJSON).
package ai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// ── Constants ──────────────────────────────────────────────────────────────────

const (
	// DefaultModel is the model used across all projects unless overridden.
	DefaultModel = "llama3.2:3b"

	// VisionModel is used only in projects that process images.
	VisionModel = "llava:7b"

	ollamaBaseURL = "http://localhost:11434"
	chatEndpoint  = ollamaBaseURL + "/api/chat"
)

// ── Types ──────────────────────────────────────────────────────────────────────

// Message represents a single turn in the conversation history.
// Ollama's /api/chat endpoint accepts a slice of these.
type Message struct {
	Role    string   `json:"role"`
	Content string   `json:"content"`
	Images  []string `json:"images,omitempty"` // base64-encoded, used by vision models only
}

// chatRequest is the body sent to Ollama's /api/chat endpoint.
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

// streamChunk is one NDJSON line returned when stream:true.
type streamChunk struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
}

// ── Chat (blocking) ────────────────────────────────────────────────────────────

// Chat sends a complete conversation history to Ollama and returns the full
// assistant reply as a single string. It uses stream:false so the response is
// one JSON object, not a stream of NDJSON lines.
//
// This is the function used by the agent loop in Project 09 because the agent
// needs the COMPLETE response before it can parse Thought/Action/Final Answer.
func Chat(model string, messages []Message) (string, error) {
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   false,
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	resp, err := http.Post(chatEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", chatEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama returned %d: %s", resp.StatusCode, raw)
	}

	// With stream:false, Ollama returns a single JSON object that looks like
	// one stream chunk with done:true.
	var chunk streamChunk
	if err := json.NewDecoder(resp.Body).Decode(&chunk); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return chunk.Message.Content, nil
}

// ── ChatStream (token-by-token) ────────────────────────────────────────────────

// ChatStream sends a conversation to Ollama with stream:true and calls onChunk
// for every text token as it arrives. The caller can use onChunk to forward
// tokens to an SSE connection.
//
// Returns an error if the HTTP request fails, a chunk cannot be decoded, or
// onChunk returns an error (e.g. the client disconnected).
func ChatStream(model string, messages []Message, onChunk func(string) error) error {
	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	resp, err := http.Post(chatEndpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST %s: %w", chatEndpoint, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama returned %d: %s", resp.StatusCode, raw)
	}

	// Ollama streams NDJSON: one JSON object per line, ending when done:true.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var chunk streamChunk
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			return fmt.Errorf("decode chunk: %w", err)
		}
		if chunk.Message.Content != "" {
			if err := onChunk(chunk.Message.Content); err != nil {
				return err
			}
		}
		if chunk.Done {
			break
		}
	}

	return scanner.Err()
}
