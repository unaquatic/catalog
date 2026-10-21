package gateway

import (
	"cmp"
	"errors"
	"fmt"
	"iter"
	"maps"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/unaquatic/catalog/modelsdev"
)

// ErrUnknownProvider reports a provider that the models.dev catalog does not
// list.
var ErrUnknownProvider = errors.New("gateway: provider not in the models.dev catalog")

// Options configures FromModelsDev.
type Options struct {
	// Providers are the models.dev providers to keep. Empty keeps them all,
	// as the models.dev site lists them.
	Providers []ProviderID
	// Infer applies the mapping's own rules on top of models.dev's ties, as
	// FromModelsDev describes them. Without it, the catalog is models.dev's
	// alone.
	Infer bool
	// Source and Generated set the catalog's fields of the same names.
	Source    string
	Generated time.Time
	// Rules are the mapping's rules; nil reads DefaultRules, rules.json's.
	Rules *Rules
}

// Basis names how FromModelsDev tied an offering to a model.
type Basis string

// Bases. The first two are models.dev's own ties; the rest are the
// mapping's rules, which Options.Infer turns on.
const (
	// BasisCanonical is models.dev's canonical_model_id.
	BasisCanonical Basis = "canonical"
	// BasisLab ties a lab's own offering to the lab's model of the same ID,
	// as the models.dev site does where models.dev lists no
	// canonical_model_id, such as Anthropic's "claude-opus-4-8".
	BasisLab Basis = "lab"
	// BasisID ties an offering models.dev ties to nothing to the model whose
	// ID its own ID names, as InferByID reads it, such as LLM Gateway's
	// "gpt-6-astra" to openai/gpt-6-astra.
	BasisID Basis = "id"
	// BasisTier ties an offering whose ID names a service tier of a model,
	// such as Vercel's "openai/gpt-6-astra-fast", to that model, as its
	// Tier.
	BasisTier Basis = "tier"
	// BasisPro ties an offering whose ID names the pro mode of a model,
	// such as OpenRouter's "openai/gpt-6-astra-pro", to the model derived
	// for that mode, openai/gpt-6-astra-pro.
	BasisPro Basis = "pro"
	// BasisRule ties a listing to the model a rule names for it.
	BasisRule Basis = "rule"
	// BasisInferred ties an offering models.dev ties to nothing to a model
	// the mapping makes for its model ID, under the lab the ID names: one
	// that two or more providers list alike, as Hugging Face's
	// "Qwen/Qwen3-235B-A22B-Thinking-2507" and others are
	// alibaba/qwen3-235b-a22b-thinking-2507, or one that names its maker,
	// or is its maker's own, as Mistral's "mistral-medium-2508" is.
	BasisInferred Basis = "inferred"
	// BasisNone leaves an offering provider-specific.
	BasisNone Basis = "provider-specific"
)

// Note is something FromModelsDev did with a models.dev entry that a reader
// of the catalog may want to check.
type Note struct {
	Provider ProviderID
	// Model is the provider's model ID, or empty for a note about the
	// provider.
	Model string
	Text  string
}

// String returns the note as "provider/model: text".
func (n Note) String() string {
	if n.Model == "" {
		return string(n.Provider) + ": " + n.Text
	}
	return string(n.Provider) + "/" + n.Model + ": " + n.Text
}

// Report describes what FromModelsDev made of the models.dev catalog.
type Report struct {
	Providers int
	Models    int
	// Listings counts models.dev's entries of the providers kept, and
	// Offerings, Variants and AliasIDs what they were grouped into: the
	// offerings, their variants, and the model IDs among their aliases.
	// Dropped counts Bedrock's copies of a model for other regions, which
	// the grouping leaves out.
	Listings  int
	Offerings int
	Variants  int
	AliasIDs  int
	Dropped   int
	// Bases counts listings by how they were tied to a model.
	Bases map[Basis]int
	// Merged lists the models.dev models merged into another as a tier,
	// and Derived the models derived for a mode.
	Merged  []ModelID
	Derived []ModelID
	// Inferred lists the models made for providers' model IDs that name
	// their lab, as shareUnlisted makes them.
	Inferred []ModelID
	// Services lists the providers' model IDs left out of the catalog as no
	// model but a service, as ServiceOf reads them: a router, a preset or a
	// tool, the Note's Text.
	Services []Note
	// Unknown lists the providers' model IDs left out of the catalog
	// because no rule ties them to a model or finds the lab that made them,
	// as Poe's "trytako/tako".
	Unknown []Note
	// Tiers lists the offerings whose IDs name a tier of another offering.
	Tiers []Note
	// Notes lists other things to check, such as protocols the mapping
	// does not know.
	Notes []Note
}

