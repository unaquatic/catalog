package gateway

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
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
func (b *builder) Group() *Catalog {
	c := &Catalog{Models: b.out.Models, Providers: make(map[ProviderID]Provider, len(b.out.Providers))}
	for lp := range sortedValues(b.out.Providers) {
		prov := Provider{ID: lp.ID, Name: lp.Name, Kind: lp.Kind, Type: lp.Type, Endpoint: lp.Endpoint, Doc: lp.Doc,
			Models: make(map[ModelID]Offering)}
		tied := make(map[ModelID][]Listing)
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
				prov.Unlisted = make(map[string]Offering)
			}
			prov.Unlisted[key] = named(b.offering(prov, ModelID(key), ls, nil), UnlistedName(key, b.spell))
		}
		runs := make(map[ModelID][]run)
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
	c.Link()
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
func named(off Offering, name string) Offering {
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
func (b *builder) unlistedGroups(pid ProviderID, ls []Listing) map[string][]Listing {
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
	tier   Tier
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
			if rule.Tier == TierFree && o.ID == p.ID+s {
				return step{parent: p.ID, tier: TierFree}, true
			}
		}
		for s, rule := range r.Suffixes() {
			if rule.Tier != "" && rule.Tier != TierFree && strings.Contains(o.ID, s) && strings.Replace(o.ID, s, "", 1) == p.ID {
				return step{parent: p.ID, tier: rule.Tier}, true
			}
		}
		for s, rule := range r.Suffixes() {
			if rule.Mode == ModeThinking && o.ID == p.ID+s {
				return step{parent: p.ID, mode: ModeThinking}, true
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
	tier  Tier
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
func (b *builder) nameRank(pid ProviderID, id string, m ModelID) int {
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
func known(tier Tier, mode string) bool {
	return (tier == "" || slices.Contains(Tiers, tier)) && (mode == "" || slices.Contains(Modes, mode))
}

// offering returns provider prov's offering of model m from its listings ls
// of m and the listings rs that run m in a mode. For an unlisted listing, m
// is its own ID.
func (b *builder) offering(prov Provider, m ModelID, ls []Listing, rs []run) Offering {
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

	var off Offering
	var base Listing
	switch {
	case len(roots) > 0:
		base = roots[0]
		off.ProviderOptions = ProviderOptions{ID: base.ID, Name: base.Name, Endpoint: base.Endpoint}
		off.Price, off.Status, off.Basis, off.ModelsDev = base.Price, base.Status, base.Basis, base.ModelsDev
		for _, r := range rs {
			b.note(prov.ID, r.listing.ID, "runs %s in mode %s too; the provider's own model ID %s is the call", m, r.mode, base.ID)
		}
	case len(rs) > 0:
		r := rs[0]
		base = r.listing
		off.ProviderOptions = ProviderOptions{ID: base.ID, Name: base.Name, Endpoint: base.Endpoint,
			Body: cloneBody(r.opt.Body), Headers: maps.Clone(r.opt.Headers)}
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
	own := func(ep *Endpoint) Endpoint {
		if ep != nil {
			return *ep
		}
		return prov.Endpoint
	}
	call := own(base.Endpoint)
	add := func(v Variant, from string) bool {
		key := v.Key()
		if !known(v.Tier, v.Mode) {
			b.note(prov.ID, from, "variant %s is no tier or mode the catalog lists; left out", key)
			return false
		}
		if _, taken := off.Variants[key]; taken {
			return false
		}
		if off.Variants == nil {
			off.Variants = make(map[string]Variant)
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
		v := Variant{Tier: p.tier, Mode: p.mode, ProviderOptions: ProviderOptions{ID: o.ID, Name: o.Name}, Price: o.Price, Status: o.Status}
		if ep := own(o.Endpoint); !ep.Equal(call) {
			v.ProviderOptions.Endpoint = &ep
		}
		if !add(v, o.ID) {
			aliases = append(aliases, o)
		}
	}
	// Tiers and modes by request fields: the call's, and those of its
	// listings of a mode alone.
	byFields := func(o Listing, mode string, v Variant) {
		for _, t := range slices.Sorted(maps.Keys(o.Tiers)) {
			opt := o.Tiers[t]
			w := v
			w.Tier, w.Mode = t, mode
			w.ProviderOptions.Body, w.ProviderOptions.Headers = cloneBody(opt.Body), maps.Clone(opt.Headers)
			w.Price = cmp.Or(opt.Price, v.Price)
			if _, taken := off.Variants[w.Key()]; taken {
				b.note(prov.ID, o.ID, "tier %s: both request fields and a model ID of its own; the ID's variant kept", t)
				continue
			}
			add(w, o.ID)
		}
	}
	byFields(base, "", Variant{})
	for _, mode := range slices.Sorted(maps.Keys(base.Modes)) {
		opt := base.Modes[mode]
		if opt.Model != "" {
			continue
		}
		add(Variant{Mode: mode, ProviderOptions: ProviderOptions{Body: cloneBody(opt.Body), Headers: maps.Clone(opt.Headers)}, Price: opt.Price}, base.ID)
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

// Resolved is a call as a client sends it: an offering's own, or one of its
// variants combined with it, with its provider and model, and its endpoint
// always set.
type Resolved struct {
	Provider     ProviderID `json:"provider"`
	ProviderName string     `json:"provider_name"`
	// Model is the model called, empty for an unlisted one.
	Model ModelID `json:"model,omitempty"`
	// ID is the call's variant ID, as VariantID writes it.
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	Tier Tier   `json:"tier,omitempty"`
	Mode string `json:"mode,omitempty"`
	// ProviderOptions are the offering's, with the variant's over them.
	ProviderOptions ProviderOptions `json:"provider_options"`
	Price           *Price          `json:"price,omitempty"`
	Status          Status          `json:"status,omitempty"`
	// Basis and ModelsDev are the offering's, which the API leaves out.
	Basis     Basis   `json:"-"`
	ModelsDev ModelID `json:"-"`
}

// Variants returns every call of provider p, resolved: offering by offering
// in model ID order and then unlisted ones in ID order, each offering's own
// call followed by its variants, in VariantKeys order.
func (c *Catalog) Variants(p ProviderID) iter.Seq[Resolved] {
	return func(yield func(Resolved) bool) {
		prov, ok := c.Providers[p]
		if !ok {
			return
		}
		each := func(key string, model ModelID, off Offering) bool {
			if !yield(resolve(prov, key, model, off, "")) {
				return false
			}
			for _, k := range off.VariantKeys() {
				if !yield(resolve(prov, key, model, off, k)) {
					return false
				}
			}
			return true
		}
		for _, m := range slices.Sorted(maps.Keys(prov.Models)) {
			if !each(string(m), m, prov.Models[m]) {
				return
			}
		}
		for _, id := range slices.Sorted(maps.Keys(prov.Unlisted)) {
			if !each(id, "", prov.Unlisted[id]) {
				return
			}
		}
	}
}

// resolve returns the call of variant k, or the offering's own for "", of
// offering off, keyed key at provider prov, of model, empty for an unlisted
// one.
func resolve(prov Provider, key string, model ModelID, off Offering, k string) Resolved {
	r := Resolved{Provider: prov.ID, ProviderName: prov.Name, Model: model, ID: VariantID(key, k), Name: off.Name,
		ProviderOptions: off.ProviderOptions, Price: off.Price, Status: off.Status, Basis: off.Basis, ModelsDev: off.ModelsDev}
	r.ProviderOptions.Body = MergeBody(nil, off.ProviderOptions.Body)
	if v, ok := off.Variants[k]; ok {
		r.Name, r.Tier, r.Mode = cmp.Or(v.Name, off.Name), v.Tier, v.Mode
		r.ProviderOptions = ProviderOptions{
			ID:       cmp.Or(v.ProviderOptions.ID, off.ProviderOptions.ID),
			Name:     cmp.Or(v.ProviderOptions.Name, off.ProviderOptions.Name),
			Endpoint: cmp.Or(v.ProviderOptions.Endpoint, off.ProviderOptions.Endpoint),
			Body:     MergeBody(off.ProviderOptions.Body, v.ProviderOptions.Body),
			Headers:  mergeHeaders(off.ProviderOptions.Headers, v.ProviderOptions.Headers),
		}
		r.Price, r.Status = cmp.Or(v.Price, off.Price), cmp.Or(v.Status, off.Status)
	}
	if r.ProviderOptions.Endpoint == nil {
		ep := prov.Endpoint
		r.ProviderOptions.Endpoint = &ep
	}
	return r
}

// Lookup returns the key of the offering that provider p's variant id
// calls, in the provider's Models or, when unlisted is set, its Unlisted,
// and the variant's key, "" for the offering's own call. It reports whether
// the catalog lists the variant. It ignores case, as the catalog's IDs are
// all lower case.
func (c *Catalog) Lookup(p ProviderID, id string) (offering string, unlisted bool, variant string, ok bool) {
	id = strings.ToLower(id)
	prov, ok := c.Providers[p]
	if !ok {
		return "", false, "", false
	}
	find := func(key string) (Offering, bool, bool) {
		if o, ok := prov.Models[ModelID(key)]; ok {
			return o, false, true
		}
		o, ok := prov.Unlisted[key]
		return o, true, ok
	}
	if _, u, ok := find(id); ok {
		return id, u, "", true
	}
	// A variant's key holds a mode, a tier, or both: one or two segments.
	for cut, segs := id, 0; segs < 2; segs++ {
		i := strings.LastIndexByte(cut, ':')
		if i < 0 {
			break
		}
		cut = cut[:i]
		if o, u, ok := find(cut); ok {
			if _, has := o.Variants[id[i+1:]]; has {
				return cut, u, id[i+1:], true
			}
		}
	}
	return "", false, "", false
}

// Resolve returns provider p's variant id, resolved, and reports whether
// the catalog lists it.
func (c *Catalog) Resolve(p ProviderID, id string) (Resolved, bool) {
	key, unlisted, k, ok := c.Lookup(p, id)
	if !ok {
		return Resolved{}, false
	}
	prov := c.Providers[p]
	if unlisted {
		return resolve(prov, key, "", prov.Unlisted[key], k), true
	}
	return resolve(prov, key, ModelID(key), prov.Models[ModelID(key)], k), true
}

// MergeBody returns request fields over merged into base deeply: an object
// in both merges key by key, and any other value of over replaces base's.
// It returns nil when both are empty, and copies what it returns.
func MergeBody(base, over map[string]any) map[string]any {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := cloneBody(base)
	if out == nil {
		out = make(map[string]any, len(over))
	}
	for k, v := range over {
		if bm, ok := out[k].(map[string]any); ok {
			if om, ok := v.(map[string]any); ok {
				out[k] = MergeBody(bm, om)
				continue
			}
		}
		out[k] = cloneValue(v)
	}
	return out
}

// mergeHeaders returns over's headers added to base's, or nil for none.
func mergeHeaders(base, over map[string]string) map[string]string {
	if len(base) == 0 && len(over) == 0 {
		return nil
	}
	out := maps.Clone(base)
	if out == nil {
		out = make(map[string]string, len(over))
	}
	maps.Copy(out, over)
	return out
}

// String returns r as "provider variant".
func (r Resolved) String() string {
	return fmt.Sprintf("%s %s", r.Provider, r.ID)
}
