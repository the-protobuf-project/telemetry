package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go"
)

// Malenia Conversation Pipeline - AI Assistant Tracing Example
// This example demonstrates distributed tracing for a conversational AI assistant named Malenia.
// The pipeline includes 7 main components, each traced as a separate span with detailed events:
// 1. Input Processing - Validate and normalize user input
// 2. Context Retrieval - Fetch conversation history and user context
// 3. Intent Classification - Determine user intent and extract entities
// 4. Knowledge Search - Search knowledge base for relevant information
// 5. Response Generation - Generate AI response using LLM
// 6. Response Validation - Validate and filter response
// 7. Output Formatting - Format response for delivery

// ConversationRequest represents the input for the conversation pipeline
type ConversationRequest struct {
	RequestID     string `telemetry:"trace:request.id"`
	UserID        string `telemetry:"trace:user.id"`
	SessionID     string `telemetry:"trace:session.id"`
	UserMessage   string `telemetry:"trace:input.message"`
	MessageLength int    `telemetry:"trace:input.length"`
	Timestamp     string `telemetry:"trace:request.timestamp"`
}

// InputProcessingRequest represents input validation and normalization
type InputProcessingRequest struct {
	RequestID   string `telemetry:"trace:request.id"`
	RawInput    string `telemetry:"trace:input.raw"`
	InputLength int    `telemetry:"trace:input.length"`
	Language    string `telemetry:"trace:input.language"`
}

// InputProcessingResponse represents processed input
type InputProcessingResponse struct {
	RequestID        string  `telemetry:"trace:request.id"`
	ProcessedInput   string  `telemetry:"trace:input.processed"`
	IsValid          bool    `telemetry:"trace:input.valid"`
	TokenCount       int     `telemetry:"trace:input.tokens"`
	DetectedLanguage string  `telemetry:"trace:input.detected_language"`
	ProcessingTimeMs float64 `telemetry:"trace:processing_time_ms"`
}

// ContextRetrievalRequest represents fetching conversation context
type ContextRetrievalRequest struct {
	RequestID string `telemetry:"trace:request.id"`
	UserID    string `telemetry:"trace:user.id"`
	SessionID string `telemetry:"trace:session.id"`
	MaxTurns  int    `telemetry:"trace:context.max_turns"`
}

// ContextRetrievalResponse represents retrieved context
type ContextRetrievalResponse struct {
	RequestID        string   `telemetry:"trace:request.id"`
	HistoryTurns     int      `telemetry:"trace:context.history_turns"`
	UserPreferences  []string `telemetry:"trace:context.preferences"`
	ContextTokens    int      `telemetry:"trace:context.tokens"`
	CacheHit         bool     `telemetry:"trace:context.cache_hit"`
	ProcessingTimeMs float64  `telemetry:"trace:processing_time_ms"`
}

// IntentClassificationRequest represents intent detection
type IntentClassificationRequest struct {
	RequestID string `telemetry:"trace:request.id"`
	Message   string `telemetry:"trace:intent.message"`
	Context   string `telemetry:"trace:intent.context"`
}

// IntentClassificationResponse represents detected intent
type IntentClassificationResponse struct {
	RequestID        string   `telemetry:"trace:request.id"`
	Intent           string   `telemetry:"trace:intent.name"`
	Confidence       float64  `telemetry:"trace:intent.confidence"`
	Entities         []string `telemetry:"trace:intent.entities"`
	EntityCount      int      `telemetry:"trace:intent.entity_count"`
	ProcessingTimeMs float64  `telemetry:"trace:processing_time_ms"`
}

// KnowledgeSearchRequest represents knowledge base search
type KnowledgeSearchRequest struct {
	RequestID string  `telemetry:"trace:request.id"`
	Query     string  `telemetry:"trace:search.query"`
	Intent    string  `telemetry:"trace:search.intent"`
	TopK      int     `telemetry:"trace:search.top_k"`
	Threshold float64 `telemetry:"trace:search.threshold"`
}

// KnowledgeSearchResponse represents search results
type KnowledgeSearchResponse struct {
	RequestID        string   `telemetry:"trace:request.id"`
	ResultCount      int      `telemetry:"trace:search.result_count"`
	DocumentIDs      []string `telemetry:"trace:search.document_ids"`
	AvgRelevance     float64  `telemetry:"trace:search.avg_relevance"`
	CacheHit         bool     `telemetry:"trace:search.cache_hit"`
	ProcessingTimeMs float64  `telemetry:"trace:processing_time_ms"`
}

