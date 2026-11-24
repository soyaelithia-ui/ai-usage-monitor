package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

var (
	secretFormat = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	quotaUSDRe   = regexp.MustCompile(`^(?:0|[1-9]\d*)(?:\.\d{1,12})?$`)
	quotaTokRe   = regexp.MustCompile(`^(?:0|[1-9]\d*)$`)
	validWindows = map[string]bool{"": true, "15m": true, "1h": true, "24h": true, "7d": true}
)

// QuotaLimit defines optional exact-decimal spend and/or integer token limits for a time window.
type QuotaLimit struct {
	USD    string `json:"usd,omitempty"`
	Tokens string `json:"tokens,omitempty"`
	Window string `json:"window,omitempty"`
}

// QuotasConfig configures default and per-agent real-time spend/token quotas.
type QuotasConfig struct {
	Window  string                `json:"window,omitempty"`
	Default QuotaLimit            `json:"default,omitempty"`
	Agents  map[string]QuotaLimit `json:"agents,omitempty"`
}

// Config holds runtime configuration for the AI Usage Monitor backend.
type Config struct {
	Port               int
	BindAddr           string
	AuthMode           string
	AllowLAN           bool
	AccessToken        string
	SessionTTL         time.Duration
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
	Quotas             QuotasConfig
	RateLimitMax       int
}

// Overrides carries explicit CLI flag overrides applied after config file and environment variables.
type Overrides struct {
	BindAddr string
	HasBind  bool
	Port     int
	HasPort  bool
	AuthMode string
	AllowLAN bool
}

type fileConfig struct {
	Origin             string        `json:"origin"`
	AllowedOrigins     []string      `json:"allowedOrigins"`
	BackendPort        int           `json:"backendPort"`
	BindAddr           string        `json:"bindAddr"`
	AuthMode           string        `json:"authMode"`
	AllowLAN           *bool         `json:"allowLan"`
	AccessToken        string        `json:"accessToken"`
	SessionTTL         string        `json:"sessionTtl"`
	GatewayPort        int           `json:"gatewayPort"`
	GatewayKey         string        `json:"gatewayKey"`
	IngestKey          string        `json:"ingestKey"`
	DB                 string        `json:"db"`
	CodexRoot          string        `json:"codexRoot"`
	OpenCodeDB         string        `json:"openCodeDb"`
	ClaudeRoot         string        `json:"claudeRoot"`
	AntigravityRoot    string        `json:"antigravityRoot"`
	GeminiRoot         string        `json:"geminiRoot"`
	CursorRoot         string        `json:"cursorRoot"`
	CopilotRoot        string        `json:"copilotRoot"`
	HermesRoot         string        `json:"hermesRoot"`
	PricingSyncEnabled *bool         `json:"pricingSyncEnabled"`
	PricingCachePath   string        `json:"pricingCachePath"`
	PricingSources     []string      `json:"pricingSources"`
	Quotas             *QuotasConfig `json:"quotas"`
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
	return LoadWithOverrides(Overrides{})
}

