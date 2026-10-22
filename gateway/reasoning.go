package gateway

import (
	"fmt"
	"maps"
	"slices"
)

// ApplyReasoning sets the facts of r's reasoning rules on the offerings of
// their models at every provider, and returns notes of what to check: a
// model a rule names that the catalog lacks, a provider whose models.dev
// listing says otherwise than the rule, and a rule whose mandatory
// models.dev now lists itself at every provider, which can go.
//
// The facts are the model's, as its lab documents them, so they hold at
// every provider that serves it. Of an offering that reasons, a rule sets
// Mandatory and DefaultEnabled when it says them, and DefaultEffort when it
// says one the offering's Efforts list.
func (c *Catalog) ApplyReasoning(r *Rules) []Note {
	var notes []Note
	providers := slices.Sorted(maps.Keys(c.Providers))
	for _, rule := range r.reasons {
		f := rule.Reasoning
		for _, id := range rule.Match.Models {
			id := ModelID(id)
			if _, ok := c.Models[id]; !ok {
				notes = append(notes, Note{Text: fmt.Sprintf("a reasoning rule names %s, which the catalog lacks", id)})
				continue
			}
			repeated, served := f.Mandatory != nil, false
			for _, pid := range providers {
				off, ok := c.Providers[pid].Models[id]
				if !ok || off.Reasoning == nil {
					continue
				}
				served = true
				rs := *off.Reasoning
				rs.Efforts = slices.Clone(rs.Efforts)
				if f.Mandatory != nil {
					switch {
					case rs.Mandatory == nil:
						repeated = false
					case *rs.Mandatory != *f.Mandatory:
						repeated = false
						notes = append(notes, Note{Provider: pid, Model: off.ProviderOptions.ID,
							Text: fmt.Sprintf("models.dev says reasoning mandatory is %t, a rule %t", *rs.Mandatory, *f.Mandatory)})
					}
					rs.Mandatory = new(*f.Mandatory)
				}
				if f.DefaultEnabled != nil {
					rs.DefaultEnabled = new(*f.DefaultEnabled)
				}
				if f.DefaultEffort != "" && slices.Contains(rs.Efforts, f.DefaultEffort) {
					rs.DefaultEffort = f.DefaultEffort
				}
				off.Reasoning = &rs
				c.Providers[pid].Models[id] = off
			}
			if repeated && served {
				notes = append(notes, Note{Text: fmt.Sprintf("models.dev lists reasoning mandatory %t for %s at every provider, as a rule does, which can drop it", *f.Mandatory, id)})
			}
		}
	}
	return notes
}

// ListOff starts the Efforts of every offering whose reasoning can be
// turned off, as Mandatory false says, with none, unless they list it, so
// that none turns reasoning off at every provider: a client sends it as its
// API turns reasoning off, by the effort none or by a switch of its own, as
// Anthropic's thinking type "disabled". An offering that lists no efforts,
// which a token budget alone controls, lists none still.
func (c *Catalog) ListOff() {
	for _, p := range c.Providers {
		listOff(p.Models)
		listOff(p.Unlisted)
	}
}

// listOff does as ListOff does to the offerings offs.
func listOff[K comparable](offs map[K]Offering) {
	for id, off := range offs {
		r := off.Reasoning
		if r == nil || r.Mandatory == nil || *r.Mandatory || len(r.Efforts) == 0 || slices.Contains(r.Efforts, EffortNone) {
			continue
		}
		rs := *r
		rs.Efforts = slices.Insert(slices.Clone(r.Efforts), 0, EffortNone)
		off.Reasoning = &rs
		offs[id] = off
	}
}
