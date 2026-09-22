package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mdfranz/mcpctl/internal/config"
)

type sourceDefinition struct {
	Client string
	Name   string
	Target string
	Source SourceKind
	Path   string
}

// globalSourceDefinitions reads user-level configs for attribution only. It
// never participates in project writes and ignores values other than server
// names and commands/URLs needed for matching.
func globalSourceDefinitions(dir string) []sourceDefinition {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		absDir = dir
	}
	var definitions []sourceDefinition
	definitions = append(definitions, claudeUserDefinitions(filepath.Join(home, ".claude.json"), absDir)...)
	definitions = append(definitions, codexUserDefinitions(filepath.Join(home, ".codex", "config.toml"))...)
	return definitions
}

func claudeUserDefinitions(path, projectDir string) []sourceDefinition {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var top struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
		Projects   map[string]struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		} `json:"projects"`
	}
	if json.Unmarshal(raw, &top) != nil {
		return nil
	}
	servers := top.MCPServers
	if project, ok := top.Projects[projectDir]; ok && len(project.MCPServers) > 0 {
		servers = project.MCPServers
	}
	definitions := make([]sourceDefinition, 0, len(servers))
	for name, rawServer := range servers {
		var server struct {
			Command string `json:"command"`
			URL     string `json:"url"`
		}
		if json.Unmarshal(rawServer, &server) != nil {
			continue
		}
		definitions = append(definitions, sourceDefinition{Client: "claude", Name: name, Target: serverTarget(server.Command, server.URL), Source: SourceUser, Path: path})
	}
	return definitions
}

func codexUserDefinitions(path string) []sourceDefinition {
	snap, err := config.LoadCodexFile(path)
	if err != nil {
		return nil
	}
	definitions := make([]sourceDefinition, 0, len(snap.Servers))
	for name, server := range snap.Servers {
		definitions = append(definitions, sourceDefinition{Client: "codex", Name: name, Target: configServerTarget(server), Source: SourceGlobal, Path: path})
	}
	return definitions
}

func serverTarget(command, url string) string {
	if command != "" {
		return command
	}
	return url
}

func configServerTarget(server config.Server) string {
	if server.Type == config.ServerTypeStdio {
		return server.Command
	}
	return server.URL
}

func observedTarget(target string) string {
	return strings.TrimSpace(strings.TrimSuffix(target, " (HTTP)"))
}

func applyGlobalAttribution(report *ClientReport, dir, clientName string) {
	definitions := globalSourceDefinitions(dir)
	for i := range report.Results {
		result := &report.Results[i]
		var sameName []sourceDefinition
		for _, definition := range definitions {
			if definition.Client != clientName || definition.Name != result.ServerName {
				continue
			}
			sameName = append(sameName, definition)
			if observedTarget(result.Target) != "" && observedTarget(result.Target) == definition.Target {
				if result.Source == SourceUnknown || result.Source == "" {
					result.Source = definition.Source
					result.SourceConfidence = SourceConfirmed
					result.SourcePath = definition.Path
					result.Scope = ScopeOther
					result.Evidence = append(result.Evidence, Evidence{Summary: "matched user-level configuration at " + definition.Path})
				}
				break
			}
		}
		if len(sameName) > 0 && result.Source == SourceUnknown && result.Target != "" {
			result.Evidence = append(result.Evidence, Evidence{Summary: "same server name exists in user-level configuration but its target differs; possible scope shadowing"})
		}
	}
}
