package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/hooks"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
)

const maxHookStdinBytes = 256 * 1024

// runHookCommand executes a native, strictly non-interrupting agent lifecycle hook.
// It always exits cleanly with code 0 and prints valid native stdout so it can never break or block an agent.
func runHookCommand(args []string) {
	fs := flag.NewFlagSet("hook", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	agentFlag := fs.String("agent", "auto", "Target agent runtime")
	eventFlag := fs.String("event", "", "Optional hook event name override")
	windowFlag := fs.String("window", "", "Quota window override (15m|1h|24h|7d)")
	quotaUSD := fs.String("quota-usd", "", "Optional USD quota override")
	quotaTokens := fs.String("quota-tokens", "", "Optional token quota override")
	formatFlag := fs.String("format", "native", "Output format (native|json|line|markdown)")
	syncFlag := fs.Bool("sync", false, "Scan local agent logs before querying")
	noGlobal := fs.Bool("no-global", false, "Omit global total when showing agent-specific usage")
	debugFlag := fs.Bool("debug", false, "Write diagnostic messages to stderr")

	if err := fs.Parse(args); err != nil {
		if *debugFlag {
			fmt.Fprintf(os.Stderr, "ai-usage-monitor hook: %v\n", err)
		}
		_, _ = os.Stdout.Write([]byte("{}\n"))
		return
	}

	rawStdin := readStdinNonBlocking()
	in := hooks.ParseInput(rawStdin)

	agent := strings.ToLower(strings.TrimSpace(*agentFlag))
	if agent == "" || agent == "auto" {
		agent = in.DetectedAgent
	}
	evName := strings.TrimSpace(*eventFlag)
	if evName == "" {
		evName = in.EventName
	}

	cfg, err := config.Load()
	if err != nil {
		if *debugFlag {
			fmt.Fprintf(os.Stderr, "ai-usage-monitor hook config warning: %v\n", err)
		}
		cfg = &config.Config{DBPath: "./data/usage.db"}
	}

	var store *db.Store
	if *syncFlag {
		if st, err := db.Open(cfg.DBPath, cfg.MachineKeyPath, nil); err == nil {
			store = st
			defer store.Close()
			resolver := pricing.NewExternalCatalogResolver(cfg.PricingCachePath, cfg.PricingSyncEnabled, cfg.PricingSources)
			store.SetRateResolver(resolver)
			mgr := ingest.NewManager(store, cfg, nil)
			mgr.Scan()
		} else if *debugFlag {
			fmt.Fprintf(os.Stderr, "ai-usage-monitor hook sync warning: %v\n", err)
		}
	} else {
		if st, err := db.OpenReadOnly(cfg.DBPath, nil); err == nil {
			store = st
			defer store.Close()
		} else if *debugFlag {
			fmt.Fprintf(os.Stderr, "ai-usage-monitor hook readonly warning: %v\n", err)
		}
	}

	snap := hooks.QuerySnapshot(store, cfg, hooks.QueryOptions{
		AgentID:         agent,
		SessionID:       in.SessionID,
		ModelHint:       in.Model,
		WindowOverride:  strings.TrimSpace(*windowFlag),
		QuotaUSDOver:    strings.TrimSpace(*quotaUSD),
		QuotaTokensOver: strings.TrimSpace(*quotaTokens),
		ContextUsedPct:  in.ContextUsedPct,
		SessionCostHint: in.SessionCostHint,
	})

	payload, isJSON := hooks.BuildResponse(agent, evName, snap, *formatFlag, !*noGlobal)
	if isJSON {
		_, _ = os.Stdout.Write(append(payload, '\n'))
	} else {
		_, _ = os.Stdout.Write(payload)
	}
}

func readStdinNonBlocking() []byte {
	info, err := os.Stdin.Stat()
	if err != nil {
		return nil
	}
	// If stdin is an interactive TTY with nothing piped, don't hang waiting for EOF.
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(os.Stdin, maxHookStdinBytes))
	if err != nil {
		return nil
	}
	return data
}

func resolveSelfBinary(override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		if abs, err := filepath.Abs(exe); err == nil {
			return abs
		}
		return exe
	}
	return "ai-usage-monitor"
}

