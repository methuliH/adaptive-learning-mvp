package practice

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/methuliH/adaptive-learning-mvp/data"
	"github.com/methuliH/adaptive-learning-mvp/engine"
	"github.com/methuliH/adaptive-learning-mvp/store"
)

func newService(t *testing.T) *Service {
	t.Helper()
	bank, err := store.LoadBank(bytes.NewReader(data.Questions))
	if err != nil {
		t.Fatal(err)
	}
	return NewService(store.NewMemory(bank), engine.DefaultThresholds)
}

func TestNewStudentStartsEasy(t *testing.T) {
	svc := newService(t)
	resp, err := svc.Next("s1", "chemistry", "")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Question == nil || resp.Question.Difficulty != engine.Easy {
		t.Fatalf("want an easy first question, got %+v", resp.Question)
	}
	if resp.Mastery != engine.DefaultParams.Prior {
		t.Fatalf("want prior mastery, got %v", resp.Mastery)
	}
}

func TestCorrectAnswersStepUpAndNeverRepeat(t *testing.T) {
	svc := newService(t)
	resp, _ := svc.Next("s1", "chemistry", "Chemical Bonding")
	var diffs []engine.Difficulty
	seen := map[uint]bool{}

	for i := 0; i < 3; i++ {
		q := resp.Question
		if seen[q.ID] {
			t.Fatalf("question %d served twice", q.ID)
		}
		seen[q.ID] = true
		diffs = append(diffs, q.Difficulty)
		full, _ := svc.store.Question(q.ID)
		ans, err := svc.Answer("s1", q.ID, full.Correct)
		if err != nil {
			t.Fatal(err)
		}
		if !ans.Correct || ans.MasteryAfter <= ans.MasteryBefore {
			t.Fatalf("correct answer should raise mastery: %+v", ans)
		}
		resp = ans.Next
	}
	want := []engine.Difficulty{engine.Easy, engine.Medium, engine.Hard}
	for i := range want {
		if diffs[i] != want[i] {
			t.Fatalf("difficulties = %v, want %v", diffs, want)
		}
	}
}

func TestWrongAnswerLowersMastery(t *testing.T) {
	svc := newService(t)
	resp, _ := svc.Next("s1", "chemistry", "")
	ans, err := svc.Answer("s1", resp.Question.ID, wrongKey(t, svc, resp.Question.ID))
	if err != nil {
		t.Fatal(err)
	}
	if ans.Correct || ans.MasteryAfter >= ans.MasteryBefore {
		t.Fatalf("wrong answer should lower mastery: %+v", ans)
	}
}

func TestStrugglingStudentStaysEasyAndRetries(t *testing.T) {
	svc := newService(t)
	resp, _ := svc.Next("s1", "chemistry", "Chemical Bonding")
	retried := false
	for i := 0; i < 8; i++ {
		q := resp.Question
		if q == nil {
			t.Fatalf("answer %d: no question: %s", i+1, resp.Message)
		}
		if q.Difficulty != engine.Easy || q.Skill != "Chemical Bonding" {
			t.Fatalf("answer %d: struggling student got %s %s", i+1, q.Skill, q.Difficulty)
		}
		retried = retried || resp.Retry
		ans, err := svc.Answer("s1", q.ID, wrongKey(t, svc, q.ID))
		if err != nil {
			t.Fatalf("answer %d: %v", i+1, err)
		}
		resp = ans.Next
	}
	if !retried {
		t.Fatal("after the easy questions ran out, wrong ones should come back as retries")
	}
}

func TestMovesUnitWhenNothingAtLevel(t *testing.T) {
	svc := newService(t)
	svc.retryGap = 100 // no retries yet
	resp, _ := svc.Next("s1", "chemistry", "Chemical Bonding")
	for i := 0; i < 5; i++ {
		ans, err := svc.Answer("s1", resp.Question.ID, wrongKey(t, svc, resp.Question.ID))
		if err != nil {
			t.Fatal(err)
		}
		resp = ans.Next
	}
	if resp.Question == nil || resp.Skill == "Chemical Bonding" || resp.Question.Difficulty != engine.Easy {
		t.Fatalf("want an easy question from another unit, got %+v", resp)
	}
	if resp.Message == "" {
		t.Fatal("want a message explaining the move")
	}
}

