// Package practice is the adaptive practice flow: serve a question, grade the
// answer, update mastery, pick the next question. It depends only on the
// engine and the store.Store interface.
package practice

import (
	"errors"
	"sync"
	"time"

	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
	"github.com/methuliH/learnsteer-adaptive-mvp/store"
)

var (
	ErrUnknownSubject  = errors.New("unknown subject")
	ErrUnknownSkill    = errors.New("unknown skill for this subject")
	ErrUnknownQuestion = errors.New("unknown question")
	ErrInvalidOption   = errors.New("selected option is not one of the question's options")
	ErrAlreadyAnswered = errors.New("question already answered")
)

// PublicQuestion is what clients see: no correct answer, no explanation.
type PublicQuestion struct {
	ID         uint              `json:"id"`
	Subject    string            `json:"subject"`
	Skill      string            `json:"skill"`
	Difficulty engine.Difficulty `json:"difficulty"`
	Text       string            `json:"text"`
	Options    []store.Option    `json:"options"`
}

type SkillMastery struct {
	Skill    string            `json:"skill"`
	Mastery  float64           `json:"mastery"`
	Attempts int               `json:"attempts"`
	Mastered bool              `json:"mastered"`
	Target   engine.Difficulty `json:"target_difficulty"`
}

type NextResponse struct {
	Question *PublicQuestion   `json:"question"` // nil when Done
	Skill    string            `json:"skill,omitempty"`
	Mastery  float64           `json:"mastery"`
	Target   engine.Difficulty `json:"target_difficulty,omitempty"`
	Retry    bool              `json:"retry"` // a question the student got wrong earlier
	Done     bool              `json:"done"`
	Message  string            `json:"message,omitempty"`
}

type AnswerResponse struct {
	Correct       bool         `json:"is_correct"`
	CorrectOption string       `json:"correct_option"`
	Explanation   string       `json:"explanation"`
	Skill         string       `json:"skill"`
	MasteryBefore float64      `json:"mastery_before"`
	MasteryAfter  float64      `json:"mastery_after"`
	Mastered      bool         `json:"mastered"`
	JustMastered  bool         `json:"just_mastered"`
	Next          NextResponse `json:"next"`
}

type Service struct {
	store    store.Store
	th       engine.Thresholds
	retryGap int
	now      func() time.Time
	mu       sync.Mutex // serialises answers so a mastery read-modify-write is not lost
}

func NewService(s store.Store, th engine.Thresholds) *Service {
	return &Service{store: s, th: th, retryGap: engine.DefaultRetryGap, now: time.Now}
}

// seen maps each question the user has answered to their latest attempt at it.
func (s *Service) seen(user string) map[uint]engine.Seen {
	attempts := s.store.UserAttempts(user)
	out := make(map[uint]engine.Seen, len(attempts))
	for i, a := range attempts {
		out[a.QuestionID] = engine.Seen{Correct: a.Correct, Ago: len(attempts) - 1 - i}
	}
	return out
}

func (s *Service) Subjects() []store.Subject { return s.store.Subjects() }

// current returns the student's mastery for each skill, falling back to the
// skill's prior for skills never attempted.
func (s *Service) current(user string, subj store.Subject) map[string]float64 {
	recs := s.store.Mastery(user, subj.ID)
	out := make(map[string]float64, len(subj.Skills))
	for _, sk := range subj.Skills {
		if r, ok := recs[sk]; ok {
			out[sk] = r.P
		} else {
			out[sk] = s.store.Params(subj.ID, sk).Prior
		}
	}
	return out
}

