package server

import (
	"strings"

	"github.com/labstack/echo/v4"
)

// ActorContextKey is the Echo context key for the X-Actor header value.
const ActorContextKey = "actor"

// RequireActor is an Echo middleware that extracts the X-Actor header.
// If the header is absent or empty, defaultActor (from config) is used instead.
// Apply to POST, PUT, PATCH route groups only.
func RequireActor(defaultActor string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			actor := strings.TrimSpace(c.Request().Header.Get("X-Actor"))
			if actor == "" {
				actor = defaultActor
			}
			c.Set(ActorContextKey, actor)
			return next(c)
		}
	}
}
