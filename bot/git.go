package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitClone clones a repo into the user's workspace directory.
// If the workspace already has contents, it wipes them first.
// repo can be "owner/repo" (GitHub shorthand) or a full URL.
func gitClone(ctx context.Context, workspace, repo string) (string, error) {
	repoURL := repo
	if !strings.Contains(repo, "://") && !strings.HasPrefix(repo, "git@") {
		repoURL = "https://github.com/" + repo + ".git"
	}

	// Wipe existing workspace contents.
	entries, _ := os.ReadDir(workspace)
	for _, e := range entries {
		os.RemoveAll(filepath.Join(workspace, e.Name()))
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth=1", repoURL, ".")
	cmd.Dir = workspace
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git clone failed: %s", strings.TrimSpace(string(out)))
	}

	// Configure git user for commits.
	for _, c := range [][]string{
		{"config", "user.email", "claw@openclaw.local"},
		{"config", "user.name", "Claw Bot"},
	} {
		cmd := exec.CommandContext(ctx, "git", c...)
		cmd.Dir = workspace
		cmd.Run()
	}

	return fmt.Sprintf("Cloned %s", repo), nil
}

// gitStatus runs `git status --short` in the workspace.
func gitStatus(ctx context.Context, workspace string) (string, error) {
	return gitRun(ctx, workspace, "status", "--short")
}

// gitDiff runs `git diff` in the workspace.
func gitDiff(ctx context.Context, workspace string) (string, error) {
	out, err := gitRun(ctx, workspace, "diff")
	if err != nil {
		return "", err
	}
	if out == "" {
		// Also check staged changes.
		staged, _ := gitRun(ctx, workspace, "diff", "--cached")
		if staged != "" {
			return staged, nil
		}
		return "(no changes)", nil
	}
	return out, nil
}

// gitBranch creates and checks out a new branch.
func gitBranch(ctx context.Context, workspace, name string) (string, error) {
	_, err := gitRun(ctx, workspace, "checkout", "-b", name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("Switched to new branch '%s'", name), nil
}

// gitLog returns recent commit log.
func gitLog(ctx context.Context, workspace string) (string, error) {
	return gitRun(ctx, workspace, "log", "--oneline", "-20")
}

// gitCreatePR commits all changes, pushes to a new branch, and returns a
// GitHub PR URL via the `gh` CLI. title is optional — if empty, a default
// is generated from the branch name.
func gitCreatePR(ctx context.Context, workspace, title string) (string, error) {
	// Check for changes.
	status, err := gitRun(ctx, workspace, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "No changes to create a PR from.", nil
	}

	// Generate branch name.
	branch := fmt.Sprintf("claw/%d", time.Now().Unix())

	// Create and switch to branch.
	if _, err := gitRun(ctx, workspace, "checkout", "-b", branch); err != nil {
		return "", fmt.Errorf("branch create failed: %w", err)
	}

	// Stage all changes.
	if _, err := gitRun(ctx, workspace, "add", "-A"); err != nil {
		return "", fmt.Errorf("git add failed: %w", err)
	}

	// Commit.
	commitMsg := title
	if commitMsg == "" {
		commitMsg = "Changes by Claw Bot"
	}
	if _, err := gitRun(ctx, workspace, "commit", "-m", commitMsg); err != nil {
		return "", fmt.Errorf("git commit failed: %w", err)
	}

	// Push.
	if _, err := gitRun(ctx, workspace, "push", "-u", "origin", branch); err != nil {
		return "", fmt.Errorf("git push failed: %w", err)
	}

	// Create PR via gh CLI.
	prTitle := title
	if prTitle == "" {
		prTitle = "Claw Bot changes"
	}
	prBody := "Automated PR created by [Claw Bot](https://claw.biswas.me)"

	cmd := exec.CommandContext(ctx, "gh", "pr", "create",
		"--title", prTitle,
		"--body", prBody,
		"--head", branch,
	)
	cmd.Dir = workspace
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("gh pr create failed: %s", strings.TrimSpace(string(out)))
	}

	return strings.TrimSpace(string(out)), nil
}

// gitRun is a helper to run a git command and return stdout.
func gitRun(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
