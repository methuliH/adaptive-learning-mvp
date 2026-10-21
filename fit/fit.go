// Package fit runs the pyBKT fitter (tools/fit_pybkt.py) on the answer log
// and loads the fitted parameters into the practice service.
//
// In LearnSteer this is the offline job from the implementation doc: export
// answers, fit with pyBKT, write skill_params. The live engine never calls
// Python; it only reads the parameters this step stores.
package fit

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/methuliH/adaptive-learning-mvp/practice"
)

var ErrUnavailable = errors.New("pyBKT fitter is not available")

type Runner struct {
	Python     string // interpreter with pyBKT installed
	Script     string // path to fit_pybkt.py
	MinAnswers int    // skills with fewer answers keep their current parameters
	Timeout    time.Duration
}

// DefaultPython prefers the project's .venv, where tools/requirements.txt is installed.
func DefaultPython() string {
	venv := filepath.Join(".venv", "bin", "python")
	if runtime.GOOS == "windows" {
		venv = filepath.Join(".venv", "Scripts", "python.exe")
	}
	if _, err := os.Stat(venv); err == nil {
		return venv
	}
	return "python"
}

type Skipped struct {
	Subject string `json:"subject"`
	Skill   string `json:"skill"`
	Answers int    `json:"answers"`
	Reason  string `json:"reason"`
}

// Output is the fitter's JSON.
type Output struct {
	Version string                  `json:"pybkt"`
	Rows    int                     `json:"rows"`
	Params  []practice.FittedParams `json:"params"`
	Skipped []Skipped               `json:"skipped"`
}

type Result struct {
	Output
	Seconds float64 `json:"seconds"`
}

// Check reports whether the interpreter and script can be found.
func (r Runner) Check() error {
	if _, err := exec.LookPath(r.Python); err != nil {
		return fmt.Errorf("%w: python %q not found", ErrUnavailable, r.Python)
	}
	if _, err := os.Stat(r.Script); err != nil {
		return fmt.Errorf("%w: script %q not found", ErrUnavailable, r.Script)
	}
	return nil
}

// Fit exports the answer log, runs pyBKT and applies valid parameters.
// Fitted values the engine cannot use (e.g. guess + slip >= 1) are reported
// as skipped and the skill keeps its current parameters.
func (r Runner) Fit(ctx context.Context, svc *practice.Service) (Result, error) {
	if err := r.Check(); err != nil {
		return Result{}, err
	}
	start := time.Now()

	f, err := os.CreateTemp("", "bkt_answers_*.csv")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(f.Name())
	if err := svc.ExportCSV(f); err != nil {
		f.Close()
		return Result{}, err
	}
	if err := f.Close(); err != nil {
		return Result{}, err
	}

	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, r.Python, r.Script, f.Name(), "--json",
		"--min-answers", strconv.Itoa(r.MinAnswers))
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("pyBKT fit failed: %v: %s", err, lastLine(stderr.String()))
	}

	out, err := ParseOutput(stdout.Bytes())
	if err != nil {
		return Result{}, err
	}
	var valid []practice.FittedParams
	for _, p := range out.Params {
		if err := practice.ValidateParams(p.Params); err != nil {
			out.Skipped = append(out.Skipped, Skipped{Subject: p.Subject, Skill: p.Skill, Answers: p.Answers, Reason: err.Error()})
			continue
		}
		valid = append(valid, p)
	}
	if err := svc.ApplyParams(valid, "fitted"); err != nil {
		return Result{}, err
	}
	out.Params = valid
	return Result{Output: out, Seconds: time.Since(start).Seconds()}, nil
}

func ParseOutput(b []byte) (Output, error) {
	var out Output
	if err := json.Unmarshal(b, &out); err != nil {
		return out, fmt.Errorf("read pyBKT output: %w", err)
	}
	return out, nil
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
