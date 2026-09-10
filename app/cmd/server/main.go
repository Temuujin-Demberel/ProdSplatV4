package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/demberel-temuujin/prodsplat-production/app/internal/api"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/config"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/events"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/jobs"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/storage"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/system"
	"github.com/demberel-temuujin/prodsplat-production/app/internal/tasks"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	cfg := config.Load()
	store, err := storage.NewLocal(cfg.Workspace)
	must(err)
	jobRepo, err := jobs.NewFileRepository(store.JobsRoot())
	must(err)
	taskRepo, err := tasks.NewFileRepository(store.TasksRoot())
	must(err)
	broker := events.NewBroker()
	monitor := system.NewWorkerMonitor()
	service := jobs.NewService(jobRepo, taskRepo, store, broker, cfg.TaskLease, cfg.DefaultProfile)
	if err := service.Reconcile(context.Background()); err != nil {
		slog.Error("startup_reconcile_failed", "error", err)
	}
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := service.Reconcile(ctx); err != nil {
				slog.Error("periodic_reconcile_failed", "error", err)
			}
			cancel()
		}
	}()
	h := api.NewHandler(service, store, broker, monitor, cfg)
	mux := http.NewServeMux()
	api.RegisterRoutes(mux, h)
	appFiles := http.StripPrefix("/app/", http.FileServer(http.Dir("/opt/prodsplat/web")))
	mux.Handle("/app/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		appFiles.ServeHTTP(w, r)
	}))
	mux.Handle("/editor/", http.StripPrefix("/editor/", http.FileServer(http.Dir("/opt/prodsplat/editor"))))
	mux.Handle("/licenses/", http.StripPrefix("/licenses/", http.FileServer(http.Dir("/opt/prodsplat/licenses"))))

	server := &http.Server{Addr: ":" + cfg.Port, Handler: api.Middleware(mux), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("server_started", "addr", server.Addr, "workspace", filepath.Clean(cfg.Workspace))
		errCh <- server.ListenAndServe()
	}()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		slog.Info("shutdown_signal", "signal", sig.String())
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server_error", "error", err)
			os.Exit(1)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		slog.Error("shutdown_error", "error", err)
	}
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
