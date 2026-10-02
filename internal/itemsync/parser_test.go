package itemsync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindTestFunctions(t *testing.T) {
	tests := []struct {
		name    string
		content string
		keyword string
		want    int
	}{
		{
			name:    "python new functions",
			content: "def test_NEW_example():\n    pass\ndef test_NEW_other():\n    pass\n",
			keyword: "def test_NEW_",
			want:    2,
		},
		{
			name:    "python update functions",
			content: "def test_TC_6_example():\n    pass\ndef test_TC_123_other():\n    pass\n",
			keyword: "def test_TC_",
			want:    2,
		},
		{
			name:    "go new functions",
			content: "func Test_NEW_example(t *testing.T) {\n}\nfunc Test_NEW_other(t *testing.T) {\n}\n",
			keyword: "func Test_NEW_",
			want:    2,
		},
		{
			name:    "go update functions",
			content: "func Test_TC_6_example(t *testing.T) {\n}\n",
			keyword: "func Test_TC_",
			want:    1,
		},
		{
			name:    "typescript new functions",
			content: "test('TC-NEW-example', async () => {\n});\ntest('TC-NEW-other', async () => {\n});\n",
			keyword: "test('TC-NEW-",
			want:    2,
		},
		{
			name:    "typescript update functions",
			content: "test('TC-6-example', async () => {\n});\ntest('TC-123-other', async () => {\n});\n",
			keyword: "test('TC-",
			want:    2,
		},
		{
			name:    "no matches",
			content: "def regular_function():\n    pass\n",
			keyword: "def test_NEW_",
			want:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := FindTestFunctions(tt.content, tt.keyword)
			if len(matches) != tt.want {
				t.Errorf("got %d matches, want %d", len(matches), tt.want)
			}
		})
	}
}

func TestKeywordForFileType(t *testing.T) {
	tests := []struct {
		ext  string
		mode string
		want string
	}{
		{".py", "new", "def test_NEW_"},
		{".py", "update", "def test_TC_"},
		{".go", "new", "func Test_NEW_"},
		{".go", "update", "func Test_TC_"},
		{".ts", "new", "test('TC-NEW-"},
		{".ts", "update", "test('TC-"},
		{".rb", "new", ""},     // unsupported
		{".py", "invalid", ""}, // invalid mode
	}

	for _, tt := range tests {
		got := KeywordForFileType(tt.ext, tt.mode)
		if got != tt.want {
			t.Errorf("KeywordForFileType(%q, %q) = %q, want %q", tt.ext, tt.mode, got, tt.want)
		}
	}
}

func TestExtractTCNumber(t *testing.T) {
	tests := []struct {
		name     string
		funcLine string
		want     string
	}{
		{
			name:     "python TC_6",
			funcLine: "def test_TC_6_example():",
			want:     "TC-6",
		},
		{
			name:     "python TC_123",
			funcLine: "def test_TC_123_some_test():",
			want:     "TC-123",
		},
		{
			name:     "go TC_6",
			funcLine: "func Test_TC_6_example(t *testing.T) {",
			want:     "TC-6",
		},
		{
			name:     "go TC_1377",
			funcLine: "func Test_TC_1377_Name(t *testing.T) {",
			want:     "TC-1377",
		},
		{
			name:     "typescript TC-6",
			funcLine: "test('TC-6-example', async () => {",
			want:     "TC-6",
		},
		{
			name:     "typescript TC-123",
			funcLine: "test('TC-123-some-test', async () => {",
			want:     "TC-123",
		},
		{
			name:     "python NEW_ function",
			funcLine: "def test_NEW_example():",
			want:     "",
		},
		{
			name:     "go NEW_ function",
			funcLine: "func Test_NEW_example(t *testing.T) {",
			want:     "",
		},
		{
			name:     "typescript TC-NEW-",
			funcLine: "test('TC-NEW-example', async () => {",
			want:     "",
		},
		{
			name:     "no match",
			funcLine: "def regular_function():",
			want:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractTCNumber(tt.funcLine)
			if got != tt.want {
				t.Errorf("ExtractTCNumber(%q) = %q, want %q", tt.funcLine, got, tt.want)
			}
		})
	}
}

