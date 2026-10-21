package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/methuliH/learnsteer-adaptive-mvp/data"
	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
	"github.com/methuliH/learnsteer-adaptive-mvp/practice"
	"github.com/methuliH/learnsteer-adaptive-mvp/store"
)

func router(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	bank, err := store.LoadBank(bytes.NewReader(data.Questions))
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory(bank)
	r := gin.New()
	NewHandler(practice.NewService(st, engine.DefaultThresholds), st, nil).RegisterRoutes(r.Group("/api/v1"))
	return r
}

func TestModelParamsOverHTTP(t *testing.T) {
	r := router(t)
	w := do(r, "PUT", "/api/v1/model/params", "", `{"source":"fitted","params":[{"subject":"chemistry","skill":"Gaseous State","prior":0.4,"learn":0.2,"guess":0.2,"slip":0.05}]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("put: %d %s", w.Code, w.Body)
	}
	w = do(r, "GET", "/api/v1/model/params?subject_id=chemistry", "", "")
	if !strings.Contains(w.Body.String(), `"skill":"Gaseous State","prior":0.4`) || !strings.Contains(w.Body.String(), `"source":"fitted"`) {
		t.Fatalf("get: %s", w.Body)
	}
	w = do(r, "PUT", "/api/v1/model/params", "", `{"params":[{"subject":"chemistry","skill":"Gaseous State","prior":0.4,"learn":0.2,"guess":0.6,"slip":0.5}]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("guess+slip >= 1 should be rejected, got %d", w.Code)
	}
	if w := do(r, "POST", "/api/v1/model/fit", "", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("fit without a fitter: want 503, got %d", w.Code)
	}
}

func TestSimulateOverHTTP(t *testing.T) {
	r := router(t)
	w := do(r, "POST", "/api/v1/model/simulate", "", `{"subject_id":"chemistry","students":5,"steps":4}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"answers":20`) {
		t.Fatalf("simulate: %d %s", w.Code, w.Body)
	}
}

func do(r *gin.Engine, method, path, user, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.Header.Set("X-User-ID", user)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestPracticeFlowOverHTTP(t *testing.T) {
	r := router(t)

	w := do(r, "POST", "/api/v1/practice/next", "u1", `{"subject_id":"chemistry"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("next: %d %s", w.Code, w.Body)
	}
	var next struct {
		Data practice.NextResponse `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &next)
	if next.Data.Question == nil {
		t.Fatalf("no question: %s", w.Body)
	}

	body := `{"question_id":` + jsonNum(next.Data.Question.ID) + `,"selected_option":"a"}`
	w = do(r, "POST", "/api/v1/practice/answer", "u1", body)
	if w.Code != http.StatusOK {
		t.Fatalf("answer: %d %s", w.Code, w.Body)
	}
	w = do(r, "POST", "/api/v1/practice/answer", "u1", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("repeat answer: want 409, got %d", w.Code)
	}

	w = do(r, "GET", "/api/v1/practice/mastery?subject_id=chemistry", "u1", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"attempts":1`) {
		t.Fatalf("mastery: %d %s", w.Code, w.Body)
	}
}

func TestRequiresUserHeader(t *testing.T) {
	w := do(router(t), "POST", "/api/v1/practice/next", "", `{"subject_id":"chemistry"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestUnknownSubjectIs404(t *testing.T) {
	w := do(router(t), "POST", "/api/v1/practice/next", "u1", `{"subject_id":"physics"}`)
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", w.Code)
	}
}

func jsonNum(n uint) string {
	b, _ := json.Marshal(n)
	return string(b)
}
