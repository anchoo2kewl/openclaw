package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ToolManager manages MCP server configurations for Claude Code.
// It reads/writes the Claude Code settings.json to enable tool integrations.
type ToolManager struct {
	mu          sync.Mutex
	claudeHome  string // path to .claude directory
	settingsPath string
}

// mcpServerConfig represents a single MCP server entry in Claude Code settings.
type mcpServerConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// claudeSettings is a subset of Claude Code's settings.json that we manage.
type claudeSettings struct {
	MCPServers map[string]mcpServerConfig `json:"mcpServers,omitempty"`
}

// builtinTools are pre-configured MCP servers that can be enabled by name.
var builtinTools = map[string]struct {
	Description string
	Config      mcpServerConfig
}{
	"github": {
		Description: "GitHub — issues, PRs, repos, code search",
		Config: mcpServerConfig{
			Command: "gh",
			Args:    []string{"mcp"},
		},
	},
	"filesystem": {
		Description: "Filesystem — read/write files, directory listing",
		Config: mcpServerConfig{
			Command: "npx",
			Args:    []string{"-y", "@anthropic-ai/mcp-filesystem"},
		},
	},
	"fetch": {
		Description: "Fetch — HTTP requests, web scraping",
		Config: mcpServerConfig{
			Command: "npx",
			Args:    []string{"-y", "@anthropic-ai/mcp-fetch"},
		},
	},
}

func NewToolManager(claudeHome string) *ToolManager {
	return &ToolManager{
		claudeHome:   claudeHome,
		settingsPath: filepath.Join(claudeHome, "settings.json"),
	}
}

func (tm *ToolManager) loadSettings() claudeSettings {
	data, err := os.ReadFile(tm.settingsPath)
	if err != nil {
		return claudeSettings{MCPServers: make(map[string]mcpServerConfig)}
	}
	var s claudeSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return claudeSettings{MCPServers: make(map[string]mcpServerConfig)}
	}
	if s.MCPServers == nil {
		s.MCPServers = make(map[string]mcpServerConfig)
	}
	return s
}

func (tm *ToolManager) saveSettings(s claudeSettings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tm.settingsPath, data, 0o600)
}

// EnableTool enables a built-in tool by name.
func (tm *ToolManager) EnableTool(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	builtin, ok := builtinTools[name]
	if !ok {
		return fmt.Errorf("unknown tool '%s'. Available: %s", name, tm.AvailableToolNames())
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	settings := tm.loadSettings()
	settings.MCPServers[name] = builtin.Config
	return tm.saveSettings(settings)
}

// DisableTool removes an MCP server by name.
func (tm *ToolManager) DisableTool(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))

	tm.mu.Lock()
	defer tm.mu.Unlock()

	settings := tm.loadSettings()
	if _, ok := settings.MCPServers[name]; !ok {
		return fmt.Errorf("tool '%s' is not enabled", name)
	}
	delete(settings.MCPServers, name)
	return tm.saveSettings(settings)
}

// ListEnabled returns currently enabled MCP servers.
func (tm *ToolManager) ListEnabled() map[string]mcpServerConfig {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	return tm.loadSettings().MCPServers
}

// AvailableToolNames returns a comma-separated list of built-in tool names.
func (tm *ToolManager) AvailableToolNames() string {
	names := make([]string, 0, len(builtinTools))
	for name := range builtinTools {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

// FormatToolList returns a formatted string of available and enabled tools.
func (tm *ToolManager) FormatToolList() string {
	enabled := tm.ListEnabled()
	var sb strings.Builder

	sb.WriteString("Available tools:\n")
	for name, tool := range builtinTools {
		status := "  "
		if _, ok := enabled[name]; ok {
			status = "✅"
		}
		fmt.Fprintf(&sb, "%s %s — %s\n", status, name, tool.Description)
	}

	// Show any custom MCP servers.
	for name := range enabled {
		if _, isBuiltin := builtinTools[name]; !isBuiltin {
			fmt.Fprintf(&sb, "✅ %s (custom)\n", name)
		}
	}

	sb.WriteString("\nUse /tool enable <name> or /tool disable <name>")
	return sb.String()
}

// AddCustomTool adds a custom MCP server configuration.
func (tm *ToolManager) AddCustomTool(name, command string, args []string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return fmt.Errorf("tool name is required")
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	settings := tm.loadSettings()
	settings.MCPServers[name] = mcpServerConfig{
		Command: command,
		Args:    args,
	}
	return tm.saveSettings(settings)
}
