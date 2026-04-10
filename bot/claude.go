package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
)

// claudeJSON is the subset of `claude -p --output-format json` we care about.
type claudeJSON struct {
	Result    string `json:"result"`
	SessionID string `json:"session_id"`
	IsError   bool   `json:"is_error"`
}

// runClaude shells out to `claude -p` in YOLO mode and captures the final
// text response. sess.SessionID is updated on success so the next call
// resumes the same conversation.
func runClaude(ctx context.Context, sess *Session, model, prompt string) (string, error) {
	args := []string{
		"-p", prompt,
		"--dangerously-skip-permissions",
		"--output-format", "json",
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if sess.SessionID != "" {
		args = append(args, "--resume", sess.SessionID)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = sess.Cwd
	cmd.Env = os.Environ()

	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			stderr := string(ee.Stderr)
			if len(stderr) > 1500 {
				stderr = stderr[len(stderr)-1500:]
			}
			return "", fmt.Errorf("claude exit %d: %s", ee.ExitCode(), stderr)
		}
		return "", err
	}

	var res claudeJSON
	if err := json.Unmarshal(out, &res); err != nil {
		// Fall back to raw output if claude ever changes format.
		return string(out), nil
	}
	if res.SessionID != "" {
		sess.SessionID = res.SessionID
	}
	if res.IsError {
		return "", fmt.Errorf("%s", res.Result)
	}
	if res.Result == "" {
		return "(claude returned no text)", nil
	}
	return res.Result, nil
}
