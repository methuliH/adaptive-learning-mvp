// Command demo serves the adaptive practice API and the one-page demo UI.
package main

import (
	"bytes"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/methuliH/learnsteer-adaptive-mvp/api"
	"github.com/methuliH/learnsteer-adaptive-mvp/data"
	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
	"github.com/methuliH/learnsteer-adaptive-mvp/fit"
	"github.com/methuliH/learnsteer-adaptive-mvp/practice"
	"github.com/methuliH/learnsteer-adaptive-mvp/store"
	"github.com/methuliH/learnsteer-adaptive-mvp/web"
)

func main() {
	addr := flag.String("addr", ":8090", "listen address")
	questions := flag.String("questions", "", "question bank JSON (default: built-in sample)")
	python := flag.String("python", fit.DefaultPython(), "Python interpreter with pyBKT installed")
	script := flag.String("fit-script", filepath.Join("tools", "fit_pybkt.py"), "pyBKT fitter script")
	minAnswers := flag.Int("min-answers", 50, "minimum answers per unit before pyBKT fits it")
	flag.Parse()

	var src io.Reader = bytes.NewReader(data.Questions)
	if *questions != "" {
		f, err := os.Open(*questions)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		src = f
	}
	bank, err := store.LoadBank(src)
	if err != nil {
		log.Fatal(err)
	}

	st := store.NewMemory(bank)
	svc := practice.NewService(st, engine.DefaultThresholds)

	fitter := &fit.Runner{Python: *python, Script: *script, MinAnswers: *minAnswers, Timeout: 10 * time.Minute}
	if err := fitter.Check(); err != nil {
		log.Printf("pyBKT fitting disabled until fixed: %v", err)
	} else {
		log.Printf("pyBKT fitter: %s %s", fitter.Python, fitter.Script)
	}

	r := gin.Default()
	r.Use(cors())
	api.NewHandler(svc, st, fitter).RegisterRoutes(r.Group("/api/v1"))
	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", web.Index)
	})

	log.Printf("Adaptive practice demo on http://localhost%s (%d questions)", *addr, len(bank.Questions))
	log.Fatal(r.Run(*addr))
}

// cors lets a frontend dev server (e.g. Vite on :5173) call the MVP directly.
func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Headers", "Content-Type, X-User-ID, Authorization")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, OPTIONS")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