func TestParsePythonDocstring(t *testing.T) {
	content := `def test_NEW_docstring_generation():
    """
    ---
    title: "Docstring Generation Example"
    folder: F-TC-532
    description: |
      Demonstrates programmatic docstring generation.
    assumptions: |
      <ul>
       <li>Test environment is configured.</li>
       <li>Helpers are available.</li>
      </ul>
    steps:
      - action: "Initialize test data structure"
      - action: "<b>Manual:</b> Manually do something"
      - action: "Verify the value equals expected"
        expected: "Value is 42"
        RequirementLink: "SOFT-123"
      - action: "Verify status is active"
        expected: "Status is active"
        RequirementLink: "SOFT-456"
    labels:
      - Automated
    up_links: "SOFT-123, SOFT-456"
    ---
    """
    pass`

	loc := ParseDocstring(content, "def test_NEW_docstring_generation():")
	if loc == nil {
		t.Fatal("expected parsed location, got nil")
	}

	item := loc.Item
	if item.Title != "Docstring Generation Example" {
		t.Errorf("title: got %q, want %q", item.Title, "Docstring Generation Example")
	}
	if item.Folder != "F-TC-532" {
		t.Errorf("folder: got %q, want %q", item.Folder, "F-TC-532")
	}
	if len(item.Labels) != 1 || item.Labels[0] != "Automated" {
		t.Errorf("labels: got %v, want [Automated]", item.Labels)
	}
	if len(item.UpLinks) != 2 {
		t.Errorf("uplinks: got %d, want 2", len(item.UpLinks))
	}

	// Check legacy flat fields are converted
	if _, ok := item.Fields["Description"]; !ok {
		t.Error("expected Description field from legacy format")
	}
	if _, ok := item.Fields["Assumptions"]; !ok {
		t.Error("expected Assumptions field from legacy format")
	}
	if _, ok := item.Fields["Steps"]; !ok {
		t.Error("expected Steps field from legacy format (JSON)")
	}

	// Steps should be valid JSON
	stepsJSON := item.Fields["Steps"]
	if !strings.HasPrefix(stepsJSON, "[") {
		t.Error("Steps field should be a JSON array")
	}
}

func TestParseGoDocstring(t *testing.T) {
	content := `func Test_NEW_docstring_generation(t *testing.T) {
	/*
	---
	title: "Docstring Generation Example"
	folder: F-TC-532
	description: |
	  Demonstrates programmatic docstring generation.
	assumptions: |
	  <ul>
	   <li>Test environment is configured.</li>
	   <li>Helpers are available.</li>
	  </ul>
	steps:
	  - action: "Initialize test data structure"
	  - action: "Verify the value equals expected"
	    expected: "Value is 42"
	    RequirementLink: "SOFT-123"
	labels:
	  - Automated
	up_links: "SOFT-123"
	---
	*/
	// test code
}`

	loc := ParseDocstring(content, "func Test_NEW_docstring_generation(t *testing.T) {")
	if loc == nil {
		t.Fatal("expected parsed location, got nil")
	}

	item := loc.Item
	if item.Title != "Docstring Generation Example" {
		t.Errorf("title: got %q, want %q", item.Title, "Docstring Generation Example")
	}
	if item.Folder != "F-TC-532" {
		t.Errorf("folder: got %q, want %q", item.Folder, "F-TC-532")
	}
	if len(item.Labels) != 1 || item.Labels[0] != "Automated" {
		t.Errorf("labels: got %v, want [Automated]", item.Labels)
	}
}

func TestParseTypeScriptDocstring(t *testing.T) {
	content := `test('TC-NEW-docstring-generation', async () => {
  /*
  ---
  title: "Docstring Generation Example"
  folder: F-TC-532
  description: |
    Demonstrates programmatic docstring generation.
  steps:
    - action: "Initialize test data structure"
    - action: "Verify the value equals expected"
      expected: "Value is 42"
      RequirementLink: "SOFT-123"
  labels:
    - Automated
  up_links: "SOFT-123"
  ---
  */
  // test code
});`

	loc := ParseDocstring(content, "test('TC-NEW-docstring-generation', async () => {")
	if loc == nil {
		t.Fatal("expected parsed location, got nil")
	}

	item := loc.Item
	if item.Title != "Docstring Generation Example" {
		t.Errorf("title: got %q, want %q", item.Title, "Docstring Generation Example")
	}
	if item.Folder != "F-TC-532" {
		t.Errorf("folder: got %q, want %q", item.Folder, "F-TC-532")
	}
}

