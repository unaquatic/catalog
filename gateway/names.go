package gateway

import (
	"iter"
	"maps"
	"unicode"
	"unicode/utf8"
)

// capitalize returns id with a capital first letter, as the name of an ID
// the rules name no other way: "Medium" of medium.
func capitalize[T ~string](id T) string {
	first, size := utf8.DecodeRuneInString(string(id))
	return string(unicode.ToUpper(first)) + string(id[size:])
}

// offerings yields every offering of c, its providers' unlisted ones too.
func (c *Catalog) offerings() iter.Seq[Offering] {
	return func(yield func(Offering) bool) {
		for _, p := range c.Providers {
			for _, offs := range []iter.Seq[Offering]{maps.Values(p.Models), maps.Values(p.Unlisted)} {
				for off := range offs {
					if !yield(off) {
						return
					}
				}
			}
		}
	}
}

// EffortNames returns the name of every reasoning effort c's offerings
// take, or name as their default, by its ID: the name r gives it, or else
// its ID with a capital first letter, as "Medium" of medium.
func (c *Catalog) EffortNames(r *Rules) map[Effort]EffortName {
	names := map[Effort]EffortName{}
	add := func(e Effort) {
		if _, ok := names[e]; ok || e == "" {
			return
		}
		name, ok := r.EffortName(e)
		if !ok {
			name = capitalize(e)
		}
		names[e] = EffortName{ID: e, Name: name}
	}
	for off := range c.offerings() {
		if off.Reasoning == nil {
			continue
		}
		for _, e := range off.Reasoning.Efforts {
			add(e)
		}
		add(off.Reasoning.DefaultEffort)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// NameVariants returns the name of every tier and mode c's variants run in,
// by its ID: the name r gives it, or else its ID with a capital first
// letter, as "Ultrafast" of ultrafast; or nil when they run in none.
func (c *Catalog) NameVariants(r *Rules) *VariantNames {
	n := &VariantNames{}
	for off := range c.offerings() {
		for _, v := range off.Variants {
			if t := v.Tier; t != "" {
				if _, ok := n.Tiers[t]; !ok {
					name, ok := r.TierName(t)
					if !ok {
						name = capitalize(t)
					}
					if n.Tiers == nil {
						n.Tiers = map[Tier]TierName{}
					}
					n.Tiers[t] = TierName{ID: t, Name: name}
				}
			}
			if m := v.Mode; m != "" {
				if _, ok := n.Modes[m]; !ok {
					name, ok := r.ModeName(m)
					if !ok {
						name = capitalize(m)
					}
					if n.Modes == nil {
						n.Modes = map[string]ModeName{}
					}
					n.Modes[m] = ModeName{ID: m, Name: name}
				}
			}
		}
	}
	if n.Tiers == nil && n.Modes == nil {
		return nil
	}
	return n
}
