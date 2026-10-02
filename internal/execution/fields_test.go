package execution

import (
	"testing"

	"github.com/VladGavrila/matrixreq-cli/internal/api"
)

func itemWithFields(values map[int]string) *api.TrimItem {
	list := &api.FieldValListType{}
	for id, value := range values {
		list.FieldVal = append(list.FieldVal, api.FieldValType{ID: id, Value: value})
	}
	return &api.TrimItem{FieldValList: list}
}

func asMap(fields []api.FieldValSetType) map[int]string {
	out := make(map[int]string, len(fields))
	for _, f := range fields {
		out[f.ID] = f.Value
	}
	return out
}

// An update is a full replacement, so every field the item holds has to be
// sent back or Matrix clears it.
func TestMergeFieldValuesPreservesUnrelatedFields(t *testing.T) {
	const (
		assumptions   = 10808
		testSetup     = 10809
		testMaterials = 10810
		tester        = 10811
		steps         = 10815
	)

	item := itemWithFields(map[int]string{
		assumptions:   "<ul><li>Instrument is idle.</li></ul>",
		testSetup:     "<ul><li>Powered on.</li></ul>",
		testMaterials: "<ul><li>2x75 kit.</li></ul>",
		steps:         `[{"action":"old"}]`,
	})

	got := asMap(mergeFieldValues(item, map[int]string{
		tester: "automation",
		steps:  `[{"action":"new"}]`,
	}))

	if got[assumptions] != "<ul><li>Instrument is idle.</li></ul>" {
		t.Errorf("Assumptions lost: %q", got[assumptions])
	}
	if got[testSetup] != "<ul><li>Powered on.</li></ul>" {
		t.Errorf("Test Setup lost: %q", got[testSetup])
	}
	if got[testMaterials] != "<ul><li>2x75 kit.</li></ul>" {
		t.Errorf("Test Materials lost: %q", got[testMaterials])
	}
	if got[steps] != `[{"action":"new"}]` {
		t.Errorf("override not applied: %q", got[steps])
	}
	if got[tester] != "automation" {
		t.Errorf("new field not added: %q", got[tester])
	}
	if len(got) != 5 {
		t.Errorf("want 5 fields, got %v", got)
	}
}

func TestMergeFieldValuesSkipsEmptyAndSortsByID(t *testing.T) {
	item := itemWithFields(map[int]string{10810: "", 10808: "keep", 10815: "steps"})

	fields := mergeFieldValues(item, map[int]string{10811: "automation"})

	for _, f := range fields {
		if f.ID == 10810 {
			t.Errorf("an empty field was sent: %+v", f)
		}
	}
	for i := 1; i < len(fields); i++ {
		if fields[i-1].ID > fields[i].ID {
			t.Fatalf("fields not sorted by id: %v", fields)
		}
	}
}

func TestMergeFieldValuesHandlesItemWithoutFields(t *testing.T) {
	for name, item := range map[string]*api.TrimItem{
		"nil item":   nil,
		"no list":    {},
		"empty list": {FieldValList: &api.FieldValListType{}},
	} {
		t.Run(name, func(t *testing.T) {
			fields := mergeFieldValues(item, map[int]string{10811: "automation"})
			if len(fields) != 1 || fields[0].Value != "automation" {
				t.Errorf("got %v", fields)
			}
		})
	}
}
