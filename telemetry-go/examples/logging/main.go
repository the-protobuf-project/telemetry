package main

import (
	"fmt"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go"
)

// ChatMessage represents a message in an LLM chat room
type ChatMessage struct {
	MessageID string `json:"message_id" telemetry:"attribute:message.id"`
	RoomID    string `json:"room_id" telemetry:"attribute:room.id"`
	UserID    string `json:"user_id" telemetry:"attribute:user.id"`
	Language  string `json:"language" telemetry:"attribute:message.language"`
	Type      string `json:"type" telemetry:"attribute:message.type"` // text, speech, llm_response

	// Additional data
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
}

// TranscriptionEvent represents speech-to-text processing
type TranscriptionEvent struct {
	TranscriptionID string  `json:"transcription_id" telemetry:"attribute:transcription.id"`
	UserID          string  `json:"user_id" telemetry:"attribute:user.id"`
	RoomID          string  `json:"room_id" telemetry:"attribute:room.id"`
	Language        string  `json:"language" telemetry:"attribute:transcription.language"`
	Status          string  `json:"status" telemetry:"attribute:transcription.status"` // success, failed
	Duration        float64 `json:"duration_ms" telemetry:"attribute:transcription.duration"`

	// Additional data
	AudioLength int64   `json:"audio_length_ms"`
	Model       string  `json:"model"`
	Confidence  float64 `json:"confidence,omitempty"`
	ErrorMsg    string  `json:"error_message,omitempty"`
}

