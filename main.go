// Project 09: AI Agent with Tool Use
// Part of "Build 10 AI Projects in 30 Days" using Go 1.22+, Ollama, and HTMX.
//
// This project implements a ReAct (Reasoning + Acting) agent from scratch.
// The agent receives a user question, reasons about which tool to use, executes
// it, observes the result, and repeats until it has a final answer. Each step
// streams to the browser in real-time via Server-Sent Events.
//
// Usage: go run main.go   (Ollama must be running with llama3.2:3b pulled)
package main

import (
	"log"
	"net/http"

	"github.com/EOEboh/mb-project-09-ai-agent/handlers"
)

func main() {
	mux := http.NewServeMux()

	// Static assets (CSS).
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Main page: renders the question form.
	mux.HandleFunc("/", handlers.Index)

	// SSE stream: runs the agent and streams step cards as HTML fragments.
	// The question is passed as ?q=<question> because EventSource is GET-only.
	mux.HandleFunc("GET /stream", handlers.Stream)

	log.Println("AI Agent running -> http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}
