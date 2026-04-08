// Package server provides the API server for the application.
package server

import (
	"fmt"
	"time"

	"github.com/fardinabir/go-svc-boilerplate/internal/cache"
	"github.com/fardinabir/go-svc-boilerplate/internal/controller"
	"github.com/fardinabir/go-svc-boilerplate/internal/db"
	"github.com/fardinabir/go-svc-boilerplate/internal/model"
	"github.com/fardinabir/go-svc-boilerplate/internal/repository"
	"github.com/fardinabir/go-svc-boilerplate/internal/service"
	"github.com/fardinabir/go-svc-boilerplate/internal/utils"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	log "github.com/sirupsen/logrus"
)

// APIServerOpts is the options for the API server.
type APIServerOpts struct {
	ListenPort int
	Config     model.Config
}

// NewAPI returns a new instance of the API server.
func NewAPI(opts APIServerOpts) (Server, error) {
	logger := log.NewEntry(log.StandardLogger())
	log.SetFormatter(&log.JSONFormatter{})
	utils.InitLogger(logger)

	dbInstance, err := db.New(opts.Config.PostgreSQL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %v", err)
	}

	engine := echo.New()
	engine.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{echo.GET, echo.POST, echo.PUT, echo.DELETE, echo.PATCH},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization, "X-Actor"},
	}))

	s := &userAPIServer{
		engine: engine,
		log:    logger,
		db:     dbInstance,
		cache:  initCache(opts.Config.Redis),
		cfg:    opts.Config,
	}

	s.setupRoutes(engine)
	engine.Use(requestLogger())

	return s, nil
}

// initCache connects to Redis and returns a Cache. Falls back to NoopCache if Redis
// is not configured or unavailable so the service starts without caching.
func initCache(cfg model.Redis) cache.Cache {
	if cfg.Host == "" {
		log.Info("Redis not configured — running without cache")
		return cache.NoopCache{}
	}

	rc, err := cache.NewRedisCache(cache.RedisConfig{
		Host:     cfg.Host,
		Port:     cfg.Port,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	if err != nil {
		log.WithError(err).Warn("Redis unavailable — running without cache")
		return cache.NoopCache{}
	}

	log.Info("Redis cache connected")
	return rc
}

// ttlOr returns d if non-zero, otherwise fallback.
func ttlOr(d, fallback time.Duration) time.Duration {
	if d == 0 {
		return fallback
	}
	return d
}

// initResolveController wires OverrideRepository + DefaultRepository → ResolveService → ResolveHandler.
func (s *userAPIServer) initResolveController() controller.ResolveHandler {
	overrideRepo := repository.NewCachedOverrideRepository(
		repository.NewOverrideRepository(s.db), s.cache,
		ttlOr(s.cfg.Redis.OverrideTTL, 5*time.Minute))
	defaultRepo := repository.NewCachedDefaultRepository(
		repository.NewDefaultRepository(s.db), s.cache,
		ttlOr(s.cfg.Redis.DefaultTTL, time.Hour))
	return controller.NewResolveHandler(service.NewResolveService(overrideRepo, defaultRepo))
}

// initOverrideController wires OverrideRepository → OverrideService → OverrideHandler.
func (s *userAPIServer) initOverrideController() controller.OverrideHandler {
	overrideRepo := repository.NewCachedOverrideRepository(
		repository.NewOverrideRepository(s.db), s.cache,
		ttlOr(s.cfg.Redis.OverrideTTL, 5*time.Minute))
	return controller.NewOverrideHandler(service.NewOverrideService(overrideRepo))
}

// setupRoutes registers all routes for the application.
func (s *userAPIServer) setupRoutes(e *echo.Echo) {
	e.Validator = controller.NewCustomValidator()

	api := e.Group("/api")
	api.GET("/health", controller.NewHealth().Health)

	controller.InitResolveRoutes(api, s.initResolveController())
	controller.InitOverrideRoutes(api, s.initOverrideController(), RequireActor(s.cfg.DefaultActor))
}
