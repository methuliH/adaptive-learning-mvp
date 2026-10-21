// Package sim runs simulated students through the real practice service.
//
// Each simulated student has a hidden true state per skill (knows it or not),
// driven by "true" BKT parameters that differ from the engine's defaults.
// That shows two things before any real data exists:
//   - the engine's estimates track the hidden state even with hand-set defaults;
//   - pyBKT, fitted on the exported answers, recovers the true parameters.
package sim

import (
	"fmt"
	"io"
	"math"
	"math/rand"

	"github.com/methuliH/adaptive-learning-mvp/engine"
	"github.com/methuliH/adaptive-learning-mvp/practice"
	"github.com/methuliH/adaptive-learning-mvp/store"
)

type Config struct {
	Students          int
	Steps             int // answers per student
	Skills            int // synthetic bank only
	QuestionsPerLevel int // synthetic bank only: per skill per difficulty
	Truth             engine.Params
	Seed              int64
}

var DefaultConfig = Config{
	Students: 200, Steps: 30, Skills: 3, QuestionsPerLevel: 20,
	Truth: engine.Params{Prior: 0.20, Learn: 0.15, Guess: 0.25, Slip: 0.08},
	Seed:  1,
}

// Presets give each unit of a real subject different true parameters, so a
// pyBKT fit has something to tell apart. Units take them in syllabus order.
var Presets = []engine.Params{
	{Prior: 0.25, Learn: 0.15, Guess: 0.20, Slip: 0.08},
	{Prior: 0.10, Learn: 0.10, Guess: 0.25, Slip: 0.10},
	{Prior: 0.40, Learn: 0.20, Guess: 0.20, Slip: 0.05},
}

type Result struct {
	Answers int
	// Of final estimates, the share that put the student on the right side of
	// 0.5 compared with their hidden state.
	Accuracy float64
	// Mean final estimate for students who truly know / do not know a skill.
	MeanKnown, MeanUnknown float64
	// Share of questions served at each difficulty, by fifths of the session.
	DifficultyByPhase [5]map[engine.Difficulty]float64
	// Share of student-skill pairs that reached mastery.
	MasteredShare float64
	Service       *practice.Service
}

const subjectID = "sim"

// Bank builds a synthetic question bank for the simulation.
func Bank(cfg Config) store.Bank {
	b := store.Bank{Subjects: []store.Subject{{ID: subjectID, Name: "Simulated subject"}}}
	id := uint(1)
	opts := []store.Option{{Key: "a", Text: "A"}, {Key: "b", Text: "B"}, {Key: "c", Text: "C"}, {Key: "d", Text: "D"}, {Key: "e", Text: "E"}}
	for s := 1; s <= cfg.Skills; s++ {
		for _, d := range []engine.Difficulty{engine.Easy, engine.Medium, engine.Hard} {
			for i := 0; i < cfg.QuestionsPerLevel; i++ {
				b.Questions = append(b.Questions, store.Question{
					ID: id, Subject: subjectID, Skill: fmt.Sprintf("Skill %d", s), Difficulty: d,
					Text: fmt.Sprintf("Q%d", id), Options: opts, Correct: "a",
				})
				id++
			}
		}
	}
	return b
}

// tally is what a batch of simulated students leaves behind.
type tally struct {
	answers                int
	served                 [5]map[engine.Difficulty]int
	right, pairs, mastered int
	nKnown, nUnknown       int
	sumKnown, sumUnknown   float64
}

// students runs n simulated students named prefix001… through svc.
func students(svc *practice.Service, st store.Store, subject, prefix string, n, steps int,
	truth func(skill string) engine.Params, rng *rand.Rand) (tally, error) {

	var t tally
	for i := range t.served {
		t.served[i] = map[engine.Difficulty]int{}
	}
	subj, ok := st.Subject(subject)
	if !ok {
		return t, practice.ErrUnknownSubject
	}

	for s := 0; s < n; s++ {
		user := fmt.Sprintf("%s%03d", prefix, s+1)
		knows := map[string]bool{}
		for _, sk := range subj.Skills {
			knows[sk] = rng.Float64() < truth(sk).Prior
		}

		next, err := svc.Next(user, subject, "")
		if err != nil {
			return t, err
		}
		for step := 0; step < steps && !next.Done; step++ {
			q := next.Question
			tp := truth(q.Skill)
			t.served[step*5/steps][q.Difficulty]++

			var correct bool
			if knows[q.Skill] {
				correct = rng.Float64() >= tp.Slip
			} else {
				correct = rng.Float64() < tp.Guess
			}
			full, _ := st.Question(q.ID)
			ans, err := svc.Answer(user, q.ID, pickOption(full, correct))
			if err != nil {
				return t, err
			}
			t.answers++
			// Learning happens after the practice opportunity
			if !knows[q.Skill] && rng.Float64() < tp.Learn {
				knows[q.Skill] = true
			}
			next = ans.Next
		}

		ms, err := svc.Mastery(user, subject)
		if err != nil {
			return t, err
		}
		for _, m := range ms {
			if m.Attempts == 0 {
				continue
			}
			t.pairs++
			if m.Mastered {
				t.mastered++
			}
			if (m.Mastery >= 0.5) == knows[m.Skill] {
				t.right++
			}
			if knows[m.Skill] {
				t.nKnown++
				t.sumKnown += m.Mastery
			} else {
				t.nUnknown++
				t.sumUnknown += m.Mastery
			}
		}
	}
	return t, nil
}