// LoadWithOverrides reads configuration from backend.json, environment variables, and CLI overrides.
func LoadWithOverrides(ov Overrides) (*Config, error) {
	cfg := &Config{
		Port:           8080,
		BindAddr:       "127.0.0.1",
		AuthMode:       "token",
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
		if strings.TrimSpace(fc.BindAddr) != "" {
			cfg.BindAddr = strings.TrimSpace(fc.BindAddr)
		}
		if strings.TrimSpace(fc.AuthMode) != "" {
			cfg.AuthMode = fc.AuthMode
		}
		if fc.AllowLAN != nil {
			cfg.AllowLAN = *fc.AllowLAN
		}
		if fc.AccessToken != "" {
			cfg.AccessToken = fc.AccessToken
		}
		if strings.TrimSpace(fc.SessionTTL) != "" {
			d, err := ParseDuration(fc.SessionTTL)
			if err != nil {
				return nil, fmt.Errorf("invalid config sessionTtl: %w", err)
			}
			cfg.SessionTTL = d
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
		if fc.Quotas != nil {
			cfg.Quotas = normalizeQuotasConfig(*fc.Quotas)
		}
	}

	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			return nil, fmt.Errorf("invalid PORT: %q", v)
		}
		cfg.Port = p
	}
	if v := strings.TrimSpace(os.Getenv("BIND_ADDR")); v != "" {
		cfg.BindAddr = v
	}
	if v := strings.TrimSpace(os.Getenv("AUTH_MODE")); v != "" {
		cfg.AuthMode = v
	}
	if v := os.Getenv("ALLOW_LAN"); v != "" {
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return nil, fmt.Errorf("invalid ALLOW_LAN: %q", v)
		}
		cfg.AllowLAN = b
	}
	if v := os.Getenv("ACCESS_TOKEN"); v != "" {
		cfg.AccessToken = v
	}
	if v := strings.TrimSpace(os.Getenv("SESSION_TTL")); v != "" {
		d, err := ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid SESSION_TTL: %w", err)
		}
		cfg.SessionTTL = d
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
	if v := strings.TrimSpace(os.Getenv("QUOTAS_JSON")); v != "" {
		var q QuotasConfig
		if err := json.Unmarshal([]byte(v), &q); err != nil {
			return nil, fmt.Errorf("invalid QUOTAS_JSON: %w", err)
		}
		cfg.Quotas = normalizeQuotasConfig(q)
	}
	if v := strings.TrimSpace(os.Getenv("QUOTA_WINDOW")); v != "" {
		cfg.Quotas.Window = v
	}
	if v := strings.TrimSpace(os.Getenv("QUOTA_USD")); v != "" {
		cfg.Quotas.Default.USD = v
	}
	if v := strings.TrimSpace(os.Getenv("QUOTA_TOKENS")); v != "" {
		cfg.Quotas.Default.Tokens = v
	}

	if err := validateQuotasConfig(&cfg.Quotas); err != nil {
		return nil, err
	}

	if ov.HasPort {
		if ov.Port < 1 || ov.Port > 65535 {
			return nil, fmt.Errorf("invalid --port: %d", ov.Port)
		}
		cfg.Port = ov.Port
	}
	if ov.HasBind {
		cfg.BindAddr = strings.TrimSpace(ov.BindAddr)
		if cfg.BindAddr == "" {
			return nil, errors.New("empty --bind address")
		}
	}
	if ov.AuthMode != "" {
		cfg.AuthMode = ov.AuthMode
	}
	if ov.AllowLAN {
		cfg.AllowLAN = true
	}

	normAuthMode, err := NormalizeAuthMode(cfg.AuthMode)
	if err != nil {
		return nil, err
	}
	cfg.AuthMode = normAuthMode

	if cfg.BindAddr == "" {
		cfg.BindAddr = "127.0.0.1"
	}
	bindAddr, err := netip.ParseAddr(cfg.BindAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid bind address %q: %w", cfg.BindAddr, err)
	}
	bindAddr = bindAddr.Unmap()
	cfg.BindAddr = bindAddr.String()

	if cfg.AuthMode == "local" || !bindAddr.IsLoopback() {
		cfg.AllowLAN = true
	}

	if cfg.PricingCachePath == "" && cfg.DBPath != "" && cfg.DBPath != ":memory:" && !strings.HasPrefix(cfg.DBPath, "file::memory:") {
		cfg.PricingCachePath = filepath.Join(filepath.Dir(cfg.DBPath), "pricing-cache.json")
	}

	if cfg.Origin == "" {
		host := "127.0.0.1"
		if !bindAddr.IsLoopback() && !bindAddr.IsUnspecified() {
			host = bindAddr.String()
		}
		cfg.Origin = "http://" + net.JoinHostPort(host, strconv.Itoa(cfg.Port))
	}
	normalizedOrigin, err := ValidateOriginWithOptions(cfg.Origin, cfg.AllowLAN)
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
	if cfg.AccessToken != "" && (!secretFormat.MatchString(cfg.AccessToken) ||
		(cfg.GatewayKey != "" && cfg.AccessToken == cfg.GatewayKey) ||
		(cfg.IngestKey != "" && cfg.AccessToken == cfg.IngestKey)) {
		return nil, errors.New("invalid static access token credential")
	}
	return cfg, nil
}