func runHooksAdminCommand(args []string) int {
	if len(args) == 0 {
		printHooksUsage()
		return 1
	}

	sub := strings.ToLower(strings.TrimSpace(args[0]))
	rest := args[1:]

	switch sub {
	case "list":
		fs := flag.NewFlagSet("hooks list", flag.ContinueOnError)
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return 1
		}
		specs := hooks.SupportedSpecs()
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(specs)
			return 0
		}
		for _, sp := range specs {
			fmt.Printf("%-18s %-24s eventos: %-28s canal: %s\n", sp.ID, sp.Name, strings.Join(sp.Events, ","), sp.Channel)
			fmt.Printf("  workspace: %s  |  global: %s\n", sp.WorkspaceConfig, sp.GlobalConfig)
		}
		return 0

	case "print":
		fs := flag.NewFlagSet("hooks print", flag.ContinueOnError)
		agent := fs.String("agent", "", "Target agent (antigravity|codex|claude|claude-statusline|gemini|cursor|copilot)")
		binary := fs.String("binary", "", "Path to ai-usage-monitor binary")
		if err := fs.Parse(rest); err != nil {
			return 1
		}
		if strings.TrimSpace(*agent) == "" {
			fmt.Fprintln(os.Stderr, "falta --agent (antigravity|codex|claude|claude-statusline|gemini|cursor|copilot)")
			return 1
		}
		snippet, err := hooks.GenerateSnippet(*agent, resolveSelfBinary(*binary))
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return 1
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snippet)
		return 0

	case "install":
		fs := flag.NewFlagSet("hooks install", flag.ContinueOnError)
		agent := fs.String("agent", "", "Target agent or 'all'")
		scope := fs.String("scope", hooks.ScopeWorkspace, "Config scope: workspace or global")
		binary := fs.String("binary", "", "Path to ai-usage-monitor binary")
		dryRun := fs.Bool("dry-run", false, "Preview changes without writing files")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return 1
		}
		if strings.TrimSpace(*agent) == "" {
			fmt.Fprintln(os.Stderr, "falta --agent (antigravity|codex|claude|claude-statusline|gemini|cursor|copilot|all)")
			return 1
		}

		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		binPath := resolveSelfBinary(*binary)
		targets := resolveInstallTargets(*agent)
		results := make([]hooks.InstallResult, 0, len(targets))
		for _, ag := range targets {
			res, err := hooks.Install(ag, *scope, cwd, home, binPath, *dryRun)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error instalando hook %s: %v\n", ag, err)
				return 1
			}
			results = append(results, res)
		}
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(results)
			return 0
		}
		for _, r := range results {
			status := "sin cambios"
			if r.Changed {
				if r.DryRun {
					status = "vista previa (dry-run)"
				} else {
					status = "instalado"
				}
			}
			fmt.Printf("[%s] %s (%s) -> %s\n", status, r.Agent, r.Scope, r.Path)
			if r.DryRun && r.Preview != "" {
				fmt.Println(r.Preview)
			}
		}
		return 0

	case "uninstall":
		fs := flag.NewFlagSet("hooks uninstall", flag.ContinueOnError)
		agent := fs.String("agent", "", "Target agent or 'all'")
		scope := fs.String("scope", hooks.ScopeWorkspace, "Config scope: workspace or global")
		dryRun := fs.Bool("dry-run", false, "Preview changes without writing files")
		if err := fs.Parse(rest); err != nil {
			return 1
		}
		if strings.TrimSpace(*agent) == "" {
			fmt.Fprintln(os.Stderr, "falta --agent (antigravity|codex|claude|claude-statusline|gemini|cursor|copilot|all)")
			return 1
		}
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		targets := resolveUninstallTargets(*agent)
		for _, ag := range targets {
			res, err := hooks.Uninstall(ag, *scope, cwd, home, *dryRun)
			if err != nil {
				fmt.Fprintf(os.Stderr, "error desinstalando hook %s: %v\n", ag, err)
				return 1
			}
			status := "no presente"
			if res.Changed {
				if res.DryRun {
					status = "se eliminaría (dry-run)"
				} else {
					status = "eliminado"
				}
			}
			fmt.Printf("[%s] %s (%s) -> %s\n", status, res.Agent, res.Scope, res.Path)
		}
		return 0

	case "status":
		fs := flag.NewFlagSet("hooks status", flag.ContinueOnError)
		agent := fs.String("agent", "", "Optional specific agent")
		window := fs.String("window", "", "Optional window override (15m|1h|24h|7d)")
		asJSON := fs.Bool("json", false, "Output JSON")
		if err := fs.Parse(rest); err != nil {
			return 1
		}
		cfg, err := config.Load()
		if err != nil {
			fmt.Fprintf(os.Stderr, "config error: %v\n", err)
			return 1
		}
		store, _ := db.OpenReadOnly(cfg.DBPath, nil)
		if store != nil {
			defer store.Close()
		}
		if strings.TrimSpace(*agent) != "" {
			snap := hooks.QuerySnapshot(store, cfg, hooks.QueryOptions{
				AgentID:        *agent,
				WindowOverride: *window,
			})
			if *asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				_ = enc.Encode(snap)
				return 0
			}
			fmt.Println(hooks.FormatMessage(snap, false, false, true))
			return 0
		}
		cwd, _ := os.Getwd()
		overview := hooks.InspectIntegrations(cfg, store, cwd)
		if *asJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(overview)
			return 0
		}
		fmt.Printf("Ventana de cuota: %s | Gasto global: %s (%s tok) | Estado: %s\n",
			overview.Window,
			hooks.FormatUSD(overview.GlobalQuota.UsedUSD),
			hooks.FormatTokensCompact(overview.GlobalQuota.UsedTokens),
			overview.GlobalQuota.State,
		)
		for _, item := range overview.Integrations {
			inst := "no instalado"
			if item.Installed {
				inst = "instalado (" + item.InstallScope + ")"
			}
			fmt.Printf("  - %-18s %-20s gasto: %-9s cuota: %s\n",
				item.ID, inst, hooks.FormatUSD(item.Quota.UsedUSD), item.Quota.State)
		}
		return 0

	default:
		printHooksUsage()
		return 1
	}
}