// ResponseGenerationRequest represents LLM response generation
type ResponseGenerationRequest struct {
	RequestID        string  `telemetry:"trace:request.id"`
	UserMessage      string  `telemetry:"trace:llm.user_message"`
	SystemPrompt     string  `telemetry:"trace:llm.system_prompt"`
	Context          string  `telemetry:"trace:llm.context"`
	KnowledgeContext string  `telemetry:"trace:llm.knowledge"`
	ModelName        string  `telemetry:"trace:llm.model"`
	Temperature      float64 `telemetry:"trace:llm.temperature"`
	MaxTokens        int     `telemetry:"trace:llm.max_tokens"`
}

// ResponseGenerationResponse represents generated response
type ResponseGenerationResponse struct {
	RequestID        string  `telemetry:"trace:request.id"`
	Response         string  `telemetry:"trace:llm.response"`
	TokensPrompt     int     `telemetry:"trace:llm.tokens_prompt"`
	TokensCompletion int     `telemetry:"trace:llm.tokens_completion"`
	TokensTotal      int     `telemetry:"trace:llm.tokens_total"`
	FinishReason     string  `telemetry:"trace:llm.finish_reason"`
	ProcessingTimeMs float64 `telemetry:"trace:processing_time_ms"`
}

// ResponseValidationRequest represents response validation
type ResponseValidationRequest struct {
	RequestID      string `telemetry:"trace:request.id"`
	Response       string `telemetry:"trace:validation.response"`
	ResponseLength int    `telemetry:"trace:validation.length"`
}

// ResponseValidationResponse represents validation results
type ResponseValidationResponse struct {
	RequestID        string  `telemetry:"trace:request.id"`
	IsValid          bool    `telemetry:"trace:validation.is_valid"`
	IsSafe           bool    `telemetry:"trace:validation.is_safe"`
	HasPII           bool    `telemetry:"trace:validation.has_pii"`
	ToxicityScore    float64 `telemetry:"trace:validation.toxicity_score"`
	ProcessingTimeMs float64 `telemetry:"trace:processing_time_ms"`
}

// OutputFormattingRequest represents output formatting
type OutputFormattingRequest struct {
	RequestID string `telemetry:"trace:request.id"`
	Response  string `telemetry:"trace:output.raw_response"`
	Format    string `telemetry:"trace:output.format"`
}

// OutputFormattingResponse represents formatted output
type OutputFormattingResponse struct {
	RequestID        string  `telemetry:"trace:request.id"`
	FormattedOutput  string  `telemetry:"trace:output.formatted"`
	OutputLength     int     `telemetry:"trace:output.length"`
	Markdown         bool    `telemetry:"trace:output.markdown"`
	ProcessingTimeMs float64 `telemetry:"trace:processing_time_ms"`
}

func main() {
	// Run the Malenia conversation pipeline example
	runMaleniaConversationPipeline()

	// Run error handling example
	runMaleniaWithError()
}

func runMaleniaConversationPipeline() {
	// Uses telemetry.toml config for service info and OTLP endpoint
	k, err := telemetry.New().Build()
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = k.Close()
	}()

	k.Logger.Info("=== Malenia Conversation Pipeline ===", nil)
	k.Logger.Info("Processing user conversation with 7 traced components", nil)

	// Simulate a user conversation
	conversationReq := ConversationRequest{
		RequestID:     "conv-12345",
		UserID:        "user-789",
		SessionID:     "session-abc",
		UserMessage:   "What are the best practices for distributed tracing in microservices?",
		MessageLength: 73,
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	// Process the conversation pipeline
	ctx := context.Background()
	err = processMaleniaConversation(ctx, k, conversationReq)
	if err != nil {
		_ = k.Logger.Error("Conversation failed", map[string]interface{}{"error": err.Error()})
	} else {
		k.Logger.Info("✅ Conversation completed successfully!", nil)
	}

	// Give time for spans to be exported
	time.Sleep(2 * time.Second)
}

