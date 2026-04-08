// Package server provides the API server for the application.
package server

import (
	"fmt"

	"github.com/fardinabir/go-svc-boilerplate/internal/controller"
	"github.com/fardinabir/go-svc-boilerplate/internal/db"
	"github.com/fardinabir/go-svc-boilerplate/internal/model"
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
		AllowMethods: []string{echo.GET, echo.POST, echo.PUT, echo.DELETE},
		AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept, echo.HeaderAuthorization},
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

// setupRoutes registers the routes for the application.
func (s *userAPIServer) setupRoutes(e *echo.Echo) {
	e.Validator = controller.NewCustomValidator()

	api := e.Group("/api/v1")
	api.GET("/health", controller.NewHealth().Health)
}
