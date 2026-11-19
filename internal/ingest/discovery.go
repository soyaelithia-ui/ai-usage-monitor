package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/tokenizer"
)

// DiscoveredTech describes an auto-detected local agent technology without leaking local paths.
type DiscoveredTech struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Reader     string `json:"reader"`
	Tone       string `json:"tone"`
	Symbol     string `json:"symbol"`
	Detected   bool   `json:"detected"`
	Configured bool   `json:"configured"`
	Carrier    string `json:"carrier,omitempty"`
	path       string
}

func hasBinary(names ...string) bool {
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = "/usr/local/bin:/usr/bin:/bin"
	}
	dirs := strings.Split(pathEnv, string(os.PathListSeparator))
	for _, name := range names {
		if name == "" {
			continue
		}
		for _, dir := range dirs {
			if dir == "" {
				continue
			}
			p := filepath.Join(dir, name)
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return true
			}
		}
	}
	return false
}

func firstExistingPath(candidates []string, wantDir bool) string {
	for _, p := range candidates {
		if p == "" {
			continue
		}
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if wantDir && info.IsDir() {
			return p
		}
		if !wantDir && info.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// DiscoverLocalTechnologies inspects standard local locations and binaries for all 8 agent
// technologies supported by the UI (without following symlinks or leaking local paths).
func DiscoverLocalTechnologies(cfg *config.Config) []DiscoveredTech {
	if cfg == nil {
		cfg = &config.Config{}
	}
	home, _ := os.UserHomeDir()
	xdgData := os.Getenv("XDG_DATA_HOME")
	if xdgData == "" && home != "" {
		xdgData = filepath.Join(home, ".local", "share")
	}
	xdgConfig := os.Getenv("XDG_CONFIG_HOME")
	if xdgConfig == "" && home != "" {
		xdgConfig = filepath.Join(home, ".config")
	}

	codexPath := firstExistingPath([]string{
		cfg.CodexRoot,
		os.Getenv("CODEX_ROOT"),
		filepath.Join(xdgData, "codex"),
		filepath.Join(home, ".codex"),
	}, true)

	opencodePath := firstExistingPath([]string{
		cfg.OpenCodeDB,
		os.Getenv("OPENCODE_DB"),
		filepath.Join(xdgData, "opencode", "opencode.db"),
		filepath.Join(home, ".opencode", "opencode.db"),
	}, false)

	claudePath := firstExistingPath([]string{
		cfg.ClaudeRoot,
		os.Getenv("CLAUDE_ROOT"),
		filepath.Join(home, ".claude", "projects"),
		filepath.Join(home, ".claude"),
	}, true)

	antigravityPath := firstExistingPath([]string{
		cfg.AntigravityRoot,
		os.Getenv("ANTIGRAVITY_ROOT"),
		filepath.Join(home, ".gemini", "antigravity-cli"),
		filepath.Join(home, ".gemini", "antigravity"),
		filepath.Join(xdgData, "antigravity"),
	}, true)

	// Gemini CLI paths intentionally avoid ~/.gemini/antigravity* to prevent false positives.
	geminiPath := firstExistingPath([]string{
		cfg.GeminiRoot,
		os.Getenv("GEMINI_ROOT"),
		filepath.Join(home, ".gemini", "sessions"),
		filepath.Join(home, ".gemini", "telemetry"),
		filepath.Join(xdgData, "gemini"),
	}, true)
	geminiSettings := firstExistingPath([]string{
		filepath.Join(home, ".gemini", "settings.json"),
	}, false)

	cursorPath := firstExistingPath([]string{
		cfg.CursorRoot,
		os.Getenv("CURSOR_ROOT"),
		filepath.Join(home, ".cursor", "logs"),
		filepath.Join(home, ".cursor"),
		filepath.Join(xdgConfig, "Cursor"),
		filepath.Join(home, "Library", "Application Support", "Cursor"),
	}, true)

	copilotPath := firstExistingPath([]string{
		cfg.CopilotRoot,
		os.Getenv("COPILOT_ROOT"),
		filepath.Join(home, ".copilot", "session-state"),
		filepath.Join(home, ".copilot"),
		filepath.Join(xdgConfig, "github-copilot"),
		filepath.Join(xdgData, "github-copilot"),
	}, true)

	hermesPath := firstExistingPath([]string{
		cfg.HermesRoot,
		os.Getenv("HERMES_ROOT"),
		filepath.Join(home, ".hermes", "sessions"),
		filepath.Join(home, ".hermes"),
		filepath.Join(xdgData, "hermes"),
		filepath.Join(xdgConfig, "hermes"),
	}, true)

	return []DiscoveredTech{
		{
			ID:         "codex",
			Name:       "Codex",
			Reader:     "jsonl",
			Tone:       "mint",
			Symbol:     "C",
			Detected:   codexPath != "" || hasBinary("codex"),
			Configured: codexPath != "",
			path:       codexPath,
		},
		{
			ID:         "opencode",
			Name:       "OpenCode",
			Reader:     "sqlite",
			Tone:       "blue",
			Symbol:     "O",
			Detected:   opencodePath != "" || hasBinary("opencode"),
			Configured: opencodePath != "",
			path:       opencodePath,
		},
		{
			ID:         "claude",
			Name:       "Claude Code",
			Reader:     "jsonl",
			Tone:       "amber",
			Symbol:     "C",
			Detected:   claudePath != "" || hasBinary("claude"),
			Configured: claudePath != "",
			path:       claudePath,
		},
		{
			ID:         "antigravity",
			Name:       "Antigravity",
			Reader:     "contract",
			Tone:       "violet",
			Symbol:     "A",
			Detected:   antigravityPath != "" || hasBinary("agy", "antigravity"),
			Configured: antigravityPath != "",
			path:       antigravityPath,
		},
		{
			ID:         "gemini",
			Name:       "Gemini CLI",
			Reader:     "jsonl",
			Tone:       "blue",
			Symbol:     "G",
			Detected:   geminiPath != "" || geminiSettings != "" || hasBinary("gemini"),
			Configured: geminiPath != "",
			path:       geminiPath,
		},
		{
			ID:         "cursor",
			Name:       "Cursor",
			Reader:     "jsonl",
			Tone:       "cyan",
			Symbol:     "U",
			Detected:   cursorPath != "" || hasBinary("cursor", "cursor-agent"),
			Configured: cursorPath != "",
			path:       cursorPath,
		},
		{
			ID:         "copilot",
			Name:       "GitHub Copilot",
			Reader:     "jsonl",
			Tone:       "rose",
			Symbol:     "P",
			Detected:   copilotPath != "" || hasBinary("copilot", "github-copilot-cli"),
			Configured: copilotPath != "",
			path:       copilotPath,
		},
		{
			ID:         "hermes",
			Name:       "Hermes Agent",
			Reader:     "jsonl",
			Tone:       "violet",
			Symbol:     "H",
			Detected:   hermesPath != "" || hasBinary("hermes", "hermes-agent"),
			Configured: hermesPath != "",
			path:       hermesPath,
		},
	}
}

// Manager coordinates all passive JSONL collectors and SQLite readers.
type Manager struct {
	store    *db.Store
	cfg      *config.Config
	counter  *tokenizer.Counter
	onChange func()

	mu         sync.Mutex
	collectors map[string]*Collector
	sqliteRdrs map[string]*SQLiteReader
}

// NewManager initializes passive readers from config and local discovery with shared hidden tokenizers.
func NewManager(store *db.Store, cfg *config.Config, onChange func()) *Manager {
	if onChange == nil {
		onChange = func() {}
	}
	counter := tokenizer.NewCounter("")
	m := &Manager{
		store:      store,
		cfg:        cfg,
		counter:    counter,
		onChange:   onChange,
		collectors: make(map[string]*Collector),
		sqliteRdrs: make(map[string]*SQLiteReader),
	}

	discovered := DiscoverLocalTechnologies(cfg)
	discMap := make(map[string]DiscoveredTech, len(discovered))
	for _, d := range discovered {
		discMap[d.ID] = d
	}

	codexRoot := cfg.CodexRoot
	if codexRoot == "" {
		codexRoot = discMap["codex"].path
	}
	if col, err := NewCollectorWithCounter(store, codexRoot, CodexSpec(), counter, onChange); err == nil {
		m.collectors["codex"] = col
	}

	optionalJSONL := []struct {
		id       string
		cfgRoot  string
		specFunc func() AgentSpec
	}{
		{"claude", cfg.ClaudeRoot, ClaudeSpec},
		{"gemini", cfg.GeminiRoot, GeminiSpec},
		{"cursor", cfg.CursorRoot, CursorSpec},
		{"copilot", cfg.CopilotRoot, CopilotSpec},
		{"hermes", cfg.HermesRoot, HermesSpec},
	}
	for _, item := range optionalJSONL {
		root := item.cfgRoot
		if root == "" {
			root = discMap[item.id].path
		}
		if root != "" {
			if col, err := NewCollectorWithCounter(store, root, item.specFunc(), counter, onChange); err == nil {
				m.collectors[item.id] = col
			}
		}
	}

	ocPath := cfg.OpenCodeDB
	if ocPath == "" {
		ocPath = discMap["opencode"].path
	}
	m.sqliteRdrs["opencode"] = NewSQLiteReaderWithCounter(store, ocPath, OpenCodeSpec(), counter, onChange)

	return m
}

// Start starts all configured collectors and SQLite readers.
func (m *Manager) Start() {
	m.mu.Lock()
	cols := make([]*Collector, 0, len(m.collectors))
	for _, c := range m.collectors {
		cols = append(cols, c)
	}
	rdrs := make([]*SQLiteReader, 0, len(m.sqliteRdrs))
	for _, r := range m.sqliteRdrs {
		rdrs = append(rdrs, r)
	}
	m.mu.Unlock()

	for _, c := range cols {
		c.Start()
	}
	for _, r := range rdrs {
		r.Start()
	}
}

// Stop stops all readers cleanly.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.collectors {
		c.Stop()
	}
	for _, r := range m.sqliteRdrs {
		r.Stop()
	}
}

