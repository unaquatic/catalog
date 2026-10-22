package build

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/unaquatic/catalog/gateway"
)

// Group groups each provider's listings into offerings, one per model it
// serves, keyed by the model's ID, and its provider-specific listings into
// Unlisted, keyed by its own IDs:
//
//   - One listing is the offering's call, its ProviderOptions and price:
//     of those that are no tier or mode of another, the one whose ID names
//     the model most closely, then the one with the most tiers and modes
//     of its own, then the shortest ID. A listing that runs the model in a mode, as OpenAI's
//     gpt-6.1-sol runs GPT-6.1 Sol Pro, is the call when the provider
//     lists no ID of the model's own.
//   - The call's Variants are its tiers and modes: the listings whose IDs
//     add a tier, a free or a thinking suffix to the call's, or to such a
//     listing's, as Vercel's "openai/gpt-6.1-sol-fast" does, or that the
//     mapping tied as a tier of the model; and the tiers and modes the
//     call offers by request fields, and those its thinking listings do.
//     A listed ID wins over request fields of the same variant. Tiers and
//     modes other than Tiers and Modes are left out, with a note.
//   - The provider's other listings of the model, and their tiers and
//     modes, are the offering's Aliases, but for those at another endpoint
//     than the call's, which are left out, with a note.
//   - The offering's facts are the call's listing's.
func (b *builder) Group() *gateway.Catalog {
	c := &gateway.Catalog{Models: b.out.Models, Providers: make(map[gateway.ProviderID]gateway.Provider, len(b.out.Providers))}
	for lp := range sortedOf(b.out.Providers) {
		prov := gateway.Provider{ID: lp.ID, Name: lp.Name, Kind: lp.Kind, Type: lp.Type, Endpoint: lp.Endpoint, Doc: lp.Doc,
			Models: make(map[gateway.ModelID]gateway.Offering)}
		tied := make(map[gateway.ModelID][]Listing)
		var unlisted []Listing
		for o := range lp.Sorted() {
			if o.Model == "" {
				unlisted = append(unlisted, o)
				continue
			}
			tied[o.Model] = append(tied[o.Model], o)
		}
		for key, ls := range b.unlistedGroups(prov.ID, unlisted) {
			if prov.Unlisted == nil {
				prov.Unlisted = make(map[string]gateway.Offering)
			}
			prov.Unlisted[key] = named(b.offering(prov, gateway.ModelID(key), ls, nil), UnlistedName(key, b.spell))
		}
		runs := make(map[gateway.ModelID][]run)
		for _, ls := range tied {
			for _, o := range ls {
				for _, mode := range slices.Sorted(maps.Keys(o.Modes)) {
					if opt := o.Modes[mode]; opt.Model != "" {
						runs[opt.Model] = append(runs[opt.Model], run{listing: o, mode: mode, opt: opt})
					}
				}
			}
		}
		for _, m := range slices.Sorted(maps.Keys(tied)) {
			prov.Models[m] = named(b.offering(prov, m, tied[m], runs[m]), c.Models[m].Name)
		}
		for _, m := range slices.Sorted(maps.Keys(runs)) {
			if _, ok := prov.Models[m]; !ok {
				prov.Models[m] = named(b.offering(prov, m, nil, runs[m]), c.Models[m].Name)
			}
		}
		c.Providers[prov.ID] = prov
	}
	LinkProviders(c)
	for _, p := range c.Providers {
		b.report.Offerings += len(p.Models) + len(p.Unlisted)
		for _, o := range p.Models {
			b.report.Variants += len(o.Variants)
			b.report.AliasIDs += len(o.Aliases)
		}
		for _, o := range p.Unlisted {
			b.report.Variants += len(o.Variants)
			b.report.AliasIDs += len(o.Aliases)
		}
	}
	return c
}

// named returns off named name, and each of its variants as VariantName
// names it.
func named(off gateway.Offering, name string) gateway.Offering {
	off.Name = name
	for k, v := range off.Variants {
		v.Name = VariantName(name, k)
		off.Variants[k] = v
	}
	return off
}

