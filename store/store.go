// Package store defines what the practice service needs from storage.
//
// The MVP uses the in-memory implementation in memory.go. Plugging into
// LearnSteer means adding a Postgres implementation of Store over the
// questions, skill_params, student_skill_mastery and practice_attempts tables.
package store

import (
	"time"

	"github.com/methuliH/learnsteer-adaptive-mvp/engine"
)

type Option struct {
	Key  string `json:"key"` // "a".."e", as the backend stores selected_option
	Text string `json:"text"`
}

// Question is the full stored question, including the correct answer.
// Never send it to clients as-is; the practice service maps it to a public type.
type Question struct {
	ID          uint              `json:"id"`
	Subject     string            `json:"subject"`
	Skill       string            `json:"skill"` // = questions.unit_name in LearnSteer
	Difficulty  engine.Difficulty `json:"difficulty"`
	Text        string            `json:"text"`
	Options     []Option          `json:"options"`
	Correct     string            `json:"correct"`
	Explanation string            `json:"explanation"`
}

type Subject struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Skills []string `json:"skills"` // in syllabus order
}

// ParamRecord is one row of skill_params.
type ParamRecord struct {
	Subject   string        `json:"subject"`
	Skill     string        `json:"skill"`
	Params    engine.Params `json:"params"`
	Source    string        `json:"source"`  // "manual" or "fitted"
	Answers   int           `json:"answers"` // answers the fit used; 0 for manual
	UpdatedAt time.Time     `json:"updated_at"`
}

// MasteryRecord is one row of student_skill_mastery.
type MasteryRecord struct {
	Subject    string     `json:"subject"`
	Skill      string     `json:"skill"`
	P          float64    `json:"p_mastery"`
	Attempts   int        `json:"attempts"`
	MasteredAt *time.Time `json:"mastered_at,omitempty"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// Attempt is one row of practice_attempts.
type Attempt struct {
	UserID     string            `json:"user_id"`
	QuestionID uint              `json:"question_id"`
	Subject    string            `json:"subject"`
	Skill      string            `json:"skill"`
	Difficulty engine.Difficulty `json:"difficulty"`
	Selected   string            `json:"selected_option"`
	Correct    bool              `json:"is_correct"`
	Before     float64           `json:"mastery_before"`
	After      float64           `json:"mastery_after"`
	AnsweredAt time.Time         `json:"answered_at"`
}

type Store interface {
	Subjects() []Subject
	Subject(id string) (Subject, bool)
	Questions(subject string) []Question
	Question(id uint) (Question, bool)

	// Params returns stored BKT parameters for a skill, or the defaults.
	Params(subject, skill string) engine.Params
	SetParams(r ParamRecord)
	// ParamRecords lists stored (non-default) parameters.
	ParamRecords() []ParamRecord

	Mastery(user, subject string) map[string]MasteryRecord
	SaveMastery(user string, r MasteryRecord)

	// UserAttempts returns the user's attempts, oldest first.
	UserAttempts(user string) []Attempt
	AddAttempt(a Attempt)
	Attempts() []Attempt

	// ResetUser clears a user's mastery and attempts (demo only).
	ResetUser(user string)
}