// Scan triggers an immediate scan across all active readers.
func (m *Manager) Scan() {
	m.mu.Lock()
	cols := make([]*Collector, 0, len(m.collectors))
	for _, c := range m.collectors {
		cols = append(cols, c)
	}
	rdrs := make([]*SQLiteReader, 0, len(m.sqliteRdrs))
	for _, r := range m.sqliteRdrs {
		rdrs = append(rdrs, r)
	}
	m.mu.Unlock()

	for _, c := range cols {
		c.Scan()
	}
	for _, r := range rdrs {
		r.Scan()
	}
}

// StatusMap returns the current reader statuses keyed by agent ID.
func (m *Manager) StatusMap() map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]any)
	for id, c := range m.collectors {
		out[id] = c.Status()
	}
	for id, r := range m.sqliteRdrs {
		out[id] = r.Status()
	}
	// Built-in contract agent status
	out["antigravity"] = CollectorStatus{
		State:    "running",
		LastScan: m.store.NowFn().UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	for _, da := range m.safeDynamicAgents() {
		if _, exists := out[da.ID]; !exists {
			out[da.ID] = CollectorStatus{
				State:    "running",
				LastScan: m.store.NowFn().UTC().Format("2006-01-02T15:04:05.000Z"),
			}
		}
	}
	return out
}

func (m *Manager) safeDynamicAgents() []db.DynamicAgent {
	list, err := m.store.GetDynamicAgents()
	if err != nil {
		return nil
	}
	return list
}

// BuiltinAgentIDs returns true if id belongs to an immutable built-in catalog agent.
func BuiltinAgentIDs(id string) bool {
	for _, a := range pricing.BuiltinAgents {
		if a.ID == id {
			return true
		}
	}
	return false
}
