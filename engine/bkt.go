// Package engine holds the adaptive-quiz logic: Bayesian Knowledge Tracing
// (BKT) mastery updates and next-question selection.
//
// It has no storage or HTTP dependencies, so it can be copied into the
// LearnSteer backend (internal/adaptive) or imported as a module unchanged.
package engine

// Params are the four BKT parameters for one skill (unit).
type Params struct {
	Prior float64 `json:"prior"` // P(known) before any practice
	Learn float64 `json:"learn"` // P(learns it after one practice opportunity)
	Guess float64 `json:"guess"` // P(correct | not known)
	Slip  float64 `json:"slip"`  // P(wrong | known)
}

// DefaultParams are hand-set cold-start values used until pyBKT has fitted
// real ones. Guess is 0.2 because A/L MCQs have five options.
var DefaultParams = Params{Prior: 0.30, Learn: 0.10, Guess: 0.20, Slip: 0.10}

// UpdateMastery returns the new probability that the student knows the skill
// after one observed answer.
func UpdateMastery(p float64, correct bool, prm Params) float64 {
	var post float64
	if correct {
		post = p * (1 - prm.Slip) / (p*(1-prm.Slip) + (1-p)*prm.Guess)
	} else {
		post = p * prm.Slip / (p*prm.Slip + (1-p)*(1-prm.Guess))
	}
	// Chance they learned it from this practice opportunity
	return clamp(post + (1-post)*prm.Learn)
}

func clamp(p float64) float64 {
	if p < 0 {
		return 0
	}
	if p > 1 {
		return 1
	}
	return p
}