func pickOption(q store.Question, correct bool) string {
	if correct {
		return q.Correct
	}
	for _, o := range q.Options {
		if o.Key != q.Correct {
			return o.Key
		}
	}
	return q.Correct
}

// Run simulates students on a synthetic bank with one set of true parameters.
func Run(cfg Config) (Result, error) {
	st := store.NewMemory(Bank(cfg))
	svc := practice.NewService(st, engine.DefaultThresholds)
	rng := rand.New(rand.NewSource(cfg.Seed))
	t, err := students(svc, st, subjectID, "sim_", cfg.Students, cfg.Steps,
		func(string) engine.Params { return cfg.Truth }, rng)
	if err != nil {
		return Result{}, err
	}

	res := Result{Service: svc, Answers: t.answers}
	res.Accuracy = ratio(t.right, t.pairs)
	res.MasteredShare = ratio(t.mastered, t.pairs)
	res.MeanKnown = t.sumKnown / math.Max(1, float64(t.nKnown))
	res.MeanUnknown = t.sumUnknown / math.Max(1, float64(t.nUnknown))
	for i, m := range t.served {
		total := m[engine.Easy] + m[engine.Medium] + m[engine.Hard]
		res.DifficultyByPhase[i] = map[engine.Difficulty]float64{}
		for d, n := range m {
			res.DifficultyByPhase[i][d] = ratio(n, total)
		}
	}
	return res, nil
}

// PopulateResult describes a batch of simulated history added to a live store.
type PopulateResult struct {
	Students int                      `json:"students"`
	Answers  int                      `json:"answers"`
	Truth    map[string]engine.Params `json:"truth"` // per skill
}

// Populate adds simulated students to a live subject so there is enough
// history to fit pyBKT. Unit i uses Presets[i % len(Presets)] as its truth.
func Populate(svc *practice.Service, st store.Store, subject string, n, steps int, seed int64) (PopulateResult, error) {
	subj, ok := st.Subject(subject)
	if !ok {
		return PopulateResult{}, practice.ErrUnknownSubject
	}
	truth := map[string]engine.Params{}
	for i, sk := range subj.Skills {
		truth[sk] = Presets[i%len(Presets)]
	}
	rng := rand.New(rand.NewSource(seed))
	prefix := fmt.Sprintf("sim%d_", seed%100000)
	t, err := students(svc, st, subject, prefix, n, steps, func(sk string) engine.Params { return truth[sk] }, rng)
	if err != nil {
		return PopulateResult{}, err
	}
	return PopulateResult{Students: n, Answers: t.answers, Truth: truth}, nil
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Report prints a short human-readable summary.
func (r Result) Report(w io.Writer, cfg Config) {
	fmt.Fprintf(w, "Simulated %d students x up to %d answers (%d answers total)\n", cfg.Students, cfg.Steps, r.Answers)
	fmt.Fprintf(w, "True params:   prior %.2f  learn %.2f  guess %.2f  slip %.2f\n", cfg.Truth.Prior, cfg.Truth.Learn, cfg.Truth.Guess, cfg.Truth.Slip)
	d := engine.DefaultParams
	fmt.Fprintf(w, "Engine params: prior %.2f  learn %.2f  guess %.2f  slip %.2f (hand-set defaults)\n\n", d.Prior, d.Learn, d.Guess, d.Slip)
	fmt.Fprintf(w, "Estimate on the right side of 0.5 vs hidden state: %.0f%%\n", r.Accuracy*100)
	fmt.Fprintf(w, "Mean estimate when the student truly knows the skill:  %.2f\n", r.MeanKnown)
	fmt.Fprintf(w, "Mean estimate when the student does not know it:      %.2f\n", r.MeanUnknown)
	fmt.Fprintf(w, "Student-skill pairs reaching mastery (0.95): %.0f%%\n\n", r.MasteredShare*100)
	fmt.Fprintln(w, "Difficulty served, by fifth of the session:")
	fmt.Fprintln(w, "  phase   easy  medium  hard")
	for i, m := range r.DifficultyByPhase {
		fmt.Fprintf(w, "  %d/5    %3.0f%%   %3.0f%%  %3.0f%%\n", i+1, m[engine.Easy]*100, m[engine.Medium]*100, m[engine.Hard]*100)
	}
}
