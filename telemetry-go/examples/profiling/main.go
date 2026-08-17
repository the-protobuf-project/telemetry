package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"github.com/the-protobuf-project/telemetry/telemetry-go"
)

// LLMConfig represents configuration for LLM inference
type LLMConfig struct {
	ModelSize      int     // Number of parameters (in millions)
	ContextLength  int     // Maximum context length
	BatchSize      int     // Batch size for inference
	UseGPU         bool    // Whether to simulate GPU usage
	MemoryPerToken float64 // Memory per token in MB
}

// InferenceRequest represents a single inference request
type InferenceRequest struct {
	ID            string
	Prompt        string
	MaxTokens     int
	Temperature   float64
	ProcessingLoc string // "cpu", "gpu", or "memory"
}

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

	// Configure LLM
	config := LLMConfig{
		ModelSize:      7000, // 7B parameters
		ContextLength:  4096,
		BatchSize:      4,
		UseGPU:         true,
		MemoryPerToken: 0.5, // MB per token
	}

	p.Logger.Info("LLM Inference Profiling Started")
	p.Logger.Info("Configuration",
		"model_size", fmt.Sprintf("%dB", config.ModelSize/1000),
		"context_length", config.ContextLength,
		"batch_size", config.BatchSize,
		"use_gpu", config.UseGPU,
	)
	p.Logger.Info("View profiles at: http://localhost:3000 (Grafana)")
	p.Logger.Info("Running inference simulation for 60 seconds...")

	// Run inference simulation
	ctx := context.Background()
	runInferenceSimulation(ctx, p, config, 60*time.Second)

	p.Logger.Info("Simulation completed")
}

// runInferenceSimulation simulates LLM inference workload
func runInferenceSimulation(ctx context.Context, p *telemetry.Telemetry, config LLMConfig, duration time.Duration) {
	var wg sync.WaitGroup
	stopChan := make(chan struct{})
	requestChan := make(chan InferenceRequest, 100)

	// Start request generator
	wg.Add(1)
	go func() {
		defer wg.Done()
		generateRequests(requestChan, stopChan)
	}()

	// Start inference workers
	numWorkers := runtime.NumCPU()
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			inferenceWorker(ctx, p, config, workerID, requestChan, stopChan)
		}(i)
	}

	// Run for specified duration
	time.Sleep(duration)
	close(stopChan)
	close(requestChan)
	wg.Wait()
}

// generateRequests creates inference requests
func generateRequests(requestChan chan<- InferenceRequest, stopChan <-chan struct{}) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	requestID := 0
	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			// Randomly select processing location
			locations := []string{"cpu", "gpu", "memory"}
			location := locations[rand.Intn(len(locations))]

			req := InferenceRequest{
				ID:            fmt.Sprintf("req-%d", requestID),
				Prompt:        generatePrompt(),
				MaxTokens:     50 + rand.Intn(200),
				Temperature:   0.7 + rand.Float64()*0.3,
				ProcessingLoc: location,
			}

			select {
			case requestChan <- req:
				requestID++
			case <-stopChan:
				return
			}
		}
	}
}

// inferenceWorker processes inference requests
func inferenceWorker(ctx context.Context, p *telemetry.Telemetry, config LLMConfig, workerID int, requestChan <-chan InferenceRequest, stopChan <-chan struct{}) {
	for {
		select {
		case <-stopChan:
			return
		case req, ok := <-requestChan:
			if !ok {
				return
			}
			processInferenceRequest(ctx, p, config, workerID, req)
		}
	}
}

