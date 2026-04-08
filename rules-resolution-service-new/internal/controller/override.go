package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/fardinabir/go-svc-boilerplate/internal/domain"
	apierrors "github.com/fardinabir/go-svc-boilerplate/internal/errors"
	"github.com/fardinabir/go-svc-boilerplate/internal/service"
)

// OverrideHandler handles all /api/overrides endpoints.
type OverrideHandler interface {
	List(c echo.Context) error
	GetByID(c echo.Context) error
	Create(c echo.Context) error
	Update(c echo.Context) error
	UpdateStatus(c echo.Context) error
	GetHistory(c echo.Context) error
	GetConflicts(c echo.Context) error
}

type overrideHandler struct {
	Handler
	svc service.OverrideService
}

// NewOverrideHandler creates an OverrideHandler.
func NewOverrideHandler(svc service.OverrideService) OverrideHandler {
	return &overrideHandler{svc: svc}
}

func (h *overrideHandler) List(c echo.Context) error {
	f := service.OverrideFilter{Page: 1, PageSize: 50}
	if v := c.QueryParam("stepKey"); v != "" {
		f.StepKey = &v
	}
	if v := c.QueryParam("traitKey"); v != "" {
		f.TraitKey = &v
	}
	if v := c.QueryParam("state"); v != "" {
		f.State = &v
	}
	if v := c.QueryParam("client"); v != "" {
		f.Client = &v
	}
	if v := c.QueryParam("investor"); v != "" {
		f.Investor = &v
	}
	if v := c.QueryParam("caseType"); v != "" {
		f.CaseType = &v
	}
	if v := c.QueryParam("status"); v != "" {
		f.Status = &v
	}
	// Parse pagination params
	if v := c.QueryParam("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Page = n
		}
	}
	if v := c.QueryParam("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.PageSize = n
		}
	}
	overrides, total, err := h.svc.List(c.Request().Context(), f)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ResponseError{
			Errors: []Error{{Code: apierrors.CodeInternalServerError, Message: err.Error()}},
		})
	}
	return c.JSON(http.StatusOK, map[string]any{
		"data": overrides, "total": total,
		"page": f.Page, "pageSize": f.PageSize,
	})
}

func (h *overrideHandler) GetByID(c echo.Context) error {
	id := c.Param("id")
	o, err := h.svc.GetByID(c.Request().Context(), id)
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ResponseError{
			Errors: []Error{{Code: apierrors.CodeNotFound, Message: "override not found"}},
		})
	}
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ResponseError{
			Errors: []Error{{Code: apierrors.CodeInternalServerError, Message: err.Error()}},
		})
	}
	return c.JSON(http.StatusOK, o)
}

func (h *overrideHandler) Create(c echo.Context) error {
	actor, _ := c.Get("actor").(string)
	var req service.CreateOverrideRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: err.Error()}},
		})
	}
	o, err := h.svc.Create(c.Request().Context(), req, actor)
	if err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: err.Error()}},
		})
	}
	return c.JSON(http.StatusCreated, o)
}

func (h *overrideHandler) Update(c echo.Context) error {
	actor, _ := c.Get("actor").(string)
	id := c.Param("id")
	var req service.UpdateOverrideRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: err.Error()}},
		})
	}
	o, err := h.svc.Update(c.Request().Context(), id, req, actor)
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ResponseError{
			Errors: []Error{{Code: apierrors.CodeNotFound, Message: "override not found"}},
		})
	}
	if err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: err.Error()}},
		})
	}
	return c.JSON(http.StatusOK, o)
}

type updateStatusRequest struct {
	Status string `json:"status"`
}

func (h *overrideHandler) UpdateStatus(c echo.Context) error {
	actor, _ := c.Get("actor").(string)
	id := c.Param("id")
	var req updateStatusRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: err.Error()}},
		})
	}
	if req.Status == "" {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: "status is required"}},
		})
	}
	err := h.svc.UpdateStatus(c.Request().Context(), id, req.Status, actor)
	if errors.Is(err, domain.ErrNotFound) {
		return c.JSON(http.StatusNotFound, ResponseError{
			Errors: []Error{{Code: apierrors.CodeNotFound, Message: "override not found"}},
		})
	}
	if err != nil {
		return c.JSON(http.StatusBadRequest, ResponseError{
			Errors: []Error{{Code: apierrors.CodeBadRequest, Message: err.Error()}},
		})
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *overrideHandler) GetHistory(c echo.Context) error {
	id := c.Param("id")
	history, err := h.svc.GetHistory(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ResponseError{
			Errors: []Error{{Code: apierrors.CodeInternalServerError, Message: err.Error()}},
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"history": history})
}

func (h *overrideHandler) GetConflicts(c echo.Context) error {
	conflicts, err := h.svc.GetConflicts(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, ResponseError{
			Errors: []Error{{Code: apierrors.CodeInternalServerError, Message: err.Error()}},
		})
	}
	return c.JSON(http.StatusOK, map[string]any{"conflicts": conflicts})
}