// FromModelsDev builds the catalog from b, models.dev's catalog.json.
//
// models.dev's own ties come first:
//
//   - Every model of b is a Model, whether or not a kept provider serves it,
//     as the models.dev site lists them all, under its ID as CatalogID
//     writes it, and named by NameOf from that ID. The words are spelt as
//     models.dev's names, of models and of providers' models, most often
//     spell them; the catalog's Spellings list the spellings its names use.
//   - Every model of a kept provider is an Offering of the provider. It
//     serves the model its canonical_model_id names; or, when it has none
//     and the provider is a lab, the lab's model of the same ID; or else no
//     model: it is provider-specific. A provider is a lab when b lists
//     models of its ID.
//   - The npm package of the provider and its base URL give its Endpoint,
//     by FormatOf and AuthOf, with the package's default base URL where
//     models.dev lists none. An offering whose npm package, API shape or
//     base URL differs has an Endpoint of its own, whole.
//   - A mode, in experimental.modes, whose body sets service_tier or speed
//     is a tier: flex, fast for priority and Anthropic's speed, or
//     ultrafast. One that sets reasoning.mode is a mode of that name. Any
//     other mode keeps its name as a mode, with a note. An option without
//     long-prompt prices of its own gets the offering's, scaled as its
//     short-prompt prices scale them, marked estimated.
//   - Effort controls list their levels without null and "default"; toggle
//     and budget_tokens controls set Toggle and Budget.
//
// With opts.Infer, the mapping's own steps follow, in this order. They ask
// opts.Rules, the declarative rules, what the parts of IDs they read mean:
// which maker names a lab, which suffix a tier, which listing is a service
// or a fine-tune.
//
//  0. A listing a rule ties to a model the catalog lists serves it
//     (BasisRule), whatever the steps below would say.
//  1. A model of b whose ID is another's of the same lab with a tier suffix
//     the rules know, such as openai/gpt-6-astra-fast, is merged into the
//     other: its offerings serve the other in that tier.
//  2. An offering models.dev ties to nothing serves the model its ID names,
//     as InferByID reads it (BasisID); or, when its ID names a tier of a
//     language model, that model in the tier (BasisTier); or, when it
//     names the pro mode of a model whose lab offers that mode, the model
//     derived for the mode (BasisPro). An offering models.dev ties to such
//     a model, but whose ID names its pro mode, as OpenRouter's
//     "openai/gpt-5.6-sol-pro" does of openai/gpt-5.6-sol, moves to the
//     model derived for the mode too (BasisPro), keeping models.dev's tie
//     in ModelsDev.
//  3. The offerings still tied to nothing are tied, or make models of their
//     own, as shareUnlisted does; a "-latest" ID is no different from any
//     other. What is left is left out, with a note in the report.
//  4. Every model whose lab, or any provider, offers a pro mode gets a
//     derived model, its ID with "-pro", unless b lists one: GPT-6 Astra
//     Pro, openai/gpt-6-astra-pro, served by OpenAI's gpt-6-astra in mode
//     pro and by the offerings step 2 ties to it.
//
// Last, an offering whose ID is another offering's of the same provider
// with a tier suffix, and which serves the same model, is marked as that
// tier of the other.
//
// Group then groups each provider's listings into offerings.
//
// It fails with ErrUnknownProvider when b lacks a provider to keep.
func FromModelsDev(b modelsdev.Bundle, opts Options) (*Catalog, Report, error) {
	bl, err := list(b, opts)
	if err != nil {
		return nil, Report{}, err
	}
	c := bl.Group()
	c.Version, c.Source, c.Generated = Version, opts.Source, opts.Generated
	c.Spellings = bl.spell.For(c.NameParts())
	c.Labs, c.Families = bl.names(c.Models)
	return c, bl.report, nil
}

