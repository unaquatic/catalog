package gateway

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/unaquatic/catalog/modelsdev"
)

// RulesVersion is the version of the rules document format DecodeRules
// reads.
const RulesVersion = 1

// ErrRules reports a rules document the mapping cannot use.
var ErrRules = errors.New("rules")

//go:embed rules.json
var rulesJSON []byte

// DefaultRules returns the rules built in, rules.json's.
var DefaultRules = sync.OnceValue(func() *Rules {
	r, err := DecodeRules(rulesJSON)
	if err != nil {
		panic("gateway: rules.json: " + err.Error())
	}
	return r
})

// Rules are the mapping's rules, as one list: each rule matches part of a
// provider's model ID, or a listing, and says one thing of what it
// matches. Of rules that say the same kind of thing of the same match, the
// first wins. The mapping's steps, which FromModelsDev describes, ask the
// rules; the steps themselves, and their order, are the mapping's own.
type Rules struct {
	Version int    `json:"version"`
	Rules   []Rule `json:"rules"`

	makers   map[string]ProviderID
	added    []ProviderID
	products []Rule
	brands   []Rule
	words    []Rule
	segments map[string]bool
	prefixes map[ProviderID][]string
	services []Rule
	tunes    []Rule
	bases    map[string]FinetuneBase
	suffixes []Rule
	regions  []string
	listed   []ProviderID
	excluded []ProviderID
	seps     map[ProviderID][]string
	pins     []Rule
	labNames map[ProviderID]string
	famNames map[string]string
}

// Rule is one rule: what it matches, and the one thing it says of it, by
// one of the fields after Match. Why says why, for its reader.
type Rule struct {
	Match Match `json:"match,omitzero"`
	// Lab is the lab that Match's maker, product, brand or word names.
	Lab ProviderID `json:"lab,omitempty"`
	// Open, with Lab, marks a brand or word of weights others fine-tune.
	Open bool `json:"open,omitempty"`
	// AddLab is a lab models.dev lists no models of, which providers'
	// model IDs name as their maker. It matches nothing.
	AddLab ProviderID `json:"add_lab,omitempty"`
	// Skip reads past Match's segment when looking for a model ID's maker.
	Skip bool `json:"skip,omitempty"`
	// Strip removes Match's prefix from the model part of the provider's
	// model IDs.
	Strip bool `json:"strip,omitempty"`
	// Service leaves the listing out of the catalog, as no model but a
	// service of this kind.
	Service Service `json:"service,omitempty"`
	// Finetune makes the listing a fine-tune of this kind, of Base when it
	// is set: the model it tunes, as its maker names it.
	Finetune TuneKind `json:"finetune,omitempty"`
	Base     string   `json:"base,omitempty"`
	// Tier, Mode and Alias say what Match's suffix adds to a model ID: a
	// service tier, a reasoning mode, or nothing that makes it another
	// model, as "-nothink".
	Tier  Tier   `json:"tier,omitempty"`
	Mode  string `json:"mode,omitempty"`
	Alias bool   `json:"alias,omitempty"`
	// Regions are the region prefixes clouds put before model IDs, in the
	// order Bedrock keeps one listing of each model by; "" is no prefix.
	// It matches nothing.
	Regions []string `json:"regions,omitempty"`
	// Listed keeps Match's providers in a catalog of the providers listed.
	Listed bool `json:"listed,omitempty"`
	// Exclude leaves Match's providers out of every catalog, as if
	// models.dev listed none of them.
	Exclude bool `json:"exclude,omitempty"`
	// Separator is what Match's providers write between the parts of their
	// model IDs besides a slash, as SAP AI Core's "--".
	Separator string `json:"separator,omitempty"`
	// Model ties Match's listing to this model, which the catalog lists,
	// whatever the other rules say.
	Model ModelID `json:"model,omitempty"`
	// Name names Match's lab or family in the catalog, where the name the
	// mapping would give it is wrong, as TheDrummer of thedrummer.
	Name string `json:"name,omitempty"`
	Why  string `json:"why,omitempty"`
}

