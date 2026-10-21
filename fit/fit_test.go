package fit

import (
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/methuliH/learnsteer-adaptive-mvp/data"
	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
	"github.com/methuliH/learnsteer-adaptive-mvp/practice"
	"github.com/methuliH/learnsteer-adaptive-mvp/sim"
	"github.com/methuliH/learnsteer-adaptive-mvp/store"
)

func TestParseOutput(t *testing.T) {
	out, err := ParseOutput([]byte(`{"pybkt":"1.4.3","rows":900,
		"params":[{"subject":"chemistry","skill":"Gaseous State","prior":0.41,"learn":0.19,"guess":0.21,"slip":0.06,"answers":300,"students":150}],
		"skipped":[{"subject":"chemistry","skill":"Atomic Structure","answers":12,"reason":"fewer than 50 answers"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	p := out.Params[0]
	if p.Skill != "Gaseous State" || p.Prior != 0.41 || p.Slip != 0.06 || p.Answers != 300 {
		t.Fatalf("bad parse: %+v", p)
	}
	if len(out.Skipped) != 1 || out.Rows != 900 {
		t.Fatalf("bad parse: %+v", out)
	}
}

func TestCheckReportsMissingPython(t *testing.T) {
	r := Runner{Python: "no-such-python-xyz", Script: "fit_pybkt.py"}
	if err := r.Check(); err == nil {
		t.Fatal("want an error for a missing interpreter")
	}
}

// TestFitRecoversSimulatedParams runs the real pyBKT fitter end to end. It is
// skipped unless the project's .venv has tools/requirements.txt installed.
func TestFitRecoversSimulatedParams(t *testing.T) {
	root, _ := filepath.Abs("..")
	r := Runner{Script: filepath.Join(root, "tools", "fit_pybkt.py"), MinAnswers: 50, Timeout: 5 * time.Minute}
	for _, p := range []string{filepath.Join(root, ".venv", "Scripts", "python.exe"), filepath.Join(root, ".venv", "bin", "python")} {
		if _, err := os.Stat(p); err == nil {
			r.Python = p
		}
	}
	if r.Python == "" || testing.Short() {
		t.Skip("no .venv with pyBKT; see README")
	}

	bank, err := store.LoadBank(bytes.NewReader(data.Questions))
	if err != nil {
		t.Fatal(err)
	}
	st := store.NewMemory(bank)
	svc := practice.NewService(st, engine.DefaultThresholds)
	pop, err := sim.Populate(svc, st, "chemistry", 300, 20, 7)
	if err != nil {
		t.Fatal(err)
	}

	res, err := r.Fit(context.Background(), svc)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Params) == 0 {
		t.Fatalf("no skills fitted: %+v", res.Skipped)
	}
	params, _ := svc.ModelParams("chemistry")
	for _, p := range params {
		truth := pop.Truth[p.Skill]
		t.Logf("%-16s source=%s prior %.2f (true %.2f)  learn %.2f (%.2f)  guess %.2f (%.2f)  slip %.2f (%.2f)",
			p.Skill, p.Source, p.Prior, truth.Prior, p.Learn, truth.Learn, p.Guess, truth.Guess, p.Slip, truth.Slip)
		if p.Source != "fitted" {
			t.Errorf("%s: not fitted", p.Skill)
		}
		// Loose bounds: EM on a few thousand simulated answers lands near, not on, the truth
		if math.Abs(p.Guess-truth.Guess) > 0.15 || math.Abs(p.Slip-truth.Slip) > 0.15 {
			t.Errorf("%s: guess/slip too far from truth", p.Skill)
		}
	}
}
