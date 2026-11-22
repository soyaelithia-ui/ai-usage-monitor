package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
)

const (
	ScopeWorkspace = "workspace"
	ScopeGlobal    = "global"
	managedMarker  = "ai-usage-monitor"
)

// InstallResult describes the outcome of installing or uninstalling a native agent hook.
type InstallResult struct {
	Agent   string   `json:"agent"`
	Scope   string   `json:"scope"`
	Path    string   `json:"path"`
	Changed bool     `json:"changed"`
	DryRun  bool     `json:"dryRun"`
	Events  []string `json:"events"`
	Preview string   `json:"preview,omitempty"`
}

// IntegrationStatus is a privacy-safe view of a native hook integration for the web UI and /api/hooks.
// It deliberately never exposes resolved local filesystem paths or home directory usernames.
type IntegrationStatus struct {
	ID              string      `json:"id"`
	AgentID         string      `json:"agentId"`
	Name            string      `json:"name"`
	Detected        bool        `json:"detected"`
	Installed       bool        `json:"installed"`
	InstallScope    string      `json:"installScope,omitempty"`
	WorkspaceConfig string      `json:"workspaceConfig"`
	GlobalConfig    string      `json:"globalConfig"`
	Events          []string    `json:"events"`
	Channel         string      `json:"channel"`
	NonInterrupting bool        `json:"nonInterrupting"`
	SupportsInstall bool        `json:"supportsInstall"`
	InstallCommand  string      `json:"installCommand"`
	Description     string      `json:"description"`
	Quota           QuotaStatus `json:"quota"`
}

// HooksOverview aggregates global/per-agent quota state and native hook integration statuses.
type HooksOverview struct {
	Window       string              `json:"window"`
	GlobalQuota  QuotaStatus         `json:"globalQuota"`
	Integrations []IntegrationStatus `json:"integrations"`
}

// HookCommand returns the shell command invoked by an agent hook.
func HookCommand(binaryPath, agent string) string {
	bin := strings.TrimSpace(binaryPath)
	if bin == "" {
		bin = "ai-usage-monitor"
	}
	if strings.ContainsAny(bin, " \t'\"") {
		bin = `"` + strings.ReplaceAll(bin, `"`, `\"`) + `"`
	}
	return fmt.Sprintf("%s hook --agent %s", bin, agent)
}

// ResolveTargetPath computes the local config file path for an agent and scope ("workspace" or "global").
func ResolveTargetPath(agent, scope, cwd, home string) (string, error) {
	if scope == "" {
		scope = ScopeWorkspace
	}
	if scope != ScopeWorkspace && scope != ScopeGlobal {
		return "", fmt.Errorf("invalid scope %q (expected workspace or global)", scope)
	}
	if scope == ScopeGlobal && home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil || home == "" {
			return "", errors.New("cannot resolve user home directory")
		}
	}
	if scope == ScopeWorkspace && cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil || cwd == "" {
			return "", errors.New("cannot resolve working directory")
		}
	}

	switch strings.ToLower(strings.TrimSpace(agent)) {
	case "antigravity":
		if scope == ScopeGlobal {
			return filepath.Join(home, ".gemini", "config", "hooks.json"), nil
		}
		return filepath.Join(cwd, ".agents", "hooks.json"), nil
	case "codex":
		if scope == ScopeGlobal {
			return filepath.Join(home, ".codex", "hooks.json"), nil
		}
		return filepath.Join(cwd, ".codex", "hooks.json"), nil
	case "claude", "claude-statusline":
		if scope == ScopeGlobal {
			return filepath.Join(home, ".claude", "settings.json"), nil
		}
		return filepath.Join(cwd, ".claude", "settings.json"), nil
	case "gemini":
		if scope == ScopeGlobal {
			return filepath.Join(home, ".gemini", "settings.json"), nil
		}
		return filepath.Join(cwd, ".gemini", "settings.json"), nil
	case "cursor":
		if scope == ScopeGlobal {
			return filepath.Join(home, ".cursor", "hooks.json"), nil
		}
		return filepath.Join(cwd, ".cursor", "hooks.json"), nil
	case "copilot":
		if scope == ScopeGlobal {
			return filepath.Join(home, ".copilot", "hooks", "ai-usage-monitor.json"), nil
		}
		return filepath.Join(cwd, ".github", "hooks", "ai-usage-monitor.json"), nil
	default:
		return "", fmt.Errorf("agent %q does not have a managed hook config file", agent)
	}
}

