package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/soyaelithia-ui/ai-usage-monitor/internal/api"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/config"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/db"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/ingest"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/pricing"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/sse"
	"github.com/soyaelithia-ui/ai-usage-monitor/web"
)

func main() {
	var subcmd string
	var subArgs []string
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		subcmd = os.Args[1]
		subArgs = os.Args[2:]
	}

	switch subcmd {
	case "hook":
		runHookCommand(subArgs)
		return
	case "hooks":
		os.Exit(runHooksAdminCommand(subArgs))
	case "issue", "revoke":
		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("config error: %v", err)
		}
		store, err := db.Open(cfg.DBPath, cfg.MachineKeyPath, nil)
		if err != nil {
			log.Fatalf("database error: %v", err)
		}
		defer store.Close()

		if subcmd == "issue" {
			opts, err := parseIssueOptions(subArgs)
			if err != nil {
				log.Fatalf("issue flags error: %v", err)
			}
			tok, err := store.IssueWithOptions(opts)
			if err != nil {
				log.Fatalf("issue token failed: %v", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(tok)
			return
		}
		if err := store.Revoke(); err != nil {
			log.Fatalf("revoke failed: %v", err)
		}
		fmt.Println("Todos los accesos revocados.")
		return
	case "":
		// Server startup mode (optionally with --lan, --local/--no-token, --bind, --port).
	default:
		log.Fatalf("unknown command %q (expected issue | revoke | hook | hooks)", subcmd)
	}

	ov, err := parseServerOverrides(os.Args[1:])
	if err != nil {
		log.Fatalf("flag error: %v", err)
	}
	cfg, err := config.LoadWithOverrides(ov)
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	store, err := db.Open(cfg.DBPath, cfg.MachineKeyPath, nil)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer store.Close()

	if cfg.IngestKey == "" {
		if gen, err := security.GenerateSecret(); err == nil {
			cfg.IngestKey = gen
		}
	}

	resolver := pricing.NewExternalCatalogResolver(
		cfg.PricingCachePath,
		cfg.PricingSyncEnabled,
		cfg.PricingSources,
	)
	store.SetRateResolver(resolver)

	broker := sse.NewBroker()
	mgr := ingest.NewManager(store, cfg, broker.BroadcastChange)
	srv, err := api.NewServer(cfg, store, broker, mgr, web.DistFS())
	if err != nil {
		log.Fatalf("server init error: %v", err)
	}

	addr := net.JoinHostPort(cfg.BindAddr, strconv.Itoa(cfg.Port))
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	mgr.Start()
	log.Printf("AI Usage Monitor ready on http://%s (auth=%s, lan=%v, db=%s)", addr, cfg.AuthMode, cfg.AllowLAN, cfg.DBPath)

	errCh := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("shutting down on signal %s", sig)
	case err := <-errCh:
		if err != nil {
			log.Printf("server error: %v", err)
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpServer.Shutdown(ctx)
	mgr.Stop()
	srv.Close()
}

func parseIssueOptions(args []string) (db.IssueOptions, error) {
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	permanentFlag := fs.Bool("permanent", false, "Issue a permanent reusable token with 365d default session TTL")
	reusableFlag := fs.Bool("reusable", false, "Allow multiple exchanges until token expiry")
	ttlFlag := fs.String("ttl", "", "Access/session duration (e.g. 24h, 30d, 365d)")
	keepFlag := fs.Bool("keep", false, "Keep existing access tokens and active sessions")
	if err := fs.Parse(args); err != nil {
		return db.IssueOptions{}, err
	}
	if fs.NArg() > 0 {
		return db.IssueOptions{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	var ttl time.Duration
	if strings.TrimSpace(*ttlFlag) != "" {
		d, err := config.ParseDuration(*ttlFlag)
		if err != nil {
			return db.IssueOptions{}, fmt.Errorf("invalid --ttl: %w", err)
		}
		ttl = d
	}
	return db.IssueOptions{
		Permanent:    *permanentFlag,
		Reusable:     *reusableFlag,
		TTL:          ttl,
		KeepExisting: *keepFlag,
	}, nil
}

func parseServerOverrides(args []string) (config.Overrides, error) {
	fs := flag.NewFlagSet("ai-usage-monitor", flag.ContinueOnError)
	lanFlag := fs.Bool("lan", false, "Bind to 0.0.0.0 and allow local-network origins")
	localFlag := fs.Bool("local", false, "Enable tokenless mode restricted to local-network clients")
	noTokenFlag := fs.Bool("no-token", false, "Alias for --local")
	bindFlag := fs.String("bind", "", "Bind IP address override")
	portFlag := fs.Int("port", 0, "HTTP port override")
	if err := fs.Parse(args); err != nil {
		return config.Overrides{}, err
	}
	if fs.NArg() > 0 {
		return config.Overrides{}, fmt.Errorf("unexpected argument %q (expected subcommand issue | revoke | hook | hooks)", fs.Arg(0))
	}

	var ov config.Overrides
	visited := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) {
		visited[f.Name] = true
	})

	if *lanFlag {
		ov.AllowLAN = true
		if !visited["bind"] {
			ov.BindAddr = "0.0.0.0"
			ov.HasBind = true
		}
	}
	if *localFlag || *noTokenFlag {
		ov.AuthMode = "local"
		ov.AllowLAN = true
	}
	if visited["bind"] {
		ov.BindAddr = *bindFlag
		ov.HasBind = true
	}
	if visited["port"] {
		ov.Port = *portFlag
		ov.HasPort = true
	}
	return ov, nil
}
