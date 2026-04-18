package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AgentRole defines a single agent in a multi-agent workflow.
type AgentRole struct {
	Name   string // display name
	Prompt string // prompt template — %s replaced with the user's task description
}

// Strategy defines a multi-agent workflow.
type Strategy struct {
	Name        string
	Description string
	Agents      []AgentRole
}

// Built-in strategies.
var strategies = map[string]Strategy{
	"review": {
		Name:        "review",
		Description: "Code review — analyze, test ideas, and review in parallel",
		Agents: []AgentRole{
			{Name: "Analyzer", Prompt: "Analyze the codebase structure and identify the key files related to: %s\nBe concise, list file paths and their purpose."},
			{Name: "Tester", Prompt: "Suggest test cases and edge cases for: %s\nFocus on what could break. Be specific and concise."},
			{Name: "Reviewer", Prompt: "Review the code quality, security, and performance for: %s\nFlag issues with file:line references. Be concise."},
		},
	},
	"implement": {
		Name:        "implement",
		Description: "Implementation — plan, code, and verify in parallel",
		Agents: []AgentRole{
			{Name: "Planner", Prompt: "Create a step-by-step implementation plan for: %s\nList files to create/modify, dependencies, and order of operations. Be concise."},
			{Name: "Coder", Prompt: "Write the code implementation for: %s\nFocus on clean, working code. Include file paths."},
			{Name: "Verifier", Prompt: "What tests and verification steps are needed for: %s\nInclude edge cases and integration concerns. Be concise."},
		},
	},
	"debug": {
		Name:        "debug",
		Description: "Debug — investigate, hypothesize, and fix in parallel",
		Agents: []AgentRole{
			{Name: "Investigator", Prompt: "Investigate the root cause of: %s\nTrace through the code, check logs, identify the failure point. Be concise."},
			{Name: "Hypothesizer", Prompt: "List possible causes and hypotheses for: %s\nRank by likelihood. Be concise."},
			{Name: "Fixer", Prompt: "Propose a concrete fix for: %s\nInclude the exact code changes needed. Be concise."},
		},
	},
}

// AgentResult is the output from a single agent.
type AgentResult struct {
	Name    string
	Result  string
	Error   string
	Elapsed time.Duration
}

// Orchestrate runs a multi-agent workflow and returns the combined results.
func Orchestrate(ctx context.Context, strategy Strategy, task, workspace, model string) []AgentResult {
	results := make([]AgentResult, len(strategy.Agents))
	var wg sync.WaitGroup

	for i, agent := range strategy.Agents {
		wg.Add(1)
		go func(idx int, a AgentRole) {
			defer wg.Done()
			start := time.Now()

			prompt := fmt.Sprintf(a.Prompt, task)

			// Each agent gets its own session (no shared state).
			sess := &Session{
				Cwd: workspace,
			}

			agentCtx, cancel := context.WithTimeout(ctx, 15*time.Minute)
			defer cancel()

			result, err := runClaude(agentCtx, sess, model, prompt)

			r := AgentResult{
				Name:    a.Name,
				Elapsed: time.Since(start),
			}
			if err != nil {
				r.Error = err.Error()
				log.Warn().Err(err).Str("agent", a.Name).Msg("orchestrate: agent failed")
			} else {
				r.Result = result
			}
			results[idx] = r
		}(i, agent)
	}

	wg.Wait()
	return results
}

// FormatOrchestrationResults formats the results for display.
func FormatOrchestrationResults(strategyName string, results []AgentResult) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Multi-agent %s complete:\n\n", strategyName)

	for _, r := range results {
		fmt.Fprintf(&sb, "━━━ %s (%s) ━━━\n", r.Name, r.Elapsed.Round(time.Second))
		if r.Error != "" {
			fmt.Fprintf(&sb, "Error: %s\n", r.Error)
		} else {
			sb.WriteString(r.Result)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// ListStrategies returns a formatted list of available strategies.
func ListStrategies() string {
	var sb strings.Builder
	sb.WriteString("Available strategies:\n\n")
	for name, s := range strategies {
		fmt.Fprintf(&sb, "  %s — %s\n", name, s.Description)
		for _, a := range s.Agents {
			fmt.Fprintf(&sb, "    • %s\n", a.Name)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Usage: /orchestrate <strategy> <task description>\n")
	sb.WriteString("Example: /orchestrate review the authentication module")
	return sb.String()
}
