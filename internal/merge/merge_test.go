package merge

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func decode(t *testing.T, s string) any {
	t.Helper()
	v, err := Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestDiff checks the ops of what theirs adds, removes and replaces, in
// path order, with arrays as one value, keys escaped, and skipped paths
// left out.
func TestDiff(t *testing.T) {
	base := decode(t, `{"models": {"a/1": {"price": 1, "tags": ["x"], "name": "A"}, "gone": {}}, "generated": "t1"}`)
	theirs := decode(t, `{"models": {"a/1": {"price": 2, "tags": ["x", "y"], "name": "A"}, "new": {"name": "N"}}, "generated": "t2"}`)
	ops := Diff(base, theirs, func(p []string) bool { return slices.Equal(p, []string{"generated"}) })
	var got []string
	for _, op := range ops {
		got = append(got, op.Op+" "+op.Path)
	}
	want := []string{"replace /models/a~11/price", "replace /models/a~11/tags", "remove /models/gone", "add /models/new"}
	if !slices.Equal(got, want) {
		t.Errorf("Diff = %q, want %q", got, want)
	}
	if ops[0].Old != json.Number("1") || ops[0].Value != json.Number("2") {
		t.Errorf("price op = %+v, want 1 to 2", ops[0])
	}
}

// TestApply checks the three-way rules: ours takes what it did not change,
// keeps what it changed, with a conflict, keeps what it added, and loses
// what theirs removes, its own additions included.
func TestApply(t *testing.T) {
	base := decode(t, `{"m": {"a": {"price": 1, "name": "A"}, "b": {"price": 1}, "c": {"x": 1}}}`)
	theirs := decode(t, `{"m": {"a": {"price": 2, "name": "A2"}, "b": {"price": 3}, "d": {"x": 4}}}`)
	ours := decode(t, `{"m": {"a": {"price": 1, "name": "Ours", "default_effort": "high"}, "b": {"price": 3}, "c": {"x": 1, "ours": true}}}`)
	got, conflicts := Apply(ours, Diff(base, theirs, nil))
	want := decode(t, `{"m": {"a": {"price": 2, "name": "Ours", "default_effort": "high"}, "b": {"price": 3}, "d": {"x": 4}}}`)
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		t.Errorf("Apply = %s", g)
	}
	if len(conflicts) != 1 || conflicts[0].Path != "/m/a/name" || conflicts[0].Ours != "Ours" || conflicts[0].Why != "ours changed it too" {
		t.Errorf("conflicts = %+v, want ours' name of a kept", conflicts)
	}
}

// TestApplyConflicts checks an add where ours holds another value, or
// lacks the object to add to, and a replace of a value ours removed.
func TestApplyConflicts(t *testing.T) {
	ours := decode(t, `{"m": {"a": {"x": 1}}}`)
	_, conflicts := Apply(ours, []Op{
		{Op: OpAdd, Path: "/m/a/x", Value: json.Number("2")},
		{Op: OpAdd, Path: "/m/a/x", Value: json.Number("1")},
		{Op: OpAdd, Path: "/m/gone/x", Value: json.Number("1")},
		{Op: OpReplace, Path: "/m/a/y", Old: json.Number("1"), Value: json.Number("2")},
	})
	var whys []string
	for _, c := range conflicts {
		whys = append(whys, c.Path+": "+c.Why)
	}
	want := []string{"/m/a/x: ours holds another value there", "/m/gone/x: ours lacks the object to add it to", "/m/a/y: ours holds no value there"}
	if !slices.Equal(whys, want) {
		t.Errorf("conflicts = %q, want %q", whys, want)
	}
}

func TestPointer(t *testing.T) {
	path := []string{"models", "anthropic/claude", "a~b"}
	if p := Pointer(path); p != "/models/anthropic~1claude/a~0b" {
		t.Errorf("Pointer = %q", p)
	}
	if got := ParsePointer(Pointer(path)); !slices.Equal(got, path) {
		t.Errorf("ParsePointer = %q, want %q", got, path)
	}
}