// names returns the labs and the families of models, each named:
//
//   - A lab as the rules name it; or as models.dev names the provider of
//     its ID, less a note in brackets, as "StepFun (China)" is "StepFun";
//     or else by Name from its ID, as bytedance-seed is "ByteDance Seed".
//   - A family as the rules name it, or else by Name from its ID, as
//     claude-opus is "Claude Opus".
func (b *builder) names(models map[ModelID]Model) (map[ProviderID]Lab, map[string]Family) {
	labs, families := make(map[ProviderID]Lab), make(map[string]Family)
	for _, m := range models {
		if _, ok := labs[m.Lab]; !ok {
			labs[m.Lab] = Lab{ID: m.Lab, Name: b.labName(m.Lab)}
		}
		if _, ok := families[m.Family]; m.Family != "" && !ok {
			name, ok := b.rules.FamilyName(m.Family)
			if !ok {
				name = b.spell.Name(m.Family)
			}
			families[m.Family] = Family{ID: m.Family, Name: name}
		}
	}
	return labs, families
}

// labName returns the name of lab, as names names labs.
func (b *builder) labName(lab ProviderID) string {
	if name, ok := b.rules.LabName(lab); ok {
		return name
	}
	if p, ok := b.bundle.Providers[modelsdev.ProviderID(lab)]; ok && p.Name != "" {
		if before, _, ok := strings.Cut(p.Name, " ("); ok && strings.HasSuffix(p.Name, ")") {
			return before
		}
		return p.Name
	}
	return b.spell.Name(string(lab))
}

// ListModelsDev maps b, models.dev's catalog.json, to Listings, as
// FromModelsDev does before it groups them.
func ListModelsDev(b modelsdev.Bundle, opts Options) (*Listings, Report, error) {
	bl, err := list(b, opts)
	if err != nil {
		return nil, Report{}, err
	}
	return bl.out, bl.report, nil
}

// list maps b to Listings, in a builder that can then group them.
func list(b modelsdev.Bundle, opts Options) (*builder, error) {
	rules := cmp.Or(opts.Rules, DefaultRules())
	// A provider the rules exclude is none of the bundle's.
	b.Providers = maps.Clone(b.Providers)
	maps.DeleteFunc(b.Providers, func(id modelsdev.ProviderID, _ modelsdev.Provider) bool { return rules.Excluded(ProviderID(id)) })
	providers := opts.Providers
	if len(providers) == 0 {
		for id := range b.Providers {
			providers = append(providers, ProviderID(id))
		}
	}
	b.Models = CatalogModels(b.Models)
	bl := &builder{
		rules:   rules,
		bundle:  b,
		spell:   SpellingsOf(BundleNames(b)),
		infer:   opts.Infer,
		labs:    make(map[ProviderID]bool),
		merged:  make(map[ModelID]merge),
		wantPro: make(map[ModelID]bool),
		noted:   make(map[string]bool),
		report:  Report{Bases: make(map[Basis]int)},
	}
	out := &Listings{
		Models:    make(map[ModelID]Model, len(b.Models)),
		Providers: make(map[ProviderID]ListedProvider),
	}
	bl.out = out
	for id, lm := range b.Models {
		m := labModel(ModelID(id), lm, bl.spell)
		out.Models[m.ID] = m
		bl.labs[m.Lab] = true
	}
	if bl.infer {
		for lab := range bl.rules.AddedLabs() {
			bl.labs[lab] = true
		}
	}
	if bl.infer {
		bl.mergeTierModels()
		bl.index()
	}
	for _, pid := range slices.Compact(slices.Sorted(slices.Values(providers))) {
		p, ok := b.Providers[modelsdev.ProviderID(pid)]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, pid)
		}
		out.Providers[pid] = bl.provider(pid, p)
	}
	if bl.infer {
		bl.shareUnlisted()
		bl.derivePro()
		bl.dropUnknown()
	}
	for _, prov := range out.Providers {
		bl.markTiers(prov)
	}
	bl.linkModes()
	slices.SortFunc(bl.report.Tiers, func(a, b Note) int { return strings.Compare(a.String(), b.String()) })
	bl.report.Providers = len(out.Providers)
	bl.report.Models = len(out.Models)
	return bl, nil
}