// LLMRequestEvent represents LLM processing
type LLMRequestEvent struct {
	RequestID    string `json:"request_id" telemetry:"attribute:llm.request_id"`
	UserID       string `json:"user_id" telemetry:"attribute:user.id"`
	RoomID       string `json:"room_id" telemetry:"attribute:room.id"`
	Model        string `json:"model" telemetry:"attribute:llm.model"`
	Status       string `json:"status" telemetry:"attribute:llm.status"` // success, failed
	TokensUsed   int    `json:"tokens_used" telemetry:"attribute:llm.tokens"`
	ResponseTime int64  `json:"response_time_ms" telemetry:"attribute:llm.response_time"`

	// Additional data
	Prompt       string `json:"prompt"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// main demonstrates multilingual chat telemetry logging with structured messages,
// transcription events, and LLM request events.
func main() {
	// Uses telemetry.toml config for service info and OTLP endpoint
	p, err := telemetry.New().Build()
	if err != nil {
		panic(err)
	}
	defer func() {
		if err := p.Close(); err != nil {
			fmt.Printf("Error closing telemetry: %v\n", err)
		}
	}()

	p.Logger.Info("LLM Chat Room Started")
	p.Logger.Info("View logs in Grafana: http://localhost:3000")
	p.Logger.Warn("System ready for multilingual chat")
	if err := p.Logger.Error("Error logging test"); err != nil {
		fmt.Printf("Error logging test: %v\n", err)
	}

	// ========================================
	// Example 1: Traditional logging (without attributes)
	// ========================================
	p.Logger.Info("Traditional logging - map")
	p.Logger.Info("User joined room", map[string]interface{}{
		"user_id":  "user-alice",
		"room_id":  "room-ai-chat",
		"language": "en",
		"join_at":  time.Now().Unix(),
	})

	// ========================================
	// Example 2: Chat Messages with Attributes
	// ========================================
	p.Logger.Info("Chat session started")

	// English user sends text message
	msg1 := ChatMessage{
		MessageID: "msg-001",
		RoomID:    "room-ai-chat",
		UserID:    "user-alice",
		Language:  "en",
		Type:      "text",
		Content:   "Hello! Can you help me with Go programming?",
		Timestamp: time.Now().Unix(),
	}
	p.Logger.Info("User message received", msg1)

	// Spanish user sends voice message (transcribed)
	transcription1 := TranscriptionEvent{
		TranscriptionID: "trans-001",
		UserID:          "user-carlos",
		RoomID:          "room-ai-chat",
		Language:        "es",
		Status:          "success",
		Duration:        245.5,
		AudioLength:     3200,
		Model:           "whisper-large-v3",
		Confidence:      0.95,
	}
	p.Logger.Info("Speech transcribed", transcription1)

	msg2 := ChatMessage{
		MessageID: "msg-002",
		RoomID:    "room-ai-chat",
		UserID:    "user-carlos",
		Language:  "es",
		Type:      "speech",
		Content:   "¿Puedes explicarme cómo funcionan los canales en Go?",
		Timestamp: time.Now().Unix(),
	}
	p.Logger.Info("Voice message received", msg2)

	// LLM processes the request
	llmReq1 := LLMRequestEvent{
		RequestID:    "llm-req-001",
		UserID:       "user-alice",
		RoomID:       "room-ai-chat",
		Model:        "gpt-4",
		Status:       "success",
		TokensUsed:   450,
		ResponseTime: 1250,
		Prompt:       "Help with Go programming",
	}
	p.Logger.Info("LLM request processed", llmReq1)

	// LLM response
	llmResponse1 := ChatMessage{
		MessageID: "msg-003",
		RoomID:    "room-ai-chat",
		UserID:    "assistant",
		Language:  "en",
		Type:      "llm_response",
		Content:   "I'd be happy to help you with Go programming! Go is a statically typed...",
		Timestamp: time.Now().Unix(),
	}
	p.Logger.Info("LLM response sent", llmResponse1)

	// ========================================
	// Example 3: Failed Transcription
	// ========================================
	transcription2 := TranscriptionEvent{
		TranscriptionID: "trans-002",
		UserID:          "user-yuki",
		RoomID:          "room-ai-chat",
		Language:        "ja",
		Status:          "failed",
		Duration:        0,
		AudioLength:     150,
		Model:           "whisper-large-v3",
		ErrorMsg:        "Audio quality too low",
	}
	p.Logger.Warn("Transcription failed", transcription2)

	// ========================================
	// Example 4: Multiple Users in Different Languages
	// ========================================
	p.Logger.Info("Multilingual chat session")

	users := []struct {
		id   string
		lang string
		msg  string
	}{
		{"user-alice", "en", "How do I handle errors in Go?"},
		{"user-carlos", "es", "¿Qué son las goroutines?"},
		{"user-yuki", "ja", "Goのインターフェースについて教えてください"},
		{"user-marie", "fr", "Comment gérer la concurrence en Go?"},
	}

	for i, user := range users {
		msg := ChatMessage{
			MessageID: fmt.Sprintf("msg-%03d", 100+i),
			RoomID:    "room-ai-chat",
			UserID:    user.id,
			Language:  user.lang,
			Type:      "text",
			Content:   user.msg,
			Timestamp: time.Now().Unix(),
		}
		p.Logger.Info("Multilingual message", msg)

		// LLM processes each message
		llmReq := LLMRequestEvent{
			RequestID:    fmt.Sprintf("llm-req-%03d", 100+i),
			UserID:       user.id,
			RoomID:       "room-ai-chat",
			Model:        "gpt-4",
			Status:       "success",
			TokensUsed:   300 + (i * 50),
			ResponseTime: 1000 + int64(i*200),
			Prompt:       user.msg,
		}
		p.Logger.Info("LLM processing", llmReq)
	}

	p.Logger.Info("Demo completed!")
	p.Logger.Info("Query examples in Grafana:")
	p.Logger.Info("Logging - {room.id=\"room-ai-chat\"} - All chat room messages")
	p.Logger.Info("Logging - {message.language=\"es\"} - Spanish messages")
	p.Logger.Info("Logging - {llm.model=\"gpt-4\"} - GPT-4 requests")
	p.Logger.Info("Logging - {transcription.status=\"failed\"} - Failed transcriptions")
}
