package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/abir/rules-resolution-service/internal/api/httputil"
	"github.com/abir/rules-resolution-service/internal/domain"
	"github.com/abir/rules-resolution-service/internal/service"
)

type ResolveHandler struct {
	svc *service.ResolveService
}

func NewResolveHandler(svc *service.ResolveService) *ResolveHandler {
	return &ResolveHandler{svc: svc}
}

// resolveRequest extends CaseContext with an optional string asOfDate.
type resolveRequest struct {
	State    string `json:"state"`
	Client   string `json:"client"`
	Investor string `json:"investor"`
	CaseType string `json:"caseType"`
	AsOfDate string `json:"asOfDate,omitempty"` // "2025-06-15" — optional
}

func (h *ResolveHandler) parseContext(r *http.Request) (domain.CaseContext, error) {
	var req resolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return domain.CaseContext{}, err
	}
	ctx := domain.CaseContext{
		State:    req.State,
		Client:   req.Client,
		Investor: req.Investor,
		CaseType: req.CaseType,
	}
	if req.AsOfDate != "" {
		t, err := time.Parse("2006-01-02", req.AsOfDate)
		if err != nil {
			return domain.CaseContext{}, err
		}
		ctx.AsOfDate = t
	}
	return ctx, nil
}

func (h *ResolveHandler) Resolve(w http.ResponseWriter, r *http.Request) {
	ctx, err := h.parseContext(r)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	result, err := h.svc.Resolve(r.Context(), ctx)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, result)
}

func (h *ResolveHandler) Explain(w http.ResponseWriter, r *http.Request) {
	ctx, err := h.parseContext(r)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
		return
	}
	result, err := h.svc.Explain(r.Context(), ctx)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httputil.JSON(w, http.StatusOK, result)
}