// GenerateSnippet builds a standalone native JSON config snippet for the specified agent.
func GenerateSnippet(agent, binaryPath string) (map[string]any, error) {
	ag := strings.ToLower(strings.TrimSpace(agent))
	cmd := HookCommand(binaryPath, ag)
	doc := make(map[string]any)
	if err := mergeAgentHook(doc, ag, cmd); err != nil {
		return nil, err
	}
	return doc, nil
}

// Install idempotently merges the non-interrupting AI Usage Monitor hook into the agent's native config file,
// preserving all existing user hooks and settings.
func Install(agent, scope, cwd, home, binaryPath string, dryRun bool) (InstallResult, error) {
	ag := strings.ToLower(strings.TrimSpace(agent))
	spec, ok := LookupSpec(ag)
	if !ok || !spec.SupportsInstall {
		return InstallResult{}, fmt.Errorf("unsupported install agent %q", agent)
	}
	target, err := ResolveTargetPath(ag, scope, cwd, home)
	if err != nil {
		return InstallResult{}, err
	}

	doc, mode, err := readJSONFileSafe(target)
	if err != nil {
		return InstallResult{}, err
	}
	beforeBytes, _ := json.MarshalIndent(doc, "", "  ")

	cmd := HookCommand(binaryPath, ag)
	if err := mergeAgentHook(doc, ag, cmd); err != nil {
		return InstallResult{}, err
	}
	afterBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return InstallResult{}, err
	}
	afterBytes = append(afterBytes, '\n')
	changed := string(beforeBytes)+"\n" != string(afterBytes)

	res := InstallResult{
		Agent:   ag,
		Scope:   scope,
		Path:    target,
		Changed: changed,
		DryRun:  dryRun,
		Events:  spec.Events,
		Preview: string(afterBytes),
	}
	if !dryRun && changed {
		if err := writeFileAtomicSafe(target, afterBytes, mode); err != nil {
			return InstallResult{}, err
		}
	}
	return res, nil
}

// Uninstall removes only AI Usage Monitor entries from the agent's native config file, preserving user entries.
func Uninstall(agent, scope, cwd, home string, dryRun bool) (InstallResult, error) {
	ag := strings.ToLower(strings.TrimSpace(agent))
	spec, ok := LookupSpec(ag)
	if !ok || !spec.SupportsInstall {
		return InstallResult{}, fmt.Errorf("unsupported uninstall agent %q", agent)
	}
	target, err := ResolveTargetPath(ag, scope, cwd, home)
	if err != nil {
		return InstallResult{}, err
	}

	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		return InstallResult{Agent: ag, Scope: scope, Path: target, Changed: false, DryRun: dryRun, Events: spec.Events}, nil
	}

	doc, mode, err := readJSONFileSafe(target)
	if err != nil {
		return InstallResult{}, err
	}
	beforeBytes, _ := json.MarshalIndent(doc, "", "  ")

	removeAgentHook(doc, ag)
	afterBytes, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return InstallResult{}, err
	}
	afterBytes = append(afterBytes, '\n')
	changed := string(beforeBytes)+"\n" != string(afterBytes)

	res := InstallResult{
		Agent:   ag,
		Scope:   scope,
		Path:    target,
		Changed: changed,
		DryRun:  dryRun,
		Events:  spec.Events,
		Preview: string(afterBytes),
	}
	if !dryRun && changed {
		if ag == "copilot" && isEmptyHooksDoc(doc) {
			_ = os.Remove(target)
		} else if err := writeFileAtomicSafe(target, afterBytes, mode); err != nil {
			return InstallResult{}, err
		}
	}
	return res, nil
}

func isEmptyHooksDoc(doc map[string]any) bool {
	h, ok := doc["hooks"].(map[string]any)
	if !ok {
		return len(doc) == 0
	}
	return len(h) == 0
}

func readJSONFileSafe(path string) (map[string]any, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]any), 0o600, nil
		}
		return nil, 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, fmt.Errorf("refusing to follow symlink: %s", path)
	}
	if !info.Mode().IsRegular() {
		return nil, 0, fmt.Errorf("target is not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return make(map[string]any), info.Mode().Perm(), nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil || doc == nil {
		return nil, 0, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return doc, info.Mode().Perm(), nil
}

func writeFileAtomicSafe(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to overwrite symlink: %s", path)
	}
	tmp, err := os.CreateTemp(dir, ".ai-usage-monitor-hook-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if m, ok := parent[key].(map[string]any); ok && m != nil {
		return m
	}
	m := make(map[string]any)
	parent[key] = m
	return m
}

func isManagedCommand(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	return strings.Contains(s, managedMarker+" hook") || strings.Contains(s, managedMarker+`" hook`)
}