// builder holds the state of one FromModelsDev call.
type builder struct {
	// rules are the mapping's rules, Options.Rules or DefaultRules.
	rules  *Rules
	bundle modelsdev.Bundle
	// spell spells the words of model IDs in names, as the names in bundle
	// most often do.
	spell Spellings
	infer bool
	out   *Listings
	labs  map[ProviderID]bool
	// merged maps the models merged into another to that model and tier.
	merged map[ModelID]merge
	// byWords holds the catalog's models by their words, as looseKey reads
	// them, as shareUnlisted finds them before it makes models of its own.
	byWords map[string][]ModelID
	// hosts holds hostWords, once read.
	hosts []string
	// noted holds the ID parts lookup has noted as several labs' models.
	noted map[string]bool
	// spellings holds how labs' models.dev IDs write their brands'
	// versions, as labSpelling reads them, by lab and brand.
	spellings map[string]spelling
	// tails maps model IDs without their lab, read as Normalize reads
	// them, to the models of that ID.
	tails map[string][]ModelID
	// wantPro holds the models whose pro mode an offering's ID names.
	wantPro map[ModelID]bool
	report  Report
}

// merge is where a merged model went: the model, and the tier it is of it.
type merge struct {
	into ModelID
	tier Tier
}

// note records n in the report.
func (b *builder) note(p ProviderID, model, format string, args ...any) {
	b.report.Notes = append(b.report.Notes, Note{Provider: p, Model: model, Text: fmt.Sprintf(format, args...)})
}

// BundleNames returns the names of b's models and of its providers' models.
func BundleNames(b modelsdev.Bundle) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, m := range b.Models {
			if !yield(m.Name) {
				return
			}
		}
		for _, p := range b.Providers {
			for _, m := range p.Models {
				if !yield(m.Name) {
					return
				}
			}
		}
	}
}

// labModel returns the catalog model of models.dev's model id, named by
// NameOf from its ID with spellings sp.
func labModel(id ModelID, lm modelsdev.LabModel, sp Spellings) Model {
	m := Model{
		ID:          id,
		Lab:         id.Lab(),
		Name:        NameOf(id, sp),
		Family:      deref(lm.Family),
		Type:        ModelType(lm.Type),
		Released:    lm.ReleaseDate,
		Updated:     lm.LastUpdated,
		Knowledge:   deref(lm.Knowledge),
		OpenWeights: lm.OpenWeights,
		License:     lm.License,
		Reasoning:   lm.Reasoning,
		Capabilities: Capabilities{
			Tools:            lm.ToolCall,
			Attachments:      lm.Attachment,
			StructuredOutput: clonePtr(lm.StructuredOutput),
			Temperature:      clonePtr(lm.Temperature),
			Input:            modalities(lm.Modalities.Input),
			Output:           modalities(lm.Modalities.Output),
		},
		Limit: limitOf(lm.Limit),
	}
	for _, w := range lm.Weights {
		m.Weights = append(m.Weights, Weight(w))
	}
	for _, bm := range lm.Benchmarks {
		m.Benchmarks = append(m.Benchmarks, Benchmark(bm))
	}
	for _, l := range lm.Links {
		m.Links = append(m.Links, Link(l))
	}
	return m
}