func TestParseDocstringNoDocstring(t *testing.T) {
	content := `def test_NEW_example():
    pass`

	loc := ParseDocstring(content, "def test_NEW_example():")
	if loc != nil {
		t.Error("expected nil for function without docstring")
	}
}

func TestParseDocstringExistingTC(t *testing.T) {
	content := `def test_TC_6_example():
    """
    ---
    title: "Existing Test"
    folder: F-TC-100
    description: |
      Some description
    steps:
      - action: "Do something"
        expected: "Result"
        RequirementLink: "SOFT-3315"
    ---
    """
    pass`

	loc := ParseDocstring(content, "def test_TC_6_example():")
	if loc == nil {
		t.Fatal("expected parsed location, got nil")
	}
	if loc.Item.ItemRef != "TC-6" {
		t.Errorf("item_ref: got %q, want %q", loc.Item.ItemRef, "TC-6")
	}
}

func TestUpdateFunctionNamePython(t *testing.T) {
	content := "def test_NEW_example():\n    pass\n"
	newContent, newLine := UpdateFunctionName(content, 0, "def test_NEW_example():", "TC-42", ".py")

	if !strings.Contains(newContent, "def test_TC_42_example():") {
		t.Errorf("expected renamed function, got:\n%s", newContent)
	}
	if newLine != "def test_TC_42_example():" {
		t.Errorf("new line: got %q, want %q", newLine, "def test_TC_42_example():")
	}
}

func TestUpdateFunctionNameGo(t *testing.T) {
	content := "func Test_NEW_Example(t *testing.T) {\n}\n"
	newContent, newLine := UpdateFunctionName(content, 0, "func Test_NEW_Example(t *testing.T) {", "TC-99", ".go")

	if !strings.Contains(newContent, "func Test_TC_99_Example(t *testing.T) {") {
		t.Errorf("expected renamed function, got:\n%s", newContent)
	}
	if !strings.Contains(newLine, "Test_TC_99_Example") {
		t.Errorf("new line should contain Test_TC_99_Example: %q", newLine)
	}
}

func TestUpdateFunctionNameTypeScript(t *testing.T) {
	content := "test('TC-NEW-example', async () => {\n});\n"
	newContent, newLine := UpdateFunctionName(content, 0, "test('TC-NEW-example', async () => {", "TC-55", ".ts")

	if !strings.Contains(newContent, "test('TC-55-example'") {
		t.Errorf("expected renamed function, got:\n%s", newContent)
	}
	_ = newLine
}

func TestWriteItemRefToYAML(t *testing.T) {
	content := `items:
  - title: "Config Precedence"
    folder: F-SOFT-1
    fields:
      Description: "some text"
    labels:
      - Draft

  - title: "XDG Config"
    folder: F-SOFT-1
    fields:
      Description: "another"
`
	// Insert into first entry
	got := WriteItemRefToYAML(content, "Config Precedence", "SOFT-100")
	if !strings.Contains(got, "  item_ref: SOFT-100\n    folder: F-SOFT-1") {
		t.Errorf("item_ref not inserted correctly:\n%s", got)
	}
	// Second entry unchanged
	if strings.Contains(got, "item_ref: SOFT-100\n    folder: F-SOFT-1\n    fields:\n      Description: \"another\"") {
		t.Errorf("wrong entry modified")
	}

	// Idempotent: calling again should not duplicate
	got2 := WriteItemRefToYAML(got, "Config Precedence", "SOFT-100")
	if got2 != got {
		t.Errorf("WriteItemRefToYAML is not idempotent")
	}

	// Title not found returns content unchanged
	got3 := WriteItemRefToYAML(content, "Nonexistent Title", "SOFT-999")
	if got3 != content {
		t.Errorf("expected content unchanged for missing title")
	}
}