func filterManagedGroupedEntries(raw any) []any {
	arr, ok := raw.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		if name, _ := m["name"].(string); name == managedMarker {
			continue
		}
		if isManagedCommand(m["command"]) || isManagedCommand(m["bash"]) {
			continue
		}
		if nested, ok := m["hooks"].([]any); ok {
			kept := make([]any, 0, len(nested))
			for _, h := range nested {
				hm, ok := h.(map[string]any)
				if !ok {
					kept = append(kept, h)
					continue
				}
				if name, _ := hm["name"].(string); name == managedMarker {
					continue
				}
				if isManagedCommand(hm["command"]) || isManagedCommand(hm["bash"]) {
					continue
				}
				kept = append(kept, h)
			}
			if len(kept) == 0 {
				continue
			}
			m["hooks"] = kept
		}
		out = append(out, m)
	}
	return out
}

func mergeAgentHook(doc map[string]any, agent, cmd string) error {
	switch agent {
	case "antigravity":
		doc[managedMarker] = map[string]any{
			"enabled": true,
			"SessionStart": []any{
				map[string]any{
					"type":    "command",
					"command": cmd,
					"timeout": 5,
				},
			},
			"PostInvocation": []any{
				map[string]any{
					"type":    "command",
					"command": cmd,
					"timeout": 5,
				},
			},
		}
	case "codex":
		hooksObj := ensureMap(doc, "hooks")
		for _, ev := range []string{"SessionStart", "Stop"} {
			existing := filterManagedGroupedEntries(hooksObj[ev])
			existing = append(existing, map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd,
						"timeout": 5,
					},
				},
			})
			hooksObj[ev] = existing
		}
	case "claude":
		hooksObj := ensureMap(doc, "hooks")
		for _, ev := range []string{"SessionStart", "Stop"} {
			existing := filterManagedGroupedEntries(hooksObj[ev])
			existing = append(existing, map[string]any{
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": cmd,
						"timeout": 5,
					},
				},
			})
			hooksObj[ev] = existing
		}
	case "claude-statusline":
		doc["statusLine"] = map[string]any{
			"type":    "command",
			"command": cmd,
			"padding": 1,
		}
	case "gemini":
		hooksObj := ensureMap(doc, "hooks")
		for _, ev := range []string{"SessionStart", "AfterAgent"} {
			existing := filterManagedGroupedEntries(hooksObj[ev])
			existing = append(existing, map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{
						"name":    managedMarker,
						"type":    "command",
						"command": cmd,
						"timeout": 5000,
					},
				},
			})
			hooksObj[ev] = existing
		}
	case "cursor":
		doc["version"] = 1
		hooksObj := ensureMap(doc, "hooks")
		for _, ev := range []string{"afterAgentResponse", "stop"} {
			existing := filterManagedGroupedEntries(hooksObj[ev])
			existing = append(existing, map[string]any{
				"command": cmd,
			})
			hooksObj[ev] = existing
		}
	case "copilot":
		doc["version"] = 1
		hooksObj := ensureMap(doc, "hooks")
		for _, ev := range []string{"postToolUse", "sessionEnd"} {
			existing := filterManagedGroupedEntries(hooksObj[ev])
			existing = append(existing, map[string]any{
				"type":       "command",
				"bash":       cmd,
				"powershell": cmd,
				"timeoutSec": 5,
			})
			hooksObj[ev] = existing
		}
	default:
		return fmt.Errorf("unsupported agent %q", agent)
	}
	return nil
}

func removeAgentHook(doc map[string]any, agent string) {
	switch agent {
	case "antigravity":
		delete(doc, managedMarker)
	case "claude-statusline":
		if sl, ok := doc["statusLine"].(map[string]any); ok && isManagedCommand(sl["command"]) {
			delete(doc, "statusLine")
		}
	case "codex", "claude":
		if hooksObj, ok := doc["hooks"].(map[string]any); ok {
			for _, ev := range []string{"SessionStart", "PostToolUse", "Stop"} {
				if _, exists := hooksObj[ev]; exists {
					kept := filterManagedGroupedEntries(hooksObj[ev])
					if len(kept) == 0 {
						delete(hooksObj, ev)
					} else {
						hooksObj[ev] = kept
					}
				}
			}
			if len(hooksObj) == 0 {
				delete(doc, "hooks")
			}
		}
	case "gemini":
		if hooksObj, ok := doc["hooks"].(map[string]any); ok {
			for _, ev := range []string{"SessionStart", "BeforeAgent", "AfterAgent"} {
				if _, exists := hooksObj[ev]; exists {
					kept := filterManagedGroupedEntries(hooksObj[ev])
					if len(kept) == 0 {
						delete(hooksObj, ev)
					} else {
						hooksObj[ev] = kept
					}
				}
			}
			if len(hooksObj) == 0 {
				delete(doc, "hooks")
			}
		}
	case "cursor":
		if hooksObj, ok := doc["hooks"].(map[string]any); ok {
			for _, ev := range []string{"afterAgentResponse", "stop"} {
				if _, exists := hooksObj[ev]; exists {
					kept := filterManagedGroupedEntries(hooksObj[ev])
					if len(kept) == 0 {
						delete(hooksObj, ev)
					} else {
						hooksObj[ev] = kept
					}
				}
			}
		}
	case "copilot":
		if hooksObj, ok := doc["hooks"].(map[string]any); ok {
			for _, ev := range []string{"postToolUse", "sessionEnd"} {
				if _, exists := hooksObj[ev]; exists {
					kept := filterManagedGroupedEntries(hooksObj[ev])
					if len(kept) == 0 {
						delete(hooksObj, ev)
					} else {
						hooksObj[ev] = kept
					}
				}
			}
		}
	}
}