// provider returns the catalog provider of models.dev's provider p, its
// offerings tied to models as far as one provider's models can be.
func (b *builder) provider(pid ProviderID, p modelsdev.Provider) ListedProvider {
	kind := KindGateway
	if b.labs[pid] {
		kind = KindLab
	}
	prov := ListedProvider{
		ID:       pid,
		Name:     p.Name,
		Kind:     kind,
		Endpoint: b.endpoint(pid, "", p.NPM, "", p.API),
		Doc:      p.Doc,
		Models:   make(map[string]Listing, len(p.Models)),
	}
	for _, id := range slices.Sorted(maps.Keys(p.Models)) {
		m := p.Models[id]
		o := b.listing(prov, m)
		b.tie(pid, &o, m)
		prov.Models[o.ID] = o
		b.report.Listings++
	}
	return prov
}

// listing returns the listing of model m of provider prov, not yet tied to
// a model.
func (b *builder) listing(prov ListedProvider, m modelsdev.Model) Listing {
	o := Listing{
		ID:          string(m.ID),
		Name:        m.Name,
		Description: m.Description,
		Family:      deref(m.Family),
		Type:        ModelType(m.Type),
		Released:    m.ReleaseDate,
		Updated:     m.LastUpdated,
		Knowledge:   deref(m.Knowledge),
		OpenWeights: m.OpenWeights,
		Capabilities: Capabilities{
			Tools:            m.ToolCall,
			Attachments:      m.Attachment,
			StructuredOutput: clonePtr(m.StructuredOutput),
			Temperature:      clonePtr(m.Temperature),
			Input:            modalities(m.Modalities.Input),
			Output:           modalities(m.Modalities.Output),
		},
		Limit:     limitOf(m.Limit),
		Reasoning: reasoningOf(m),
		Price:     price(m.Cost),
	}
	if ov := m.Override; ov != nil && (ov.NPM != "" || ov.Shape != "" || ov.API != "") {
		// An override keeps the provider's base URL unless it names its
		// own, as models.dev's clients pass it to whatever package.
		p := b.bundle.Providers[modelsdev.ProviderID(prov.ID)]
		if ep := b.endpoint(prov.ID, o.ID, cmp.Or(ov.NPM, p.NPM), ov.Shape, cmp.Or(ov.API, p.API)); !ep.Equal(prov.Endpoint) {
			o.Endpoint = &ep
		}
	}
	if s := m.Lifecycle(); s != modelsdev.StatusActive {
		o.Status = Status(s)
	}
	if m.Interleaved != nil && m.Interleaved.Enabled {
		o.Interleaved = &Interleaved{Field: m.Interleaved.Field}
	}
	for _, name := range m.Modes() {
		b.addMode(&o, prov.ID, m, name)
	}
	return o
}

// tie sets the model that offering o of provider pid serves: models.dev's
// tie, moved by a merge or to a pro model, or, with inference, what rule 2
// finds.
func (b *builder) tie(pid ProviderID, o *Listing, m modelsdev.Model) {
	defer func() { b.report.Bases[cmp.Or(o.Basis, BasisNone)]++ }()
	if model, ok := b.rules.Pin(pid, o.ID); ok && b.infer {
		if _, listed := b.out.Models[model]; listed {
			o.Model, o.Basis = model, BasisRule
			return
		}
		b.note(pid, o.ID, "a rule ties it to %s, which the catalog lists no model of", model)
	}
	if c := CatalogID(m.CanonicalModelID); c != "" {
		if _, ok := b.bundle.Models[c]; ok {
			o.ModelsDev, o.Model, o.Basis = ModelID(c), ModelID(c), BasisCanonical
		} else {
			b.note(pid, o.ID, "canonical_model_id %q names no models.dev model", m.CanonicalModelID)
		}
	} else if b.labs[pid] {
		if id := ModelID(CatalogID(string(pid) + "/" + o.ID)); hasModel(b.bundle, id) {
			o.ModelsDev, o.Model, o.Basis = id, id, BasisLab
		}
	}
	if !b.infer {
		if o.Model == "" {
			o.Basis = ""
		}
		return
	}
	if mg, ok := b.merged[o.Model]; ok {
		o.Model, o.Basis, o.Tier = mg.into, BasisTier, mg.tier
		return
	}
	if o.Model != "" {
		if b.NamesPro(pid, o.ID, o.Model) {
			b.wantPro[o.Model] = true
			o.Model, o.Basis = o.Model+"-pro", BasisPro
		}
		return
	}
	if id, basis, tier := b.InferByID(pid, o.ID); id != "" {
		o.Model, o.Basis, o.Tier = id, basis, tier
	}
}