// Next picks the next question. With skill set it stays in that skill while
// it has unseen questions; otherwise it starts from the weakest unmastered skill.
func (s *Service) Next(user, subjectID, skill string) (NextResponse, error) {
	subj, ok := s.store.Subject(subjectID)
	if !ok {
		return NextResponse{}, ErrUnknownSubject
	}
	if skill != "" && !contains(subj.Skills, skill) {
		return NextResponse{}, ErrUnknownSkill
	}

	mastery := s.current(user, subj)
	prior := func(sk string) float64 { return s.store.Params(subj.ID, sk).Prior }
	order := engine.RankSkills(subj.Skills, mastery, prior, s.th)
	if skill != "" {
		order = append([]string{skill}, without(order, skill)...)
	}
	if len(order) == 0 {
		return NextResponse{Done: true, Message: "Every unit in this subject is mastered."}, nil
	}

	qs := s.store.Questions(subj.ID)
	cands := make([]engine.Candidate, len(qs))
	for i, q := range qs {
		cands[i] = engine.Candidate{ID: q.ID, Skill: q.Skill, Difficulty: q.Difficulty}
	}
	seen := s.seen(user)

	for _, sk := range order {
		target := s.th.Target(mastery[sk])
		pick, ok := engine.PickQuestion(cands, sk, target, seen, s.retryGap)
		if !ok {
			continue
		}
		q, _ := s.store.Question(pick.ID)
		resp := NextResponse{Question: public(q), Skill: sk, Mastery: mastery[sk], Target: target, Retry: pick.Retry}
		if skill != "" && sk != skill {
			resp.Message = "No more questions at your level in " + skill + " for now; moved to " + sk + "."
		}
		return resp, nil
	}
	return NextResponse{Done: true, Message: "You've used every practice question at your level for now. Questions you got wrong come back after a few others."}, nil
}

// Answer grades one answer, updates mastery and returns the next question.
func (s *Service) Answer(user string, questionID uint, selected string) (AnswerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	q, ok := s.store.Question(questionID)
	if !ok {
		return AnswerResponse{}, ErrUnknownQuestion
	}
	if !hasOption(q, selected) {
		return AnswerResponse{}, ErrInvalidOption
	}
	// A question may be answered again only as a retry of an earlier wrong answer
	if prev, ok := s.seen(user)[q.ID]; ok && !engine.CanRetry(prev, s.retryGap) {
		return AnswerResponse{}, ErrAlreadyAnswered
	}

	prm := s.store.Params(q.Subject, q.Skill)
	rec, ok := s.store.Mastery(user, q.Subject)[q.Skill]
	if !ok {
		rec = store.MasteryRecord{Subject: q.Subject, Skill: q.Skill, P: prm.Prior}
	}
	correct := selected == q.Correct
	before := rec.P
	after := engine.UpdateMastery(before, correct, prm)

	now := s.now()
	justMastered := rec.MasteredAt == nil && s.th.IsMastered(after)
	if justMastered {
		rec.MasteredAt = &now
	}
	rec.P, rec.Attempts, rec.UpdatedAt = after, rec.Attempts+1, now
	s.store.SaveMastery(user, rec)
	s.store.AddAttempt(store.Attempt{
		UserID: user, QuestionID: q.ID, Subject: q.Subject, Skill: q.Skill,
		Difficulty: q.Difficulty, Selected: selected, Correct: correct,
		Before: before, After: after, AnsweredAt: now,
	})

	mastered := s.th.IsMastered(after)
	nextSkill := q.Skill
	if mastered {
		nextSkill = "" // move on to the weakest unit
	}
	next, err := s.Next(user, q.Subject, nextSkill)
	if err != nil {
		return AnswerResponse{}, err
	}

	return AnswerResponse{
		Correct: correct, CorrectOption: q.Correct, Explanation: q.Explanation,
		Skill: q.Skill, MasteryBefore: before, MasteryAfter: after,
		Mastered: mastered, JustMastered: justMastered, Next: next,
	}, nil
}

// Mastery lists the student's mastery per skill, in syllabus order.
func (s *Service) Mastery(user, subjectID string) ([]SkillMastery, error) {
	subj, ok := s.store.Subject(subjectID)
	if !ok {
		return nil, ErrUnknownSubject
	}
	recs := s.store.Mastery(user, subj.ID)
	cur := s.current(user, subj)
	out := make([]SkillMastery, 0, len(subj.Skills))
	for _, sk := range subj.Skills {
		p := cur[sk]
		out = append(out, SkillMastery{
			Skill: sk, Mastery: p, Attempts: recs[sk].Attempts,
			Mastered: s.th.IsMastered(p), Target: s.th.Target(p),
		})
	}
	return out, nil
}

func (s *Service) Reset(user string) { s.store.ResetUser(user) }

func public(q store.Question) *PublicQuestion {
	return &PublicQuestion{
		ID: q.ID, Subject: q.Subject, Skill: q.Skill,
		Difficulty: q.Difficulty, Text: q.Text, Options: q.Options,
	}
}

func hasOption(q store.Question, key string) bool {
	for _, o := range q.Options {
		if o.Key == key {
			return true
		}
	}
	return false
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func without(xs []string, x string) []string {
	out := make([]string, 0, len(xs))
	for _, v := range xs {
		if v != x {
			out = append(out, v)
		}
	}
	return out
}
