package execution

import (
	"encoding/json"
	"testing"
)

// reqStep is an upstream step that verifies a requirement.
func reqStep(req string) TestStep {
	return TestStep{Action: "verify " + req, Expected: "ok", RequirementLink: req}
}

// actionStep is an upstream step with no requirement link.
func actionStep(action string) TestStep {
	return TestStep{Action: action}
}

func pass(req, actual string) ExecutionResultStep {
	return ExecutionResultStep{Requirement: req, Actual: actual, Status: "PASS"}
}

func fail(req, actual string) ExecutionResultStep {
	return ExecutionResultStep{Requirement: req, Actual: actual, Status: "FAIL"}
}

func TestMatchStepsInSync(t *testing.T) {
	upstream := []TestStep{
		actionStep("navigate"),
		reqStep("SOFT-5"),
		reqStep("SOFT-6"),
		actionStep("discard"),
	}
	local := []ExecutionResultStep{
		{Actual: "navigate", Status: "PASS"},
		pass("SOFT-5", "defaults populated"),
		pass("SOFT-6", "minimum enforced"),
		{Actual: "discard", Status: "PASS"},
	}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if m.OutOfSync {
		t.Fatalf("in-sync test reported out of sync: %v", m.Issues)
	}
	if m.RunResult != "p" {
		t.Errorf("RunResult: got %q, want %q", m.RunResult, "p")
	}
	if len(m.Issues) != 0 {
		t.Errorf("Issues: got %v, want none", m.Issues)
	}
	if m.Steps[1].Result != "p" || m.Steps[1].Human != "passed" || m.Steps[1].Render != "ok" {
		t.Errorf("SOFT-5 step not marked passed: %+v", m.Steps[1])
	}
	if m.Steps[1].Comment != "defaults populated" {
		t.Errorf("SOFT-5 comment: got %q", m.Steps[1].Comment)
	}
	// Every step is recorded, including those with no requirement, so none is
	// left blank in a run Matrix shows as finished.
	for i, step := range m.Steps {
		if step.Result != "p" {
			t.Errorf("step %d not recorded: %+v", i, step)
		}
	}
}

