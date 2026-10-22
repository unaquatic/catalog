package gateway

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestApplyReasoning checks that a reasoning rule's facts reach its models'
// offerings at every provider; that its default effort reaches only those
// that list it; that it wins over models.dev, with a note; and the notes of
// a missing model and of a mandatory models.dev now lists itself.
func TestApplyReasoning(t *testing.T) {
	rules, err := DecodeRules([]byte(`{"version": 1, "rules": [
		{"match": {"models": ["acme/a", "acme/gone"]}, "reasoning": {"mandatory": false, "default_enabled": false, "default_effort": "high"}},
		{"match": {"models": "acme/b"}, "reasoning": {"mandatory": true, "default_enabled": true}},
		{"match": {"models": "acme/c"}, "reasoning": {"mandatory": false}}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	effort := func(es ...Effort) *Reasoning { return &Reasoning{Efforts: es} }
	off := func(id string, r *Reasoning) Offering {
		return Offering{ProviderOptions: ProviderOptions{ID: id}, Reasoning: r}
	}
	c := &Catalog{
		Models: map[ModelID]Model{"acme/a": {}, "acme/b": {}, "acme/c": {}},
		Providers: map[ProviderID]Provider{
			"acme": {Models: map[ModelID]Offering{
				"acme/a": off("a", effort("low", "high")),
				"acme/b": off("b", effort("low")),
				"acme/c": off("c", &Reasoning{Mandatory: new(false)}),
			}},
			"gw": {Models: map[ModelID]Offering{
				"acme/a": off("acme/a", effort("low", "medium")),
				"acme/b": off("acme/b", &Reasoning{Mandatory: new(false), Efforts: []Effort{"none", "low"}}),
				"acme/c": off("acme/c", &Reasoning{Mandatory: new(false)}),
			}},
		},
	}
	notes := c.ApplyReasoning(rules)

	for _, tt := range []struct {
		provider ProviderID
		model    ModelID
		want     *Reasoning
	}{
		{"acme", "acme/a", &Reasoning{Mandatory: new(false), DefaultEnabled: new(false), Efforts: []Effort{"low", "high"}, DefaultEffort: "high"}},
		{"gw", "acme/a", &Reasoning{Mandatory: new(false), DefaultEnabled: new(false), Efforts: []Effort{"low", "medium"}}},
		{"acme", "acme/b", &Reasoning{Mandatory: new(true), DefaultEnabled: new(true), Efforts: []Effort{"low"}}},
		{"gw", "acme/b", &Reasoning{Mandatory: new(true), DefaultEnabled: new(true), Efforts: []Effort{"none", "low"}}},
	} {
		if got := c.Providers[tt.provider].Models[tt.model].Reasoning; !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s %s reasoning = %s, want %s", tt.provider, tt.model, show(got), show(tt.want))
		}
	}
	want := []string{
		": a reasoning rule names acme/gone, which the catalog lacks",
		"gw/acme/b: models.dev says reasoning mandatory is false, a rule true",
		": models.dev lists reasoning mandatory false for acme/c at every provider, as a rule does, which can drop it",
	}
	var got []string
	for _, n := range notes {
		got = append(got, n.String())
	}
	if !slices.Equal(got, want) {
		t.Errorf("notes =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// TestListOff checks that reasoning that can be turned off lists the effort
// none first, at every provider and for unlisted offerings too; and that
// reasoning that cannot, or of which no source says, or that lists no
// efforts or none already, keeps its efforts.
func TestListOff(t *testing.T) {
	r := func(mandatory *bool, es ...Effort) *Reasoning { return &Reasoning{Mandatory: mandatory, Efforts: es} }
	c := &Catalog{Providers: map[ProviderID]Provider{
		"acme": {
			Models: map[ModelID]Offering{
				"acme/off":       {Reasoning: r(new(false), "low", "high")},
				"acme/on":        {Reasoning: r(new(true), "low", "high")},
				"acme/unknown":   {Reasoning: r(nil, "low", "high")},
				"acme/budget":    {Reasoning: r(new(false))},
				"acme/own-none":  {Reasoning: r(new(false), "none", "low")},
				"acme/no-reason": {},
			},
			Unlisted: map[string]Offering{"x": {Reasoning: r(new(false), "low")}},
		},
	}}
	c.ListOff()
	p := c.Providers["acme"]
	for _, tt := range []struct {
		got  *Reasoning
		want []Effort
	}{
		{p.Models["acme/off"].Reasoning, []Effort{"none", "low", "high"}},
		{p.Models["acme/on"].Reasoning, []Effort{"low", "high"}},
		{p.Models["acme/unknown"].Reasoning, []Effort{"low", "high"}},
		{p.Models["acme/budget"].Reasoning, nil},
		{p.Models["acme/own-none"].Reasoning, []Effort{"none", "low"}},
		{p.Unlisted["x"].Reasoning, []Effort{"none", "low"}},
	} {
		if !slices.Equal(tt.got.Efforts, tt.want) {
			t.Errorf("efforts = %v, want %v", tt.got.Efforts, tt.want)
		}
	}
}

// TestDecodeReasoningRules checks the rules a reasoning rule must keep: a
// match of models, and at least one fact; and that a match of models is for
// reasoning alone.
func TestDecodeReasoningRules(t *testing.T) {
	for _, tt := range []struct{ rule, want string }{
		{`{"match": {"provider": "acme"}, "reasoning": {"mandatory": true}}`, "reasoning needs a match of models"},
		{`{"match": {"models": "acme/a"}, "reasoning": {}}`, "reasoning sets no fact"},
		{`{"match": {"models": "acme/a"}, "exclude": true}`, "a match of models is for reasoning"},
	} {
		_, err := DecodeRules([]byte(`{"version": 1, "rules": [` + tt.rule + `]}`))
		if !errors.Is(err, ErrRules) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("DecodeRules(%s) = %v, want %q", tt.rule, err, tt.want)
		}
	}
}

// show writes r with the values of its pointers.
func show(r *Reasoning) string {
	if r == nil {
		return "nil"
	}
	b := func(p *bool) any {
		if p == nil {
			return nil
		}
		return *p
	}
	return fmt.Sprintf("{mandatory %v, default_enabled %v, efforts %v, default_effort %q, budget %v}",
		b(r.Mandatory), b(r.DefaultEnabled), r.Efforts, r.DefaultEffort, r.Budget)
}

// TestEffortNames checks that every effort the offerings take, or name as
// their default, gets a name: the rules', or its ID with a capital first
// letter; and that Keep keeps the names of the efforts of the providers it
// keeps alone.
func TestEffortNames(t *testing.T) {
	rules, err := DecodeRules([]byte(`{"version": 1, "rules": [{"match": {"effort": "xhigh"}, "name": "Extra High"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	c := &Catalog{
		Models: map[ModelID]Model{"acme/a": {ID: "acme/a", Lab: "acme"}, "acme/b": {ID: "acme/b", Lab: "acme"}},
		Providers: map[ProviderID]Provider{
			"acme": {ID: "acme", Models: map[ModelID]Offering{"acme/a": {Reasoning: &Reasoning{Efforts: []Effort{"none", "xhigh"}, DefaultEffort: "xhigh"}}}},
			"gw":   {ID: "gw", Models: map[ModelID]Offering{"acme/b": {Reasoning: &Reasoning{Efforts: []Effort{"medium"}}}}},
		},
	}
	c.Efforts = c.EffortNames(rules)
	want := map[Effort]EffortName{"none": {"none", "None"}, "xhigh": {"xhigh", "Extra High"}, "medium": {"medium", "Medium"}}
	if !reflect.DeepEqual(c.Efforts, want) {
		t.Errorf("EffortNames = %v, want %v", c.Efforts, want)
	}
	kept := c.Keep("acme")
	if _, ok := kept.Efforts["medium"]; ok || kept.Efforts["xhigh"].Name != "Extra High" {
		t.Errorf("Keep(acme) efforts = %v, want none and xhigh alone", kept.Efforts)
	}
}

// TestNameVariants checks that every tier and mode the variants run in gets
// a name, the rules' or its ID with a capital first letter, and that Keep
// keeps those of the providers it keeps alone.
func TestNameVariants(t *testing.T) {
	rules, err := DecodeRules([]byte(`{"version": 1, "rules": [{"match": {"tier": "ultrafast"}, "name": "Ultra Fast"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	c := &Catalog{
		Models: map[ModelID]Model{"acme/a": {ID: "acme/a", Lab: "acme"}, "acme/b": {ID: "acme/b", Lab: "acme"}},
		Providers: map[ProviderID]Provider{
			"acme": {ID: "acme", Models: map[ModelID]Offering{"acme/a": {Variants: map[string]Variant{
				"fast": {Tier: "fast"}, "thinking:ultrafast": {Tier: "ultrafast", Mode: "thinking"}}}}},
			"gw": {ID: "gw", Models: map[ModelID]Offering{"acme/b": {Variants: map[string]Variant{"flex": {Tier: "flex"}}}}},
		},
	}
	c.Variant = c.NameVariants(rules)
	want := &VariantNames{
		Tiers: map[Tier]TierName{"fast": {"fast", "Fast"}, "ultrafast": {"ultrafast", "Ultra Fast"}, "flex": {"flex", "Flex"}},
		Modes: map[string]ModeName{"thinking": {"thinking", "Thinking"}},
	}
	if !reflect.DeepEqual(c.Variant, want) {
		t.Errorf("NameVariants = %+v, want %+v", c.Variant, want)
	}
	if kept := c.Keep("acme"); kept.Variant == nil || len(kept.Variant.Tiers) != 2 || kept.Variant.Modes["thinking"].Name != "Thinking" {
		t.Errorf("Keep(acme) variant = %+v, want fast, ultrafast and thinking alone", kept.Variant)
	}
	if n := (&Catalog{}).NameVariants(rules); n != nil {
		t.Errorf("NameVariants of no variants = %+v, want nil", n)
	}
}