// NormalizeAuthMode normalizes AUTH_MODE values to "token" or "local".
func NormalizeAuthMode(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "token":
		return "token", nil
	case "local", "lan", "no-token", "local-no-token":
		return "local", nil
	default:
		return "", fmt.Errorf("invalid auth mode %q (expected token or local)", raw)
	}
}

// ParseDuration parses a positive duration string supporting standard Go units
// as well as "d" (24h) and "w" (7*24h) suffixes such as "30d" or "365d".
func ParseDuration(raw string) (time.Duration, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	if s == "" {
		return 0, errors.New("empty duration")
	}
	if !strings.ContainsAny(s, "dw") {
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, err
		}
		if d <= 0 {
			return 0, errors.New("duration must be positive")
		}
		if d < time.Millisecond {
			return 0, errors.New("duration must be at least 1ms")
		}
		return d, nil
	}
	var total time.Duration
	for i := 0; i < len(s); {
		numStart := i
		hasDigit := false
		hasDot := false
		for i < len(s) {
			ch := s[i]
			if ch >= '0' && ch <= '9' {
				hasDigit = true
				i++
			} else if ch == '.' && !hasDot {
				hasDot = true
				i++
			} else {
				break
			}
		}
		if !hasDigit {
			return 0, fmt.Errorf("invalid duration %q", raw)
		}
		numStr := s[numStart:i]
		unitStart := i
		for i < len(s) && (s[i] < '0' || s[i] > '9') && s[i] != '.' {
			i++
		}
		unit := s[unitStart:i]
		if unit == "" {
			return 0, fmt.Errorf("missing unit in duration %q", raw)
		}
		var mult time.Duration
		switch unit {
		case "w":
			mult = 7 * 24 * time.Hour
		case "d":
			mult = 24 * time.Hour
		default:
			part, err := time.ParseDuration(numStr + unit)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", raw, err)
			}
			if part <= 0 {
				return 0, fmt.Errorf("invalid non-positive duration %q", raw)
			}
			if total > math.MaxInt64-part {
				return 0, fmt.Errorf("duration overflow %q", raw)
			}
			total += part
			continue
		}
		var part time.Duration
		if !hasDot {
			n, err := strconv.ParseInt(numStr, 10, 64)
			if err != nil || n <= 0 || n > math.MaxInt64/int64(mult) {
				return 0, fmt.Errorf("invalid or overflowing duration %q", raw)
			}
			part = time.Duration(n) * mult
		} else {
			val, err := strconv.ParseFloat(numStr, 64)
			if err != nil || math.IsNaN(val) || math.IsInf(val, 0) || val <= 0 {
				return 0, fmt.Errorf("invalid duration %q", raw)
			}
			ns := val * float64(mult)
			if ns >= float64(math.MaxInt64) {
				return 0, fmt.Errorf("duration overflow %q", raw)
			}
			part = time.Duration(ns)
			if part <= 0 {
				return 0, fmt.Errorf("duration too small %q", raw)
			}
		}
		if total > math.MaxInt64-part {
			return 0, fmt.Errorf("duration overflow %q", raw)
		}
		total += part
	}
	if total <= 0 {
		return 0, errors.New("duration must be positive")
	}
	if total < time.Millisecond {
		return 0, errors.New("duration must be at least 1ms")
	}
	return total, nil
}

