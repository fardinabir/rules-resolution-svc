package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/abir/rules-resolution-service/internal/api/httputil"
	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/abir/rules-resolution-service/internal/repository"
	"github.com/abir/rules-resolution-service/internal/service"
)

type OverrideHandler struct {
	svc *service.OverrideService
}

func NewOverrideHandler(svc *service.OverrideService) *OverrideHandler {
	return &OverrideHandler{svc: svc}
}

func (h *OverrideHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := repository.OverrideFilter{}
	if v := q.Get("stepKey"); v != "" {
		f.StepKey = &v
	}
	if v := q.Get("traitKey"); v != "" {
		f.TraitKey = &v
	}
	if v := q.Get("state"); v != "" {
		f.State = &v
	}
	if v := q.Get("client"); v != "" {
		f.Client = &v
	}
	if v := q.Get("investor"); v != "" {
		f.Investor = &v
	}
	if v := q.Get("caseType"); v != "" {
		f.CaseType = &v
	}
	if v := q.Get("status"); v != "" {
		f.Status = &v
	}

	overrides, err := h.svc.List(r.Context(), f)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"overrides": overrides})
}

func (h *OverrideHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	o, err := h.svc.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			httputil.Error(w, http.StatusNotFound, "NOT_FOUND", "override not found")
			return
		}
		httputil.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, o)
}

func (h *OverrideHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req service.CreateOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	o, err := h.svc.Create(r.Context(), req)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httputil.JSON(w, http.StatusCreated, o)
}

func (h *OverrideHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req service.UpdateOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	o, err := h.svc.Update(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			httputil.Error(w, http.StatusNotFound, "NOT_FOUND", "override not found")
			return
		}
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, o)
}

func (h *OverrideHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req service.UpdateStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	if err := h.svc.UpdateStatus(r.Context(), id, req); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			httputil.Error(w, http.StatusNotFound, "NOT_FOUND", "override not found")
			return
		}
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *OverrideHandler) GetHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	entries, err := h.svc.GetHistory(r.Context(), id)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"history": entries})
}

func (h *OverrideHandler) GetConflicts(w http.ResponseWriter, r *http.Request) {
	conflicts, err := h.svc.GetConflicts(r.Context())
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if conflicts == nil {
		conflicts = []domain.ConflictPair{}
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"conflicts": conflicts})
}
