package engine

import "sort"

// Difficulty is a question's difficulty tag.
type Difficulty string

const (
	Easy   Difficulty = "easy"
	Medium Difficulty = "medium"
	Hard   Difficulty = "hard"
)

// Valid reports whether d is one of the three known tags.
func (d Difficulty) Valid() bool {
	return d == Easy || d == Medium || d == Hard
}

// Thresholds map a mastery probability to a difficulty.
type Thresholds struct {
	Medium   float64 `json:"medium"`   // at or above: serve medium
	Hard     float64 `json:"hard"`     // at or above: serve hard
	Mastered float64 `json:"mastered"` // at or above: skill is mastered
}

var DefaultThresholds = Thresholds{Medium: 0.40, Hard: 0.70, Mastered: 0.95}

// Target returns the difficulty to serve for mastery p.
func (t Thresholds) Target(p float64) Difficulty {
	switch {
	case p >= t.Hard:
		return Hard
	case p >= t.Medium:
		return Medium
	default:
		return Easy
	}
}

// IsMastered reports whether p counts as mastered.
func (t Thresholds) IsMastered(p float64) bool {
	return p >= t.Mastered
}

// Candidate is the minimum the selector needs to know about a question.
type Candidate struct {
	ID         uint
	Skill      string
	Difficulty Difficulty
}

// atOrBelow lists the target difficulty and the easier ones, nearest first.
// The selector never steps up past the target: a struggling student who runs
// out of easy questions is not pushed to medium.
var atOrBelow = map[Difficulty][]Difficulty{
	Easy:   {Easy},
	Medium: {Medium, Easy},
	Hard:   {Hard, Medium, Easy},
}

// DefaultRetryGap is how many other answers must come between a wrong answer
// and serving that question again.
const DefaultRetryGap = 3

// Seen is the student's latest attempt at a question.
type Seen struct {
	Correct bool
	Ago     int // answers given since this one; 0 = the most recent answer
}

// Pick is the selector's choice.
type Pick struct {
	Candidate
	Retry bool // a question the student got wrong before
}

// PickQuestion returns a question in skill for the target difficulty:
//  1. an unseen question at the target, then at each easier difficulty;
//  2. otherwise a question the student last got wrong, at least retryGap
//     answers ago, at the target or easier (longest ago first).
//
// It never serves a question harder than the target. ok is false when the
// skill has nothing suitable, so the caller can move to another skill.
// Ties go to the lowest ID so the result is deterministic.
func PickQuestion(cands []Candidate, skill string, target Difficulty, seen map[uint]Seen, retryGap int) (Pick, bool) {
	levels, ok := atOrBelow[target]
	if !ok {
		levels = atOrBelow[Medium]
	}
	for _, d := range levels {
		var best *Candidate
		for i := range cands {
			c := &cands[i]
			if _, done := seen[c.ID]; done || c.Skill != skill || c.Difficulty != d {
				continue
			}
			if best == nil || c.ID < best.ID {
				best = c
			}
		}
		if best != nil {
			return Pick{Candidate: *best}, true
		}
	}
	for _, d := range levels {
		var best *Candidate
		for i := range cands {
			c := &cands[i]
			s, done := seen[c.ID]
			if !done || !CanRetry(s, retryGap) || c.Skill != skill || c.Difficulty != d {
				continue
			}
			if best == nil || s.Ago > seen[best.ID].Ago || (s.Ago == seen[best.ID].Ago && c.ID < best.ID) {
				best = c
			}
		}
		if best != nil {
			return Pick{Candidate: *best, Retry: true}, true
		}
	}
	return Pick{}, false
}

// CanRetry reports whether a seen question may be answered again.
func CanRetry(s Seen, retryGap int) bool {
	return !s.Correct && s.Ago >= retryGap
}

// RankSkills orders skills weakest first, leaving out mastered ones.
// mastery holds the current estimate per skill; skills missing from it use
// prior(skill). Ties keep the input order.
func RankSkills(skills []string, mastery map[string]float64, prior func(string) float64, t Thresholds) []string {
	type sp struct {
		skill string
		p     float64
	}
	var open []sp
	for _, s := range skills {
		p, ok := mastery[s]
		if !ok {
			p = prior(s)
		}
		if !t.IsMastered(p) {
			open = append(open, sp{s, p})
		}
	}
	sort.SliceStable(open, func(i, j int) bool { return open[i].p < open[j].p })
	out := make([]string, len(open))
	for i, o := range open {
		out[i] = o.skill
	}
	return out
}
