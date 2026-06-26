// Package handlers contains the HTTP handlers for the AI Agent web application.
// This project uses the simple function-based handler pattern (no Handler struct)
// consistent with Projects 01-04 and 06-07.
package handlers

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"

	"github.com/EOEboh/mb-project-09-ai-agent/agent"
)

// ── What is new in Project 09 ──────────────────────────────────────────────────
// Previous SSE projects (Project 01) streamed raw text tokens one by one.
// This project streams COMPLETE HTML fragments -- one per agent step -- so the
// browser can insert fully-formed step cards without any client-side templating.
//
// The agent loop runs synchronously inside the SSE handler goroutine. Each time
// the agent completes a step it calls back into renderStep(), which produces an
// HTML string that is immediately sent as an SSE event.

// tmpl is parsed once at startup. Only the "index" template is needed here;
// step cards are built as plain strings in renderStep(), not via the template engine.
var tmpl = template.Must(template.ParseFiles("templates/index.html"))

// ── Index ──────────────────────────────────────────────────────────────────────

// Index serves the main page (GET /).
// On first visit the user sees only the question form and tool badges.
func Index(w http.ResponseWriter, r *http.Request) {
	if err := tmpl.ExecuteTemplate(w, "index", nil); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ── Stream ─────────────────────────────────────────────────────────────────────

// Stream handles GET /stream?q=<question>.
// It sets up an SSE connection, runs the agent loop, and streams each step
// as an HTML fragment. When the agent finishes (or errors), it sends [DONE].
//
// The question travels as a query parameter because EventSource (used in the
// browser) only supports GET requests.
func Stream(w http.ResponseWriter, r *http.Request) {
	question := strings.TrimSpace(r.URL.Query().Get("q"))
	if question == "" {
		http.Error(w, "missing query parameter: q", http.StatusBadRequest)
		return
	}

	// ── SSE headers ───────────────────────────────────────────────────────────
	// These headers tell the browser this is a long-lived event stream and
	// instruct proxies/CDNs not to buffer it (X-Accel-Buffering for nginx).
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// send writes one SSE event. Newlines inside the HTML payload are stripped
	// because SSE treats each "data: ...\n\n" as one event; embedded newlines
	// would split a card across multiple events and corrupt the fragment.
	send := func(html string) {
		html = strings.ReplaceAll(html, "\n", "")
		fmt.Fprintf(w, "data: %s\n\n", html)
		flusher.Flush()
	}

	// ── Run the agent ─────────────────────────────────────────────────────────
	tools := agent.Registry()

	_, err := agent.Run(question, tools, func(step agent.Step) {
		// This callback fires after each completed step (Thought+Action+Observation
		// OR Thought+FinalAnswer). We immediately convert it to HTML and stream it.
		send(renderStep(step))
	})

	if err != nil {
		log.Printf("agent error: %v", err)
		send(renderError(err.Error()))
	}

	// Signal the browser's EventSource listener that the stream is finished.
	fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// ── renderStep ─────────────────────────────────────────────────────────────────

// renderStep converts one agent Step into a safe HTML fragment.
//
// It uses strings.Builder for performance and template.HTMLEscapeString() on
// every piece of user-derived or model-derived content to prevent XSS.
// We intentionally do NOT use html/template here because these fragments are
// inserted via innerHTML in the browser and the template engine would double-escape.
func renderStep(step agent.Step) string {
	var b strings.Builder

	if step.IsFinal {
		// ── Final answer card ─────────────────────────────────────────────────
		b.WriteString(`<div class="step-card final">`)

		if step.Thought != "" {
			b.WriteString(`<div class="step-thought"><span class="step-icon">💭</span> `)
			b.WriteString(template.HTMLEscapeString(step.Thought))
			b.WriteString(`</div>`)
		}

		b.WriteString(`<div class="final-answer">`)
		b.WriteString(`<span class="step-icon">✅</span> `)
		b.WriteString(`<strong>Final Answer:</strong> `)
		b.WriteString(template.HTMLEscapeString(step.FinalAnswer))
		b.WriteString(`</div>`)

		b.WriteString(`</div>`)
		return b.String()
	}

	// ── Intermediate step card ────────────────────────────────────────────────
	fmt.Fprintf(&b, `<div class="step-card" data-step="%d">`, step.Number)

	// Thought section (may be empty if the model skipped it).
	if step.Thought != "" {
		b.WriteString(`<div class="step-thought"><span class="step-icon">💭</span> `)
		b.WriteString(template.HTMLEscapeString(step.Thought))
		b.WriteString(`</div>`)
	}

	// Action section: badge with tool name + code block with input.
	b.WriteString(`<div class="step-action">`)
	b.WriteString(`<span class="action-badge">🔧 `)
	b.WriteString(template.HTMLEscapeString(step.Action))
	b.WriteString(`</span>`)
	b.WriteString(`<code class="action-input">`)
	b.WriteString(template.HTMLEscapeString(step.ActionInput))
	b.WriteString(`</code>`)
	b.WriteString(`</div>`)

	// Observation section: the tool's result or error.
	b.WriteString(`<div class="step-observation"><span class="step-icon">📊</span> `)
	b.WriteString(template.HTMLEscapeString(step.Observation))
	b.WriteString(`</div>`)

	b.WriteString(`</div>`)
	return b.String()
}

// renderError produces an error card HTML fragment.
// Used when the agent exceeds maxSteps or encounters an unrecoverable error.
func renderError(msg string) string {
	var b strings.Builder
	b.WriteString(`<div class="step-card error">`)
	b.WriteString(`<span class="step-icon">⚠</span> `)
	b.WriteString(template.HTMLEscapeString(msg))
	b.WriteString(`</div>`)
	return b.String()
}