// processMaleniaConversation orchestrates the complete conversation pipeline with 7 traced components
func processMaleniaConversation(ctx context.Context, k *telemetry.Telemetry, req ConversationRequest) error {
	// Create root span for the entire conversation pipeline
	return k.Tracing.Trace(ctx, "MaleniaConversationPipeline", req, func(ctx context.Context, span *telemetry.Span) error {
		span.AddEvent("conversation_started")
		span.SetAttribute("assistant.name", "Malenia")
		span.SetAttribute("assistant.version", "1.0.0")

		var totalTime float64

		// Component 1: Input Processing
		span.AddEvent("component_1_input_processing")
		inputResp, err := processInput(ctx, k, InputProcessingRequest{
			RequestID:   req.RequestID,
			RawInput:    req.UserMessage,
			InputLength: req.MessageLength,
			Language:    "en",
		})
		if err != nil {
			return fmt.Errorf("input processing failed: %w", err)
		}
		span.SetAttribute("input.valid", inputResp.IsValid)
		span.SetAttribute("input.tokens", inputResp.TokenCount)
		totalTime += inputResp.ProcessingTimeMs

		// Component 2: Context Retrieval
		span.AddEvent("component_2_context_retrieval")
		contextResp, err := retrieveContext(ctx, k, ContextRetrievalRequest{
			RequestID: req.RequestID,
			UserID:    req.UserID,
			SessionID: req.SessionID,
			MaxTurns:  10,
		})
		if err != nil {
			return fmt.Errorf("context retrieval failed: %w", err)
		}
		span.SetAttribute("context.history_turns", contextResp.HistoryTurns)
		span.SetAttribute("context.cache_hit", contextResp.CacheHit)
		totalTime += contextResp.ProcessingTimeMs

		// Component 3: Intent Classification
		span.AddEvent("component_3_intent_classification")
		intentResp, err := classifyIntent(ctx, k, IntentClassificationRequest{
			RequestID: req.RequestID,
			Message:   inputResp.ProcessedInput,
			Context:   "conversation_history",
		})
		if err != nil {
			return fmt.Errorf("intent classification failed: %w", err)
		}
		span.SetAttribute("intent.name", intentResp.Intent)
		span.SetAttribute("intent.confidence", intentResp.Confidence)
		totalTime += intentResp.ProcessingTimeMs

		// Component 4: Knowledge Search
		span.AddEvent("component_4_knowledge_search")
		searchResp, err := searchKnowledge(ctx, k, KnowledgeSearchRequest{
			RequestID: req.RequestID,
			Query:     inputResp.ProcessedInput,
			Intent:    intentResp.Intent,
			TopK:      5,
			Threshold: 0.7,
		})
		if err != nil {
			return fmt.Errorf("knowledge search failed: %w", err)
		}
		span.SetAttribute("search.result_count", searchResp.ResultCount)
		span.SetAttribute("search.avg_relevance", searchResp.AvgRelevance)
		totalTime += searchResp.ProcessingTimeMs

		// Component 5: Response Generation
		span.AddEvent("component_5_response_generation")
		responseResp, err := generateResponse(ctx, k, ResponseGenerationRequest{
			RequestID:        req.RequestID,
			UserMessage:      inputResp.ProcessedInput,
			SystemPrompt:     "You are Malenia, a helpful AI assistant specializing in software engineering and distributed systems.",
			Context:          fmt.Sprintf("History: %d turns", contextResp.HistoryTurns),
			KnowledgeContext: fmt.Sprintf("Found %d relevant documents", searchResp.ResultCount),
			ModelName:        "gpt-4-turbo",
			Temperature:      0.7,
			MaxTokens:        500,
		})
		if err != nil {
			return fmt.Errorf("response generation failed: %w", err)
		}
		span.SetAttribute("llm.tokens_total", responseResp.TokensTotal)
		span.SetAttribute("llm.finish_reason", responseResp.FinishReason)
		totalTime += responseResp.ProcessingTimeMs

		// Component 6: Response Validation
		span.AddEvent("component_6_response_validation")
		validationResp, err := validateResponse(ctx, k, ResponseValidationRequest{
			RequestID:      req.RequestID,
			Response:       responseResp.Response,
			ResponseLength: len(responseResp.Response),
		})
		if err != nil {
			return fmt.Errorf("response validation failed: %w", err)
		}
		span.SetAttribute("validation.is_valid", validationResp.IsValid)
		span.SetAttribute("validation.is_safe", validationResp.IsSafe)
		totalTime += validationResp.ProcessingTimeMs

		// Component 7: Output Formatting
		span.AddEvent("component_7_output_formatting")
		outputResp, err := formatOutput(ctx, k, OutputFormattingRequest{
			RequestID: req.RequestID,
			Response:  responseResp.Response,
			Format:    "markdown",
		})
		if err != nil {
			return fmt.Errorf("output formatting failed: %w", err)
		}
		span.SetAttribute("output.length", outputResp.OutputLength)
		span.SetAttribute("output.markdown", outputResp.Markdown)
		totalTime += outputResp.ProcessingTimeMs

		span.AddEvent("conversation_completed")
		span.SetAttribute("pipeline.total_time_ms", totalTime)
		span.SetAttribute("pipeline.components_completed", 7)
		span.SetAttribute("pipeline.success", true)

		return nil
	})
}

