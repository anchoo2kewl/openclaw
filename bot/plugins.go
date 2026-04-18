package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Plugin represents an installable MCP server plugin.
type Plugin struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	Source      string            `json:"source,omitempty"` // "catalog" or URL
}

// PluginStore manages installed plugins.
type PluginStore struct {
	mu       sync.Mutex
	dir      string // directory for plugin manifest files
	tools    *ToolManager
}

// catalog of well-known community MCP servers that can be installed by name.
var pluginCatalog = map[string]Plugin{
	"memory": {
		Name:        "memory",
		Description: "Persistent memory — store and recall facts across sessions",
		Command:     "npx",
		Args:        []string{"-y", "@anthropic-ai/mcp-memory"},
		Source:      "catalog",
	},
	"brave-search": {
		Name:        "brave-search",
		Description: "Brave Search — web search via Brave API",
		Command:     "npx",
		Args:        []string{"-y", "@anthropic-ai/mcp-brave-search"},
		Source:      "catalog",
	},
	"sqlite": {
		Name:        "sqlite",
		Description: "SQLite — query and manage SQLite databases",
		Command:     "npx",
		Args:        []string{"-y", "@anthropic-ai/mcp-sqlite"},
		Source:      "catalog",
	},
	"puppeteer": {
		Name:        "puppeteer",
		Description: "Puppeteer — browser automation and screenshots",
		Command:     "npx",
		Args:        []string{"-y", "@anthropic-ai/mcp-puppeteer"},
		Source:      "catalog",
	},
	"sequential-thinking": {
		Name:        "sequential-thinking",
		Description: "Sequential Thinking — structured step-by-step reasoning",
		Command:     "npx",
		Args:        []string{"-y", "@anthropic-ai/mcp-sequential-thinking"},
		Source:      "catalog",
	},
}

func NewPluginStore(dir string, tools *ToolManager) *PluginStore {
	os.MkdirAll(dir, 0o750)
	return &PluginStore{
		dir:   dir,
		tools: tools,
	}
}

// Install installs a plugin by catalog name or custom manifest.
func (ps *PluginStore) Install(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))

	// Check catalog first.
	plugin, ok := pluginCatalog[name]
	if !ok {
		return fmt.Errorf("unknown plugin '%s'. Use /plugin catalog to see available plugins", name)
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	// Save manifest.
	data, _ := json.MarshalIndent(plugin, "", "  ")
	manifestPath := filepath.Join(ps.dir, name+".json")
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	// Register as MCP server.
	if ps.tools != nil {
		ps.tools.AddCustomTool(name, plugin.Command, plugin.Args)
	}

	return nil
}

// InstallCustom installs a custom plugin with explicit command/args.
func (ps *PluginStore) InstallCustom(name, command string, args []string, description string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || command == "" {
		return fmt.Errorf("name and command are required")
	}

	plugin := Plugin{
		Name:        name,
		Description: description,
		Command:     command,
		Args:        args,
		Source:      "custom",
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	data, _ := json.MarshalIndent(plugin, "", "  ")
	manifestPath := filepath.Join(ps.dir, name+".json")
	if err := os.WriteFile(manifestPath, data, 0o600); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	if ps.tools != nil {
		ps.tools.AddCustomTool(name, command, args)
	}

	return nil
}

// Remove uninstalls a plugin.
func (ps *PluginStore) Remove(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))

	ps.mu.Lock()
	defer ps.mu.Unlock()

	manifestPath := filepath.Join(ps.dir, name+".json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		return fmt.Errorf("plugin '%s' is not installed", name)
	}

	os.Remove(manifestPath)

	// Remove from MCP settings.
	if ps.tools != nil {
		ps.tools.DisableTool(name)
	}

	return nil
}

// List returns all installed plugins.
func (ps *PluginStore) List() []Plugin {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	entries, _ := os.ReadDir(ps.dir)
	var plugins []Plugin
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ps.dir, e.Name()))
		if err != nil {
			continue
		}
		var p Plugin
		if json.Unmarshal(data, &p) == nil {
			plugins = append(plugins, p)
		}
	}
	return plugins
}

// FormatPluginList returns a formatted list for Telegram.
func (ps *PluginStore) FormatPluginList() string {
	installed := ps.List()
	if len(installed) == 0 {
		return "No plugins installed.\nUse /plugin catalog to see available plugins.\nUse /plugin install <name> to install one."
	}

	var sb strings.Builder
	sb.WriteString("Installed plugins:\n\n")
	for _, p := range installed {
		src := ""
		if p.Source == "custom" {
			src = " (custom)"
		}
		fmt.Fprintf(&sb, "  %s — %s%s\n", p.Name, p.Description, src)
	}
	sb.WriteString("\nUse /plugin remove <name> to uninstall.")
	return sb.String()
}

// FormatCatalog returns a formatted catalog listing.
func FormatCatalog() string {
	var sb strings.Builder
	sb.WriteString("Plugin catalog:\n\n")
	for name, p := range pluginCatalog {
		fmt.Fprintf(&sb, "  %s — %s\n", name, p.Description)
	}
	sb.WriteString("\nUse /plugin install <name> to install.\n")
	sb.WriteString("Custom: /plugin custom <name> <command> [args...]")
	return sb.String()
}