// unlistedGroups groups a provider's unlisted listings ls into one per model
// they serve, as offering groups a model's: a listing that is a tier or
// mode of another, as stepOf finds it, or the other in a region its "@"
// suffix names, as Requesty's "leanstral-1-5@eu" is, goes with the other.
// It keys each group by the ID of the listing the others lead to, as
// CatalogID writes it, numbering a key that two groups would share, with a
// note.
func (b *builder) unlistedGroups(pid gateway.ProviderID, ls []Listing) map[string][]Listing {
	root := b.rules.unlistedRoot(ls)
	groups := make(map[string][]Listing)
	for _, o := range ls {
		r := root(o.ID)
		groups[r] = append(groups[r], o)
	}
	out := make(map[string][]Listing, len(groups))
	for _, r := range slices.Sorted(maps.Keys(groups)) {
		base := cmp.Or(CatalogID(r), "model")
		key := base
		for n := 2; out[key] != nil; n++ {
			key = fmt.Sprintf("%s-%d", base, n)
			if out[key] == nil {
				b.note(pid, r, "its ID is another's as the catalog writes IDs; keyed %s", key)
			}
		}
		out[key] = groups[r]
	}
	return out
}

// unlistedRoot returns the function that gives, of the ID of one of a
// provider's unlisted listings ls, the ID of the listing it is a tier or
// mode of, as stepOf finds it, or a region of, as its "@" suffix names it,
// following such steps to the end: Requesty's "leanstral-1-5@eu" leads to
// "leanstral-1-5", and Kilo's "x:free" to "x".
func (r *Rules) unlistedRoot(ls []Listing) func(id string) string {
	listed := make(map[string]bool, len(ls))
	for _, o := range ls {
		listed[o.ID] = true
	}
	parent := make(map[string]string)
	for _, o := range ls {
		if s, ok := r.stepOf(o, ls); ok {
			parent[o.ID] = s.parent
		} else if i := strings.LastIndexByte(o.ID, '@'); i > 0 && listed[o.ID[:i]] {
			parent[o.ID] = o.ID[:i]
		}
	}
	return func(id string) string {
		for seen := make(map[string]bool); !seen[id]; {
			seen[id] = true
			p, ok := parent[id]
			if !ok {
				break
			}
			id = p
		}
		return id
	}
}

// run is a listing that runs a derived model in a mode.
type run struct {
	listing Listing
	mode    string
	opt     Option
}

// step says how a listing is a tier or mode of another of the same model at
// its provider: the other's ID, and the tier or mode it adds.
type step struct {
	parent string
	tier   gateway.Tier
	mode   string
}

// stepOf returns how listing o is a tier or mode of another of ls, and
// reports whether it is one: as the mapping tied it, or as its ID adds a
// tier, free or thinking suffix to the other's.
func (r *Rules) stepOf(o Listing, ls []Listing) (step, bool) {
	for _, p := range ls {
		switch {
		case p.ID == o.ID:
			continue
		case o.TierOf == p.ID:
			return step{parent: p.ID, tier: o.Tier}, true
		}
		for s, rule := range r.Suffixes() {
			if rule.Tier == gateway.TierFree && o.ID == p.ID+s {
				return step{parent: p.ID, tier: gateway.TierFree}, true
			}
		}
		for s, rule := range r.Suffixes() {
			if rule.Tier != "" && rule.Tier != gateway.TierFree && strings.Contains(o.ID, s) && strings.Replace(o.ID, s, "", 1) == p.ID {
				return step{parent: p.ID, tier: rule.Tier}, true
			}
		}
		for s, rule := range r.Suffixes() {
			if rule.Mode == gateway.ModeThinking && o.ID == p.ID+s {
				return step{parent: p.ID, mode: gateway.ModeThinking}, true
			}
		}
	}
	return step{}, false
}

// path is where a listing stands among a model's listings: the listing its
// steps lead to, and the tier and mode they add. A path that sets a tier or
// a mode twice, or loops, is not clean.
type path struct {
	root  string
	tier  gateway.Tier
	mode  string
	clean bool
}

// pathOf follows the steps from listing id to the listing they lead to.
func pathOf(id string, steps map[string]step) path {
	p := path{root: id, clean: true}
	seen := make(map[string]bool)
	for {
		s, ok := steps[p.root]
		if !ok {
			return p
		}
		if seen[p.root] {
			p.clean = false
			return p
		}
		seen[p.root] = true
		if s.tier != "" {
			p.clean = p.clean && p.tier == ""
			p.tier = s.tier
		}
		if s.mode != "" {
			p.clean = p.clean && p.mode == ""
			p.mode = s.mode
		}
		p.root = s.parent
	}
}

