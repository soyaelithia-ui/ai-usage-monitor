package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
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
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "hook":
			runHookCommand(os.Args[2:])
			return
		case "hooks":
			os.Exit(runHooksAdminCommand(os.Args[2:]))
		}
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config error: %v", err)
	}

	store, err := db.Open(cfg.DBPath, cfg.MachineKeyPath, nil)
	if err != nil {
		log.Fatalf("database error: %v", err)
	}
	defer store.Close()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "issue":
			tok, err := store.Issue()
			if err != nil {
				log.Fatalf("issue token failed: %v", err)
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			_ = enc.Encode(tok)
			return
		case "revoke":
			if err := store.Revoke(); err != nil {
				log.Fatalf("revoke failed: %v", err)
			}
			fmt.Println("Todos los accesos revocados.")
			return
		default:
			log.Fatalf("unknown command %q (expected issue | revoke | hook | hooks)", os.Args[1])
		}
	}

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

	addr := fmt.Sprintf("%s:%d", cfg.BindAddr, cfg.Port)
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	mgr.Start()
	log.Printf("AI Usage Monitor ready on http://%s (db=%s)", addr, cfg.DBPath)

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