func TestParseYAMLDefinitionsFromString(t *testing.T) {
	yaml := `items:
  - title: "System shall validate input"
    folder: F-REQ-1
    fields:
      Description: "<p>The system shall validate all user input...</p>"
    labels:
      - Draft
    up_links: "SPEC-100"
  - title: "Update existing requirement"
    item_ref: REQ-42
    folder: F-REQ-1
    fields:
      Description: "<p>Updated description...</p>"
    up_links: "SPEC-100, SPEC-101"
`

	items, err := ParseYAMLDefinitionsFromString(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	// First item: new (no item_ref)
	if items[0].Title != "System shall validate input" {
		t.Errorf("item 0 title: got %q", items[0].Title)
	}
	if items[0].ItemRef != "" {
		t.Errorf("item 0 should have empty item_ref, got %q", items[0].ItemRef)
	}
	if items[0].Folder != "F-REQ-1" {
		t.Errorf("item 0 folder: got %q", items[0].Folder)
	}
	if items[0].Fields["Description"] != "<p>The system shall validate all user input...</p>" {
		t.Errorf("item 0 Description field: got %q", items[0].Fields["Description"])
	}
	if len(items[0].Labels) != 1 || items[0].Labels[0] != "Draft" {
		t.Errorf("item 0 labels: got %v", items[0].Labels)
	}
	if len(items[0].UpLinks) != 1 || items[0].UpLinks[0] != "SPEC-100" {
		t.Errorf("item 0 uplinks: got %v", items[0].UpLinks)
	}

	// Second item: existing (has item_ref)
	if items[1].ItemRef != "REQ-42" {
		t.Errorf("item 1 item_ref: got %q", items[1].ItemRef)
	}
	if len(items[1].UpLinks) != 2 {
		t.Errorf("item 1 should have 2 uplinks, got %d", len(items[1].UpLinks))
	}
}

func TestParseYAMLDefinitionsLegacyFlatFields(t *testing.T) {
	// mxreq sync YAML uses top-level description/assumptions/steps
	// (same shape as docstring YAML), not nested under fields:.
	yaml := `items:
  - title: "Load step page smoke"
    folder: F-TC-14
    description: |
      Verifies the load step page renders.
    assumptions: |
      <ul>
        <li>Instrument is online.</li>
      </ul>
    steps:
      - action: "Open load step page"
        expected: "Page is visible"
        RequirementLink: "REQ-1"
    labels:
      - Automated
    up_links: "REQ-1"
`

	items, err := ParseYAMLDefinitionsFromString(yaml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}

	item := items[0]
	if got := strings.TrimSpace(item.Fields["Description"]); got != "Verifies the load step page renders." {
		t.Errorf("Description: got %q", item.Fields["Description"])
	}
	if !strings.Contains(item.Fields["Assumptions"], "<li>Instrument is online.</li>") {
		t.Errorf("Assumptions: got %q", item.Fields["Assumptions"])
	}
	if !strings.HasPrefix(item.Fields["Steps"], "[") {
		t.Errorf("Steps should be JSON array, got %q", item.Fields["Steps"])
	}
	if !strings.Contains(item.Fields["Steps"], `"RequirementLink":"REQ-1"`) {
		t.Errorf("Steps JSON missing RequirementLink: %q", item.Fields["Steps"])
	}
	if !strings.Contains(item.Fields["Steps"], `"Ref":"REQ-1"`) {
		t.Errorf("Steps JSON missing Ref (xtc_config link column): %q", item.Fields["Steps"])
	}

	// Explicit fields: map wins over legacy top-level keys
	yamlOverride := `items:
  - title: "Override"
    folder: F-TC-14
    description: "ignored legacy"
    assumptions: "ignored legacy"
    fields:
      Description: "<p>from fields</p>"
      Assumptions: "<ul><li>from fields</li></ul>"
`
	items, err = ParseYAMLDefinitionsFromString(yamlOverride)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if items[0].Fields["Description"] != "<p>from fields</p>" {
		t.Errorf("fields map should win for Description, got %q", items[0].Fields["Description"])
	}
	if items[0].Fields["Assumptions"] != "<ul><li>from fields</li></ul>" {
		t.Errorf("fields map should win for Assumptions, got %q", items[0].Fields["Assumptions"])
	}
}

func TestParseYAMLDefinitionsFile(t *testing.T) {
	content := `items:
  - title: "Test item"
    folder: F-TC-1
    fields:
      Description: "<p>Test</p>"
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	items, err := ParseYAMLDefinitions(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Title != "Test item" {
		t.Errorf("title: got %q", items[0].Title)
	}
}

func TestCategoryFromFolderRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"F-TC-186", "TC"},
		{"F-REQ-1", "REQ"},
		{"F-XTC-42", "XTC"},
		{"F-SPEC-10", "SPEC"},
		{"F-RISK-5", "RISK"},
		{"TC-123", ""}, // Not a folder ref
		{"", ""},       // Empty
	}

	for _, tt := range tests {
		got := CategoryFromFolderRef(tt.ref)
		if got != tt.want {
			t.Errorf("CategoryFromFolderRef(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestCategoryFromItemRef(t *testing.T) {
	tests := []struct {
		ref  string
		want string
	}{
		{"TC-1377", "TC"},
		{"REQ-42", "REQ"},
		{"XTC-100", "XTC"},
		{"SPEC-45", "SPEC"},
		{"SOFT-123", "SOFT"},
		{"", ""},         // Empty
		{"NOHYPHEN", ""}, // No hyphen
	}

	for _, tt := range tests {
		got := CategoryFromItemRef(tt.ref)
		if got != tt.want {
			t.Errorf("CategoryFromItemRef(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}

func TestParseFileYAML(t *testing.T) {
	content := `items:
  - title: "New item"
    folder: F-REQ-1
    fields:
      Description: "<p>Test</p>"
  - title: "Existing"
    item_ref: REQ-42
    folder: F-REQ-1
    fields:
      Description: "<p>Updated</p>"
`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.yaml")
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	entries, err := ParseFile(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Action != ActionCreate {
		t.Error("first entry should be ActionCreate")
	}
	if entries[1].Action != ActionUpdate {
		t.Error("second entry should be ActionUpdate")
	}
}

func TestParseFileUnsupportedExtension(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.rb")
	if err := os.WriteFile(filePath, []byte("# ruby"), 0o644); err != nil {
		t.Fatalf("writing test file: %v", err)
	}

	_, err := ParseFile(filePath)
	if err == nil {
		t.Error("expected error for unsupported file type")
	}
}

func TestStepsToJSON(t *testing.T) {
	steps := []yamlStep{
		{Action: "Do something", Expected: "Result", RequirementLink: "SOFT-123"},
		{Action: "Do another thing"},
	}

	result, err := stepsToJSON(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should be a JSON array
	if !strings.HasPrefix(result, "[") || !strings.HasSuffix(result, "]") {
		t.Errorf("expected JSON array, got: %s", result)
	}

	// Step without expected should default to N/A
	if !strings.Contains(result, `"expected":"N/A"`) {
		t.Error("expected N/A default for missing expected value")
	}

	// Step with expected should keep it
	if !strings.Contains(result, `"expected":"Result"`) {
		t.Error("expected to find 'Result' in JSON output")
	}
}

func TestDedentBlock(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "uniform indent",
			input: "    line1\n    line2\n    line3",
			want:  "line1\nline2\nline3",
		},
		{
			name:  "mixed indent",
			input: "    line1\n      line2\n    line3",
			want:  "line1\n  line2\nline3",
		},
		{
			name:  "no indent",
			input: "line1\nline2",
			want:  "line1\nline2",
		},
		{
			name:  "empty lines preserved",
			input: "    line1\n\n    line3",
			want:  "line1\n\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dedentBlock(tt.input)
			if got != tt.want {
				t.Errorf("dedentBlock:\ngot:  %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestExtractYAML(t *testing.T) {
	content := `Some text before
---
title: "Test"
folder: F-TC-1
---
Some text after`

	result := extractYAML(content)
	if !strings.Contains(result, "title:") {
		t.Errorf("expected YAML content, got: %q", result)
	}
}

func TestExtractYAMLNoMarkers(t *testing.T) {
	content := "no yaml markers here"
	result := extractYAML(content)
	if result != "" {
		t.Errorf("expected empty string for no YAML markers, got: %q", result)
	}
}

func TestGenericFieldsFormat(t *testing.T) {
	yaml := `---
title: "Generic Item"
folder: F-REQ-1
fields:
  Description: "<p>A requirement</p>"
  Rationale: "Because reasons"
labels:
  - Draft
up_links: "SPEC-100"
---`

	yamlContent := extractYAML(yaml)
	item, err := parseYAMLDocstring(yamlContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if item.Title != "Generic Item" {
		t.Errorf("title: got %q", item.Title)
	}
	if item.Fields["Description"] != "<p>A requirement</p>" {
		t.Errorf("Description: got %q", item.Fields["Description"])
	}
	if item.Fields["Rationale"] != "Because reasons" {
		t.Errorf("Rationale: got %q", item.Fields["Rationale"])
	}
}

func TestLegacyFlatFormatConversion(t *testing.T) {
	yaml := `---
title: "Test Case"
folder: F-TC-100
description: |
  <p>Description text</p>
assumptions: |
  <p>Assumptions</p>
steps:
  - action: "Do something"
    expected: "Result"
    RequirementLink: "SOFT-1"
labels:
  - Automated
---`

	yamlContent := extractYAML(yaml)
	item, err := parseYAMLDocstring(yamlContent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Legacy fields should be converted to Fields map
	if _, ok := item.Fields["Description"]; !ok {
		t.Error("expected Description in Fields map from legacy format")
	}
	if _, ok := item.Fields["Assumptions"]; !ok {
		t.Error("expected Assumptions in Fields map from legacy format")
	}
	if _, ok := item.Fields["Steps"]; !ok {
		t.Error("expected Steps (as JSON) in Fields map from legacy format")
	}

	// Steps should be JSON with both link keys (UI uses Ref; tooling uses RequirementLink)
	stepsJSON := item.Fields["Steps"]
	if !strings.Contains(stepsJSON, `"RequirementLink":"SOFT-1"`) {
		t.Errorf("Steps JSON should contain RequirementLink, got: %s", stepsJSON)
	}
	if !strings.Contains(stepsJSON, `"Ref":"SOFT-1"`) {
		t.Errorf("Steps JSON should contain Ref, got: %s", stepsJSON)
	}
}

func TestStepsToJSONWritesRefAndRequirementLink(t *testing.T) {
	got, err := stepsToJSON([]yamlStep{
		{Action: "Open page", Expected: ""},
		{Action: "Verify", Expected: "ok", RequirementLink: "SOFT-1"},
		{Action: "Verify via Ref", Expected: "ok", Ref: "SOFT-2"},
	})
	if err != nil {
		t.Fatalf("stepsToJSON: %v", err)
	}

	var steps []map[string]string
	if err := json.Unmarshal([]byte(got), &steps); err != nil {
		t.Fatalf("unmarshal steps JSON: %v\nraw: %s", err, got)
	}
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}

	if _, ok := steps[0]["Ref"]; ok {
		t.Errorf("step without link should omit Ref, got: %#v", steps[0])
	}
	if _, ok := steps[0]["RequirementLink"]; ok {
		t.Errorf("step without link should omit RequirementLink, got: %#v", steps[0])
	}
	if steps[0]["expected"] != "N/A" {
		t.Errorf("empty expected should default to N/A, got %q", steps[0]["expected"])
	}

	if steps[1]["Ref"] != "SOFT-1" || steps[1]["RequirementLink"] != "SOFT-1" {
		t.Errorf("RequirementLink input should emit both keys, got: %#v", steps[1])
	}
	if steps[2]["Ref"] != "SOFT-2" || steps[2]["RequirementLink"] != "SOFT-2" {
		t.Errorf("Ref input should emit both keys, got: %#v", steps[2])
	}
}
