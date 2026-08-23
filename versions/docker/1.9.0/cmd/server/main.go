package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"searchterm/internal/config"
	"searchterm/internal/magnetlog"
	"searchterm/internal/search"
	"searchterm/internal/server"
	"searchterm/internal/store"
	"searchterm/internal/tgbot"
)

func main() {
	configPath := flag.String("config", "config.json", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		log.Fatalf("create data dir: %v", err)
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "searchterm.db"), cfg.SecretKey)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()

	svc := search.New(cfg)
	tg := tgbot.New(cfg, st, svc)
	magnetLogger, err := magnetlog.New(cfg.MagnetLogPath(), cfg.MagnetLogMaxBytes(), cfg.MagnetLogMaxFiles)
	if err != nil {
		log.Fatalf("open magnet log: %v", err)
	}
	defer magnetLogger.Close()
	srv := server.New(cfg, st, svc, tg, magnetLogger)

	sites, err := st.ListSites()
	if err != nil {
		log.Fatalf("load sites: %v", err)
	}
	for _, site := range sites {
		srv.RegisterSite(site)
	}
	if err := srv.EnsureDefaultSites(); err != nil {
		log.Fatalf("ensure default sites: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tg.Start(ctx)

	httpServer := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("SearchTerm listening on %s", cfg.Listen)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