// dropUnknown leaves out the listings that neither models.dev nor the
// mapping's rules tie to a model, once the rules have made every model
// they can, and lists them in the report's Unknown.
func (b *builder) dropUnknown() {
	for _, pid := range slices.Sorted(maps.Keys(b.out.Providers)) {
		prov := b.out.Providers[pid]
		for _, id := range slices.Sorted(maps.Keys(prov.Models)) {
			if prov.Models[id].Model != "" {
				continue
			}
			delete(prov.Models, id)
			b.report.Bases[BasisNone]--
			b.report.Unknown = append(b.report.Unknown, Note{Provider: pid, Model: id, Text: "no model or lab known"})
		}
	}
}

// CatalogModels returns models with their IDs, and the keys they are
// listed by, as CatalogID writes them, whatever models.dev writes: its
// minimax/MiniMax-M2.7 is minimax/minimax-m2.7.
func CatalogModels(models map[string]modelsdev.LabModel) map[string]modelsdev.LabModel {
	out := make(map[string]modelsdev.LabModel, len(models))
	for id, m := range models {
		m.ID = CatalogID(m.ID)
		out[CatalogID(id)] = m
	}
	return out
}

// hasModel reports whether b lists model id.
func hasModel(b modelsdev.Bundle, id ModelID) bool {
	id = ModelID(CatalogID(string(id)))
	_, ok := b.Models[string(id)]
	return ok
}

// linkModes names, in each mode of a listing that runs a derived model, the
// model, as OpenAI's gpt-6.1-sol in mode pro runs openai/gpt-6.1-sol-pro.
func (b *builder) linkModes() {
	for _, prov := range b.out.Providers {
		for _, o := range prov.Models {
			for mode, opt := range o.Modes {
				if m, ok := b.out.Models[o.Model+ModelID("-"+mode)]; ok && m.Derived != nil && m.Derived.Mode == mode {
					opt.Model = m.ID
					o.Modes[mode] = opt
				}
			}
		}
	}
}

// markTiers marks the offerings of prov whose IDs name a tier of another
// offering of prov that serves the same model.
func (b *builder) markTiers(prov ListedProvider) {
	for _, id := range slices.Sorted(maps.Keys(prov.Models)) {
		o := prov.Models[id]
		base, t, ok := b.rules.CutTier(id)
		if !ok || o.Model == "" {
			continue
		}
		if bo, ok := prov.Models[base]; ok && bo.Model == o.Model {
			o.Tier, o.TierOf = t, base
			prov.Models[id] = o
			b.report.Tiers = append(b.report.Tiers, Note{Provider: prov.ID, Model: id,
				Text: fmt.Sprintf("tier %s of %s", t, base)})
		}
	}
}

// OptionKind tells what a models.dev mode becomes in the catalog.
type OptionKind int

// Option kinds.
const (
	// OptionUnknown is a mode that is neither a tier nor a reasoning mode.
	OptionUnknown OptionKind = iota
	// OptionTier is a service tier, kept in Listing.Tiers.
	OptionTier
	// OptionMode is a reasoning mode, such as pro, kept in Listing.Modes.
	OptionMode
)

