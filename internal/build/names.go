package build

import (
	"iter"
	"maps"
	"unicode"
	"unicode/utf8"

	"github.com/unaquatic/catalog/gateway"
)

// capitalize returns id with a capital first letter, as the name of an ID
// the rules name no other way: "Medium" of medium.
func capitalize[T ~string](id T) string {
	first, size := utf8.DecodeRuneInString(string(id))
	return string(unicode.ToUpper(first)) + string(id[size:])
}

// allOfferings yields every offering of c, its providers' unlisted ones too.
func allOfferings(c *gateway.Catalog) iter.Seq[gateway.Offering] {
	return func(yield func(gateway.Offering) bool) {
		for _, p := range c.Providers {
			for _, offs := range []iter.Seq[gateway.Offering]{maps.Values(p.Models), maps.Values(p.Unlisted)} {
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
func EffortNames(c *gateway.Catalog, r *Rules) map[gateway.Effort]gateway.EffortName {
	names := map[gateway.Effort]gateway.EffortName{}
	add := func(e gateway.Effort) {
		if _, ok := names[e]; ok || e == "" {
			return
		}
		name, ok := r.EffortName(e)
		if !ok {
			name = capitalize(e)
		}
		names[e] = gateway.EffortName{ID: e, Name: name}
	}
	for off := range allOfferings(c) {
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
func NameVariants(c *gateway.Catalog, r *Rules) *gateway.VariantNames {
	n := &gateway.VariantNames{}
	for off := range allOfferings(c) {
		for _, v := range off.Variants {
			if t := v.Tier; t != "" {
				if _, ok := n.Tiers[t]; !ok {
					name, ok := r.TierName(t)
					if !ok {
						name = capitalize(t)
					}
					if n.Tiers == nil {
						n.Tiers = map[gateway.Tier]gateway.TierName{}
					}
					n.Tiers[t] = gateway.TierName{ID: t, Name: name}
				}
			}
			if m := v.Mode; m != "" {
				if _, ok := n.Modes[m]; !ok {
					name, ok := r.ModeName(m)
					if !ok {
						name = capitalize(m)
					}
					if n.Modes == nil {
						n.Modes = map[string]gateway.ModeName{}
					}
					n.Modes[m] = gateway.ModeName{ID: m, Name: name}
				}
			}
		}
	}
	if n.Tiers == nil && n.Modes == nil {
		return nil
	}
	return n
}
