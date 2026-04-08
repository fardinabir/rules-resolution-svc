package controller

import "github.com/labstack/echo/v4"

// InitResolveRoutes registers POST /resolve and POST /resolve/explain.
func InitResolveRoutes(api *echo.Group, h ResolveHandler) {
	api.POST("/resolve", h.Resolve)
	api.POST("/resolve/explain", h.Explain)
}

// InitOverrideRoutes registers all /overrides endpoints.
// actorMiddleware is applied only to mutation routes (POST, PUT, PATCH).
// Note: /overrides/conflicts is registered BEFORE /overrides/:id to avoid route shadowing.
func InitOverrideRoutes(api *echo.Group, h OverrideHandler, actorMiddleware echo.MiddlewareFunc) {
	api.GET("/overrides", h.List)
	api.GET("/overrides/conflicts", h.GetConflicts)
	api.GET("/overrides/:id", h.GetByID)
	api.GET("/overrides/:id/history", h.GetHistory)

	mutate := api.Group("", actorMiddleware)
	mutate.POST("/overrides", h.Create)
	mutate.PUT("/overrides/:id", h.Update)
	mutate.PATCH("/overrides/:id/status", h.UpdateStatus)
}
