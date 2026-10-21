package store

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
)

// Bank is the JSON shape of a question file.
type Bank struct {
	Subjects  []Subject  `json:"subjects"`
	Questions []Question `json:"questions"`
}

// LoadBank reads and validates a question bank.
func LoadBank(r io.Reader) (Bank, error) {
	var b Bank
	if err := json.NewDecoder(r).Decode(&b); err != nil {
		return b, fmt.Errorf("decode question bank: %w", err)
	}
	return b, b.validate()
}

func (b *Bank) validate() error {
	subjects := map[string]bool{}
	for _, s := range b.Subjects {
		subjects[s.ID] = true
	}
	ids := map[uint]bool{}
	for i := range b.Questions {
		q := &b.Questions[i]
		q.Skill = normalizeSkill(q.Skill)
		switch {
		case q.ID == 0 || ids[q.ID]:
			return fmt.Errorf("question %d: missing or duplicate id", q.ID)
		case !subjects[q.Subject]:
			return fmt.Errorf("question %d: unknown subject %q", q.ID, q.Subject)
		case q.Skill == "":
			return fmt.Errorf("question %d: missing skill", q.ID)
		case !q.Difficulty.Valid():
			return fmt.Errorf("question %d: difficulty %q must be easy, medium or hard", q.ID, q.Difficulty)
		}
		found := false
		for _, o := range q.Options {
			if o.Key == q.Correct {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("question %d: correct option %q is not one of its options", q.ID, q.Correct)
		}
		ids[q.ID] = true
	}
	return nil
}

// normalizeSkill trims and collapses spaces so "Chemical  Bonding " and
// "Chemical Bonding" are one skill. unit_name is free text in LearnSteer.
func normalizeSkill(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Memory is a thread-safe in-memory Store.
type Memory struct {
	mu        sync.RWMutex
	subjects  []Subject
	questions []Question
	byID      map[uint]Question
	params    map[string]ParamRecord
	mastery   map[string]map[string]MasteryRecord // user -> subject|skill -> record
	attempts  []Attempt
}

func NewMemory(b Bank) *Memory {
	m := &Memory{
		byID:    map[uint]Question{},
		params:  map[string]ParamRecord{},
		mastery: map[string]map[string]MasteryRecord{},
	}
	for _, s := range b.Subjects {
		s.Skills = nil
		m.subjects = append(m.subjects, s)
	}
	// Skills come from the questions, in first-seen order
	for _, q := range b.Questions {
		m.questions = append(m.questions, q)
		m.byID[q.ID] = q
		for i := range m.subjects {
			s := &m.subjects[i]
			if s.ID == q.Subject && !contains(s.Skills, q.Skill) {
				s.Skills = append(s.Skills, q.Skill)
			}
		}
	}
	return m
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func key(subject, skill string) string { return subject + "|" + skill }

func (m *Memory) Subjects() []Subject {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Subject(nil), m.subjects...)
}

func (m *Memory) Subject(id string) (Subject, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.subjects {
		if s.ID == id {
			return s, true
		}
	}
	return Subject{}, false
}

func (m *Memory) Questions(subject string) []Question {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Question
	for _, q := range m.questions {
		if q.Subject == subject {
			out = append(out, q)
		}
	}
	return out
}

func (m *Memory) Question(id uint) (Question, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	q, ok := m.byID[id]
	return q, ok
}

func (m *Memory) Params(subject, skill string) engine.Params {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.params[key(subject, skill)]; ok {
		return r.Params
	}
	return engine.DefaultParams
}

func (m *Memory) SetParams(r ParamRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.params[key(r.Subject, r.Skill)] = r
}

func (m *Memory) ParamRecords() []ParamRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]ParamRecord, 0, len(m.params))
	for _, r := range m.params {
		out = append(out, r)
	}
	return out
}

func (m *Memory) Mastery(user, subject string) map[string]MasteryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := map[string]MasteryRecord{}
	for _, r := range m.mastery[user] {
		if r.Subject == subject {
			out[r.Skill] = r
		}
	}
	return out
}

func (m *Memory) SaveMastery(user string, r MasteryRecord) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mastery[user] == nil {
		m.mastery[user] = map[string]MasteryRecord{}
	}
	m.mastery[user][key(r.Subject, r.Skill)] = r
}

func (m *Memory) UserAttempts(user string) []Attempt {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Attempt
	for _, a := range m.attempts {
		if a.UserID == user {
			out = append(out, a)
		}
	}
	return out
}

func (m *Memory) AddAttempt(a Attempt) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.attempts = append(m.attempts, a)
}

func (m *Memory) Attempts() []Attempt {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Attempt(nil), m.attempts...)
}

func (m *Memory) ResetUser(user string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.mastery, user)
	kept := m.attempts[:0]
	for _, a := range m.attempts {
		if a.UserID != user {
			kept = append(kept, a)
		}
	}
	m.attempts = kept
}
