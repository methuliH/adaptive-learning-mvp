package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/methuliH/learnsteer-adaptive-mvp/fit"
	"github.com/methuliH/learnsteer-adaptive-mvp/practice"
	"github.com/methuliH/learnsteer-adaptive-mvp/sim"
)

func (h *Handler) GetParams(c *gin.Context) {
	params, err := h.svc.ModelParams(c.Query("subject_id"))
	if err != nil {
		failErr(c, err)
		return
	}
	ok(c, params)
}

type putParamsRequest struct {
	Source string                  `json:"source"` // "fitted" when uploading an offline pyBKT fit; default "manual"
	Params []practice.FittedParams `json:"params" binding:"required,min=1"`
}

// PutParams stores parameters fitted elsewhere (e.g. tools/fit_pybkt.py run by hand).
func (h *Handler) PutParams(c *gin.Context) {
	var req putParamsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Source != "fitted" {
		req.Source = "manual"
	}
	if err := h.svc.ApplyParams(req.Params, req.Source); err != nil {
		failErr(c, err)
		return
	}
	ok(c, gin.H{"applied": len(req.Params)})
}

// Fit runs pyBKT on every answer so far and loads the fitted parameters.
func (h *Handler) Fit(c *gin.Context) {
	if h.fitter == nil {
		fail(c, http.StatusServiceUnavailable, "pyBKT fitting is not configured")
		return
	}
	if !h.fitting.TryLock() {
		fail(c, http.StatusConflict, "a fit is already running")
		return
	}
	defer h.fitting.Unlock()

	res, err := h.fitter.Fit(c.Request.Context(), h.svc)
	if errors.Is(err, fit.ErrUnavailable) {
		fail(c, http.StatusServiceUnavailable, err.Error()+" (see README: install tools/requirements.txt)")
		return
	}
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}

type simulateRequest struct {
	SubjectID string `json:"subject_id" binding:"required"`
	Students  int    `json:"students"`
	Steps     int    `json:"steps"`
}

// Simulate adds simulated students' answers so there is history to fit.
func (h *Handler) Simulate(c *gin.Context) {
	var req simulateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	req.Students = clampInt(req.Students, 150, 1, 2000)
	req.Steps = clampInt(req.Steps, 20, 1, 100)

	res, err := sim.Populate(h.svc, h.store, req.SubjectID, req.Students, req.Steps, time.Now().UnixNano())
	if err != nil {
		failErr(c, err)
		return
	}
	ok(c, res)
}

func clampInt(v, def, lo, hi int) int {
	if v == 0 {
		return def
	}
	return max(lo, min(v, hi))
}
