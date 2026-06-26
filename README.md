# Project 09: AI Agent with Tool Use

Part of **Build 10 AI Projects in 30 Days** using Go 1.22+, Ollama, and HTMX.

---

## What You Will Build

A ReAct (Reasoning + Acting) agent built entirely from scratch -- no external agent frameworks. The user types a question into a chat-style interface. The agent:

1. **Thinks** -- generates a Thought explaining its reasoning.
2. **Acts** -- selects a tool and provides an input for it.
3. **Observes** -- receives the tool's result.
4. Repeats steps 1-3 until it has enough information to write a **Final Answer**.

Each step (Thought, Action, Observation) streams to the browser in real-time as a fully-formed HTML card via Server-Sent Events. You can watch the agent's entire reasoning chain unfold before your eyes.

### The Three Tools

| Tool | What it does |
|------|-------------|
| `calculator` | Evaluates math expressions using `github.com/expr-lang/expr`. Supports `+`, `-`, `*`, `/`, `sqrt()`, `pow()`, `round()`, `pi`, and more. |
| `datetime` | Returns current date/time info using Go's `time` package. Accepts `now`, `date`, `time`, `day`, `year`, or `timestamp`. |
| `unit_convert` | Converts between temperature (Celsius/Fahrenheit/Kelvin), length (meters/km/miles/feet/inches/cm), weight (grams/kg/pounds/ounces), and speed (km/h, mph, m/s). Zero external packages. |

---

## Key Concepts Introduced

| Concept | What It Teaches |
|---------|----------------|
| ReAct pattern | Reasoning and Acting loop: Thought, Action, Observation, repeat |
| Tool interface | A clean Go interface that makes adding new tools trivial |
| Agent loop with max steps | Safety cap preventing infinite loops |
| Conversation history accumulation | Each step appends to messages; the model sees the full history |
| SSE HTML fragment streaming | Streaming complete HTML cards vs raw text tokens (Project 01) |
| `github.com/expr-lang/expr` | Safe, sandboxed expression evaluation for the calculator tool |
| Execution tracing | Displaying the agent's internal reasoning steps to the user |

---

## Architecture

```
Browser                     Go Server                    Ollama
  |                             |                           |
  |-- GET /stream?q=question -->|                           |
  |                             |-- Chat(system+question) ->|
  |                             |<-- Thought+Action --------|
  |                             |                           |
  |                             | [execute tool locally]    |
  |                             |                           |
  |                             |-- Chat(+observation) ---->|
  |                             |<-- Thought+Action --------|
  |                             |                           |
  |<-- SSE: step card HTML -----|   (repeat up to 8 times)  |
  |                             |                           |
  |                             |-- Chat(+observation) ---->|
  |                             |<-- Final Answer ----------|
  |<-- SSE: final card HTML ----|                           |
  |<-- SSE: [DONE] -------------|                           |
```

The agent loop runs in `agent/agent.go`. The loop calls `ai.Chat()` (blocking, not streaming) because it needs the **complete** response to parse the Thought/Action/Final Answer structure. The SSE streaming is of completed step cards -- one HTML fragment per agent step.

---

## Prerequisites

- Go 1.22+
- [Ollama](https://ollama.ai) running locally

```bash
ollama pull llama3.2:3b
```

---

## Getting Started

```bash
# Clone and set up
git clone <repo-url>
cd mb-project-09-ai-agent
make setup

# Run the server
make run
# -> http://localhost:8080
```

---

## Demo Questions

These questions exercise different tools and multi-step reasoning:

1. **Single tool, one step:**
   `What is sqrt(144) + 15% of 200?`
   The agent calls `calculator` twice (or builds one expression) and sums the results.

2. **Unit conversion:**
   `Convert 72 fahrenheit to celsius`
   One step: calls `unit_convert` with `72 fahrenheit to celsius`.

3. **Date/time awareness:**
   `What day of the week is it today?`
   Calls `datetime` with input `day`, then returns the result.

4. **Multi-step, two tools:**
   `How many kilometers are in 26.2 miles, and what is 26.2 multiplied by 1.60934?`
   Step 1: `unit_convert` (26.2 miles to km). Step 2: `calculator` (26.2 * 1.60934). Final step: compares both results.

5. **Multi-step with reasoning:**
   `What is the current year, and what is that year minus 1969?`
   Step 1: `datetime` with `year`. Step 2: `calculator` using the year from step 1. The model must carry the intermediate result across steps -- this is conversation history accumulation in action.

6. **Pure math:**
   `What is pow(2, 10) + round(pi * 100)?`
   Tests the calculator's named function support.

7. **Chain of conversions:**
   `If I run 5 miles per day for 7 days, how many kilometers is that total?`
   Step 1: `unit_convert` (5 miles to km). Step 2: `calculator` (result * 7).

---

## Project Structure

```
mb-project-09-ai-agent/
├── main.go               Entry point, HTTP routes
├── go.mod                Module: github.com/EOEboh/mb-project-09-ai-agent
├── Makefile
├── .env.example
├── README.md
├── agent/
│   ├── agent.go          ReAct loop, Step type, parseStep, system prompt
│   └── tools.go          Tool interface + Calculator + Datetime + UnitConverter
├── ai/
│   └── ollama.go         Chat() and ChatStream() -- identical to scaffold
├── handlers/
│   └── agent.go          Index + Stream SSE handler + renderStep
├── templates/
│   └── index.html        Single named template "index"
└── static/
    └── style.css
```

---

## How the ReAct Loop Works

```go
messages = [system, user:question]

for step 1..maxSteps:
    raw = ai.Chat(DefaultModel, messages)
    step = parseStep(raw)

    if step.IsFinal:
        onStep(step)             // stream final card
        return step.FinalAnswer

    step.Observation = tool.Run(step.ActionInput)
    onStep(step)                 // stream intermediate card

    messages = append(messages,
        {role:"assistant", content: raw},
        {role:"user",      content: "Observation: " + step.Observation},
    )

return error("exceeded max steps")
```

The key insight: the model sees **its own reasoning AND all tool results** on every subsequent call. This is what lets it build toward a final answer across multiple steps.

---

## Adding a New Tool

Implement the `Tool` interface in `agent/tools.go`:

```go
type MyTool struct{}

func (t *MyTool) Name()        string { return "my_tool" }
func (t *MyTool) Description() string { return "What it does and input format." }
func (t *MyTool) Run(input string) (string, error) {
    // ... your logic
    return result, nil
}
```

Then register it in `Registry()`. That's all -- the agent loop, system prompt, and SSE handler all pick it up automatically.

---

## Possible Extensions

| Extension | Description |
|-----------|-------------|
| Web search tool | Add a tool that calls a search API (e.g. Brave Search) and returns snippets. This turns the agent into a research assistant. |
| Persistent memory tool | A tool that reads/writes to a JSON file, letting the agent "remember" facts across sessions. |
| File reader tool | Parse uploaded CSV or text files; the agent can answer questions about the data. |
| Code execution tool | Run Go or Python snippets in a sandbox and return stdout. Enables a code-writing agent. |
| Multi-agent orchestration | One "planner" agent breaks a complex task into sub-questions, dispatches them to specialized agents, then synthesizes the results. |

---

## What Is Next

**Project 10: Full-Stack AI SaaS**

The final project brings everything together: JWT authentication, multi-tenant user accounts, a persistent conversation database, Docker containerization, and a production-grade HTMX frontend. It is the capstone of the series.

---

## Commands

```
run     Start the development server
build   Compile to ./bin/app
tidy    Download dependencies
clean   Remove build artifacts
setup   Download Go dependencies and confirm Ollama model
help    List all available commands
```