// addMode adds mode name of model m to offering o as a tier or a mode.
func (b *builder) addMode(o *Listing, pid ProviderID, m modelsdev.Model, name string) {
	md, _ := m.Mode(name)
	opt := Option{Price: modePrice(md.Cost, m.Cost)}
	if ov := md.Override; ov != nil {
		if len(ov.Body) > 0 {
			opt.Body = cloneBody(ov.Body)
		}
		if len(ov.Headers) > 0 {
			opt.Headers = maps.Clone(ov.Headers)
		}
	}
	kind, key := ClassifyMode(name, opt.Body)
	switch kind {
	case OptionTier:
		if _, ok := o.Tiers[Tier(key)]; ok {
			b.note(pid, o.ID, "modes set tier %s twice; mode %q wins", key, name)
		}
		setOption(&o.Tiers, Tier(key), opt)
	case OptionMode:
		setOption(&o.Modes, key, opt)
	default:
		b.note(pid, o.ID, "mode %q is neither a tier nor a reasoning mode; left out", name)
	}
}

// setOption sets (*opts)[k] to o, making the map when it is nil.
func setOption[K comparable](opts *map[K]Option, k K, o Option) {
	if *opts == nil {
		*opts = make(map[K]Option)
	}
	(*opts)[k] = o
}

// ClassifyMode tells what the models.dev mode named name, which sets body
// in a request, becomes: a service tier, as OptionTier and the tier, when
// body sets service_tier (at its top or within Bedrock's
// additionalModelRequestFields) or speed, or when name is a tier's; a
// reasoning mode, as OptionMode and its name, when body sets
// reasoning.mode; or neither, as OptionUnknown and name.
func ClassifyMode(name string, body map[string]any) (kind OptionKind, key string) {
	if extra, ok := body["additionalModelRequestFields"].(map[string]any); ok {
		if st, ok := extra["service_tier"].(string); ok {
			return OptionTier, string(TierOfServiceTier(st))
		}
	}
	if st, ok := body["service_tier"].(string); ok {
		return OptionTier, string(TierOfServiceTier(st))
	}
	if speed, ok := body["speed"].(string); ok && speed == "fast" {
		return OptionTier, string(TierFast)
	}
	if r, ok := body["reasoning"].(map[string]any); ok {
		if mode, ok := r["mode"].(string); ok && mode != "" {
			return OptionMode, mode
		}
	}
	switch name {
	case "fast", "priority":
		return OptionTier, string(TierFast)
	case "flex":
		return OptionTier, string(TierFlex)
	case "ultrafast":
		return OptionTier, string(TierUltrafast)
	}
	return OptionUnknown, name
}

// TierOfServiceTier returns the tier that an OpenAI-style service_tier
// value selects: priority and fast are TierFast, and any other value is the
// tier of its own name.
func TierOfServiceTier(st string) Tier {
	switch st {
	case "priority", "fast":
		return TierFast
	}
	return Tier(st)
}

// modalities converts models.dev modalities.
func modalities(in []modelsdev.Modality) []Modality {
	out := make([]Modality, len(in))
	for i, m := range in {
		out[i] = Modality(m)
	}
	return out
}

// limitOf converts models.dev token limits.
func limitOf(l modelsdev.Limit) Limit {
	out := Limit{Context: int(l.Context), Output: int(l.Output)}
	if l.Input != nil {
		out.Input = int(*l.Input)
	}
	return out
}

// reasoningOf returns the reasoning controls models.dev lists for m, or nil
// when m does not reason.
func reasoningOf(m modelsdev.Model) *Reasoning {
	if !m.Reasoning {
		return nil
	}
	r := &Reasoning{}
	for _, o := range m.ReasoningOptions {
		switch o.Type {
		case modelsdev.ReasoningEffort:
			for _, v := range o.Values {
				if v == nil || *v == "default" || *v == "" || slices.Contains(r.Efforts, Effort(*v)) {
					continue
				}
				r.Efforts = append(r.Efforts, Effort(*v))
			}
		case modelsdev.ReasoningToggle:
			r.Toggle = true
		case modelsdev.ReasoningBudgetTokens:
			bud := &Budget{}
			if o.Min != nil {
				bud.Min = int(*o.Min)
			}
			if o.Max != nil {
				bud.Max = int(*o.Max)
			}
			r.Budget = bud
		}
	}
	return r
}

