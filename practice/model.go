package practice

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
	"github.com/methuliH/learnsteer-adaptive-mvp/store"
)

var ErrInvalidParams = errors.New("invalid BKT parameters")

// SkillParams is the BKT model for one skill as the engine currently uses it.
type SkillParams struct {
	Skill string `json:"skill"`
	engine.Params
	Source    string     `json:"source"` // "default", "manual" or "fitted"
	Answers   int        `json:"answers"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// FittedParams is one skill's parameters from a pyBKT fit (or set by hand).
type FittedParams struct {
	Subject string `json:"subject"`
	Skill   string `json:"skill"`
	engine.Params
	Answers int `json:"answers"`
}

// ModelParams lists the parameters in use for every skill of a subject.
func (s *Service) ModelParams(subjectID string) ([]SkillParams, error) {
	subj, ok := s.store.Subject(subjectID)
	if !ok {
		return nil, ErrUnknownSubject
	}
	stored := map[string]store.ParamRecord{}
	for _, r := range s.store.ParamRecords() {
		if r.Subject == subj.ID {
			stored[r.Skill] = r
		}
	}
	out := make([]SkillParams, 0, len(subj.Skills))
	for _, sk := range subj.Skills {
		sp := SkillParams{Skill: sk, Params: engine.DefaultParams, Source: "default"}
		if r, ok := stored[sk]; ok {
			at := r.UpdatedAt
			sp.Params, sp.Source, sp.Answers, sp.UpdatedAt = r.Params, r.Source, r.Answers, &at
		}
		out = append(out, sp)
	}
	return out, nil
}

// ValidateParams rejects values the engine cannot use. guess + slip must be
// below 1, otherwise a right answer would count as evidence of NOT knowing.
func ValidateParams(p engine.Params) error {
	for name, v := range map[string]float64{"prior": p.Prior, "learn": p.Learn, "guess": p.Guess, "slip": p.Slip} {
		if v < 0 || v > 1 {
			return fmt.Errorf("%w: %s = %v is outside 0..1", ErrInvalidParams, name, v)
		}
	}
	if p.Guess+p.Slip >= 1 {
		return fmt.Errorf("%w: guess + slip = %.2f must be below 1", ErrInvalidParams, p.Guess+p.Slip)
	}
	return nil
}

// ApplyParams stores parameters for several skills. It checks them all first
// and applies none if any is invalid or names an unknown skill.
func (s *Service) ApplyParams(list []FittedParams, source string) error {
	for _, f := range list {
		subj, ok := s.store.Subject(f.Subject)
		if !ok {
			return fmt.Errorf("%w: %q", ErrUnknownSubject, f.Subject)
		}
		if !contains(subj.Skills, f.Skill) {
			return fmt.Errorf("%w: %q in %q", ErrUnknownSkill, f.Skill, f.Subject)
		}
		if err := ValidateParams(f.Params); err != nil {
			return fmt.Errorf("%s: %w", f.Skill, err)
		}
	}
	now := s.now()
	for _, f := range list {
		s.store.SetParams(store.ParamRecord{
			Subject: f.Subject, Skill: f.Skill, Params: f.Params,
			Source: source, Answers: f.Answers, UpdatedAt: now,
		})
	}
	return nil
}

// ExportCSV writes every attempt, oldest first, in pyBKT's default columns
// plus the subject, so fitted parameters can be mapped back to a skill.
func (s *Service) ExportCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"order_id", "user_id", "skill_name", "correct", "subject"}); err != nil {
		return err
	}
	for i, a := range s.store.Attempts() {
		correct := "0"
		if a.Correct {
			correct = "1"
		}
		if err := cw.Write([]string{strconv.Itoa(i + 1), a.UserID, a.Skill, correct, a.Subject}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
