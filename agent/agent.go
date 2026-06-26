// Package agent implements a ReAct (Reasoning + Acting) loop from scratch.
//
// ReAct is a simple but powerful pattern:
//  1. The model receives a question and a list of available tools.
//  2. It emits a Thought (why it needs a tool) and an Action (which tool + input).
//  3. The agent executes the tool and feeds back the Observation.
//  4. Steps 2-3 repeat until the model emits "Final Answer:" instead of an Action.
//
// This implementation uses ai.Chat() (blocking, not streaming) for each step
// because the agent must parse the COMPLETE response before it can decide what
// to do next. The streaming in this project is of completed HTML step cards,
// not of raw model tokens.
package agent

import (
	"fmt"
	"strings"

	"github.com/EOEboh/mb-project-09-ai-agent/ai"
)

// ── What is new in Project 09 ──────────────────────────────────────────────────
// The agent package is brand new. Projects 01-08 sent user messages directly to
// Ollama. This project adds a loop that accumulates a conversation, calls tools,
// injects Observations back into the history, and repeats until the model
// decides it has enough information to produce a Final Answer.

const maxSteps = 8 // Safety cap: return an error if the agent hasn't finished.

// ── Step ───────────────────────────────────────────────────────────────────────

// Step holds all data for one iteration of the ReAct loop.
// The handler converts each Step into an HTML fragment for SSE delivery.
type Step struct {
	Number      int    // 1-based step counter
	Thought     string // The model's reasoning text
	Action      string // Tool name (lowercase), e.g. "calculator"
	ActionInput string // The string passed to the tool
	Observation string // The tool's return value (or error message)
	IsFinal     bool   // True when the model has produced a Final Answer
	FinalAnswer string // Populated only when IsFinal is true
}

// ── Run (ReAct loop) ───────────────────────────────────────────────────────────

// Run executes the ReAct loop for the given question.
//
// registry is a map from tool name to Tool implementation.
// onStep is called after each completed step so the caller (the SSE handler)
// can stream the step card to the browser immediately -- without waiting for
// the entire agent run to finish.
//
// Returns the final answer string on success, or an error if the agent exceeds
// maxSteps or the LLM returns something unparseable.
func Run(question string, registry map[string]Tool, onStep func(Step)) (string, error) {
	// Build the initial conversation: system prompt + user question.
	messages := []ai.Message{
		{Role: "system", Content: systemPrompt(registry)},
		{Role: "user", Content: question},
	}

	for stepNum := 1; stepNum <= maxSteps; stepNum++ {
		// Ask the model for its next Thought + Action (or Final Answer).
		// We use Chat() (blocking) because we need the full response to parse it.
		raw, err := ai.Chat(ai.DefaultModel, messages)
		if err != nil {
			return "", fmt.Errorf("step %d: LLM call failed: %w", stepNum, err)
		}

		step := parseStep(raw)
		step.Number = stepNum

		// ── Final Answer branch ────────────────────────────────────────────────
		if step.IsFinal {
			onStep(step) // Stream the final step card to the browser.
			return step.FinalAnswer, nil
		}

		// ── Tool execution branch ──────────────────────────────────────────────
		tool, ok := registry[step.Action]
		if !ok {
			// The model named a tool that doesn't exist. Tell it so and keep going.
			step.Observation = fmt.Sprintf(
				"Error: unknown tool %q. Available tools: %s",
				step.Action,
				toolNames(registry),
			)
		} else {
			result, toolErr := tool.Run(step.ActionInput)
			if toolErr != nil {
				step.Observation = "Error: " + toolErr.Error()
			} else {
				step.Observation = result
			}
		}

		onStep(step) // Stream this intermediate step card to the browser.

		// Append the model's output and the tool observation to the history.
		// This is the key mechanism: the model sees its own reasoning AND the
		// tool results on every subsequent call, allowing it to build toward
		// a Final Answer over multiple steps.
		messages = append(messages,
			ai.Message{Role: "assistant", Content: raw},
			ai.Message{
				Role:    "user",
				Content: "Observation: " + step.Observation + "\n\nContinue your reasoning.",
			},
		)
	}

	return "", fmt.Errorf("agent exceeded maximum steps (%d) without producing a final answer", maxSteps)
}

// ── parseStep ──────────────────────────────────────────────────────────────────

