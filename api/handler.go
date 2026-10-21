// Package api exposes the practice service over HTTP with the same routes and
// JSON shapes planned for the LearnSteer backend, so a frontend can switch
// between this MVP and the real backend by changing its base URL.
package api

import (
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"

	"github.com/methuliH/adaptive-learning-mvp/fit"
	"github.com/methuliH/adaptive-learning-mvp/practice"
	"github.com/methuliH/adaptive-learning-mvp/store"
)

type Handler struct {
	svc     *practice.Service
	store   store.Store
	fitter  *fit.Runner // nil: fitting disabled
	fitting sync.Mutex  // one fit at a time
}

func NewHandler(svc *practice.Service, st store.Store, fitter *fit.Runner) *Handler {
	return &Handler{svc: svc, store: st, fitter: fitter}
}

// RegisterRoutes mounts the endpoints, e.g. on /api/v1.
func (h *Handler) RegisterRoutes(r *gin.RouterGroup) {
	r.GET("/subjects", h.ListSubjects)

	p := r.Group("/practice")
	p.Use(RequireUser())
	{
		p.POST("/next", h.Next)
		p.POST("/answer", h.Answer)
		p.GET("/mastery", h.Mastery)
		p.POST("/reset", h.Reset) // MVP demo only
	}
	r.GET("/practice/export.csv", h.ExportCSV)

	// Model admin. In LearnSteer these sit behind an admin role check.
	m := r.Group("/model")
	{
		m.GET("/params", h.GetParams)
		m.PUT("/params", h.PutParams)
		m.POST("/fit", h.Fit)
		m.POST("/simulate", h.Simulate) // MVP demo only
	}
}

const userKey = "userID"

// RequireUser reads the student from the X-User-ID header.
// In the LearnSteer backend this is replaced by middleware.RequireAuth().
func RequireUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := strings.TrimSpace(c.GetHeader("X-User-ID"))
		if user == "" {
			fail(c, http.StatusUnauthorized, "X-User-ID header is required")
			c.Abort()
			return
		}
		c.Set(userKey, user)
		c.Next()
	}
}

type nextRequest struct {
	SubjectID string `json:"subject_id" binding:"required"`
	Skill     string `json:"skill"`
}

type answerRequest struct {
	QuestionID     uint   `json:"question_id" binding:"required"`
	SelectedOption string `json:"selected_option" binding:"required"`
}

func (h *Handler) ListSubjects(c *gin.Context) {
	ok(c, h.svc.Subjects())
}

func (h *Handler) Next(c *gin.Context) {
	var req nextRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.svc.Next(c.GetString(userKey), req.SubjectID, req.Skill)
	if err != nil {
		failErr(c, err)
		return
	}
	ok(c, resp)
}

func (h *Handler) Answer(c *gin.Context) {
	var req answerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := h.svc.Answer(c.GetString(userKey), req.QuestionID, req.SelectedOption)
	if err != nil {
		failErr(c, err)
		return
	}
	ok(c, resp)
}

func (h *Handler) Mastery(c *gin.Context) {
	resp, err := h.svc.Mastery(c.GetString(userKey), c.Query("subject_id"))
	if err != nil {
		failErr(c, err)
		return
	}
	ok(c, resp)
}

func (h *Handler) Reset(c *gin.Context) {
	h.svc.Reset(c.GetString(userKey))
	ok(c, gin.H{"reset": true})
}

func (h *Handler) ExportCSV(c *gin.Context) {
	c.Header("Content-Type", "text/csv")
	c.Header("Content-Disposition", `attachment; filename="bkt_answers.csv"`)
	if err := h.svc.ExportCSV(c.Writer); err != nil {
		_ = c.Error(err)
	}
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"success": false, "error": msg})
}

func failErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, practice.ErrUnknownSubject),
		errors.Is(err, practice.ErrUnknownSkill),
		errors.Is(err, practice.ErrUnknownQuestion):
		fail(c, http.StatusNotFound, err.Error())
	case errors.Is(err, practice.ErrInvalidOption),
		errors.Is(err, practice.ErrInvalidParams):
		fail(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, practice.ErrAlreadyAnswered):
		fail(c, http.StatusConflict, err.Error())
	default:
		fail(c, http.StatusInternalServerError, "internal error")
	}
}
