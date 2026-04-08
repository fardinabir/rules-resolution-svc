// Package server provides the API server for the application.
package server

import (
	"fmt"

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
		port:   opts.ListenPort,
		engine: engine,
		log:    logger,
		db:     dbInstance,
	}

	s.setupRoutes(engine)
	engine.Use(requestLogger())

	return s, nil
}

// initResolveController wires OverrideRepository + DefaultRepository → ResolveService → ResolveHandler.
func (s *userAPIServer) initResolveController() controller.ResolveHandler {
	overrideRepo := repository.NewOverrideRepository(s.db)
	defaultRepo := repository.NewDefaultRepository(s.db)
	return controller.NewResolveHandler(service.NewResolveService(overrideRepo, defaultRepo))
}

// initOverrideController wires OverrideRepository → OverrideService → OverrideHandler.
func (s *userAPIServer) initOverrideController() controller.OverrideHandler {
	overrideRepo := repository.NewOverrideRepository(s.db)
	return controller.NewOverrideHandler(service.NewOverrideService(overrideRepo))
}

// setupRoutes registers all routes for the application.
func (s *userAPIServer) setupRoutes(e *echo.Echo) {
	e.Validator = controller.NewCustomValidator()

	api := e.Group("/api")
	api.GET("/health", controller.NewHealth().Health)

	controller.InitResolveRoutes(api, s.initResolveController())
	controller.InitOverrideRoutes(api, s.initOverrideController(), RequireActor(""))
}
