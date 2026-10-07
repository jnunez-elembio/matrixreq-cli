package execution

import (
	"encoding/json"
	"fmt"
)

// ClearResults returns a copy of steps with any previously recorded execution
// stripped, which is the state an upload starts from. The copy shares no
// mutable state with the input, including the unmodeled-key map.
func ClearResults(steps []TestStep) []TestStep {
	out := make([]TestStep, len(steps))
	for i, s := range steps {
		s.Result = ""
		s.Human = ""
		s.Render = ""
		s.Comment = ""
		s.extra = cloneExtra(s.extra)
		out[i] = s
	}
	return out
}

// cloneExtra copies the key map so two steps never alias one another's
// unmodeled keys. The raw values are never mutated, so they are shared.
func cloneExtra(extra map[string]json.RawMessage) map[string]json.RawMessage {
	if extra == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(extra))
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// MatchSteps applies one test's step results to the steps an XTC holds.
//
// An XTC is created from a snapshot of its TC, so a local test that is in sync
// with Matrix has exactly the same steps in the same order. That is the whole
// contract, and it is enforced step by step: the counts must agree, and the
// step at each index must carry the same requirement link on both sides.
// Results are then recorded by index, so every local step — including those
// with no requirement — lands on its XTC step. That matters for correctness: a
// failure on a step without a requirement still fails the run, and a run can
// never finish with every step blank.
//
// Any drift sets OutOfSync, and the caller must then leave the XTC untouched:
// a half-written execution is worse than none, because Matrix shows it as a
// finished run. Every drift found is reported, not just the first, so an
// operator sees the full picture in one run.
func MatchSteps(tcName, xtcRef string, local []ExecutionResultStep, upstream []TestStep) StepMatch {
	var m StepMatch

	if len(local) != len(upstream) {
		m.flag(
			fmt.Sprintf("step count mismatch (local %d, %s %d)", len(local), xtcRef, len(upstream)),
			fmt.Sprintf(
				"%s: OUT OF SYNC — the local test has %d step(s) but %s has %d. No results "+
					"were uploaded. Re-sync the test case, recreate the XTC run, then review "+
					"%s manually.",
				tcName, len(local), xtcRef, len(upstream), xtcRef),
		)
		// Per-step findings would only be noise once the step lists have
		// different shapes.
		return m
	}

	// A run with no steps has nothing to record, and "passed" would be a lie.
	if len(upstream) == 0 {
		m.flag(
			"no steps to execute",
			fmt.Sprintf(
				"%s: OUT OF SYNC — neither the local test nor %s has any steps, so there is "+
					"nothing to record. No results were uploaded; review %s manually.",
				tcName, xtcRef, xtcRef),
		)
		return m
	}

	steps := ClearResults(upstream)
	runResult := "p"

	for i, lr := range local {
		if lr.Requirement != steps[i].RequirementLink {
			m.flag(
				fmt.Sprintf("step %d requirement differs (local %q, %s %q)",
					i+1, lr.Requirement, xtcRef, steps[i].RequirementLink),
				fmt.Sprintf(
					"%s: OUT OF SYNC — step %d verifies %s locally but %s in %s. No results "+
						"were uploaded; review %s manually.",
					tcName, i+1, describeRequirement(lr.Requirement),
					describeRequirement(steps[i].RequirementLink), xtcRef, xtcRef),
			)
			continue
		}

		switch lr.Status {
		case "PASS":
			steps[i].Result, steps[i].Human, steps[i].Render = "p", "passed", "ok"
		case "FAIL":
			steps[i].Result, steps[i].Human, steps[i].Render = "f", "failed", "error"
			runResult = "f"
		default:
			// Quietly recording anything else as a failure would hide that the
			// results file says something this uploader does not understand.
			m.flag(
				fmt.Sprintf("step %d has unknown status %q", i+1, lr.Status),
				fmt.Sprintf(
					"%s: OUT OF SYNC — step %d has status %q, which is neither PASS nor FAIL. "+
						"No results were uploaded; review the results file.",
					tcName, i+1, lr.Status),
			)
			continue
		}
		steps[i].Comment = lr.Actual
	}

	// Nothing partially filled is handed back: a caller that ignores
	// OutOfSync fails loudly instead of writing half an execution.
	if m.OutOfSync {
		return m
	}

	m.Steps = steps
	m.RunResult = runResult
	return m
}

// describeRequirement renders a requirement link for an operator message.
func describeRequirement(link string) string {
	if link == "" {
		return "no requirement"
	}
	return "requirement " + link
}

// flag marks the match out of sync, keeping the first reason as the summary.
func (m *StepMatch) flag(reason, issue string) {
	m.OutOfSync = true
	if m.Reason == "" {
		m.Reason = reason
	}
	m.Issues = append(m.Issues, issue)
}
