package execution

import "fmt"

// ClearResults returns a copy of steps with any previously recorded execution
// stripped, which is the state an upload starts from.
func ClearResults(steps []TestStep) []TestStep {
	out := make([]TestStep, len(steps))
	copy(out, steps)
	for i := range out {
		out[i].Result = ""
		out[i].Human = ""
		out[i].Render = ""
		out[i].Comment = ""
	}
	return out
}

// MatchSteps applies one test's step results to the steps an XTC holds.
//
// An XTC is created from a snapshot of its TC, so a local test that is in sync
// with Matrix has exactly the same steps in the same order. Results are still
// matched by requirement link rather than by index, because only requirement
// steps can carry one — but the step counts are compared first, since matching
// by requirement alone would quietly accept a local test that has gained or
// lost steps and write a partial result that looks complete in Matrix.
//
// Any drift sets OutOfSync, and the caller must then leave the XTC untouched:
// a half-written execution is worse than none, because Matrix shows it as a
// finished run.
func MatchSteps(tcName, xtcRef string, local []ExecutionResultStep, upstream []TestStep) StepMatch {
	m := StepMatch{Steps: ClearResults(upstream), RunResult: "p"}

	if len(local) != len(upstream) {
		m.flag(
			fmt.Sprintf("step count mismatch (local %d, %s %d)", len(local), xtcRef, len(upstream)),
			fmt.Sprintf(
				"%s: OUT OF SYNC — the local test has %d step(s) but %s has %d. No results "+
					"were uploaded. Re-sync the test case, recreate the XTC run, then review "+
					"%s manually.",
				tcName, len(local), xtcRef, len(upstream), xtcRef),
		)
		// Requirement-level findings would only be noise once the step lists
		// have different shapes.
		return m
	}

	for _, lr := range local {
		if lr.Requirement == "" {
			continue
		}
		if !m.apply(lr) {
			m.flag(
				fmt.Sprintf("requirement %s has no step in %s", lr.Requirement, xtcRef),
				fmt.Sprintf(
					"%s: OUT OF SYNC — requirement %s is covered by the local test but has no "+
						"matching step in %s. No results were uploaded; review %s manually.",
					tcName, lr.Requirement, xtcRef, xtcRef),
			)
		}
	}

	for _, s := range m.Steps {
		if s.RequirementLink != "" && s.Result == "" {
			m.flag(
				fmt.Sprintf("%s has an unexecuted step for requirement %s", xtcRef, s.RequirementLink),
				fmt.Sprintf(
					"%s: OUT OF SYNC — %s has a step for requirement %s that the local test "+
						"does not cover. No results were uploaded; review %s manually.",
					tcName, xtcRef, s.RequirementLink, xtcRef),
			)
			break
		}
	}

	for _, s := range m.Steps {
		if s.Result == "f" {
			m.RunResult = "f"
			break
		}
	}

	return m
}

// apply records one step result against the first unfilled XTC step carrying
// the same requirement link, reporting whether it found one. Repeated
// requirements are filled in the order they appear, which is why a filled step
// is skipped rather than overwritten.
func (m *StepMatch) apply(lr ExecutionResultStep) bool {
	for i := range m.Steps {
		if m.Steps[i].RequirementLink != lr.Requirement || m.Steps[i].Human != "" {
			continue
		}
		if lr.Status == "PASS" {
			m.Steps[i].Result, m.Steps[i].Human, m.Steps[i].Render = "p", "passed", "ok"
		} else {
			m.Steps[i].Result, m.Steps[i].Human, m.Steps[i].Render = "f", "failed", "error"
		}
		m.Steps[i].Comment = lr.Actual
		return true
	}
	return false
}

// flag marks the match out of sync, keeping the first reason as the summary.
func (m *StepMatch) flag(reason, issue string) {
	m.OutOfSync = true
	if m.Reason == "" {
		m.Reason = reason
	}
	m.Issues = append(m.Issues, issue)
}