// Match is what a rule matches. A rule matches when all that is set does.
type Match struct {
	// Provider is a provider ID, or a list of them.
	Provider Strings `json:"provider,omitempty"`
	// Listing is a provider's model ID, in any case.
	Listing string `json:"listing,omitempty"`
	// ID, Model, Name and Description match a provider's model ID, the
	// part of it that names the model, the listing's name, and its
	// description.
	ID          *regexp.Regexp `json:"id,omitempty"`
	Model       *regexp.Regexp `json:"model,omitempty"`
	Name        *regexp.Regexp `json:"name,omitempty"`
	Description *regexp.Regexp `json:"description,omitempty"`
	// Maker is the part of a model ID before the model's that names its
	// maker, in lower case: "meta-llama" of "meta-llama/llama-4-scout".
	Maker string `json:"maker,omitempty"`
	// Product and Brand are the first word of a model's name, followed by
	// its end or by a dash, a dot or a digit; a product, by an underscore
	// too.
	Product string `json:"product,omitempty"`
	Brand   string `json:"brand,omitempty"`
	// Word matches a whole word of a model's name, in lower case.
	Word *regexp.Regexp `json:"word,omitempty"`
	// Segment is a part of a model ID, between slashes, in lower case.
	Segment string `json:"segment,omitempty"`
	// Prefix and Suffix start the model part of a model ID, and end it.
	Prefix string `json:"prefix,omitempty"`
	Suffix string `json:"suffix,omitempty"`
	// Lab and Family are a lab of the catalog's models and a family, by
	// the IDs models' Lab and Family hold; only a name matches them.
	Lab    ProviderID `json:"lab,omitempty"`
	Family string     `json:"family,omitempty"`
}

// Strings is a list of strings, which JSON may write as one string alone.
type Strings []string

// UnmarshalJSON reads a string or a list of them.
func (s *Strings) UnmarshalJSON(data []byte) error {
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(`"`)) {
		var one string
		if err := json.Unmarshal(data, &one); err != nil {
			return err
		}
		*s = Strings{one}
		return nil
	}
	return json.Unmarshal(data, (*[]string)(s))
}

// DecodeRules reads and checks a rules document. It fails, wrapping
// ErrRules, for another version, or a rule that says nothing, more than one
// thing, or something its match cannot say it of.
func DecodeRules(data []byte) (*Rules, error) {
	var r Rules
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrRules, err)
	}
	if r.Version != RulesVersion {
		return nil, fmt.Errorf("%w: version %d, want %d", ErrRules, r.Version, RulesVersion)
	}
	r.makers, r.segments, r.prefixes, r.bases, r.seps = make(map[string]ProviderID), make(map[string]bool),
		make(map[ProviderID][]string), make(map[string]FinetuneBase), make(map[ProviderID][]string)
	r.labNames, r.famNames = make(map[ProviderID]string), make(map[string]string)
	for i, rule := range r.Rules {
		if err := r.add(rule); err != nil {
			return nil, fmt.Errorf("%w: rule %d: %w", ErrRules, i+1, err)
		}
	}
	return &r, nil
}