// parseStep extracts Thought, Action, Action Input, and Final Answer fields
// from a raw model response. It scans line by line and handles the format
// variations that llama3.2:3b commonly produces.
//
// Handled variations:
//   - Standard:      "Action: calculator" / "Action Input: 2+2" / "Final Answer: 4"
//   - Merged action: "Action: Final Answer" (model puts the answer on the action line)
//   - No Thought:    Missing Thought line is fine; step.Thought is left empty.
//   - Multi-line AI: "Action Input: 100 fahrenheit\nto celsius" -- only first line taken.
func parseStep(text string) Step {
	var step Step
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// ── Thought ───────────────────────────────────────────────────────────
		if after, ok := cutPrefixCI(trimmed, "Thought:"); ok {
			step.Thought = strings.TrimSpace(after)
			continue
		}

		// ── Final Answer (standalone line) ────────────────────────────────────
		if after, ok := cutPrefixCI(trimmed, "Final Answer:"); ok {
			step.IsFinal = true
			step.FinalAnswer = strings.TrimSpace(after)
			continue
		}

		// ── Action ────────────────────────────────────────────────────────────
		if after, ok := cutPrefixCI(trimmed, "Action:"); ok {
			actionValue := strings.TrimSpace(after)

			// Some small models write "Action: Final Answer" instead of a
			// separate "Final Answer:" line. Detect that here.
			if strings.EqualFold(actionValue, "final answer") ||
				strings.EqualFold(actionValue, "final_answer") {
				step.IsFinal = true
				// The actual answer text may be on the next non-empty line.
				for j := i + 1; j < len(lines); j++ {
					next := strings.TrimSpace(lines[j])
					if next == "" {
						continue
					}
					// Strip a "Final Answer:" prefix if the model repeated it.
					if ans, ok2 := cutPrefixCI(next, "Final Answer:"); ok2 {
						step.FinalAnswer = strings.TrimSpace(ans)
					} else {
						step.FinalAnswer = next
					}
					break
				}
			} else {
				step.Action = strings.ToLower(actionValue)
			}
			continue
		}

		// ── Action Input ──────────────────────────────────────────────────────
		if after, ok := cutPrefixCI(trimmed, "Action Input:"); ok {
			// Take only the first line to handle multi-line action inputs
			// that some models emit.
			step.ActionInput = strings.TrimSpace(after)
			continue
		}
	}

	// If the model produced a Final Answer inline without a separate Thought,
	// make sure IsFinal is set even if only FinalAnswer is populated.
	if step.FinalAnswer != "" {
		step.IsFinal = true
	}

	return step
}

// cutPrefixCI is a case-insensitive version of strings.CutPrefix.
// Returns (after, true) if s starts with prefix (case-insensitive).
func cutPrefixCI(s, prefix string) (string, bool) {
	if len(s) < len(prefix) {
		return "", false
	}
	if strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}

// ── systemPrompt ───────────────────────────────────────────────────────────────

// systemPrompt returns the ReAct system prompt with all registered tool
// descriptions injected. It includes one complete worked example to guide
// small models like llama3.2:3b.
//
// Design notes:
//   - Keep it SHORT. Small models get confused by long system prompts.
//   - Show the EXACT format, not just a description of it.
//   - Use %% to produce literal percent signs inside fmt.Sprintf.
//   - The example must show Observation being produced externally (the model
//     should never generate its own Observation).
func systemPrompt(registry map[string]Tool) string {
	var toolList strings.Builder
	for _, t := range registry {
		fmt.Fprintf(&toolList, "- %s: %s\n", t.Name(), t.Description())
	}

	return fmt.Sprintf(`You are an AI assistant that uses tools to answer questions accurately.

Available tools:
%s
Respond EXACTLY in this format for each step:
Thought: [your reasoning about what to do]
Action: [tool name]
Action Input: [input to pass to the tool]

After receiving an Observation, continue with another Thought+Action, or give the final answer:
Thought: [reasoning based on the observation]
Final Answer: [complete answer to the user's question]

IMPORTANT:
- Only use tool names from the list above.
- Never generate your own Observation. Wait for it.
- When you have enough information, write "Final Answer:" to finish.

Example:
User: What is 30%% of 200?
Thought: I need to calculate 30%% of 200, which is 200 * 0.30.
Action: calculator
Action Input: 200 * 0.30
Observation: 60
Thought: 30%% of 200 is 60. I have the answer.
Final Answer: 30%% of 200 is 60.
`, toolList.String())
}

// toolNames returns a comma-separated list of available tool names.
// Used in error messages when the model calls an unknown tool.
func toolNames(registry map[string]Tool) string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}