// ValidateOrigin ensures the configured primary origin is HTTPS or loopback HTTP.
func ValidateOrigin(raw string) (string, error) {
	return ValidateOriginWithOptions(raw, false)
}

// ValidateOriginWithOptions ensures the configured primary origin is HTTPS, loopback HTTP,
// or (when allowLAN is true) an HTTP origin on a private/local-network IP or *.local mDNS hostname.
func ValidateOriginWithOptions(raw string, allowLAN bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid configured origin")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("invalid configured origin path")
	}
	host := u.Hostname()
	isLoopback := host == "localhost" || host == "127.0.0.1" || host == "::1"
	isLAN := allowLAN && u.Scheme == "http" && security.IsLocalNetworkHost(host)
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopback) && !isLAN {
		return "", errors.New("use an HTTPS origin, or HTTP on loopback for development")
	}
	return u.Scheme + "://" + u.Host, nil
}

func normalizeQuotasConfig(q QuotasConfig) QuotasConfig {
	out := QuotasConfig{
		Window: strings.TrimSpace(q.Window),
		Default: QuotaLimit{
			USD:    strings.TrimSpace(q.Default.USD),
			Tokens: strings.TrimSpace(q.Default.Tokens),
			Window: strings.TrimSpace(q.Default.Window),
		},
	}
	if len(q.Agents) > 0 {
		out.Agents = make(map[string]QuotaLimit, len(q.Agents))
		for k, v := range q.Agents {
			key := strings.ToLower(strings.TrimSpace(k))
			if key == "" {
				continue
			}
			out.Agents[key] = QuotaLimit{
				USD:    strings.TrimSpace(v.USD),
				Tokens: strings.TrimSpace(v.Tokens),
				Window: strings.TrimSpace(v.Window),
			}
		}
	}
	return out
}

func validateQuotaLimit(label string, lim QuotaLimit) error {
	if !validWindows[lim.Window] {
		return fmt.Errorf("invalid quota window for %s: %q", label, lim.Window)
	}
	if lim.USD != "" && !quotaUSDRe.MatchString(lim.USD) {
		return fmt.Errorf("invalid quota usd for %s: %q", label, lim.USD)
	}
	if lim.Tokens != "" && !quotaTokRe.MatchString(lim.Tokens) {
		return fmt.Errorf("invalid quota tokens for %s: %q", label, lim.Tokens)
	}
	return nil
}

func validateQuotasConfig(q *QuotasConfig) error {
	if q == nil {
		return nil
	}
	q.Window = strings.TrimSpace(q.Window)
	if !validWindows[q.Window] {
		return fmt.Errorf("invalid quotas window: %q", q.Window)
	}
	if err := validateQuotaLimit("default", q.Default); err != nil {
		return err
	}
	for k, v := range q.Agents {
		if err := validateQuotaLimit("agent:"+k, v); err != nil {
			return err
		}
	}
	return nil
}

// ResolveQuota resolves the effective quota limit for a canonical agent ID (or global default when empty).
func (c *Config) ResolveQuota(agentID string) QuotaLimit {
	if c == nil {
		return QuotaLimit{Window: "24h"}
	}
	win := c.Quotas.Window
	if c.Quotas.Default.Window != "" {
		win = c.Quotas.Default.Window
	}
	if win == "" {
		win = "24h"
	}
	lim := QuotaLimit{
		USD:    c.Quotas.Default.USD,
		Tokens: c.Quotas.Default.Tokens,
		Window: win,
	}
	if key := strings.ToLower(strings.TrimSpace(agentID)); key != "" && len(c.Quotas.Agents) > 0 {
		if ag, ok := c.Quotas.Agents[key]; ok {
			if ag.USD != "" {
				lim.USD = ag.USD
			}
			if ag.Tokens != "" {
				lim.Tokens = ag.Tokens
			}
			if ag.Window != "" {
				lim.Window = ag.Window
			}
		}
	}
	return lim
}