// add checks rule and indexes it.
func (r *Rules) add(rule Rule) error {
	m := rule.Match
	says := 0
	for _, set := range []bool{rule.Lab != "", rule.AddLab != "", rule.Skip, rule.Strip, rule.Service != "",
		rule.Finetune != "", rule.Tier != "" || rule.Mode != "" || rule.Alias, rule.Regions != nil, rule.Listed, rule.Exclude,
		rule.Separator != "", rule.Model != "", rule.Name != ""} {
		if set {
			says++
		}
	}
	if says != 1 {
		return fmt.Errorf("says %d things; a rule says one", says)
	}
	if (m.Lab != "" || m.Family != "") && rule.Name == "" {
		return errors.New("a match of lab or family is for a name")
	}
	only := func(what string, ok ...bool) error {
		if !slices.Contains(ok, true) {
			return fmt.Errorf("%s needs a match of %s", ruleKind(rule), what)
		}
		return nil
	}
	switch {
	case rule.Lab != "":
		switch {
		case m.Maker != "":
			r.makers[m.Maker] = rule.Lab
		case m.Product != "":
			r.products = append(r.products, rule)
		case m.Brand != "":
			r.brands = append(r.brands, rule)
		case m.Word != nil:
			r.words = append(r.words, rule)
		default:
			return only("maker, product, brand or word")
		}
		if rule.Open && m.Brand == "" && m.Word == nil {
			return errors.New("open marks a brand or a word")
		}
	case rule.AddLab != "":
		r.added = append(r.added, rule.AddLab)
	case rule.Skip:
		if err := only("segment", m.Segment != ""); err != nil {
			return err
		}
		r.segments[m.Segment] = true
	case rule.Strip:
		if err := only("prefix", m.Prefix != ""); err != nil {
			return err
		}
		for _, p := range m.Provider {
			r.prefixes[ProviderID(p)] = append(r.prefixes[ProviderID(p)], m.Prefix)
		}
		if len(m.Provider) == 0 {
			r.prefixes[""] = append(r.prefixes[""], m.Prefix)
		}
	case rule.Service != "":
		if err := only("id, name or description", m.ID != nil, m.Name != nil, m.Description != nil); err != nil {
			return err
		}
		r.services = append(r.services, rule)
	case rule.Finetune != "":
		if m.Listing != "" {
			r.bases[strings.ToLower(m.Listing)] = FinetuneBase{Base: rule.Base, Kind: rule.Finetune}
			break
		}
		if err := only("listing, id, name, model or description", m.ID != nil, m.Name != nil, m.Model != nil, m.Description != nil); err != nil {
			return err
		}
		r.tunes = append(r.tunes, rule)
	case rule.Tier != "" || rule.Mode != "" || rule.Alias:
		if err := only("suffix", m.Suffix != ""); err != nil {
			return err
		}
		if rule.Tier != "" && !slices.Contains(Tiers, rule.Tier) || rule.Mode != "" && !slices.Contains(Modes, rule.Mode) {
			return fmt.Errorf("tier %q or mode %q is none the catalog lists", rule.Tier, rule.Mode)
		}
		r.suffixes = append(r.suffixes, rule)
	case rule.Regions != nil:
		r.regions = rule.Regions
	case rule.Listed:
		if err := only("provider", len(m.Provider) > 0); err != nil {
			return err
		}
		for _, p := range m.Provider {
			r.listed = append(r.listed, ProviderID(p))
		}
	case rule.Separator != "":
		if err := only("provider", len(m.Provider) > 0); err != nil {
			return err
		}
		for _, p := range m.Provider {
			r.seps[ProviderID(p)] = append(r.seps[ProviderID(p)], rule.Separator)
		}
	case rule.Exclude:
		if err := only("provider", len(m.Provider) > 0); err != nil {
			return err
		}
		for _, p := range m.Provider {
			r.excluded = append(r.excluded, ProviderID(p))
		}
	case rule.Model != "":
		if err := only("listing", m.Listing != ""); err != nil {
			return err
		}
		r.pins = append(r.pins, rule)
	case rule.Name != "":
		if err := only("lab or family", (m.Lab != "") != (m.Family != "")); err != nil {
			return err
		}
		if m.Lab != "" {
			if _, ok := r.labNames[m.Lab]; !ok {
				r.labNames[m.Lab] = rule.Name
			}
		} else if _, ok := r.famNames[m.Family]; !ok {
			r.famNames[m.Family] = rule.Name
		}
	}
	return nil
}

// ruleKind names what rule says, for errors.
func ruleKind(rule Rule) string {
	switch {
	case rule.Service != "":
		return "a service"
	case rule.Finetune != "":
		return "a fine-tune"
	case rule.Skip:
		return "skip"
	case rule.Strip:
		return "strip"
	case rule.Listed:
		return "listed"
	case rule.Exclude:
		return "exclude"
	case rule.Separator != "":
		return "a separator"
	case rule.Model != "":
		return "a model"
	case rule.Name != "":
		return "a name"
	}
	return "a suffix"
}

// MakerLab returns the lab that maker, a model ID's maker part in lower
// case, is another name of, and reports whether it is one.
func (r *Rules) MakerLab(maker string) (ProviderID, bool) {
	lab, ok := r.makers[maker]
	return lab, ok
}

// LabName returns the name the rules give lab, and reports whether they
// give it one.
func (r *Rules) LabName(lab ProviderID) (string, bool) {
	n, ok := r.labNames[lab]
	return n, ok
}

// FamilyName returns the name the rules give family, and reports whether
// they give it one.
func (r *Rules) FamilyName(family string) (string, bool) {
	n, ok := r.famNames[family]
	return n, ok
}

// AddedLabs returns the labs the rules add to models.dev's.
func (r *Rules) AddedLabs() iter.Seq[ProviderID] { return slices.Values(r.added) }

// Added reports whether the rules add lab to models.dev's.
func (r *Rules) Added(lab ProviderID) bool { return slices.Contains(r.added, lab) }

// starts reports whether s starts with word, followed by its end or by one
// of after.
func starts(s, word, after string) bool {
	rest, ok := strings.CutPrefix(s, word)
	return ok && (rest == "" || strings.ContainsRune(after, rune(rest[0])))
}