// Component 1: Input Processing
func processInput(ctx context.Context, k *telemetry.Telemetry, req InputProcessingRequest) (*InputProcessingResponse, error) {
	_, span := k.Tracing.Start(ctx, "InputProcessing", req)
	defer span.End()

	span.AddEvent("validating_input")
	time.Sleep(15 * time.Millisecond)

	span.AddEvent("normalizing_text")
	time.Sleep(20 * time.Millisecond)

	span.AddEvent("detecting_language")
	time.Sleep(10 * time.Millisecond)

	span.AddEvent("tokenizing")
	time.Sleep(25 * time.Millisecond)

	response := &InputProcessingResponse{
		RequestID:        req.RequestID,
		ProcessedInput:   req.RawInput,
		IsValid:          true,
		TokenCount:       18,
		DetectedLanguage: "en",
		ProcessingTimeMs: 70.0,
	}

	span.AddEvent("input_processing_complete")
	span.SetOK()

	return response, nil
}

// Component 2: Context Retrieval
func retrieveContext(ctx context.Context, k *telemetry.Telemetry, req ContextRetrievalRequest) (*ContextRetrievalResponse, error) {
	_, span := k.Tracing.Start(ctx, "ContextRetrieval", req)
	defer span.End()

	span.AddEvent("checking_cache")
	time.Sleep(5 * time.Millisecond)

	span.AddEvent("fetching_conversation_history")
	time.Sleep(40 * time.Millisecond)

	span.AddEvent("loading_user_preferences")
	time.Sleep(30 * time.Millisecond)

	span.AddEvent("aggregating_context")
	time.Sleep(15 * time.Millisecond)

	response := &ContextRetrievalResponse{
		RequestID:        req.RequestID,
		HistoryTurns:     5,
		UserPreferences:  []string{"technical", "detailed", "examples"},
		ContextTokens:    450,
		CacheHit:         true,
		ProcessingTimeMs: 90.0,
	}

	span.AddEvent("context_retrieval_complete")
	span.SetOK()

	return response, nil
}

// Component 3: Intent Classification
func classifyIntent(ctx context.Context, k *telemetry.Telemetry, req IntentClassificationRequest) (*IntentClassificationResponse, error) {
	_, span := k.Tracing.Start(ctx, "IntentClassification", req)
	defer span.End()

	span.AddEvent("loading_classifier_model")
	time.Sleep(20 * time.Millisecond)

	span.AddEvent("extracting_features")
	time.Sleep(30 * time.Millisecond)

	span.AddEvent("running_classification")
	time.Sleep(50 * time.Millisecond)

	span.AddEvent("extracting_entities")
	time.Sleep(35 * time.Millisecond)

	response := &IntentClassificationResponse{
		RequestID:        req.RequestID,
		Intent:           "technical_question",
		Confidence:       0.94,
		Entities:         []string{"distributed_tracing", "microservices", "best_practices"},
		EntityCount:      3,
		ProcessingTimeMs: 135.0,
	}

	span.AddEvent("intent_classification_complete")
	span.SetOK()

	return response, nil
}

// Component 4: Knowledge Search
func searchKnowledge(ctx context.Context, k *telemetry.Telemetry, req KnowledgeSearchRequest) (*KnowledgeSearchResponse, error) {
	_, span := k.Tracing.Start(ctx, "KnowledgeSearch", req)
	defer span.End()

	span.AddEvent("generating_query_embedding")
	time.Sleep(45 * time.Millisecond)

	span.AddEvent("searching_vector_index")
	time.Sleep(120 * time.Millisecond)

	span.AddEvent("filtering_by_relevance")
	time.Sleep(20 * time.Millisecond)

	span.AddEvent("ranking_results")
	time.Sleep(30 * time.Millisecond)

	response := &KnowledgeSearchResponse{
		RequestID:        req.RequestID,
		ResultCount:      5,
		DocumentIDs:      []string{"doc-101", "doc-202", "doc-303", "doc-404", "doc-505"},
		AvgRelevance:     0.87,
		CacheHit:         false,
		ProcessingTimeMs: 215.0,
	}

	span.AddEvent("knowledge_search_complete")
	span.SetOK()

	return response, nil
}