func isInstalledAtPath(agent, path string) bool {
	doc, _, err := readJSONFileSafe(path)
	if err != nil || len(doc) == 0 {
		return false
	}
	switch agent {
	case "antigravity":
		_, ok := doc[managedMarker]
		return ok
	case "claude-statusline":
		sl, ok := doc["statusLine"].(map[string]any)
		return ok && isManagedCommand(sl["command"])
	default:
		hooksObj, ok := doc["hooks"].(map[string]any)
		if !ok || len(hooksObj) == 0 {
			return false
		}
		raw, err := json.Marshal(hooksObj)
		if err != nil {
			return false
		}
		needle := managedMarker + " hook --agent " + agent
		s := string(raw)
		for idx := strings.Index(s, needle); idx >= 0; {
			afterIdx := idx + len(needle)
			if afterIdx >= len(s) {
				return true
			}
			ch := s[afterIdx]
			if ch == '"' || ch == ' ' || ch == '\\' {
				return true
			}
			next := strings.Index(s[afterIdx:], needle)
			if next < 0 {
				break
			}
			idx = afterIdx + next
		}
		return false
	}
}

// InspectIntegrations inspects local hook installation status and real-time quota progress
// without ever leaking absolute local filesystem paths or home directory names.
func InspectIntegrations(cfg *config.Config, store *db.Store, cwd string) HooksOverview {
	home, _ := os.UserHomeDir()
	detected := make(map[string]bool)
	for _, tech := range ingest.DiscoverLocalTechnologies(cfg) {
		detected[tech.ID] = tech.Detected
	}

	globalSnap := QuerySnapshot(store, cfg, QueryOptions{AgentID: ""})
	out := HooksOverview{
		Window:       globalSnap.Window,
		GlobalQuota:  globalSnap.GlobalQuota,
		Integrations: make([]IntegrationStatus, 0, len(SupportedSpecs())),
	}

	for _, sp := range SupportedSpecs() {
		installed := false
		installScope := ""
		if sp.SupportsInstall {
			if p, err := ResolveTargetPath(sp.ID, ScopeWorkspace, cwd, home); err == nil && isInstalledAtPath(sp.ID, p) {
				installed = true
				installScope = ScopeWorkspace
			} else if p, err := ResolveTargetPath(sp.ID, ScopeGlobal, cwd, home); err == nil && isInstalledAtPath(sp.ID, p) {
				installed = true
				installScope = ScopeGlobal
			}
		}

		snap := QuerySnapshot(store, cfg, QueryOptions{AgentID: sp.AgentID})
		installCmd := fmt.Sprintf("ai-usage-monitor hooks install --agent %s", sp.ID)
		if !sp.SupportsInstall {
			installCmd = "ai-usage-monitor hook --agent generic"
		}

		out.Integrations = append(out.Integrations, IntegrationStatus{
			ID:              sp.ID,
			AgentID:         sp.AgentID,
			Name:            sp.Name,
			Detected:        sp.AgentID == "" || detected[sp.AgentID],
			Installed:       installed,
			InstallScope:    installScope,
			WorkspaceConfig: sp.WorkspaceConfig,
			GlobalConfig:    sp.GlobalConfig,
			Events:          sp.Events,
			Channel:         sp.Channel,
			NonInterrupting: sp.NonInterrupting,
			SupportsInstall: sp.SupportsInstall,
			InstallCommand:  installCmd,
			Description:     sp.Description,
			Quota:           snap.AgentQuota,
		})
	}
	return out
}