// ProductLabs returns the labs whose products model ID part s, in lower
// case, starts with, the first rule's first.
func (r *Rules) ProductLabs(s string) iter.Seq[ProviderID] {
	return func(yield func(ProviderID) bool) {
		for _, rule := range r.products {
			if starts(s, rule.Match.Product, "-_.0123456789") && !yield(rule.Lab) {
				return
			}
		}
	}
}

// BrandLab returns the lab of the first brand that model ID part s, in
// lower case, starts with, and reports whether it starts with one.
func (r *Rules) BrandLab(s string) (ProviderID, bool) {
	for _, rule := range r.brands {
		if starts(s, rule.Match.Brand, "-.0123456789") {
			return rule.Lab, true
		}
	}
	return "", false
}

// NamesBrand reports whether s is a brand's or a product's word, as a
// provider's ID may be.
func (r *Rules) NamesBrand(s string) bool {
	return slices.ContainsFunc(r.brands, func(rule Rule) bool { return rule.Match.Brand == s }) ||
		slices.ContainsFunc(r.products, func(rule Rule) bool { return rule.Match.Product == s })
}

// OpenLab returns the lab of the open-weight model that w, a word of a
// model's name in lower case, names, by the first rule of an open word or
// brand that matches it, and reports whether one does: "l3.3" is meta's,
// and "qwen2.5" alibaba's.
func (r *Rules) OpenLab(w string) (ProviderID, bool) {
	for _, rule := range r.words {
		if rule.Open && rule.Match.Word.MatchString(w) {
			return rule.Lab, true
		}
	}
	for _, rule := range r.brands {
		if rule.Open && starts(w, rule.Match.Brand, "0123456789.xv") {
			return rule.Lab, true
		}
	}
	return "", false
}

// HostSegment reports whether s, a part of a model ID in lower case, names
// a host, a gateway or a folder rather than a maker.
func (r *Rules) HostSegment(s string) bool { return r.segments[s] }

// HostPrefixes returns the words provider pid puts before the model in its
// model IDs, which the rules strip.
func (r *Rules) HostPrefixes(pid ProviderID) iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, p := range slices.Concat(r.prefixes[pid], r.prefixes[""]) {
			if !yield(p) {
				return
			}
		}
	}
}

// FinetuneBase returns what the rules say listing id, a provider's model
// ID, tunes, and reports whether they say.
func (r *Rules) FinetuneBase(id string) (FinetuneBase, bool) {
	fb, ok := r.bases[strings.ToLower(id)]
	return fb, ok
}

// FinetuneWord reports whether a fine-tune rule matches provider's model
// ID id or name name.
func (r *Rules) FinetuneWord(id, name string) bool {
	return slices.ContainsFunc(r.tunes, func(rule Rule) bool {
		m := rule.Match
		return rule.Finetune != TuneQuantized && (m.ID != nil && m.ID.MatchString(id) || m.Name != nil && m.Name.MatchString(name))
	})
}

// FinetuneDescription reports whether a fine-tune rule matches description
// desc.
func (r *Rules) FinetuneDescription(desc string) bool {
	return slices.ContainsFunc(r.tunes, func(rule Rule) bool {
		return rule.Match.Description != nil && rule.Match.Description.MatchString(desc)
	})
}

// Quantized reports whether a quantized build's rule matches model, the
// part of a provider's model ID that names the model.
func (r *Rules) Quantized(model string) bool {
	return slices.ContainsFunc(r.tunes, func(rule Rule) bool {
		return rule.Finetune == TuneQuantized && rule.Match.Model != nil && rule.Match.Model.MatchString(model)
	})
}

// ServiceOf returns the Service that the first service rule matching a
// provider's model ID id, named name and described by description says it
// is, or "" for a model.
func (r *Rules) ServiceOf(id, name, description string) Service {
	for _, rule := range r.services {
		m := rule.Match
		if (m.ID == nil || m.ID.MatchString(id)) && (m.Name == nil || m.Name.MatchString(name)) &&
			(m.Description == nil || m.Description.MatchString(description)) {
			return rule.Service
		}
	}
	return ""
}

// IsRouter reports whether a provider's model ID id, named name, is a
// router by its ID or name alone.
func (r *Rules) IsRouter(id, name string) bool { return r.ServiceOf(id, name, "") == ServiceRouter }

// Suffixes returns the suffixes the rules give a tier, a mode or an alias,
// with what each says, the first rule's first.
func (r *Rules) Suffixes() iter.Seq2[string, Rule] {
	return func(yield func(string, Rule) bool) {
		for _, rule := range r.suffixes {
			if !yield(rule.Match.Suffix, rule) {
				return
			}
		}
	}
}

