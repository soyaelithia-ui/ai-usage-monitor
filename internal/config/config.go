package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var secretFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// Config holds runtime configuration for the AI Usage Monitor backend.
type Config struct {
	Port               int
	BindAddr           string
	DBPath             string
	MachineKeyPath     string
	Origin             string
	AllowedOrigins     []string
	GatewayKey         string
	IngestKey          string
	CodexRoot          string
	OpenCodeDB         string
	ClaudeRoot         string
	AntigravityRoot    string
	GeminiRoot         string
	CursorRoot         string
	CopilotRoot        string
	HermesRoot         string
	PricingSyncEnabled bool
	PricingCachePath   string
	PricingSources     []string
	RateLimitMax       int
}

type fileConfig struct {
	Origin             string   `json:"origin"`
	AllowedOrigins     []string `json:"allowedOrigins"`
	BackendPort        int      `json:"backendPort"`
	GatewayPort        int      `json:"gatewayPort"`
	GatewayKey         string   `json:"gatewayKey"`
	IngestKey          string   `json:"ingestKey"`
	DB                 string   `json:"db"`
	CodexRoot          string   `json:"codexRoot"`
	OpenCodeDB         string   `json:"openCodeDb"`
	ClaudeRoot         string   `json:"claudeRoot"`
	AntigravityRoot    string   `json:"antigravityRoot"`
	GeminiRoot         string   `json:"geminiRoot"`
	CursorRoot         string   `json:"cursorRoot"`
	CopilotRoot        string   `json:"copilotRoot"`
	HermesRoot         string   `json:"hermesRoot"`
	PricingSyncEnabled *bool    `json:"pricingSyncEnabled"`
	PricingCachePath   string   `json:"pricingCachePath"`
	PricingSources     []string `json:"pricingSources"`
}

// ConfigPath returns the path to the optional backend.json configuration file.
func ConfigPath() string {
	if p := os.Getenv("USAGE_MONITOR_CONFIG"); p != "" {
		return p
	}
	if p := os.Getenv("PULSE_CONFIG"); p != "" {
		return p
	}
	xdg := os.Getenv("XDG_CONFIG_HOME")
	if xdg == "" {
		home, _ := os.UserHomeDir()
		xdg = filepath.Join(home, ".config")
	}
	return filepath.Join(xdg, "ai-usage-monitor", "backend.json")
}

