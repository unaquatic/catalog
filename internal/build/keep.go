package build

import (
	"slices"

	"github.com/unaquatic/catalog/gateway"
)

// LinkProviders sets each model's Providers to the providers that offer it, in ID
// order, as the offerings say: after a change of them, as a patch makes.
func LinkProviders(c *gateway.Catalog) {
	providers := make(map[gateway.ModelID][]gateway.ProviderID, len(c.Models))
	for pid, p := range c.Providers {
		for id := range p.Models {
			providers[id] = append(providers[id], pid)
		}
	}
	for id, m := range c.Models {
		m.Providers = slices.Sorted(slices.Values(providers[id]))
		if m.Providers == nil {
			m.Providers = []gateway.ProviderID{}
		}
		c.Models[id] = m
	}
}

// Keep returns a copy of c with only providers ps, and only the models one
// of them offers, and their labs and families, as the Snapshot keeps the
// built-in providers.
func Keep(c *gateway.Catalog, ps ...gateway.ProviderID) *gateway.Catalog {
	out := &gateway.Catalog{Schema: c.Schema, Version: c.Version, Source: c.Source, Generated: c.Generated,
		Models: make(map[gateway.ModelID]gateway.Model), Providers: make(map[gateway.ProviderID]gateway.Provider)}
	for _, id := range ps {
		if p, ok := c.Providers[id]; ok {
			out.Providers[id] = p
		}
	}
	for id, m := range c.Models {
		m.Providers = slices.DeleteFunc(slices.Clone(m.Providers), func(p gateway.ProviderID) bool { return !slices.Contains(ps, p) })
		if len(m.Providers) == 0 {
			continue
		}
		out.Models[id] = m
		if l, ok := c.Labs[m.Lab]; ok {
			if out.Labs == nil {
				out.Labs = make(map[gateway.ProviderID]gateway.Lab)
			}
			out.Labs[m.Lab] = l
		}
		if f, ok := c.Families[m.Family]; ok {
			if out.Families == nil {
				out.Families = make(map[string]gateway.Family)
			}
			out.Families[m.Family] = f
		}
	}
	out.Spellings = SpellingsFor(c.Spellings, NameParts(out))
	for _, p := range out.Providers {
		for _, off := range p.Models {
			if c.Variant != nil {
				for _, v := range off.Variants {
					keepVariantNames(out, c.Variant, v)
				}
			}
			if off.Reasoning == nil {
				continue
			}
			for _, e := range append(slices.Clone(off.Reasoning.Efforts), off.Reasoning.DefaultEffort) {
				if l, ok := c.Efforts[e]; ok {
					if out.Efforts == nil {
						out.Efforts = make(map[gateway.Effort]gateway.EffortName)
					}
					out.Efforts[e] = l
				}
			}
		}
	}
	return out
}

// keepVariantNames copies the names of v's tier and mode from names to c.
func keepVariantNames(c *gateway.Catalog, names *gateway.VariantNames, v gateway.Variant) {
	if c.Variant == nil {
		c.Variant = &gateway.VariantNames{}
	}
	if t, ok := names.Tiers[v.Tier]; ok {
		if c.Variant.Tiers == nil {
			c.Variant.Tiers = map[gateway.Tier]gateway.TierName{}
		}
		c.Variant.Tiers[v.Tier] = t
	}
	if m, ok := names.Modes[v.Mode]; ok {
		if c.Variant.Modes == nil {
			c.Variant.Modes = map[string]gateway.ModeName{}
		}
		c.Variant.Modes[v.Mode] = m
	}
}