// processInferenceRequest simulates processing a single inference request
func processInferenceRequest(ctx context.Context, p *telemetry.Telemetry, config LLMConfig, workerID int, req InferenceRequest) {
	// Stage 1: Model Loading / Cache Lookup
	p.Profiler.TagWrapper(ctx, map[string]string{
		"operation":      "llm_inference",
		"stage":          "model_loading",
		"worker_id":      fmt.Sprintf("worker-%d", workerID),
		"processing_loc": req.ProcessingLoc,
	}, func(ctx context.Context) {
		simulateModelLoading()
	})

	// Stage 2: Tokenization
	p.Profiler.TagWrapper(ctx, map[string]string{
		"operation":      "llm_inference",
		"stage":          "tokenization",
		"worker_id":      fmt.Sprintf("worker-%d", workerID),
		"processing_loc": req.ProcessingLoc,
	}, func(ctx context.Context) {
		simulateTokenization(req.Prompt)
	})

	// Stage 3: Inference (CPU/GPU/Memory intensive)
	p.Profiler.TagWrapper(ctx, map[string]string{
		"operation":      "llm_inference",
		"stage":          "inference",
		"worker_id":      fmt.Sprintf("worker-%d", workerID),
		"processing_loc": req.ProcessingLoc,
	}, func(ctx context.Context) {
		switch req.ProcessingLoc {
		case "cpu":
			simulateCPUInference(req)
		case "gpu":
			simulateGPUInference(config, req)
		case "memory":
			simulateMemoryInference(config, req)
		}
	})

	// Stage 4: Decoding
	p.Profiler.TagWrapper(ctx, map[string]string{
		"operation":      "llm_inference",
		"stage":          "decoding",
		"worker_id":      fmt.Sprintf("worker-%d", workerID),
		"processing_loc": req.ProcessingLoc,
	}, func(ctx context.Context) {
		simulateDecoding(req.MaxTokens)
	})
}

// simulateModelLoading simulates loading model weights into memory
func simulateModelLoading() {
	// Allocate memory to simulate model weights (reduced for balance)
	// Each parameter is ~4 bytes (float32)
	chunkSize := 5 * 1024 * 1024 // 5MB chunks
	numChunks := 20              // Load 100MB total

	for i := 0; i < numChunks; i++ {
		chunk := make([]byte, chunkSize)
		for j := 0; j < len(chunk); j += 1000 {
			chunk[j] = byte(rand.Intn(256))
		}
		// Simulate some processing
		_ = chunk
	}

	time.Sleep(3 * time.Millisecond)
}

