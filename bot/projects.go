package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ProjectStore manages per-user named projects.
// Each project is a subdirectory under the user's workspace with its own
// Claude session. The "current project" is persisted so it survives restarts.
type ProjectStore struct {
	mu       sync.Mutex
	filePath string
	data     projectData
}

type projectData struct {
	// Current maps user ID → current project name.
	Current map[int64]string `json:"current"`
}

func NewProjectStore(filePath string) *ProjectStore {
	ps := &ProjectStore{
		filePath: filePath,
		data:     projectData{Current: make(map[int64]string)},
	}
	ps.load()
	return ps
}

func (ps *ProjectStore) load() {
	data, err := os.ReadFile(ps.filePath)
	if err != nil {
		return
	}
	var d projectData
	if err := json.Unmarshal(data, &d); err != nil {
		return
	}
	if d.Current == nil {
		d.Current = make(map[int64]string)
	}
	ps.data = d
}

func (ps *ProjectStore) save() {
	data, _ := json.MarshalIndent(ps.data, "", "  ")
	os.WriteFile(ps.filePath, data, 0o600)
}

// SwitchProject creates the project dir if needed, updates the user's current
// project, and returns the project workspace path.
func (ps *ProjectStore) SwitchProject(uid int64, userWorkspace, name string) (string, error) {
	name = sanitizeName(name)
	if name == "" {
		return "", fmt.Errorf("invalid project name")
	}

	projectDir := filepath.Join(userWorkspace, name)
	if err := os.MkdirAll(projectDir, 0o750); err != nil {
		return "", fmt.Errorf("create project dir: %w", err)
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.data.Current[uid] = name
	ps.save()
	return projectDir, nil
}

// CurrentProject returns the current project name for a user, or "default".
func (ps *ProjectStore) CurrentProject(uid int64) string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if name, ok := ps.data.Current[uid]; ok && name != "" {
		return name
	}
	return "default"
}

// ListProjects returns all project directory names under the user's workspace.
func (ps *ProjectStore) ListProjects(userWorkspace string) ([]string, error) {
	entries, err := os.ReadDir(userWorkspace)
	if err != nil {
		return nil, err
	}
	var projects []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			projects = append(projects, e.Name())
		}
	}
	return projects, nil
}

// DeleteProject removes a project directory. Cannot delete the current project.
func (ps *ProjectStore) DeleteProject(uid int64, userWorkspace, name string) error {
	name = sanitizeName(name)
	current := ps.CurrentProject(uid)
	if name == current {
		return fmt.Errorf("cannot delete the active project — switch first with /project <name>")
	}

	projectDir := filepath.Join(userWorkspace, name)
	if _, err := os.Stat(projectDir); os.IsNotExist(err) {
		return fmt.Errorf("project '%s' not found", name)
	}

	return os.RemoveAll(projectDir)
}

// ProjectDir returns the full path to the user's current project workspace.
func (ps *ProjectStore) ProjectDir(uid int64, userWorkspace string) string {
	name := ps.CurrentProject(uid)
	return filepath.Join(userWorkspace, name)
}

// sanitizeName removes path separators and dots to prevent traversal.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ReplaceAll(name, "/", "")
	name = strings.ReplaceAll(name, "\\", "")
	name = strings.ReplaceAll(name, "..", "")
	return name
}
