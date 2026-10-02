package execution

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/VladGavrila/matrixreq-cli/internal/api"
	"github.com/VladGavrila/matrixreq-cli/internal/fieldmap"
	"github.com/VladGavrila/matrixreq-cli/internal/service"
)

// BuildTCToXTCMapping builds a mapping from TC refs to XTC items.
// XTC titles are expected to contain the TC ref in parentheses, e.g., "Title (TC-1377)".
func BuildTCToXTCMapping(folderItems []api.TrimFolder) map[string]api.TrimFolder {
	mapping := make(map[string]api.TrimFolder)
	for _, item := range folderItems {
		if len(item.ItemList) > 0 {
			// Recurse into subfolders
			sub := BuildTCToXTCMapping(item.ItemList)
			for k, v := range sub {
				mapping[k] = v
			}
		} else if item.IsFolder == 0 {
			// Extract TC ref from title: "Title (TC-1377)" → "TC-1377"
			title := item.Title
			start := strings.LastIndex(title, "(")
			end := strings.LastIndex(title, ")")
			if start != -1 && end != -1 && end > start {
				tcRef := title[start+1 : end]
				mapping[tcRef] = item
			}
		}
	}
	return mapping
}

// UploadResults uploads execution results to XTCs in a folder.
func UploadResults(svc *service.MatrixService, project string, folderRef string, results *ExecutionResults, fm *fieldmap.FieldMap) (*UploadResult, error) {
	// Get folder items to build TC→XTC mapping
	folderItems, err := resolveFolderItems(svc, project, folderRef)
	if err != nil {
		return nil, fmt.Errorf("getting folder: %w", err)
	}

	tcToXTC := BuildTCToXTCMapping(folderItems)
	if len(tcToXTC) == 0 {
		return nil, fmt.Errorf("no XTCs found in folder %s", folderRef)
	}

	uploadResult := &UploadResult{
		Successes: make(map[string]bool),
		Skipped:   make(map[string]string),
	}

	for _, local := range groupByTest(results.Results) {
		tcName := local.testName
		xtcFolder, ok := tcToXTC[tcName]
		if !ok {
			uploadResult.Issues = append(uploadResult.Issues, fmt.Sprintf("test %q not found in folder", tcName))
			continue
		}
		xtcRef := xtcFolder.ItemRef

		item, err := svc.Items.Get(project, xtcRef, false)
		if err != nil {
			uploadResult.Issues = append(uploadResult.Issues, fmt.Sprintf("failed to get %s: %v", xtcRef, err))
			continue
		}

		// Two results for one XTC cannot both be the execution it records, and
		// their concatenated steps would misreport as a count mismatch.
		if local.entries > 1 {
			uploadResult.skip(xtcRef, fmt.Sprintf("%d results claim this XTC", local.entries), fmt.Sprintf(
				"%s: OUT OF SYNC — %d results in this run execute %s. No results were "+
					"uploaded; review %s manually.",
				tcName, local.entries, xtcRef, xtcRef))
			continue
		}

		match := MatchSteps(tcName, xtcRef, local.steps, parseStepsFromItem(item, fm))
		uploadResult.Issues = append(uploadResult.Issues, match.Issues...)
		if match.OutOfSync {
			uploadResult.Successes[xtcRef] = false
			uploadResult.Skipped[xtcRef] = match.Reason
			continue
		}

		err = updateXTCResults(svc, project, fm, xtcRef, match.Steps, item, match.RunResult, results)
		uploadResult.Successes[xtcRef] = err == nil
		if err != nil {
			uploadResult.Issues = append(uploadResult.Issues,
				fmt.Sprintf("failed to update %s: %v", xtcRef, err))
		}
	}

	return uploadResult, nil
}

// localTest is every step result a run recorded for one test name.
type localTest struct {
	testName string
	steps    []ExecutionResultStep
	// entries counts the results carrying this test name; more than one is a
	// conflict rather than something to merge.
	entries int
}

// groupByTest collapses results onto the test they execute, preserving the
// order they appear in the results file so uploads are deterministic.
func groupByTest(results []ExecutionResultTest) []*localTest {
	var order []*localTest
	byName := make(map[string]*localTest)
	for _, r := range results {
		lt, seen := byName[r.TestName]
		if !seen {
			lt = &localTest{testName: r.TestName}
			byName[r.TestName] = lt
			order = append(order, lt)
		}
		lt.entries++
		lt.steps = append(lt.steps, r.Steps...)
	}
	return order
}

// skip records that an XTC was deliberately left untouched.
func (u *UploadResult) skip(xtcRef, reason, issue string) {
	u.Successes[xtcRef] = false
	u.Skipped[xtcRef] = reason
	u.Issues = append(u.Issues, issue)
}

// parseStepsFromItem extracts test steps from an item's field values.
func parseStepsFromItem(item *api.TrimItem, fm *fieldmap.FieldMap) []TestStep {
	if item.FieldValList == nil {
		return nil
	}

	// Resolve the Steps field ID for XTC category
	stepsFieldID, err := fm.Resolve("XTC", "Test Case Steps")
	if err != nil {
		// Try alternative field name
		stepsFieldID, err = fm.Resolve("XTC", "Steps")
		if err != nil {
			return nil
		}
	}

	for _, fv := range item.FieldValList.FieldVal {
		if fv.ID == stepsFieldID && fv.Value != "" {
			var steps []TestStep
			if err := json.Unmarshal([]byte(fv.Value), &steps); err != nil {
				return nil
			}
			return steps
		}
	}
	return nil
}

// updateXTCResults updates an XTC with execution results via the API.
func updateXTCResults(svc *service.MatrixService, project string, fm *fieldmap.FieldMap, xtcRef string, steps []TestStep, item *api.TrimItem, runResult string, results *ExecutionResults) error {
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("marshaling steps: %w", err)
	}

	// Convert date format from YYYY-MM-DD to YYYY/MM/DD for Matrix
	testDate := strings.ReplaceAll(results.ExecutionDate, "-", "/")

	// Resolve XTC field IDs
	testerID, err := fm.Resolve("XTC", "Tester")
	if err != nil {
		return fmt.Errorf("resolving Tester field: %w", err)
	}
	dateID, err := fm.Resolve("XTC", "Test Date")
	if err != nil {
		return fmt.Errorf("resolving Test Date field: %w", err)
	}
	runResultID, err := fm.Resolve("XTC", "Test Run Result")
	if err != nil {
		return fmt.Errorf("resolving Test Run Result field: %w", err)
	}
	stepsID, err := fm.Resolve("XTC", "Test Case Steps")
	if err != nil {
		// Try alternative
		stepsID, err = fm.Resolve("XTC", "Steps")
		if err != nil {
			return fmt.Errorf("resolving Steps field: %w", err)
		}
	}

	fields := []api.FieldValSetType{
		{ID: testerID, Value: results.Tester},
		{ID: dateID, Value: testDate},
		{ID: runResultID, Value: runResult},
		{ID: stepsID, Value: string(stepsJSON)},
	}

	// Optionally set version field
	if results.SUTVersion != "" {
		versionID, err := fm.Resolve("XTC", "Version")
		if err == nil {
			fields = append(fields, api.FieldValSetType{ID: versionID, Value: results.SUTVersion})
		}
	}

	updateReq := &api.UpdateItemRequest{
		Title:  item.Title,
		Reason: "synced by mxreq",
		Fields: fields,
	}

	_, err = svc.Items.Update(project, xtcRef, updateReq)
	return err
}