// Load reads configuration from optional backend.json and environment variables.
func Load() (*Config, error) {
	cfg := &Config{
		Port:           8080,
		BindAddr:       "127.0.0.1",
		DBPath:         "./data/usage.db",
		PricingSources: []string{"models.dev", "litellm", "openrouter"},
		RateLimitMax:   600,
	}

	cfgPath := ConfigPath()
	if raw, err := os.ReadFile(cfgPath); err == nil {
		var fc fileConfig
		if err := json.Unmarshal(raw, &fc); err != nil {
			return nil, fmt.Errorf("invalid config file %s: %w", cfgPath, err)
		}
		dir := filepath.Dir(cfgPath)
		resolveRel := func(p string) string {
			if p == "" || p == ":memory:" || filepath.IsAbs(p) {
				return p
			}
			return filepath.Join(dir, p)
		}
		if fc.BackendPort > 0 {
			cfg.Port = fc.BackendPort
		}
		if fc.DB != "" {
			cfg.DBPath = resolveRel(fc.DB)
		}
		if fc.Origin != "" {
			cfg.Origin = fc.Origin
		}
		if len(fc.AllowedOrigins) > 0 {
			cfg.AllowedOrigins = fc.AllowedOrigins
		}
		if fc.GatewayKey != "" {
			cfg.GatewayKey = fc.GatewayKey
		}
		if fc.IngestKey != "" {
			cfg.IngestKey = fc.IngestKey
		}
		cfg.CodexRoot = resolveRel(fc.CodexRoot)
		cfg.OpenCodeDB = resolveRel(fc.OpenCodeDB)
		cfg.ClaudeRoot = resolveRel(fc.ClaudeRoot)
		cfg.AntigravityRoot = resolveRel(fc.AntigravityRoot)
		cfg.GeminiRoot = resolveRel(fc.GeminiRoot)
		cfg.CursorRoot = resolveRel(fc.CursorRoot)
		cfg.CopilotRoot = resolveRel(fc.CopilotRoot)
		cfg.HermesRoot = resolveRel(fc.HermesRoot)
		if fc.PricingSyncEnabled != nil {
			cfg.PricingSyncEnabled = *fc.PricingSyncEnabled
		}
		if fc.PricingCachePath != "" {
			cfg.PricingCachePath = resolveRel(fc.PricingCachePath)
		}
		if len(fc.PricingSources) > 0 {
			cfg.PricingSources = fc.PricingSources
		}
	}

	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid PORT: %q", v)
		}
		cfg.Port = p
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("USAGE_MONITOR_MACHINE_KEY"); v != "" {
		cfg.MachineKeyPath = v
	}
	if v := os.Getenv("ORIGIN"); v != "" {
		cfg.Origin = v
	}
	if v := os.Getenv("ALLOWED_ORIGINS"); v != "" {
		parts := strings.Split(v, ",")
		cfg.AllowedOrigins = nil
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				cfg.AllowedOrigins = append(cfg.AllowedOrigins, t)
			}
		}
	}
	if v := os.Getenv("GATEWAY_KEY"); v != "" {
		cfg.GatewayKey = v
	}
	if v := os.Getenv("INGEST_KEY"); v != "" {
		cfg.IngestKey = v
	}
	if v := os.Getenv("CODEX_ROOT"); v != "" {
		cfg.CodexRoot = v
	}
	if v := os.Getenv("OPENCODE_DB"); v != "" {
		cfg.OpenCodeDB = v
	}
	if v := os.Getenv("CLAUDE_ROOT"); v != "" {
		cfg.ClaudeRoot = v
	}
	if v := os.Getenv("ANTIGRAVITY_ROOT"); v != "" {
		cfg.AntigravityRoot = v
	}
	if v := os.Getenv("GEMINI_ROOT"); v != "" {
		cfg.GeminiRoot = v
	}
	if v := os.Getenv("CURSOR_ROOT"); v != "" {
		cfg.CursorRoot = v
	}
	if v := os.Getenv("COPILOT_ROOT"); v != "" {
		cfg.CopilotRoot = v
	}
	if v := os.Getenv("HERMES_ROOT"); v != "" {
		cfg.HermesRoot = v
	}
	if v := os.Getenv("PRICING_SYNC_ENABLED"); v != "" {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("invalid PRICING_SYNC_ENABLED: %q", v)
		}
		cfg.PricingSyncEnabled = b
	}
	if v := os.Getenv("PRICING_CACHE_PATH"); v != "" {
		cfg.PricingCachePath = v
	}
	if v := os.Getenv("PRICING_SOURCES"); v != "" {
		parts := strings.Split(v, ",")
		cfg.PricingSources = nil
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				cfg.PricingSources = append(cfg.PricingSources, t)
			}
		}
	}
	if v := os.Getenv("RATE_LIMIT_MAX"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid RATE_LIMIT_MAX: %q", v)
		}
		cfg.RateLimitMax = n
	}

	if cfg.PricingCachePath == "" && cfg.DBPath != "" && cfg.DBPath != ":memory:" && !strings.HasPrefix(cfg.DBPath, "file::memory:") {
		cfg.PricingCachePath = filepath.Join(filepath.Dir(cfg.DBPath), "pricing-cache.json")
	}

	if cfg.Origin == "" {
		cfg.Origin = fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	}
	normalizedOrigin, err := ValidateOrigin(cfg.Origin)
	if err != nil {
		return nil, err
	}
	cfg.Origin = normalizedOrigin

	if cfg.GatewayKey != "" && !secretFormat.MatchString(cfg.GatewayKey) {
		return nil, errors.New("invalid gateway credential format")
	}
	if cfg.IngestKey != "" && (!secretFormat.MatchString(cfg.IngestKey) || (cfg.GatewayKey != "" && cfg.IngestKey == cfg.GatewayKey)) {
		return nil, errors.New("invalid independent ingestion credential")
	}
	return cfg, nil
}

// ValidateOrigin ensures the configured primary origin is HTTPS or loopback HTTP.
func ValidateOrigin(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid configured origin")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("invalid configured origin path")
	}
	host := u.Hostname()
	isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback) {
		return "", errors.New("use an HTTPS origin, or HTTP on loopback for development")
	}
	return u.Scheme + "://" + u.Host, nil
}
