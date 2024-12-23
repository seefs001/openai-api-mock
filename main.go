package main

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"
)

const (
	StreamResponseInterval = 50
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatCompletionRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type ChatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int     `json:"index"`
		Message Message `json:"message"`
	} `json:"choices"`
}

type DeltaMessage struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type ChatCompletionChunk struct {
	ID                string `json:"id"`
	Object            string `json:"object"`
	Created           int64  `json:"created"`
	Model             string `json:"model"`
	SystemFingerprint string `json:"system_fingerprint"`
	Choices           []struct {
		Index        int          `json:"index"`
		Delta        DeltaMessage `json:"delta"`
		LogProbs     interface{}  `json:"logprobs"`
		FinishReason string       `json:"finish_reason,omitempty"`
	} `json:"choices"`
}

func main() {
	http.HandleFunc("/v1/chat/completions", handleChatCompletion)
	http.HandleFunc("/rand_sleep/v1/chat/completions", handleRandomSleep)
	http.HandleFunc("/rand_fail/v1/chat/completions", handleRandomFail)
	http.HandleFunc("/rand_all/v1/chat/completions", handleRandom)
	http.ListenAndServe(":5000", nil)
}

func handleRandomSleep(w http.ResponseWriter, r *http.Request) {
	time.Sleep(time.Duration(rand.Intn(5000)) * time.Millisecond)
	handleChatCompletion(w, r)
}

func handleRandomFail(w http.ResponseWriter, r *http.Request) {
	if rand.Intn(10) < 5 {
		http.Error(w, "Random error", http.StatusInternalServerError)
		return
	}
	handleChatCompletion(w, r)
}

func handleRandom(w http.ResponseWriter, r *http.Request) {
	if rand.Intn(10) < 5 {
		handleRandomFail(w, r)
		return
	}
	time.Sleep(time.Duration(rand.Intn(5000)) * time.Millisecond)
	handleChatCompletion(w, r)
}

func handleChatCompletion(w http.ResponseWriter, r *http.Request) {

	if r.Header.Get("Content-Encoding") == "gzip" {
		gzipReader, err := gzip.NewReader(r.Body)
		if err != nil {
			http.Error(w, "Failed to decompress request body", http.StatusBadRequest)
			return
		}
		defer gzipReader.Close()

		// Replace the request body with the decompressed data
		r.Body = io.NopCloser(gzipReader)
	}

	// Continue processing the request

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChatCompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	slog.Info("handleChatCompletion", "req", req, "stream", req.Stream)
	if req.Stream {
		handleStreamingResponse(w, req)
		return
	}
	handleNonStreamingResponse(w, req)
}

func handleNonStreamingResponse(w http.ResponseWriter, req ChatCompletionRequest) {
	response := ChatCompletionResponse{
		ID:      "chatcmpl-" + randomString(10),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   req.Model,
		Choices: []struct {
			Index   int     `json:"index"`
			Message Message `json:"message"`
		}{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: generateResponse(req.Messages),
				},
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func handleStreamingResponse(w http.ResponseWriter, req ChatCompletionRequest) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	response := generateResponse(req.Messages)
	id := "chatcmpl-" + randomString(10)
	created := time.Now().Unix()

	// Send initial chunk with role
	initialChunk := ChatCompletionChunk{
		ID:                id,
		Object:            "chat.completion.chunk",
		Created:           created,
		Model:             req.Model,
		SystemFingerprint: "fp_44709d6fcb",
		Choices: []struct {
			Index        int          `json:"index"`
			Delta        DeltaMessage `json:"delta"`
			LogProbs     interface{}  `json:"logprobs"`
			FinishReason string       `json:"finish_reason,omitempty"`
		}{
			{
				Index: 0,
				Delta: DeltaMessage{
					Role: "assistant",
				},
				LogProbs:     nil,
				FinishReason: "",
			},
		},
	}

	writeChunk(w, initialChunk)

	// Send two characters at a time
	runes := []rune(response)
	for i := 0; i < len(runes); i += 2 {
		var content string
		if i+1 < len(runes) {
			content = string(runes[i : i+2])
		} else {
			content = string(runes[i:])
		}

		chunk := ChatCompletionChunk{
			ID:                id,
			Object:            "chat.completion.chunk",
			Created:           created,
			Model:             req.Model,
			SystemFingerprint: "fp_44709d6fcb",
			Choices: []struct {
				Index        int          `json:"index"`
				Delta        DeltaMessage `json:"delta"`
				LogProbs     interface{}  `json:"logprobs"`
				FinishReason string       `json:"finish_reason,omitempty"`
			}{
				{
					Index: 0,
					Delta: DeltaMessage{
						Content: content,
					},
					LogProbs:     nil,
					FinishReason: "",
				},
			},
		}

		writeChunk(w, chunk)
		time.Sleep(time.Duration(StreamResponseInterval) * time.Millisecond)
	}

	// Send final chunk
	finalChunk := ChatCompletionChunk{
		ID:                id,
		Object:            "chat.completion.chunk",
		Created:           created,
		Model:             req.Model,
		SystemFingerprint: "fp_44709d6fcb",
		Choices: []struct {
			Index        int          `json:"index"`
			Delta        DeltaMessage `json:"delta"`
			LogProbs     interface{}  `json:"logprobs"`
			FinishReason string       `json:"finish_reason,omitempty"`
		}{
			{
				Index:        0,
				Delta:        DeltaMessage{},
				LogProbs:     nil,
				FinishReason: "stop",
			},
		},
	}

	writeChunk(w, finalChunk)
	w.Write([]byte("data: [DONE]\n\n"))
}

func writeChunk(w http.ResponseWriter, chunk interface{}) {
	data := "data: " + toJSON(chunk) + "\n\n"
	w.Write([]byte(data))
	slog.Info("writeChunk", "chunk", chunk)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func generateResponse(messages []Message) string {
	return "who are you? and what are you doing here? and what is your purpose?"
}

func splitIntoWords(s string) []string {
	return strings.Fields(s)
}

func randomString(n int) string {
	var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
