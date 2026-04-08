package controller

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
	"github.com/fardinabir/go-svc-boilerplate/internal/errors"
	"github.com/fardinabir/go-svc-boilerplate/internal/service"
)

// ResolveHandler handles POST /api/resolve and POST /api/resolve/explain.
type ResolveHandler interface {
	Resolve(c echo.Context) error
	Explain(c echo.Context) error
}

type resolveHandler struct {
	Handler
	svc service.ResolveService
}

// NewResolveHandler creates a ResolveHandler.
func NewResolveHandler(svc service.ResolveService) ResolveHandler {
	return &resolveHandler{svc: svc}
}

type resolveRequest struct {
	State    string `json:"state"`
	Client   string `json:"client"`
	Investor string `json:"investor"`
	CaseType string `json:"caseType"`
	AsOfDate string `json:"asOfDate,omitempty"` // "2006-01-02"
}

func (h *resolveHandler) parseContext(c echo.Context) (domain.CaseContext, error) {
	var req resolveRequest
	if err := c.Bind(&req); err != nil {
		return domain.CaseContext{}, err
	}
	ctx := domain.CaseContext{
		State: req.State, Client: req.Client,
		Investor: req.Investor, CaseType: req.CaseType,
	}
	if req.AsOfDate != "" {
		t, err := time.Parse("2006-01-02", req.AsOfDate)
		if err != nil {
			return domain.CaseContext{}, echo.NewHTTPError(http.StatusBadRequest,
				"asOfDate must be YYYY-MM-DD")
		}
		ctx.AsOfDate = t.UTC()
	}
	return ctx, nil
}

// Resolve handles POST /api/resolve.
func (h *resolveHandler) Resolve(c echo.Context) error {
	caseCtx, err := h.parseContext(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: errors.CodeBadRequest, Message: err.Error()}},
		})
	}
	result, err := h.svc.Resolve(c.Request().Context(), caseCtx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ResponseError{
			Errors: []Error{{Code: errors.CodeInternalServerError, Message: "resolution failed"}},
		})
	}
	return c.JSON(http.StatusOK, result)
}

// Explain handles POST /api/resolve/explain.
func (h *resolveHandler) Explain(c echo.Context) error {
	caseCtx, err := h.parseContext(c)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: errors.CodeBadRequest, Message: err.Error()}},
		})
	}
	traces, err := h.svc.Explain(c.Request().Context(), caseCtx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ResponseError{
			Errors: []Error{{Code: errors.CodeInternalServerError, Message: "explain failed"}},
		})
	}
	return c.JSON(http.StatusOK, traces)
}
