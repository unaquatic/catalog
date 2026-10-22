package jsonschema

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Kind is a kind of thing.
type Kind string

// The kinds.
const (
	KindA Kind = "a"
	KindB Kind = "b"
)

// Base is embedded.
type Base struct {
	// Rate is inlined.
	Rate float64 `json:"rate"`
}

// Thing is the root.
type Thing struct {
	// Kind says what it is.
	Kind  Kind              `json:"kind"`
	Count int               `json:"count,omitempty"`
	At    time.Time         `json:"at,omitzero"`
	Tags  []string          `json:"tags"`
	Next  *Thing            `json:"next,omitempty"`
	By    map[Kind]Base     `json:"by,omitempty"`
	Body  map[string]any    `json:"body,omitempty"`
	Skip  string            `json:"-"`
	Extra map[string]string `json:"extra"`
	Base
}

// descs describe Thing's definitions.
var descs = Descriptions{
	"thing": {Description: "The root.", Properties: map[string]string{
		"kind": "What it is.", "count": "How many.", "at": "When.", "tags": "Tags.", "next": "The next.",
		"by": "Bases by kind.", "body": "Any fields.", "extra": "More.", "rate": "Inlined.",
	}},
	"kind": {Description: "A kind."},
	"base": {Description: "A base.", Properties: map[string]string{"rate": "A rate."}},
}

// TestGenerate checks the schema of a type of this package: its snake_case
// definitions and their descriptions, required and nullable fields, an
// embedded struct, a recursive type and a pinned property; and that
// Validate holds a document to it.
func TestGenerate(t *testing.T) {
	consts, err := ReadConstsOf("jsonschema_test.go")
	if err != nil {
		t.Fatal(err)
	}
	out, err := Generate(reflect.TypeFor[Thing](), Options{ID: "https://x/thing.json", Descriptions: descs, Consts: consts,
		Properties: map[string]map[string]any{"thing.count": {"minimum": 0}}})
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Ref  string                    `json:"$ref"`
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	thing := s.Defs["thing"]
	props := thing["properties"].(map[string]any)
	if s.Ref != "#/$defs/thing" || thing["description"] != "The root." {
		t.Errorf("root = %s, thing = %v", s.Ref, thing)
	}
	if got := thing["required"]; !reflect.DeepEqual(got, []any{"kind", "tags", "extra", "rate"}) {
		t.Errorf("required = %v", got)
	}
	if _, ok := props["Skip"]; ok {
		t.Error(`a json:"-" field is a property`)
	}
	if got := props["kind"].(map[string]any)["description"]; got != "What it is." {
		t.Errorf("kind's description = %v", got)
	}
	if got := props["rate"].(map[string]any)["description"]; got != "Inlined." {
		t.Errorf("the embedded rate's description = %v, want thing's", got)
	}
	if got := s.Defs["kind"]["examples"]; !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("kind's examples = %v", got)
	}
	if _, ok := props["tags"].(map[string]any)["anyOf"]; !ok {
		t.Error("a slice encoding/json may write as null is not nullable")
	}
	if got := props["next"].(map[string]any)["$ref"]; got != "#/$defs/thing" {
		t.Errorf("next = %v", got)
	}
	if got := props["count"].(map[string]any)["minimum"]; got != 0.0 {
		t.Errorf("count's added keyword = %v", got)
	}

	good := `{"kind": "a", "tags": null, "extra": {}, "rate": 1.5, "next": {"kind": "b", "tags": [], "extra": {"x": "y"}, "rate": 2}, "by": {"a": {"rate": 1}}}`
	if err := Validate(out, []byte(good)); err != nil {
		t.Errorf("Validate(good) = %v", err)
	}
	bad := `{"kind": 1, "tags": [2], "extra": {"x": 3}, "next": {"kind": "b", "tags": [], "extra": {}, "rate": 2}, "count": 1.5}`
	err = Validate(out, []byte(bad))
	for _, want := range []string{"/kind: is integer, want string", "/tags/0: is integer, want string", "/extra/x: is integer, want string", `/: has no "rate"`, "/count: is number, want integer"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Validate(bad) = %v, want %q", err, want)
		}
	}
}

// TestGenerateDescriptions checks that Generate fails on a definition or
// property its descriptions leave out, and on one they describe that the
// schema has not.
func TestGenerateDescriptions(t *testing.T) {
	d := Descriptions{
		"thing": {Description: "The root.", Properties: map[string]string{"kind": "What it is.", "gone": "Stale."}},
		"base":  {Description: "A base.", Properties: map[string]string{"rate": "A rate."}},
		"old":   {Description: "Stale."},
	}
	_, err := Generate(reflect.TypeFor[Thing](), Options{Descriptions: d})
	for _, want := range []string{"definition kind has no description", "property thing.tags has no description",
		"thing.gone is described, but no property", "old is described, but no definition"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Generate = %v, want %q", err, want)
		}
	}
}

func TestSnake(t *testing.T) {
	for in, want := range map[string]string{"PriceTier": "price_tier", "ModelID": "model_id", "Catalog": "catalog",
		"ProviderOptions": "provider_options", "APIKey": "api_key", "ID": "id"} {
		if got := Snake(in); got != want {
			t.Errorf("Snake(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestValidatePattern checks that Validate holds a string to its pattern.
func TestValidatePattern(t *testing.T) {
	schema := []byte(`{"type": "object", "properties": {"d": {"type": "string", "pattern": "^\\d{4}-\\d{2}-\\d{2}$"}}}`)
	if err := Validate(schema, []byte(`{"d": "2026-10-22"}`)); err != nil {
		t.Errorf("a date: %v", err)
	}
	for _, doc := range []string{`{"d": ""}`, `{"d": "Oct 22"}`} {
		if err := Validate(schema, []byte(doc)); err == nil || !strings.Contains(err.Error(), "/d: is ") {
			t.Errorf("%s: %v, want it to break the pattern", doc, err)
		}
	}
}