// simulateTokenization simulates converting text to tokens
func simulateTokenization(prompt string) {
	// Simulate tokenization with string processing
	tokens := make([]int, 0, 1000)

	// Generate more tokens to make this visible
	for i := 0; i < 1000; i++ {
		tokenID := rand.Intn(50000)
		tokens = append(tokens, tokenID)
	}

	// Simulate vocabulary lookup with more work
	vocab := make(map[int]string, len(tokens))
	for _, tokenID := range tokens {
		vocab[tokenID] = fmt.Sprintf("token_%d_%s", tokenID, prompt[:min(10, len(prompt))])
	}

	// Simulate BPE encoding (CPU intensive)
	for i := 0; i < 500; i++ {
		text := fmt.Sprintf("%s_%d", prompt, i)
		for j := 0; j < len(text); j++ {
			_ = text[j]
		}
	}

	time.Sleep(5 * time.Millisecond)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// simulateCPUInference simulates CPU-bound inference
func simulateCPUInference(req InferenceRequest) {
	// Simulate matrix operations (CPU intensive)
	matrixSize := 256

	// Perform multiple matrix multiplications to simulate layers
	for layer := 0; layer < 3; layer++ {
		matrix1 := make([][]float64, matrixSize)
		matrix2 := make([][]float64, matrixSize)
		result := make([][]float64, matrixSize)

		for i := 0; i < matrixSize; i++ {
			matrix1[i] = make([]float64, matrixSize)
			matrix2[i] = make([]float64, matrixSize)
			result[i] = make([]float64, matrixSize)

			for j := 0; j < matrixSize; j++ {
				matrix1[i][j] = rand.Float64()
				matrix2[i][j] = rand.Float64()
			}
		}

		// Matrix multiplication (CPU intensive)
		for i := 0; i < matrixSize; i++ {
			for j := 0; j < matrixSize; j++ {
				sum := 0.0
				for k := 0; k < matrixSize; k++ {
					sum += matrix1[i][k] * matrix2[k][j]
				}
				result[i][j] = sum
			}
		}
	}

	// Simulate attention mechanism
	seqLen := min(req.MaxTokens, 100)
	for i := 0; i < seqLen; i++ {
		attention := make([]float64, seqLen)
		for j := range attention {
			attention[j] = math.Exp(rand.Float64())
		}

		// Softmax
		sum := 0.0
		for _, v := range attention {
			sum += v
		}
		for j := range attention {
			attention[j] /= sum
		}
	}

	time.Sleep(10 * time.Millisecond)
}

// simulateGPUInference simulates GPU-accelerated inference
func simulateGPUInference(config LLMConfig, req InferenceRequest) {
	// GPU inference is typically faster but still requires memory allocation
	// Simulate batch processing
	batchSize := config.BatchSize
	hiddenSize := 4096

	// Allocate GPU memory (simulated with Go memory)
	hiddenStates := make([][][]float64, batchSize)
	for b := 0; b < batchSize; b++ {
		hiddenStates[b] = make([][]float64, req.MaxTokens)
		for t := 0; t < req.MaxTokens; t++ {
			hiddenStates[b][t] = make([]float64, hiddenSize)
			for h := 0; h < hiddenSize; h++ {
				hiddenStates[b][t][h] = rand.Float64()
			}
		}
	}

	// Simulate GPU computation (faster than CPU)
	for b := 0; b < batchSize; b++ {
		for t := 0; t < req.MaxTokens; t++ {
			// Simulate layer computation
			for h := 0; h < hiddenSize; h++ {
				hiddenStates[b][t][h] = math.Tanh(hiddenStates[b][t][h])
			}
		}
	}

	// GPU operations are faster
	time.Sleep(5 * time.Millisecond)
}

// simulateMemoryInference simulates memory-intensive inference
func simulateMemoryInference(config LLMConfig, req InferenceRequest) {
	// Simulate KV cache (memory intensive)
	kvCacheSize := config.ContextLength * config.ModelSize / 1000
	kvCache := make([][]float64, kvCacheSize)

	for i := range kvCache {
		kvCache[i] = make([]float64, 128)
		for j := range kvCache[i] {
			kvCache[i][j] = rand.Float64()
		}
	}

	// Simulate cache lookups and updates
	for i := 0; i < req.MaxTokens; i++ {
		// Read from cache
		idx := rand.Intn(len(kvCache))
		_ = kvCache[idx]

		// Update cache
		kvCache[idx] = make([]float64, 128)
		for j := range kvCache[idx] {
			kvCache[idx][j] = rand.Float64()
		}
	}

	time.Sleep(10 * time.Millisecond)
}

// simulateDecoding simulates converting tokens back to text
func simulateDecoding(maxTokens int) {
	// Simulate token-by-token generation
	output := make([]string, 0, maxTokens)

	for i := 0; i < maxTokens; i++ {
		// Simulate vocabulary lookup
		tokenID := rand.Intn(50000)
		word := fmt.Sprintf("word_%d", tokenID)
		output = append(output, word)

		// Simulate logit processing (CPU intensive)
		logits := make([]float64, 50000)
		for j := range logits {
			logits[j] = rand.Float64()
		}

		// Softmax calculation
		maxLogit := logits[0]
		for _, l := range logits {
			if l > maxLogit {
				maxLogit = l
			}
		}

		expSum := 0.0
		for j := range logits {
			logits[j] = math.Exp(logits[j] - maxLogit)
			expSum += logits[j]
		}
	}

	// Simulate string concatenation and post-processing
	result := ""
	for _, word := range output {
		result += word + " "
		// Simulate text cleaning
		for j := 0; j < len(word); j++ {
			_ = word[j]
		}
	}

	time.Sleep(3 * time.Millisecond)
}

// generatePrompt generates a random prompt
func generatePrompt() string {
	prompts := []string{
		"Explain quantum computing in simple terms",
		"Write a function to calculate fibonacci numbers",
		"What are the benefits of exercise?",
		"Describe the water cycle",
		"How does machine learning work?",
	}
	return prompts[rand.Intn(len(prompts))]
}