// price returns the price models.dev lists as c, or nil when it lists none.
// A catalog without context tiers can list the price of prompts over
// 200,000 tokens apart, which becomes a tier.
func price(c *modelsdev.Cost) *Price {
	if c == nil {
		return nil
	}
	p := &Price{Rates: rates(c.Price)}
	for _, t := range c.Tiers {
		if t.Tier.Type == modelsdev.TierContext {
			p.Tiers = append(p.Tiers, PriceTier{Above: int(t.Tier.Size), Rates: rates(t.Price)})
		}
	}
	if len(c.Tiers) == 0 && c.ContextOver200K != nil {
		p.Tiers = append(p.Tiers, PriceTier{Above: 200_000, Rates: rates(*c.ContextOver200K)})
	}
	slices.SortFunc(p.Tiers, func(a, b PriceTier) int { return cmp.Compare(a.Above, b.Above) })
	return p
}

// modePrice returns the price of a mode that models.dev lists as c, on a
// model it lists as base. A mode without long-prompt prices takes base's,
// scaled as the mode scales the short-prompt prices, marked estimated.
func modePrice(c, base *modelsdev.Cost) *Price {
	p := price(c)
	bp := price(base)
	if p == nil || bp == nil || len(p.Tiers) > 0 {
		return p
	}
	for _, t := range bp.Tiers {
		p.Tiers = append(p.Tiers, PriceTier{
			Above:     t.Above,
			Rates:     scaleRates(t.Rates, bp.Rates, p.Rates),
			Estimated: true,
		})
	}
	return p
}

// scaleRates returns tier scaled by the ratio of mode to base, rate by rate.
// A rate the mode does not list is not listed.
func scaleRates(tier, base, mode Rates) Rates {
	return Rates{
		Input:       scale(tier.Input, base.Input, mode.Input),
		Output:      scale(tier.Output, base.Output, mode.Output),
		CacheRead:   scalePtr(tier.CacheRead, base.CacheRead, mode.CacheRead),
		CacheWrite:  scalePtr(tier.CacheWrite, base.CacheWrite, mode.CacheWrite),
		Reasoning:   scalePtr(tier.Reasoning, base.Reasoning, mode.Reasoning),
		InputAudio:  scalePtr(tier.InputAudio, base.InputAudio, mode.InputAudio),
		OutputAudio: scalePtr(tier.OutputAudio, base.OutputAudio, mode.OutputAudio),
	}
}

// scale returns tier * mode / base, or mode when base is zero, rounded to
// nine decimal places so that exact ratios stay exact.
func scale(tier, base, mode float64) float64 {
	if base == 0 {
		return mode
	}
	return math.Round(tier*mode/base*1e9) / 1e9
}

// scalePtr is scale for rates that may be unlisted.
func scalePtr(tier, base, mode *float64) *float64 {
	if tier == nil || mode == nil {
		return nil
	}
	v := scale(*tier, deref(base), *mode)
	return &v
}

// rates converts a models.dev price.
func rates(p modelsdev.Price) Rates {
	return Rates{
		Input:       p.Input,
		Output:      p.Output,
		CacheRead:   clonePtr(p.CacheRead),
		CacheWrite:  clonePtr(p.CacheWrite),
		Reasoning:   clonePtr(p.Reasoning),
		InputAudio:  clonePtr(p.InputAudio),
		OutputAudio: clonePtr(p.OutputAudio),
	}
}

// cloneBody returns a deep copy of a request body.
func cloneBody(body map[string]any) map[string]any {
	out := make(map[string]any, len(body))
	for k, v := range body {
		out[k] = cloneValue(v)
	}
	return out
}

// cloneValue returns a deep copy of a decoded JSON value.
func cloneValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return cloneBody(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = cloneValue(e)
		}
		return out
	}
	return v
}

// clonePtr returns a pointer to a copy of *p, or nil when p is nil.
func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// deref returns *p, or the zero value when p is nil.
func deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
