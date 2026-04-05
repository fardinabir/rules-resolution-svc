package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/abir/rules-resolution-service/internal/api"
	"github.com/abir/rules-resolution-service/internal/api/handler"
	"github.com/abir/rules-resolution-service/internal/config"
	"github.com/abir/rules-resolution-service/internal/repository"
	"github.com/abir/rules-resolution-service/internal/server"
	"github.com/abir/rules-resolution-service/internal/service"
)

func main() {
	cfg := config.Load()

	// Structured logger
	level := slog.LevelInfo
	if cfg.LogLevel == "debug" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	// Run migrations
	migrationsPath := "file://migrations"
	if mp := os.Getenv("MIGRATIONS_PATH"); mp != "" {
		migrationsPath = mp
	}
	m, err := migrate.New(migrationsPath, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("migrate init: %v", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrate up: %v", err)
	}
	slog.Info("migrations applied")

	// Connect DB pool
	ctx := context.Background()
	pool, err := repository.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect: %v", err)
	}
	defer pool.Close()

	// Wire repositories
	overrideRepo := repository.NewOverrideRepository(pool)
	defaultRepo := repository.NewDefaultRepository(pool)

	// Wire services
	resolveSvc := service.NewResolveService(overrideRepo, defaultRepo)
	overrideSvc := service.NewOverrideService(overrideRepo)

	// Wire handlers + router
	resolveH := handler.NewResolveHandler(resolveSvc)
	overrideH := handler.NewOverrideHandler(overrideSvc)
	apiRouter := api.NewRouter(resolveH, overrideH)

	apiServer := &http.Server{
		Addr:         ":" + cfg.APIServer.Port,
		Handler:      apiRouter,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Start API server in goroutine — mirrors boilerplate's runServe pattern
	go func() {
		slog.Info("API server starting", "addr", apiServer.Addr)
		if err := apiServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("API server: %v", err)
		}
	}()

	// Start Swagger UI server in separate goroutine on separate port
	var swaggerServer *http.Server
	if cfg.SwaggerServer.Enable {
		swaggerServer = &http.Server{
			Addr:         ":" + cfg.SwaggerServer.Port,
			Handler:      server.NewSwaggerHandler(),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
		}
		go func() {
			slog.Info("Swagger UI starting",
				"addr", swaggerServer.Addr,
				"ui", "http://localhost:"+cfg.SwaggerServer.Port+"/swagger/index.html",
				"spec", "http://localhost:"+cfg.SwaggerServer.Port+"/openapi.yaml",
			)
			if err := swaggerServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("Swagger server: %v", err)
			}
		}()
	}

	// Graceful shutdown on SIGINT / SIGTERM
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down servers...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := apiServer.Shutdown(shutdownCtx); err != nil {
		slog.Error("API server shutdown error", "err", err)
	}
	if swaggerServer != nil {
		if err := swaggerServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("Swagger server shutdown error", "err", err)
		}
	}
	slog.Info("servers stopped")
}