func TestMatchStepsFailurePropagatesToRunResult(t *testing.T) {
	upstream := []TestStep{reqStep("SOFT-5"), reqStep("SOFT-6")}
	local := []ExecutionResultStep{pass("SOFT-5", "ok"), fail("SOFT-6", "expected 5 got 1")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if m.OutOfSync {
		t.Fatalf("unexpected out of sync: %v", m.Issues)
	}
	if m.RunResult != "f" {
		t.Errorf("RunResult: got %q, want %q", m.RunResult, "f")
	}
	if m.Steps[1].Result != "f" || m.Steps[1].Human != "failed" || m.Steps[1].Render != "error" {
		t.Errorf("SOFT-6 step not marked failed: %+v", m.Steps[1])
	}
}

func TestMatchStepsRepeatedRequirementFillsInOrder(t *testing.T) {
	upstream := []TestStep{reqStep("SOFT-6"), reqStep("SOFT-6")}
	local := []ExecutionResultStep{pass("SOFT-6", "first"), fail("SOFT-6", "second")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if m.OutOfSync {
		t.Fatalf("unexpected out of sync: %v", m.Issues)
	}
	if m.Steps[0].Comment != "first" || m.Steps[1].Comment != "second" {
		t.Errorf("repeated requirement filled out of order: %q, %q",
			m.Steps[0].Comment, m.Steps[1].Comment)
	}
}

func TestMatchStepsRefusesCountMismatch(t *testing.T) {
	tests := []struct {
		name     string
		local    []ExecutionResultStep
		upstream []TestStep
	}{
		{
			name:     "local has an extra step",
			local:    []ExecutionResultStep{pass("SOFT-4", "a"), pass("SOFT-5", "b"), pass("SOFT-6", "c")},
			upstream: []TestStep{reqStep("SOFT-4"), reqStep("SOFT-5")},
		},
		{
			name:     "upstream has an extra step",
			local:    []ExecutionResultStep{pass("SOFT-4", "a"), pass("SOFT-5", "b")},
			upstream: []TestStep{reqStep("SOFT-4"), reqStep("SOFT-5"), reqStep("SOFT-6")},
		},
		{
			name:     "local gained an action step only",
			local:    []ExecutionResultStep{pass("SOFT-4", "a"), {Actual: "cleanup", Status: "PASS"}},
			upstream: []TestStep{reqStep("SOFT-4")},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := MatchSteps("TC-38", "XTC-1", tc.local, tc.upstream)

			if !m.OutOfSync {
				t.Fatal("count mismatch was not reported as out of sync")
			}
			if len(m.Issues) != 1 {
				t.Fatalf("want exactly one issue for a count mismatch, got %v", m.Issues)
			}
			if m.Reason == "" {
				t.Error("Reason is empty")
			}
		})
	}
}

func TestMatchStepsRefusesRequirementDriftAtEqualCount(t *testing.T) {
	// Same number of steps, but a step's requirement changed locally: the
	// count check passes and the per-index requirement check has to catch it.
	upstream := []TestStep{reqStep("SOFT-4"), reqStep("SOFT-5")}
	local := []ExecutionResultStep{pass("SOFT-4", "a"), pass("SOFT-9", "b")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if !m.OutOfSync {
		t.Fatal("requirement drift was not reported as out of sync")
	}
	if len(m.Issues) != 1 {
		t.Errorf("want the drifted step reported once, got %v", m.Issues)
	}
	assertNothingToWrite(t, m)
}

func TestMatchStepsRefusesReorderedSteps(t *testing.T) {
	// Same steps, same count, different order: matching by requirement alone
	// would accept this; matching by position does not.
	upstream := []TestStep{reqStep("SOFT-4"), reqStep("SOFT-5")}
	local := []ExecutionResultStep{pass("SOFT-5", "b"), pass("SOFT-4", "a")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if !m.OutOfSync {
		t.Fatal("reordered steps were not reported as out of sync")
	}
	if len(m.Issues) != 2 {
		t.Errorf("want both misplaced steps reported, got %v", m.Issues)
	}
	assertNothingToWrite(t, m)
}

func TestMatchStepsReportsEveryDrift(t *testing.T) {
	upstream := []TestStep{reqStep("SOFT-1"), reqStep("SOFT-2"), reqStep("SOFT-3")}
	local := []ExecutionResultStep{pass("SOFT-7", "a"), pass("SOFT-2", "b"), pass("SOFT-8", "c")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if len(m.Issues) != 2 {
		t.Errorf("want every drifted step reported, got %v", m.Issues)
	}
}

func TestMatchStepsRefusesActionStepBecomingRequirementStep(t *testing.T) {
	upstream := []TestStep{actionStep("navigate"), reqStep("SOFT-5")}
	local := []ExecutionResultStep{pass("SOFT-5", "a"), pass("SOFT-5", "b")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if !m.OutOfSync {
		t.Fatal("a step that gained a requirement was not reported as out of sync")
	}
}

func TestMatchStepsFailureOnStepWithoutRequirementFailsRun(t *testing.T) {
	upstream := []TestStep{actionStep("navigate"), reqStep("SOFT-5")}
	local := []ExecutionResultStep{
		{Actual: "page did not load", Status: "FAIL"},
		pass("SOFT-5", "ok"),
	}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if m.OutOfSync {
		t.Fatalf("unexpected out of sync: %v", m.Issues)
	}
	if m.RunResult != "f" {
		t.Errorf("RunResult: got %q, want %q — a failed action step was lost", m.RunResult, "f")
	}
	if m.Steps[0].Result != "f" || m.Steps[0].Comment != "page did not load" {
		t.Errorf("failed action step not recorded: %+v", m.Steps[0])
	}
}

func TestMatchStepsNeverPassesABlankRun(t *testing.T) {
	// No step in this test carries a requirement. Every step must still get a
	// result, so the run cannot finish as "passed" with all of them blank.
	upstream := []TestStep{actionStep("navigate"), actionStep("discard")}
	local := []ExecutionResultStep{
		{Actual: "navigate", Status: "PASS"},
		{Actual: "discard", Status: "PASS"},
	}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if m.OutOfSync {
		t.Fatalf("unexpected out of sync: %v", m.Issues)
	}
	for i, step := range m.Steps {
		if step.Result == "" {
			t.Errorf("step %d left blank: %+v", i, step)
		}
	}
}

func TestMatchStepsRefusesEmptyRun(t *testing.T) {
	m := MatchSteps("TC-38", "XTC-1", nil, nil)

	if !m.OutOfSync {
		t.Fatal("a run with no steps was not reported as out of sync")
	}
	assertNothingToWrite(t, m)
}

func TestMatchStepsRefusesUnknownStatus(t *testing.T) {
	for _, status := range []string{"SKIP", "BLOCKED", "pass", ""} {
		t.Run(status, func(t *testing.T) {
			upstream := []TestStep{reqStep("SOFT-5")}
			local := []ExecutionResultStep{{Requirement: "SOFT-5", Actual: "a", Status: status}}

			m := MatchSteps("TC-38", "XTC-1", local, upstream)

			if !m.OutOfSync {
				t.Fatalf("status %q was accepted", status)
			}
			assertNothingToWrite(t, m)
		})
	}
}

// assertNothingToWrite checks that an out-of-sync match hands back nothing a
// caller could mistakenly upload.
func assertNothingToWrite(t *testing.T, m StepMatch) {
	t.Helper()
	if m.Steps != nil {
		t.Errorf("out-of-sync match still carries steps: %+v", m.Steps)
	}
	if m.RunResult != "" {
		t.Errorf("out-of-sync match still carries a run result: %q", m.RunResult)
	}
}

func TestClearResultsDoesNotShareUnmodeledKeys(t *testing.T) {
	const raw = `[{"action":"verify","Ref":"SOFT-5"}]`
	var steps []TestStep
	if err := json.Unmarshal([]byte(raw), &steps); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	cleared := ClearResults(steps)
	cleared[0].extra["Ref"] = json.RawMessage(`"changed"`)

	if string(steps[0].extra["Ref"]) != `"SOFT-5"` {
		t.Errorf("clearing results aliased the input's unmodeled keys: %s", steps[0].extra["Ref"])
	}
}

func TestClearResultsLeavesInputUntouched(t *testing.T) {
	steps := []TestStep{{
		Action: "verify", RequirementLink: "SOFT-4",
		Result: "p", Human: "passed", Render: "ok", Comment: "previous run",
	}}

	cleared := ClearResults(steps)

	if cleared[0].Result != "" || cleared[0].Human != "" || cleared[0].Render != "" || cleared[0].Comment != "" {
		t.Errorf("results not cleared: %+v", cleared[0])
	}
	if steps[0].Result != "p" || steps[0].Comment != "previous run" {
		t.Errorf("input was mutated: %+v", steps[0])
	}
	if cleared[0].Action != "verify" || cleared[0].RequirementLink != "SOFT-4" {
		t.Errorf("step definition lost: %+v", cleared[0])
	}
}

func TestGroupByTestCollapsesAndCountsEntries(t *testing.T) {
	results := []ExecutionResultTest{
		{TestName: "TC-38", Result: "PASS", Steps: []ExecutionResultStep{pass("SOFT-4", "a")}},
		{TestName: "TC-39", Result: "PASS", Steps: []ExecutionResultStep{pass("SOFT-5", "b")}},
		{TestName: "TC-38", Result: "PASS", Steps: []ExecutionResultStep{pass("SOFT-6", "c")}},
	}

	grouped := groupByTest(results)

	if len(grouped) != 2 {
		t.Fatalf("want 2 groups, got %d", len(grouped))
	}
	// Order follows the results file so uploads are deterministic.
	if grouped[0].testName != "TC-38" || grouped[1].testName != "TC-39" {
		t.Errorf("order not preserved: %q, %q", grouped[0].testName, grouped[1].testName)
	}
	if grouped[0].entries != 2 {
		t.Errorf("TC-38 entries: got %d, want 2", grouped[0].entries)
	}
	if len(grouped[0].steps) != 2 {
		t.Errorf("TC-38 steps: got %d, want 2", len(grouped[0].steps))
	}
}