// Component 5: Response Generation
func generateResponse(ctx context.Context, k *telemetry.Telemetry, req ResponseGenerationRequest) (*ResponseGenerationResponse, error) {
	_, span := k.Tracing.Start(ctx, "ResponseGeneration", req)
	defer span.End()

	span.AddEvent("preparing_prompt")
	time.Sleep(20 * time.Millisecond)

	span.AddEvent("tokenizing_input")
	time.Sleep(25 * time.Millisecond)

	span.AddEvent("calling_llm_api")
	time.Sleep(180 * time.Millisecond)

	span.AddEvent("parsing_response")
	time.Sleep(15 * time.Millisecond)

	span.AddEvent("detokenizing_output")
	time.Sleep(10 * time.Millisecond)

	response := &ResponseGenerationResponse{
		RequestID:        req.RequestID,
		Response:         "Distributed tracing in microservices involves several best practices: 1) Use correlation IDs across all services, 2) Implement context propagation, 3) Trace critical paths, 4) Set appropriate sampling rates, 5) Monitor span durations and error rates.",
		TokensPrompt:     520,
		TokensCompletion: 95,
		TokensTotal:      615,
		FinishReason:     "stop",
		ProcessingTimeMs: 250.0,
	}

	span.AddEvent("response_generation_complete")
	span.SetOK()

	return response, nil
}

// Component 6: Response Validation
func validateResponse(ctx context.Context, k *telemetry.Telemetry, req ResponseValidationRequest) (*ResponseValidationResponse, error) {
	_, span := k.Tracing.Start(ctx, "ResponseValidation", req)
	defer span.End()

	span.AddEvent("checking_content_safety")
	time.Sleep(40 * time.Millisecond)

	span.AddEvent("detecting_pii")
	time.Sleep(30 * time.Millisecond)

	span.AddEvent("calculating_toxicity_score")
	time.Sleep(25 * time.Millisecond)

	span.AddEvent("validating_format")
	time.Sleep(15 * time.Millisecond)

	response := &ResponseValidationResponse{
		RequestID:        req.RequestID,
		IsValid:          true,
		IsSafe:           true,
		HasPII:           false,
		ToxicityScore:    0.02,
		ProcessingTimeMs: 110.0,
	}

	span.AddEvent("response_validation_complete")
	span.SetOK()

	return response, nil
}

// Component 7: Output Formatting
func formatOutput(ctx context.Context, k *telemetry.Telemetry, req OutputFormattingRequest) (*OutputFormattingResponse, error) {
	_, span := k.Tracing.Start(ctx, "OutputFormatting", req)
	defer span.End()

	span.AddEvent("applying_markdown_formatting")
	time.Sleep(20 * time.Millisecond)

	span.AddEvent("adding_citations")
	time.Sleep(15 * time.Millisecond)

	span.AddEvent("adding_metadata")
	time.Sleep(10 * time.Millisecond)

	span.AddEvent("finalizing_output")
	time.Sleep(10 * time.Millisecond)

	response := &OutputFormattingResponse{
		RequestID:        req.RequestID,
		FormattedOutput:  req.Response + "\n\n*Powered by Malenia AI*",
		OutputLength:     len(req.Response) + 25,
		Markdown:         true,
		ProcessingTimeMs: 55.0,
	}

	span.AddEvent("output_formatting_complete")
	span.SetOK()

	return response, nil
}

// Error handling example
func runMaleniaWithError() {
	ctx := context.Background()

	// Uses telemetry.toml config for service info and OTLP endpoint
	k, err := telemetry.New().Build()
	if err != nil {
		panic(err)
	}
	defer func() { _ = k.Close() }()

	k.Logger.Info("=== Malenia Pipeline with Error Handling ===", nil)

	conversationReq := ConversationRequest{
		RequestID:     "conv-error-test",
		UserID:        "user-123",
		SessionID:     "session-xyz",
		UserMessage:   "Test error handling",
		MessageLength: 19,
		Timestamp:     time.Now().Format(time.RFC3339),
	}

	// Simulate pipeline with error
	err = k.Tracing.Trace(ctx, "MaleniaConversationPipeline", conversationReq, func(ctx context.Context, span *telemetry.Span) error {
		// Input processing succeeds
		_, err := processInput(ctx, k, InputProcessingRequest{
			RequestID:   conversationReq.RequestID,
			RawInput:    conversationReq.UserMessage,
			InputLength: conversationReq.MessageLength,
			Language:    "en",
		})
		if err != nil {
			return err
		}

		// Simulate LLM rate limit error
		span.AddEvent("llm_rate_limit_exceeded")
		return errors.New("LLM service rate limit exceeded - please retry")
	})

	if err != nil {
		_ = k.Logger.Error("❌ Conversation failed with error (automatically recorded in trace)", map[string]interface{}{"error": err.Error()})
	}

	time.Sleep(2 * time.Second)
}
