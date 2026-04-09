package server

import (
	"context"
	"fmt"

	"github.com/fardinabir/rules-resolution-svc/internal/cache"
	"github.com/fardinabir/rules-resolution-svc/internal/model"
	"github.com/labstack/echo/v4"
	log "github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// apiServer is the API server for User
type apiServer struct {
	engine *echo.Echo
	log    *log.Entry
	db     *gorm.DB
	cache  cache.Cache
	cfg    model.Config
}

func (s *apiServer) Name() string {
	return "apiServer"
}

// Run starts the User API server
func (s *apiServer) Run() error {
	log.Infof("%s serving on port %d", s.Name(), s.cfg.APIServer.Port)
	return s.engine.Start(fmt.Sprintf(":%d", s.cfg.APIServer.Port))
}

// Shutdown stops the User API server
func (s *apiServer) Shutdown(ctx context.Context) error {
	log.Infof("shutting down %s serving on port %d", s.Name(), s.cfg.APIServer.Port)
	return s.engine.Shutdown(ctx)
}