// FreeSuffixes, ThinkingSuffixes and NoThinkingSuffixes return the suffixes
// of the free tier, the thinking mode, and the model with thinking off.
func (r *Rules) FreeSuffixes() []string {
	return r.suffixesOf(func(rule Rule) bool { return rule.Tier == TierFree })
}

// ThinkingSuffixes returns the suffixes of the thinking mode.
func (r *Rules) ThinkingSuffixes() []string {
	return r.suffixesOf(func(rule Rule) bool { return rule.Mode == ModeThinking })
}

// NoThinkingSuffixes returns the suffixes of a model with thinking off.
func (r *Rules) NoThinkingSuffixes() []string {
	return r.suffixesOf(func(rule Rule) bool { return rule.Alias })
}

// suffixesOf returns the suffixes of the rules keep keeps.
func (r *Rules) suffixesOf(keep func(Rule) bool) []string {
	var out []string
	for s, rule := range r.Suffixes() {
		if keep(rule) {
			out = append(out, s)
		}
	}
	return out
}

// CutTier returns id without a tier suffix other than free, and the tier,
// and reports whether id ends in one.
func (r *Rules) CutTier(id string) (base string, tier Tier, ok bool) {
	for s, rule := range r.Suffixes() {
		if rule.Tier != "" && rule.Tier != TierFree {
			if base, ok := strings.CutSuffix(id, s); ok && base != "" {
				return base, rule.Tier, true
			}
		}
	}
	return id, "", false
}

// Regions returns the region prefixes clouds put before model IDs, in the
// order Bedrock keeps one listing of each model by.
func (r *Rules) Regions() iter.Seq[string] { return slices.Values(r.regions) }

// BedrockScope returns the region scope of a Bedrock model ID, such as
// "global" or "us", or "" for an in-region ID.
func (r *Rules) BedrockScope(id string) string {
	if p, _, ok := strings.Cut(id, "."); ok && p != "" && slices.Contains(r.regions, p) {
		return p
	}
	return ""
}

// StripRegion returns s, a normalized model ID, without a region prefix and
// its dash, the longest.
func (r *Rules) StripRegion(s string) string {
	best := ""
	for _, p := range r.regions {
		if p != "" && len(p) > len(best) && strings.HasPrefix(s, p+"-") {
			best = p
		}
	}
	if best == "" {
		return s
	}
	return s[len(best)+1:]
}

// IDParts returns the parts of provider pid's model ID id, between slashes,
// without a leading "~" or "@", and between the separators the rules give
// pid: SAP AI Core's "anthropic--claude-3-haiku" is anthropic and
// claude-3-haiku, where another provider's would be one part.
func (r *Rules) IDParts(pid ProviderID, id string) []string {
	id = strings.TrimLeft(id, "~@")
	for _, s := range r.seps[pid] {
		id = strings.ReplaceAll(id, s, "/")
	}
	return strings.FieldsFunc(id, func(c rune) bool { return c == '/' })
}

// ModelPart returns the last of IDParts, which names the model, or "" for
// an ID of none.
func (r *Rules) ModelPart(pid ProviderID, id string) string {
	parts := r.IDParts(pid, id)
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// Excluded reports whether the rules leave provider pid out of every
// catalog.
func (r *Rules) Excluded(pid ProviderID) bool { return slices.Contains(r.excluded, pid) }

// Listed returns the providers a catalog of the providers listed keeps.
func (r *Rules) Listed() iter.Seq[ProviderID] { return slices.Values(r.listed) }

// ListedIn returns the providers the rules list that b lists, in order, and
// those it does not, as models.dev drops providers now and then.
func (r *Rules) ListedIn(b modelsdev.Bundle) (keep, missing []ProviderID) {
	for _, id := range slices.Sorted(r.Listed()) {
		if _, ok := b.Providers[modelsdev.ProviderID(id)]; ok {
			keep = append(keep, id)
		} else {
			missing = append(missing, id)
		}
	}
	return keep, missing
}

// Pin returns the model the first rule that ties provider pid's listing id
// to one names, and reports whether a rule does.
func (r *Rules) Pin(pid ProviderID, id string) (ModelID, bool) {
	for _, rule := range r.pins {
		m := rule.Match
		if strings.EqualFold(m.Listing, id) && (len(m.Provider) == 0 || slices.Contains(m.Provider, string(pid))) {
			return rule.Model, true
		}
	}
	return "", false
}