// nameRank orders model IDs by how closely they name model m: m itself
// first, then m's ID within its lab, as the whole ID or after its last
// slash, then an ID that ends in it, and then any other.
func (b *builder) nameRank(pid gateway.ProviderID, id string, m gateway.ModelID) int {
	_, tail := SplitModelID(m)
	n, t := Normalize(id), Normalize(tail)
	switch {
	case n == Normalize(string(m)):
		return 0
	case n == t || Normalize(b.rules.ModelPart(pid, id)) == t:
		return 1
	case strings.HasSuffix(n, t):
		return 2
	}
	return 3
}

// known reports whether a variant in tier and mode, either empty, is one
// the catalog lists: of Tiers and Modes.
func known(tier gateway.Tier, mode string) bool {
	return (tier == "" || slices.Contains(gateway.Tiers, tier)) && (mode == "" || slices.Contains(gateway.Modes, mode))
}

// offering returns provider prov's offering of model m from its listings ls
// of m and the listings rs that run m in a mode. For an unlisted listing, m
// is its own ID.
func (b *builder) offering(prov gateway.Provider, m gateway.ModelID, ls []Listing, rs []run) gateway.Offering {
	byID := make(map[string]Listing, len(ls))
	steps := make(map[string]step)
	var roots []Listing
	for _, o := range ls {
		byID[o.ID] = o
		if s, ok := b.rules.stepOf(o, ls); ok {
			steps[o.ID] = s
			continue
		}
		roots = append(roots, o)
	}
	// The call is the listing whose ID names m most closely, then the one
	// with the most tiers and modes of its own, then the shortest: OpenAI's
	// gpt-5.6-sol, not gpt-5.6, of GPT-5.6 Sol, and NanoGPT's
	// "qwen/qwen3.5-27b", whose ":thinking" it lists, not "TEE/qwen3.5-27b".
	kids := make(map[string]int)
	for _, s := range steps {
		kids[s.parent]++
	}
	slices.SortFunc(roots, func(x, y Listing) int {
		return cmp.Or(cmp.Compare(b.nameRank(prov.ID, x.ID, m), b.nameRank(prov.ID, y.ID, m)), cmp.Compare(kids[y.ID], kids[x.ID]),
			cmp.Compare(len(x.ID), len(y.ID)), cmp.Compare(x.ID, y.ID))
	})
	// Of the listings that run m in a mode, the call of the model they run
	// first, the same way: gpt-5.6-sol, not gpt-5.6, of GPT-5.6 Sol Pro.
	slices.SortFunc(rs, func(x, y run) int {
		return cmp.Or(
			cmp.Compare(b.nameRank(prov.ID, x.listing.ID, x.listing.Model), b.nameRank(prov.ID, y.listing.ID, y.listing.Model)),
			cmp.Compare(len(x.listing.ID), len(y.listing.ID)),
			cmp.Compare(x.listing.ID, y.listing.ID), cmp.Compare(x.mode, y.mode))
	})

	var off gateway.Offering
	var base Listing
	switch {
	case len(roots) > 0:
		base = roots[0]
		off.ProviderOptions = gateway.ProviderOptions{ID: base.ID, Name: base.Name, Endpoint: base.Endpoint}
		off.Price, off.Status = base.Price, base.Status
		if _, listed := b.out.Models[m]; listed {
			if b.report.Ties[prov.ID] == nil {
				b.report.Ties[prov.ID] = make(map[gateway.ModelID]Tie)
			}
			b.report.Ties[prov.ID][m] = Tie{Basis: base.Basis, ModelsDev: base.ModelsDev}
		}
		for _, r := range rs {
			b.note(prov.ID, r.listing.ID, "runs %s in mode %s too; the provider's own model ID %s is the call", m, r.mode, base.ID)
		}
	case len(rs) > 0:
		r := rs[0]
		base = r.listing
		off.ProviderOptions = gateway.ProviderOptions{ID: base.ID, Name: base.Name, Endpoint: base.Endpoint,
			Body: copyBody(r.opt.Body), Headers: maps.Clone(r.opt.Headers)}
		off.Price, off.Status = cmp.Or(r.opt.Price, base.Price), base.Status
		for _, o := range rs[1:] {
			b.note(prov.ID, o.listing.ID, "runs %s in mode %s too; %s is the call", m, o.mode, base.ID)
		}
	default:
		return off
	}
	off.Family, off.Type, off.Lab = base.Family, base.Type, base.Lab
	off.Released, off.Updated, off.Knowledge, off.OpenWeights = base.Released, base.Updated, base.Knowledge, base.OpenWeights
	off.Capabilities, off.Limit, off.Reasoning, off.Interleaved = base.Capabilities, base.Limit, base.Reasoning, base.Interleaved
	if len(roots) == 0 {
		return off
	}

	// Where each listing stands: a listing the mapping tied as a tier of
	// the model, which no other names, is that tier of the call, as LLM
	// Gateway's "gpt-astra-fast-latest" is of its
	// "accounts/acme/routers/gpt-astra-latest".
	paths := make(map[string]path, len(ls))
	for _, o := range ls {
		p := pathOf(o.ID, steps)
		if r := byID[p.root]; p.root != base.ID && r.Tier != "" && base.Tier == "" {
			p.clean = p.clean && p.tier == ""
			p.root, p.tier = base.ID, r.Tier
		}
		paths[o.ID] = p
	}
	own := func(ep *gateway.Endpoint) gateway.Endpoint {
		if ep != nil {
			return *ep
		}
		return prov.Endpoint
	}
	call := own(base.Endpoint)
	add := func(v gateway.Variant, from string) bool {
		key := v.Key()
		if !known(v.Tier, v.Mode) {
			b.note(prov.ID, from, "variant %s is no tier or mode the catalog lists; left out", key)
			return false
		}
		if _, taken := off.Variants[key]; taken {
			return false
		}
		if off.Variants == nil {
			off.Variants = make(map[string]gateway.Variant)
		}
		off.Variants[key] = v
		return true
	}
	// Listings of their own, nearest the call first.
	var aliases []Listing
	children := slices.Clone(ls)
	slices.SortFunc(children, func(x, y Listing) int { return cmp.Compare(len(x.ID), len(y.ID)) })
	for _, o := range children {
		p := paths[o.ID]
		switch {
		case o.ID == base.ID:
			continue
		case p.root != base.ID || !p.clean:
			aliases = append(aliases, o)
			continue
		}
		v := gateway.Variant{Tier: p.tier, Mode: p.mode, ProviderOptions: gateway.ProviderOptions{ID: o.ID, Name: o.Name}, Price: o.Price, Status: o.Status}
		if ep := own(o.Endpoint); !ep.Equal(call) {
			v.ProviderOptions.Endpoint = &ep
		}
		if !add(v, o.ID) {
			aliases = append(aliases, o)
		}
	}
	// Tiers and modes by request fields: the call's, and those of its
	// listings of a mode alone.
	byFields := func(o Listing, mode string, v gateway.Variant) {
		for _, t := range slices.Sorted(maps.Keys(o.Tiers)) {
			opt := o.Tiers[t]
			w := v
			w.Tier, w.Mode = t, mode
			w.ProviderOptions.Body, w.ProviderOptions.Headers = copyBody(opt.Body), maps.Clone(opt.Headers)
			w.Price = cmp.Or(opt.Price, v.Price)
			if _, taken := off.Variants[w.Key()]; taken {
				b.note(prov.ID, o.ID, "tier %s: both request fields and a model ID of its own; the ID's variant kept", t)
				continue
			}
			add(w, o.ID)
		}
	}
	byFields(base, "", gateway.Variant{})
	for _, mode := range slices.Sorted(maps.Keys(base.Modes)) {
		opt := base.Modes[mode]
		if opt.Model != "" {
			continue
		}
		add(gateway.Variant{Mode: mode, ProviderOptions: gateway.ProviderOptions{Body: copyBody(opt.Body), Headers: maps.Clone(opt.Headers)}, Price: opt.Price}, base.ID)
	}
	for _, key := range off.VariantKeys() {
		if v := off.Variants[key]; v.Tier == "" && v.Mode != "" && v.ProviderOptions.ID != "" {
			byFields(byID[v.ProviderOptions.ID], v.Mode, v)
		}
	}
	for _, o := range aliases {
		if !own(o.Endpoint).Equal(call) {
			b.note(prov.ID, o.ID, "serves %s at another endpoint than %s; left out", m, base.ID)
			continue
		}
		off.Aliases = append(off.Aliases, o.ID)
	}
	slices.Sort(off.Aliases)
	off.Aliases = slices.Compact(off.Aliases)
	return off
}

// cmpBool orders false before true.
func cmpBool(x, y bool) int {
	switch {
	case x == y:
		return 0
	case x:
		return 1
	}
	return -1
}