func resolveInstallTargets(agent string) []string {
	if strings.ToLower(strings.TrimSpace(agent)) != "all" {
		return []string{strings.ToLower(strings.TrimSpace(agent))}
	}
	cfg, _ := config.Load()
	detected := make(map[string]bool)
	for _, tech := range ingest.DiscoverLocalTechnologies(cfg) {
		if tech.Detected {
			detected[tech.ID] = true
		}
	}
	var out []string
	for _, sp := range hooks.SupportedSpecs() {
		if !sp.SupportsInstall {
			continue
		}
		if detected[sp.AgentID] {
			out = append(out, sp.ID)
		}
	}
	if len(out) == 0 {
		return []string{"antigravity", "codex", "claude"}
	}
	return out
}

func resolveUninstallTargets(agent string) []string {
	if strings.ToLower(strings.TrimSpace(agent)) != "all" {
		return []string{strings.ToLower(strings.TrimSpace(agent))}
	}
	var out []string
	for _, sp := range hooks.SupportedSpecs() {
		if sp.SupportsInstall {
			out = append(out, sp.ID)
		}
	}
	return out
}

func printHooksUsage() {
	fmt.Fprintln(os.Stderr, `Uso:
  ai-usage-monitor hook [--agent auto|antigravity|codex|claude|claude-statusline|gemini|cursor|copilot|generic|statusline] [--window 24h] [--quota-usd 10] [--format native|json|line|markdown] [--sync]
  ai-usage-monitor hooks list [--json]
  ai-usage-monitor hooks print --agent <agente> [--binary /ruta/binario]
  ai-usage-monitor hooks install --agent <agente|all> [--scope workspace|global] [--dry-run]
  ai-usage-monitor hooks uninstall --agent <agente|all> [--scope workspace|global] [--dry-run]
  ai-usage-monitor hooks status [--agent <agente>] [--window 24h] [--json]`)
}
