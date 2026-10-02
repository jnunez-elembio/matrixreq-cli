package execution

import "testing"

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
	// Requirement-less steps carry no result, so they stay blank in Matrix.
	if m.Steps[0].Result != "" || m.Steps[3].Result != "" {
		t.Errorf("action steps gained a result: %+v, %+v", m.Steps[0], m.Steps[3])
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
	// count check passes and the requirement check has to catch it.
	upstream := []TestStep{reqStep("SOFT-4"), reqStep("SOFT-5")}
	local := []ExecutionResultStep{pass("SOFT-4", "a"), pass("SOFT-9", "b")}

	m := MatchSteps("TC-38", "XTC-1", local, upstream)

	if !m.OutOfSync {
		t.Fatal("requirement drift was not reported as out of sync")
	}
	// Both directions are reported: SOFT-9 has no upstream step, and the
	// upstream SOFT-5 step went unexecuted.
	if len(m.Issues) != 2 {
		t.Errorf("want both drift directions reported, got %v", m.Issues)
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