func TestAnswerErrors(t *testing.T) {
	svc := newService(t)
	if _, err := svc.Answer("s1", 9999, "a"); !errors.Is(err, ErrUnknownQuestion) {
		t.Fatalf("want ErrUnknownQuestion, got %v", err)
	}
	if _, err := svc.Answer("s1", 101, "z"); !errors.Is(err, ErrInvalidOption) {
		t.Fatalf("want ErrInvalidOption, got %v", err)
	}
	if _, err := svc.Answer("s1", 101, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Answer("s1", 101, "b"); !errors.Is(err, ErrAlreadyAnswered) {
		t.Fatalf("want ErrAlreadyAnswered, got %v", err)
	}
	if _, err := svc.Next("s1", "physics", ""); !errors.Is(err, ErrUnknownSubject) {
		t.Fatalf("want ErrUnknownSubject, got %v", err)
	}
}

func TestMasteringMovesToWeakestUnit(t *testing.T) {
	svc := newService(t)
	svc.store.SaveMastery("s1", store.MasteryRecord{Subject: "chemistry", Skill: "Gaseous State", P: 0.94})
	resp, _ := svc.Next("s1", "chemistry", "Gaseous State")
	full, _ := svc.store.Question(resp.Question.ID)
	ans, err := svc.Answer("s1", full.ID, full.Correct)
	if err != nil {
		t.Fatal(err)
	}
	if !ans.JustMastered || !ans.Mastered {
		t.Fatalf("want just mastered, got %+v", ans)
	}
	if ans.Next.Skill == "Gaseous State" {
		t.Fatal("after mastering, next question should come from another unit")
	}
}

func TestPublicQuestionHidesAnswer(t *testing.T) {
	svc := newService(t)
	resp, _ := svc.Next("s1", "chemistry", "")
	b, _ := json.Marshal(resp)
	if strings.Contains(string(b), "correct") || strings.Contains(string(b), "explanation") {
		t.Fatalf("question JSON leaks the answer: %s", b)
	}
}

func TestExportCSVFormat(t *testing.T) {
	svc := newService(t)
	svc.Answer("s1", 101, "b")
	svc.Answer("s1", 102, "a")
	var buf bytes.Buffer
	if err := svc.ExportCSV(&buf); err != nil {
		t.Fatal(err)
	}
	want := "order_id,user_id,skill_name,correct,subject\n1,s1,Atomic Structure,1,chemistry\n2,s1,Atomic Structure,0,chemistry\n"
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestFittedParamsChangeTheEngine(t *testing.T) {
	svc := newService(t)
	before, _ := svc.ModelParams("chemistry")
	if before[0].Source != "default" || before[0].Params != engine.DefaultParams {
		t.Fatalf("want defaults, got %+v", before[0])
	}

	fitted := engine.Params{Prior: 0.6, Learn: 0.2, Guess: 0.2, Slip: 0.05}
	err := svc.ApplyParams([]FittedParams{{Subject: "chemistry", Skill: "Atomic Structure", Params: fitted, Answers: 300}}, "fitted")
	if err != nil {
		t.Fatal(err)
	}
	after, _ := svc.ModelParams("chemistry")
	if after[0].Source != "fitted" || after[0].Params != fitted || after[0].Answers != 300 {
		t.Fatalf("want fitted params, got %+v", after[0])
	}
	// A new student now starts from the fitted prior, so the first question is medium
	resp, _ := svc.Next("new", "chemistry", "Atomic Structure")
	if resp.Mastery != 0.6 || resp.Question.Difficulty != engine.Medium {
		t.Fatalf("want prior 0.6 and a medium question, got %v %s", resp.Mastery, resp.Question.Difficulty)
	}
}

func TestApplyParamsIsAllOrNothing(t *testing.T) {
	svc := newService(t)
	good := FittedParams{Subject: "chemistry", Skill: "Atomic Structure", Params: engine.Params{Prior: 0.5, Learn: 0.1, Guess: 0.2, Slip: 0.1}}
	bad := FittedParams{Subject: "chemistry", Skill: "Gaseous State", Params: engine.Params{Prior: 0.5, Learn: 0.1, Guess: 0.6, Slip: 0.5}}
	if err := svc.ApplyParams([]FittedParams{good, bad}, "fitted"); !errors.Is(err, ErrInvalidParams) {
		t.Fatalf("want ErrInvalidParams, got %v", err)
	}
	unknown := FittedParams{Subject: "chemistry", Skill: "Thermodynamics", Params: good.Params}
	if err := svc.ApplyParams([]FittedParams{good, unknown}, "fitted"); !errors.Is(err, ErrUnknownSkill) {
		t.Fatalf("want ErrUnknownSkill, got %v", err)
	}
	if ps, _ := svc.ModelParams("chemistry"); ps[0].Source != "default" {
		t.Fatal("nothing should be applied when one entry is invalid")
	}
}

func wrongKey(t *testing.T, svc *Service, id uint) string {
	t.Helper()
	q, _ := svc.store.Question(id)
	for _, o := range q.Options {
		if o.Key != q.Correct {
			return o.Key
		}
	}
	t.Fatal("no wrong option")
	return ""
}
