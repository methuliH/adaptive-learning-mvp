package engine

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// Matches the worked example in the implementation doc.
func TestUpdateMasteryWorkedExample(t *testing.T) {
	p := DefaultParams.Prior
	steps := []struct {
		correct bool
		want    float64
		diff    Difficulty
	}{
		{true, 0.69, Medium},
		{true, 0.92, Hard},
		{false, 0.63, Medium},
	}
	for i, s := range steps {
		p = UpdateMastery(p, s.correct, DefaultParams)
		if !near(p, s.want) {
			t.Fatalf("step %d: mastery = %.3f, want %.2f", i+1, p, s.want)
		}
		if got := DefaultThresholds.Target(p); got != s.diff {
			t.Fatalf("step %d: target = %s, want %s", i+1, got, s.diff)
		}
	}
}

func TestUpdateMasteryStaysInRange(t *testing.T) {
	for _, p := range []float64{0, 0.001, 0.5, 0.999, 1} {
		for _, c := range []bool{true, false} {
			got := UpdateMastery(p, c, DefaultParams)
			if got < 0 || got > 1 || math.IsNaN(got) {
				t.Fatalf("UpdateMastery(%v, %v) = %v", p, c, got)
			}
		}
	}
}

func TestTargetThresholds(t *testing.T) {
	cases := map[float64]Difficulty{0: Easy, 0.39: Easy, 0.40: Medium, 0.69: Medium, 0.70: Hard, 1: Hard}
	for p, want := range cases {
		if got := DefaultThresholds.Target(p); got != want {
			t.Errorf("Target(%v) = %s, want %s", p, got, want)
		}
	}
	if !DefaultThresholds.IsMastered(0.95) || DefaultThresholds.IsMastered(0.949) {
		t.Error("mastered threshold should be 0.95 inclusive")
	}
}

var bonding = []Candidate{
	{1, "bonding", Easy},
	{2, "bonding", Medium},
	{3, "bonding", Hard},
	{4, "gases", Medium},
}

func TestPickQuestionFallsBackToEasier(t *testing.T) {
	seen := map[uint]Seen{}
	if p, _ := PickQuestion(bonding, "bonding", Hard, seen, DefaultRetryGap); p.ID != 3 {
		t.Fatalf("want hard question 3, got %d", p.ID)
	}
	seen[3] = Seen{Correct: true}
	if p, _ := PickQuestion(bonding, "bonding", Hard, seen, DefaultRetryGap); p.ID != 2 {
		t.Fatalf("hard exhausted: want medium 2, got %d", p.ID)
	}
	seen[2] = Seen{Correct: true}
	if p, _ := PickQuestion(bonding, "bonding", Hard, seen, DefaultRetryGap); p.ID != 1 {
		t.Fatalf("want easy 1, got %d", p.ID)
	}
	seen[1] = Seen{Correct: true}
	if _, ok := PickQuestion(bonding, "bonding", Hard, seen, DefaultRetryGap); ok {
		t.Fatal("skill exhausted: want no question")
	}
}

func TestPickQuestionNeverStepsUp(t *testing.T) {
	seen := map[uint]Seen{1: {Correct: false, Ago: 0}}
	if p, ok := PickQuestion(bonding, "bonding", Easy, seen, DefaultRetryGap); ok {
		t.Fatalf("easy exhausted: want no question, got %d (%s)", p.ID, p.Difficulty)
	}
	seen = map[uint]Seen{2: {Correct: true}}
	if p, _ := PickQuestion(bonding, "bonding", Medium, seen, DefaultRetryGap); p.ID != 1 {
		t.Fatalf("medium exhausted: want easy 1, not hard; got %d", p.ID)
	}
}

func TestPickQuestionRetriesWrongAnswersAfterGap(t *testing.T) {
	cands := []Candidate{{1, "b", Easy}, {2, "b", Easy}, {3, "b", Easy}}
	seen := map[uint]Seen{
		1: {Correct: false, Ago: 2},
		2: {Correct: true, Ago: 5},
		3: {Correct: false, Ago: 4},
	}
	p, ok := PickQuestion(cands, "b", Easy, seen, 3)
	if !ok || p.ID != 3 || !p.Retry {
		t.Fatalf("want retry of 3 (wrong, 4 ago), got %+v ok=%v", p, ok)
	}
	seen[3] = Seen{Correct: false, Ago: 0}
	if p, ok := PickQuestion(cands, "b", Easy, seen, 3); ok {
		t.Fatalf("no wrong answer old enough: want none, got %+v", p)
	}
	if CanRetry(Seen{Correct: true, Ago: 10}, 3) {
		t.Fatal("a correctly answered question should not be retried")
	}
}

func TestRankSkillsWeakestFirstSkipsMastered(t *testing.T) {
	mastery := map[string]float64{"a": 0.8, "b": 0.97, "c": 0.5}
	prior := func(string) float64 { return 0.3 }
	got := RankSkills([]string{"a", "b", "c", "d"}, mastery, prior, DefaultThresholds)
	want := []string{"d", "c", "a"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
