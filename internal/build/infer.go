package build

import (
	"cmp"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/modelsdev"
)

// Normalize reads a model ID, or a part of one, as the mapping compares
// them: in lower case, without a leading "~", without a ":variant" or
// "@deployment" suffix, and with dots and underscores read as dashes, so
// that "anthropic/claude-opus-4.8:free" and "claude-opus-4-8" compare
// alike.
func Normalize(s string) string {
	s = strings.TrimPrefix(strings.ToLower(s), "~")
	if i := strings.IndexAny(s, ":@"); i >= 0 {
		s = s[:i]
	}
	return strings.NewReplacer(".", "-", "_", "-").Replace(s)
}

// candidate is a reading of a provider's model ID: the lab it names, if it
// names one, and the model ID within it, normalized.
type candidate struct {
	lab  gateway.ProviderID
	tail string
}

// labOf returns the lab that s, a part of a model ID, names, and reports
// whether it names one.
func (b *builder) labOf(s string) (gateway.ProviderID, bool) {
	s = strings.ToLower(strings.TrimPrefix(s, "~"))
	if b.labs[gateway.ProviderID(s)] {
		return gateway.ProviderID(s), true
	}
	lab, ok := b.rules.MakerLab(s)
	return lab, ok && b.labs[lab]
}

// candidates returns the readings of a provider's model ID id, most
// specific first. An ID with slashes names its model in its last part and
// may name the lab in an earlier one, as openai/gpt-6-astra and
// auto/openai/gpt-6-astra do. An ID without slashes may start with the lab
// and a dash, as DigitalOcean's openai-gpt-6-astra does, after a region.
// Every ID is also read as a model ID of no particular lab.
func (b *builder) candidates(pid gateway.ProviderID, id string) []candidate {
	segs := b.rules.IDParts(pid, id)
	if len(segs) == 0 {
		return nil
	}
	tail := Normalize(segs[len(segs)-1])
	var out []candidate
	if len(segs) > 1 {
		for _, s := range slices.Backward(b.prefixParts(pid, id)) {
			if lab, ok := b.labOf(s); ok {
				out = append(out, candidate{lab: lab, tail: tail})
				break
			}
		}
	} else {
		// The longest prefix that names a lab wins, so that
		// bytedance-seed-seedance-2-0 is bytedance-seed's seedance-2-0.
		t := b.rules.StripRegion(tail)
		for i := len(t) - 1; i > 0; i-- {
			if t[i] != '-' {
				continue
			}
			if lab, ok := b.labOf(t[:i]); ok {
				out = append(out, candidate{lab: lab, tail: t[i+1:]})
				break
			}
		}
	}
	return append(out, candidate{tail: tail})
}

// index builds the index of model IDs without their lab that rule 2 looks
// models up in. It holds merged models too, so that an ID naming one
// finds it, and then its merge.
func (b *builder) index() {
	b.tails = make(map[string][]gateway.ModelID)
	for id := range b.bundle.Models {
		_, tail := SplitModelID(gateway.ModelID(id))
		k := Normalize(tail)
		b.tails[k] = append(b.tails[k], gateway.ModelID(id))
	}
	for k := range b.tails {
		slices.Sort(b.tails[k])
	}
}

// lookup returns the model whose ID within its lab reads as tail, at lab
// when lab is set, and reports whether one does. Of models of several labs,
// the first lab's, in byte order, is found, with a note.
func (b *builder) lookup(lab gateway.ProviderID, tail string) (gateway.ModelID, bool) {
	var found []gateway.ModelID
	for _, id := range b.tails[tail] {
		if lab == "" || id.Lab() == lab {
			found = append(found, id)
		}
	}
	if len(found) == 0 {
		return "", false
	}
	if len(found) > 1 && !b.noted[tail] {
		b.noted[tail] = true
		b.note(found[0].Lab(), tail, "an ID of models of several labs, %v; %s's taken", found, found[0].Lab())
	}
	return found[0], true
}

// language reports whether model id is a language model: of no special
// type, and writing text, as video models such as Seedance 2.0 do not,
// whose "-fast" variant is a model of its own.
func (b *builder) language(id gateway.ModelID) bool {
	m, ok := b.out.Models[id]
	return ok && m.Type == "" && slices.Contains(m.Capabilities.Output, "text")
}

// proMode reports whether the lab of model id offers it in a pro mode.
func (b *builder) proMode(id gateway.ModelID) bool {
	lab, tail := SplitModelID(id)
	_, ok := b.bundle.Providers[modelsdev.ProviderID(lab)].Models[modelsdev.ModelID(tail)].Mode("pro")
	return ok
}

// NamesPro reports whether a provider's model ID id, which models.dev ties
// to model m, names m's pro mode: it ends in "-pro" where m's ID does not,
// and m's lab offers m in a pro mode. The lab's mode keeps "Pro" models of
// other labs, such as Gemini 3.1 Pro, where models.dev ties them.
func (b *builder) NamesPro(pid gateway.ProviderID, id string, m gateway.ModelID) bool {
	_, tail := SplitModelID(m)
	return strings.HasSuffix(Normalize(b.rules.ModelPart(pid, id)), "-pro") &&
		!strings.HasSuffix(Normalize(tail), "-pro") && b.proMode(m)
}

// InferByID returns the model that a provider's model ID id names, by rule
// 2 of FromModelsDev, and how it found it: by the ID itself (BasisID), as a
// tier of a language model (BasisTier, with the tier), or as the pro mode
// of a model whose lab offers that mode (BasisPro, the derived model). It
// returns an empty ID when no rule applies.
func (b *builder) InferByID(pid gateway.ProviderID, id string) (model gateway.ModelID, basis Basis, tier gateway.Tier) {
	for _, c := range b.candidates(pid, id) {
		if m, ok := b.lookup(c.lab, c.tail); ok {
			if mg, ok := b.merged[m]; ok {
				return mg.into, BasisTier, mg.tier
			}
			return m, BasisID, ""
		}
		if base, t, ok := b.rules.CutTier(c.tail); ok {
			if m, ok := b.lookup(c.lab, base); ok && b.language(m) {
				return m, BasisTier, t
			}
		}
		if base, ok := strings.CutSuffix(c.tail, "-pro"); ok {
			if m, ok := b.lookup(c.lab, base); ok && b.proMode(m) {
				b.wantPro[m] = true
				return m + "-pro", BasisPro, ""
			}
		}
	}
	return "", "", ""
}

// mergeTierModels merges each model whose ID is another language model's
// of the same lab with a tier suffix into the other, by rule 1.
func (b *builder) mergeTierModels() {
	for _, id := range slices.Sorted(maps.Keys(b.out.Models)) {
		base, t, ok := b.rules.CutTier(string(id))
		if !ok {
			continue
		}
		into, ok := b.out.Models[gateway.ModelID(base)]
		if !ok || !b.language(into.ID) || !b.language(id) {
			continue
		}
		b.merged[id] = merge{into: into.ID, tier: t}
		into.Merged = append(into.Merged, id)
		b.out.Models[into.ID] = into
		delete(b.out.Models, id)
		b.report.Merged = append(b.report.Merged, id)
	}
}

// derivePro derives a model for the pro mode of every model whose lab, or
// any provider, offers it in that mode, or whose pro mode an offering's ID
// names, unless models.dev lists one, by rule 4.
func (b *builder) derivePro() {
	want := maps.Clone(b.wantPro)
	for _, prov := range b.out.Providers {
		for _, o := range prov.Models {
			if _, ok := o.Modes["pro"]; ok && o.Model != "" {
				want[o.Model] = true
			}
		}
	}
	for _, id := range slices.Sorted(maps.Keys(want)) {
		base, ok := b.out.Models[id]
		pro := id + "-pro"
		if _, exists := b.out.Models[pro]; !ok || exists {
			continue
		}
		m := base
		m.ID = pro
		m.Name = NameOf(pro, b.spell)
		m.Merged, m.Links, m.Weights = nil, nil, nil
		b.out.Models[pro] = m
		b.report.Origins[pro] = Origin{Derived: &Derivation{From: id, Mode: "pro"}}
		b.report.Derived = append(b.report.Derived, pro)
	}
}

// hostPrefix returns the word before the model in provider pid's model ID
// part model, with its dash, or "" when it has none: one the rules strip
// at pid; pid's own ID, when pid is no lab, before a letter, as Bee's in
// "bee-buzz"; or another provider's, of one that is no lab and names no
// brand, before a brand or a lab's name, as Databricks' in
// "databricks-gpt-oss-120b". A lab's or a brand's name is the model's own, as
// in "mistral-medium-2508" and "nova-pro", and so is a provider's ID before
// other words, as in NVIDIA's "synthetic-video-detector".
func (b *builder) hostPrefix(pid gateway.ProviderID, model string) string {
	for p := range b.rules.HostPrefixes(pid) {
		if strings.HasPrefix(model, p) {
			return p
		}
	}
	if rest, ok := strings.CutPrefix(model, string(pid)+"-"); ok && rest != "" && rest[0] >= 'a' && rest[0] <= 'z' && !b.labs[pid] {
		return string(pid) + "-"
	}
	for _, p := range b.hostWords() {
		if rest, ok := strings.CutPrefix(model, p); ok && b.namesBrand(rest) {
			return p
		}
	}
	return ""
}

// hostPrefixed reports whether the model part of provider pid's model ID id
// begins with a word the rules strip at pid.
func (b *builder) hostPrefixed(pid gateway.ProviderID, id string) bool {
	model := b.rules.ModelPart(pid, id)
	for p := range b.rules.HostPrefixes(pid) {
		if strings.HasPrefix(model, p) {
			return true
		}
	}
	return false
}

// namesBrand reports whether s, a model ID part, starts with a brand the
// rules know or a lab's name, followed by its end, a dash, a dot or a
// digit: "gpt-oss-120b", "llama-4-scout", "qwen3-32b".
func (b *builder) namesBrand(s string) bool {
	s = strings.ToLower(s)
	if _, ok := b.rules.BrandLab(s); ok {
		return true
	}
	first, _, _ := strings.Cut(s, "-")
	_, ok := b.labOf(first)
	return ok
}

// hostWords returns the IDs of the bundle's providers that are no lab and
// name no brand or product, each with a dash, the longest first.
func (b *builder) hostWords() []string {
	if b.hosts == nil {
		b.hosts = []string{}
		for id := range b.bundle.Providers {
			pid := gateway.ProviderID(id)
			if !b.labs[pid] && !b.rules.NamesBrand(string(id)) {
				b.hosts = append(b.hosts, string(id)+"-")
			}
		}
		slices.SortFunc(b.hosts, func(x, y string) int { return cmp.Or(cmp.Compare(len(y), len(x)), cmp.Compare(x, y)) })
	}
	return b.hosts
}

// LabBy says how LabOf found the lab that a provider's model ID names.
type LabBy string

// The ways LabOf finds a lab, the surest first.
const (
	// LabByMaker is a part of the ID before the model's that names the lab,
	// or another name the rules give it: "meta-llama/llama-4-scout".
	LabByMaker LabBy = "maker"
	// LabByPrefix is the lab's name and a dash before the model's own:
	// "deepseek-r1-distill-llama-70b".
	LabByPrefix LabBy = "prefix"
	// LabByProvider is the provider, which is the lab itself.
	LabByProvider LabBy = "provider"
	// LabByBrand is the first word of the model's name, a brand the rules
	// know: "grok-3".
	LabByBrand LabBy = "brand"
	// LabByBase is the lab of the model a fine-tune tunes, as FinetuneOf
	// finds it.
	LabByBase LabBy = "base"
	// LabByProduct is a product name the rules know: "inkling-256k".
	LabByProduct LabBy = "product"
	// LabByCanonical is models.dev's canonical_model_id, which names a model
	// its models list lacks: Vercel's "mistral/codestral-embed" names
	// mistral/codestral-embed-2505.
	LabByCanonical LabBy = "canonical"
)

// openBrandLab returns the lab of the open-weight model that a word of s,
// part of a model ID, names, as the rules' open words and brands say, and
// reports whether one does.
func (b *builder) openBrandLab(s string) (gateway.ProviderID, bool) {
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return strings.ContainsRune("-_/ ", r) }) {
		if lab, ok := b.rules.OpenLab(w); ok && b.labs[lab] {
			return lab, true
		}
	}
	return "", false
}

// FinetuneOf reads provider pid's model ID id, named name, as a fine-tune,
// and reports whether it is one whose tuned model's lab it finds: lab, the
// part of id that names the fine-tune, and what Finetuned says of it.
//
//   - A rule names the model it tunes: the lab is the base's maker's, or
//     else the lab of an open-weight brand the base's ID names.
//   - A quantized build's rule matches its ID, as "unsloth/qwen3.8-27b-gguf": a
//     quantized build, of the lab its maker part names, or else of the
//     open-weight brand's, as Qwen's own "Qwen/Qwen3-235B-A22B-FP8" is of
//     alibaba.
//   - A fine-tune's rule matches its ID or name, as "abliterated", or its maker
//     part names no lab and the rest an open-weight brand, as Envoid's
//     "Llama-3.05-NT-Storybreaker-Ministral-70B" does: the lab is the
//     brand's, or else the lab its maker part names, as NanoGPT's
//     "qwen/qwen3.8-27b-uncensored" names alibaba.
//   - A fine-tune's rule matches its description desc, at a provider that is no
//     lab, and its ID adds words to a model of the open-weight brand's lab
//     that the catalog lists, as NanoGPT's "gemma-4-31b-it-garnet" does to
//     google/gemma-4-31b-it.
//
// By is the maker part when it names no lab of models.dev's: the tuner.
func (b *builder) FinetuneOf(pid gateway.ProviderID, id, name, desc string) (ft Finetuned, lab gateway.ProviderID, model string, ok bool) {
	if b.rules.IsRouter(id, name) {
		return Finetuned{}, "", "", false
	}
	segs := b.rules.IDParts(pid, id)
	if len(segs) == 0 {
		return Finetuned{}, "", "", false
	}
	model = segs[len(segs)-1]
	maker := b.makerPart(pid, id)
	makerLab, makerIsLab := b.labOf(maker)
	ownLab := makerIsLab && !b.rules.Added(makerLab)
	brandLab, brand := b.openBrandLab(model)
	fb, based := b.rules.FinetuneBase(id)
	kind := cmp.Or(fb.Kind, TuneFinetune)
	switch {
	case based:
		org, rest, _ := strings.Cut(fb.Base, "/")
		if l, ok := b.labOf(org); ok {
			lab = l
		} else if l, ok := b.openBrandLab(rest); ok {
			lab = l
		}
	case b.rules.Quantized(model):
		kind = TuneQuantized
		switch {
		case ownLab:
			lab = makerLab
		case brand:
			lab = brandLab
		}
	case b.rules.FinetuneWord(id, name) || maker != "" && !makerIsLab && brand:
		switch {
		case brand:
			lab = brandLab
		case ownLab:
			lab = makerLab
		}
	case brand && !makerIsLab && !b.labs[pid] && b.rules.FinetuneDescription(desc):
		if _, ok := b.TunedModel(brandLab, model); ok {
			lab = brandLab
		}
	}
	if lab == "" {
		return Finetuned{}, "", "", false
	}
	ft = Finetuned{Base: fb.Base, Kind: kind}
	if maker != "" && !ownLab {
		ft.By = maker
	}
	return ft, lab, model, true
}

// makerPart returns the part of provider pid's model ID id before the
// model's that names its maker, in lower case: the first that the rules
// skip no segment of and that is not the provider's own ID, as "meta-llama"
// of "accounts/fireworks/models/meta-llama/…" would be. It is empty when id
// names no maker, as "groq/compound" does at Groq.
func (b *builder) makerPart(pid gateway.ProviderID, id string) string {
	for _, s := range b.prefixParts(pid, id) {
		if s = strings.TrimLeft(strings.ToLower(s), "@"); !b.rules.HostSegment(s) && !selfPart(pid, s) {
			return s
		}
	}
	return ""
}

// prefixParts returns the parts of provider pid's model ID id before its
// model's, less the name of an account that "accounts/" names, which is no
// maker's: LLM Gateway's "accounts/acme/routers/gpt-astra-latest" gives
// "accounts" and "routers".
func (b *builder) prefixParts(pid gateway.ProviderID, id string) []string {
	segs := b.rules.IDParts(pid, id)
	var out []string
	account := false
	for _, s := range segs[:max(len(segs)-1, 0)] {
		if !account {
			out = append(out, s)
		}
		account = !account && strings.EqualFold(s, "accounts")
	}
	return out
}

// selfPart reports whether s, a part of provider pid's model ID, names the
// provider itself: its ID, as NanoGPT writes it, "nanogpt", or its ID and
// a family of its own, as Kilo's "kilo-auto".
func selfPart(pid gateway.ProviderID, s string) bool {
	return s == string(pid) || alnum(s) == alnum(string(pid)) || strings.HasPrefix(s, string(pid)+"-")
}

// alnum returns s in lower case with only its letters and digits.
func alnum(s string) string {
	return strings.Map(func(r rune) rune {
		if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
			return r
		}
		if 'A' <= r && r <= 'Z' {
			return r + 'a' - 'A'
		}
		return -1
	}, s)
}

// selfName returns how provider pid names itself in its model IDs, when
// it spells its name otherwise than its ID, as NanoGPT's "nanogpt/…" does
// of nano-gpt; or else pid.
func (b *builder) selfName(pid gateway.ProviderID) gateway.ProviderID {
	for _, id := range slices.Sorted(maps.Keys(b.out.Providers[pid].Models)) {
		first, _, ok := strings.Cut(strings.TrimLeft(strings.ToLower(id), "~@"), "/")
		if ok && first != string(pid) && alnum(first) == alnum(string(pid)) {
			return gateway.ProviderID(first)
		}
	}
	return pid
}

// ownerOf returns the lab whose own models provider pid serves: pid, as
// selfName spells it, or, for a provider whose ID adds a plan or region to
// another provider's or a lab's, as "opencode-go" and
// "alibaba-token-plan-cn" do, that other's owner: opencode and alibaba.
func (b *builder) ownerOf(pid gateway.ProviderID) gateway.ProviderID {
	s := string(pid)
	for i := len(s) - 1; i > 0; i-- {
		if p := gateway.ProviderID(s[:i]); s[i] == '-' && (b.labs[p] || b.out.Providers[p].ID != "") {
			return b.ownerOf(p)
		}
	}
	return b.selfName(pid)
}

// LabOf returns the lab that provider pid's model ID id, named name, names,
// how it found it, and the part of id that names the model, and reports
// whether id names a lab. It reads past segments the rules skip, the provider's
// own ID, and past a host's word before the model's. A router names none,
// nor does a community fine-tune, nor an ID whose maker part names no lab,
// as "voyage/rerank-3".
func (b *builder) LabOf(pid gateway.ProviderID, id, name string) (lab gateway.ProviderID, by LabBy, model string, ok bool) {
	if b.rules.IsRouter(id, name) {
		return "", "", "", false
	}
	// A fine-tune's ID names the lab of the model it tunes, not its own;
	// only a product of its maker's own says who made it.
	tuned := b.rules.FinetuneWord(id, name)
	segs := b.rules.IDParts(pid, id)
	if len(segs) == 0 {
		return "", "", "", false
	}
	model = segs[len(segs)-1]
	// A host's word or name before the model's goes, as Databricks' does
	// from "databricks-gpt-oss-120b", but not a lab's own: Mistral's
	// "mistral-medium-2508" is its model's whole ID.
	for p := b.hostPrefix(pid, model); p != ""; p = b.hostPrefix(pid, model) {
		model = model[len(p):]
	}
	if maker := b.makerPart(pid, id); maker != "" {
		// A maker the rules add a lab of names its own fine-tunes, as
		// mlabonne does "mlabonne/NeuralDaredevil-8B-abliterated", when
		// FinetuneOf finds no model they tune.
		if lab, ok := b.labOf(maker); ok && (!tuned || b.rules.Added(lab)) {
			return lab, LabByMaker, model, true
		}
		return "", "", "", false
	}
	if !tuned {
		t := Normalize(model)
		for i := len(t) - 1; i > 0; i-- {
			if t[i] == '-' {
				if lab, ok := b.labOf(t[:i]); ok {
					return lab, LabByPrefix, model, true
				}
			}
		}
		if b.labs[pid] {
			return pid, LabByProvider, model, true
		}
	}
	lower := strings.ToLower(model)
	// A provider the rules exclude still makes its products, as Venice its
	// "venice-uncensored", which NanoGPT serves.
	for lab := range b.rules.ProductLabs(lower) {
		if b.labs[lab] || b.out.Providers[lab].ID != "" || b.rules.Excluded(lab) {
			return lab, LabByProduct, model, true
		}
	}
	if tuned {
		return "", "", "", false
	}
	if lab, ok := b.rules.BrandLab(lower); ok && b.labs[lab] {
		return lab, LabByBrand, model, true
	}
	return "", "", "", false
}

// shared is a model ID that a provider's unlisted listings name, as
// shareUnlisted reads them: the lab the ID names, how, the provider, and
// its listings, the one that names the model first and then its tiers and
// modes.
type shared struct {
	pid      gateway.ProviderID
	by       LabBy
	listings []string
	// ft is set for a fine-tune, as FinetuneOf reads it.
	ft *Finetuned
}

// shareUnlisted ties the listings that no rule ties to a model, by the
// model IDs LabOf reads in them, under the lab: lab/model, as CatalogID
// writes it. A listing that is a tier or mode of another, as unlistedGroups
// groups them, goes with the other.
//
// IDs of the same words, in another order or punctuation, as looseKey
// reads them, name one model: xai/grok-4-1-fast-non-reasoning and
// xai/grok-4.1-fast-non-reasoning do. A dated ID and an undated one, and a
// preview and its release, name two.
//
//   - A listing that is no model but a service, as ServiceOf reads it, is
//     left out of the catalog, as are its tiers and modes, and listed in
//     the report's Services.
//   - A free, thinking or no-thinking copy of a listing of its provider's
//     that is tied, as StepOfTied finds it, ties to that model, and then
//     groups as that offering's variant or alias.
//   - A listing whose canonical_model_id names a model models.dev's list
//     lacks names that model, under models.dev's ID for it.
//   - An ID that names a model the catalog lists, or one merged into
//     another, ties to it (BasisID), as Databricks'
//     "databricks-gpt-oss-120b" is openai/gpt-oss-120b, and Helicone's
//     "claude-4.5-haiku" anthropic/claude-haiku-4-5. So does an ID that
//     models.dev ties to one model of the same date at another provider, as
//     Kilo's "~anthropic/claude-opus-latest" is anthropic/claude-opus-5-5,
//     as Eden AI's is; a free copy of a model the catalog lists, as
//     OpenCode's "mimo-v2-flash-free" is xiaomi/mimo-v2-flash's free tier;
//     and an ID without the word its lab's IDs start with, after a word
//     the rules strip, as GitLab's "duo-chat-opus-4-8" is
//     anthropic/claude-opus-4-8.
//   - Any other ID that names a lab, by any of LabOf's ways, is a model of
//     its own, which the mapping makes (BasisInferred), under the spelling
//     with the most dots, then the shortest: named by NameOf, with the facts
//     of the lab's own listing, or else of the first provider's.
//     NanoGPT's "deepseek-ai/deepseek-v3.2-exp" is one by its maker part,
//     Mistral's "mistral-medium-2508" by its provider, and 302.AI's
//     "grok-4.1" by its brand.
//   - A fine-tune, as FinetuneOf reads it, quantized builds among them, is a
//     model of its own under the lab of the model it tunes, with its
//     Finetuned set: NanoGPT's "huihui-ai/Llama-3.3-70B-Instruct-abliterated"
//     is meta/llama-3.3-70b-instruct-abliterated, a fine-tune of
//     meta/llama-3.3-70b-instruct by huihui-ai.
//   - A listing that names no maker, and that no rule finds a lab of, is
//     the provider's own model, under the provider as its lab, or the
//     provider's owner, as ownerOf finds it: OpenCode's "big-pickle" is
//     opencode/big-pickle, and OpenCode Go's "space-bunny"
//     opencode/space-bunny.
//   - Any other listing stays provider-specific: one whose maker part names
//     no lab, with the lab in its Lab.
func (b *builder) shareUnlisted() {
	// The labs the catalog lists models of, and those some of whose models'
	// IDs start with the lab's own, as mistral/mistral-large does, and
	// meta/llama-4 does not.
	listed, selfNamed := make(map[gateway.ProviderID]bool), make(map[gateway.ProviderID]bool)
	for id := range b.out.Models {
		lab, part := SplitModelID(id)
		listed[lab] = true
		if strings.HasPrefix(part, string(lab)+"-") {
			selfNamed[lab] = true
		}
	}
	// candidate returns the model ID that a listing names as LabOf reads
	// it, lab and model, as CatalogID writes it, without the lab's name
	// before the model's where the lab's IDs do not start so: Abacus's
	// "meta-llama/Meta-Llama-3.1-8B-Instruct" is meta/llama-3.1-8b-instruct,
	// and Voyage's "voyage-4-lite", of a lab it lists no models of yet,
	// voyage/voyage-4-lite.
	// Its brand and version are written as the lab's own IDs write them, as
	// labSpelling does: DigitalOcean's "llama3.3-70b-instruct" is
	// meta/llama-3.3-70b-instruct, and Vercel's "alibaba/qwen-3-14b"
	// alibaba/qwen3-14b.
	candidate := func(lab gateway.ProviderID, model string) (gateway.ModelID, bool) {
		part := CatalogID(model)
		if rest, ok := strings.CutPrefix(part, string(lab)+"-"); ok && rest != "" && listed[lab] && !selfNamed[lab] {
			part = rest
		}
		part = b.labSpelling(lab, part)
		return gateway.ModelID(string(lab) + "/" + part), part != "" && !strings.Contains(part, "/")
	}
	// The catalog's models by their words, and by their letters and digits,
	// to find a model an ID orders or punctuates otherwise; and the words
	// each lab's model IDs start with, as anthropic's "claude".
	byWords, byLetters := make(map[string][]gateway.ModelID), make(map[string][]gateway.ModelID)
	firsts := make(map[gateway.ProviderID][]string)
	for _, id := range slices.Sorted(maps.Keys(b.out.Models)) {
		byWords[looseKey(id)] = append(byWords[looseKey(id)], id)
		byLetters[letters(id)] = append(byLetters[letters(id)], id)
		lab, part := SplitModelID(id)
		if w, _, ok := strings.Cut(part, "-"); ok && !slices.Contains(firsts[lab], w) {
			firsts[lab] = append(firsts[lab], w)
		}
	}
	b.byWords = byWords
	ids := make(map[gateway.ModelID][]shared)
	tunes := make(map[gateway.ModelID][]shared)
	for _, pid := range slices.Sorted(maps.Keys(b.out.Providers)) {
		prov := b.out.Providers[pid]
		var ls []Listing
		for o := range prov.Sorted() {
			if o.Model == "" {
				ls = append(ls, o)
			}
		}
		groups := make(map[string][]string)
		root := b.rules.unlistedRoot(ls)
		for _, o := range ls {
			r := root(o.ID)
			groups[r] = append(groups[r], o.ID)
		}
		for _, r := range slices.Sorted(maps.Keys(groups)) {
			members := append([]string{r}, slices.DeleteFunc(groups[r], func(id string) bool { return id == r })...)
			if svc := b.rules.ServiceOf(r, prov.Models[r].Name, prov.Models[r].Description); svc != "" {
				for _, id := range members {
					delete(prov.Models, id)
					b.report.Bases[BasisNone]--
					b.report.Services = append(b.report.Services, Note{Provider: pid, Model: id, Text: string(svc)})
				}
				continue
			}
			if into, basis, ok := b.StepOfTied(prov, r); ok {
				b.tieShared([]shared{{pid: pid, listings: members}}, into, basis, "")
				continue
			}
			// models.dev names the model, which its models list lacks: it is
			// made under the ID models.dev gives it, as Vercel's
			// "mistral/codestral-embed" is mistral/codestral-embed-2505.
			raw := b.bundle.Providers[modelsdev.ProviderID(pid)].Models[modelsdev.ModelID(r)]
			if c := gateway.ModelID(CatalogID(raw.CanonicalModelID)); strings.Count(string(c), "/") == 1 {
				for _, id := range members {
					o := prov.Models[id]
					o.Lab = c.Lab()
					prov.Models[id] = o
				}
				ids[c] = append(ids[c], shared{pid: pid, by: LabByCanonical, listings: members})
				continue
			}
			ft, lab, model, tuned := b.FinetuneOf(pid, r, prov.Models[r].Name, prov.Models[r].Description)
			by := LabByBase
			if !tuned {
				var ok bool
				if lab, by, model, ok = b.LabOf(pid, r, prov.Models[r].Name); !ok {
					// An ID that names no maker, and that no rule finds a
					// lab of, is the provider's own model, or its owner's.
					if b.makerPart(pid, r) != "" {
						continue
					}
					lab, by, model = b.ownerOf(pid), LabByProvider, b.rules.ModelPart(pid, r)
				}
			}
			id, ok := candidate(lab, model)
			if !ok {
				continue
			}
			for _, id := range members {
				o := prov.Models[id]
				o.Lab = lab
				prov.Models[id] = o
			}
			if tuned {
				tunes[id] = append(tunes[id], shared{pid: pid, by: by, listings: members, ft: &ft})
				continue
			}
			ids[id] = append(ids[id], shared{pid: pid, by: by, listings: members})
		}
	}
	// The models that models.dev ties the IDs of other providers' listings
	// to, by the model IDs LabOf reads in them, so that an ID another
	// provider lists alike ties as models.dev ties it there.
	tiedAs := make(map[string]map[gateway.ModelID]bool)
	for pid, prov := range b.out.Providers {
		for _, o := range prov.Models {
			if o.Basis != BasisCanonical && o.Basis != BasisLab {
				continue
			}
			lab, _, model, ok := b.LabOf(pid, o.ID, o.Name)
			if !ok {
				continue
			}
			if id, ok := candidate(lab, model); ok {
				key := looseKey(id)
				if tiedAs[key] == nil {
					tiedAs[key] = make(map[gateway.ModelID]bool)
				}
				tiedAs[key][o.Model] = true
			}
		}
	}
	// IDs of the same words, or of the same letters and digits, name one
	// model: alibaba/qwen-2.5-72b-instruct and alibaba/qwen2.5-72b-instruct
	// do, and so do cohere/embed-v-4-0 and cohere/embed-v4.0.
	root := make(map[string]string)
	var find func(string) string
	find = func(x string) string {
		if r, ok := root[x]; ok && r != x {
			r = find(r)
			root[x] = r
			return r
		}
		root[x] = x
		return x
	}
	for id := range ids {
		root[find("words:"+looseKey(id))] = find("letters:" + letters(id))
	}
	alike := make(map[string][]gateway.ModelID)
	for _, id := range slices.Sorted(maps.Keys(ids)) {
		g := find("words:" + looseKey(id))
		alike[g] = append(alike[g], id)
	}
	// named returns the model the catalog lists that id names, as it is or
	// by its words, and reports whether there is one.
	named := func(id gateway.ModelID) (gateway.ModelID, bool) {
		if _, ok := b.out.Models[id]; ok {
			return id, true
		}
		if ms := byWords[looseKey(id)]; len(ms) == 1 {
			return ms[0], true
		}
		if ms := byLetters[letters(id)]; len(ms) == 1 {
			return ms[0], true
		}
		return "", false
	}
	// cut returns the model that id names without one of suffixes, as
	// zhipuai/glm-5-free names zhipuai/glm-5.
	cut := func(id gateway.ModelID, suffixes ...string) (gateway.ModelID, bool) {
		for _, s := range suffixes {
			if base, ok := strings.CutSuffix(string(id), s); ok && !strings.HasSuffix(base, "/") {
				return named(gateway.ModelID(base))
			}
		}
		return "", false
	}
	// family returns the model that id names with a word its lab's IDs
	// start with before it: GitLab's "duo-chat-opus-4-8", which LabOf reads
	// as anthropic/opus-4-8, names anthropic/claude-opus-4-8.
	family := func(id gateway.ModelID) (gateway.ModelID, bool) {
		lab, part := SplitModelID(id)
		for _, w := range firsts[lab] {
			if !strings.HasPrefix(part, w+"-") {
				if m, ok := named(gateway.ModelID(string(lab) + "/" + w + "-" + part)); ok {
					return m, true
				}
			}
		}
		return "", false
	}
	// The ID each key's model goes by: of its spellings, one that starts
	// with a brand and version the way its lab's IDs write them, as
	// anthropic/claude-3-5-haiku does and claude-haiku-3.5 does not; then the
	// one that writes versions with dots, as models.dev's IDs do; then the
	// shortest.
	chosen := make(map[string]gateway.ModelID, len(alike))
	for key, ms := range alike {
		chosen[key] = slices.MaxFunc(ms, func(x, y gateway.ModelID) int {
			return cmp.Or(cmpBool(b.labSpelled(x), b.labSpelled(y)),
				cmp.Compare(strings.Count(string(x), "."), strings.Count(string(y), ".")),
				cmp.Compare(len(y), len(x)), strings.Compare(string(y), string(x)))
		})
	}
	// Free copies last, so that one finds the model another ID makes here.
	freeSuffixes := b.rules.FreeSuffixes()
	free := func(key string) bool {
		return slices.ContainsFunc(freeSuffixes, func(s string) bool { return strings.HasSuffix(string(chosen[key]), s) })
	}
	for _, g := range slices.SortedFunc(maps.Keys(alike), func(x, y string) int {
		return cmp.Or(cmpBool(free(x), free(y)), strings.Compare(x, y))
	}) {
		var ss []shared
		for _, id := range alike[g] {
			ss = append(ss, ids[id]...)
		}
		id := chosen[g]
		key, lkey := looseKey(id), letters(id)
		into, basis, tier := id, BasisID, gateway.Tier("")
		// What models.dev ties the same ID to at another provider, of the
		// same date: a dated ID and an undated one name two models. A
		// "-latest" ID names no date but what it serves now, as models.dev
		// says.
		var elsewhere gateway.ModelID
		if len(tiedAs[key]) == 1 {
			for m := range tiedAs[key] {
				if slices.Equal(dates(id), dates(m)) || strings.HasSuffix(string(id), "-latest") {
					elsewhere = m
				}
			}
		}
		base, isFree := cut(id, freeSuffixes...)
		// Only a product's or a plan's name, as the rules strip it, hides
		// the lab's family word, as GitLab's "duo-chat-" does "claude-".
		var kin gateway.ModelID
		inFamily := false
		if slices.ContainsFunc(ss, func(s shared) bool { return b.hostPrefixed(s.pid, s.listings[0]) }) {
			kin, inFamily = family(id)
		}
		switch mg, merged := b.merged[id]; {
		case merged:
			into, basis, tier = mg.into, BasisTier, mg.tier
		case len(byWords[key]) == 1:
			into = byWords[key][0]
		case len(byLetters[lkey]) == 1:
			into = byLetters[lkey][0]
		case elsewhere != "":
			into = elsewhere
		case isFree:
			into, basis, tier = base, BasisTier, gateway.TierFree
		case inFamily:
			into = kin
		default:
			b.out.Models[id] = b.sharedModel(id, ss)
			b.report.Inferred = append(b.report.Inferred, id)
			basis = BasisInferred
			byWords[key] = append(byWords[key], id)
			byLetters[lkey] = append(byLetters[lkey], id)
		}
		b.tieShared(ss, into, basis, tier)
	}
	// A fine-tune, quantized builds among them, is a model of its own, under
	// the lab of the model it tunes, never that model: its weights differ.
	// A model the catalog lists by the fine-tune's own ID is the same one.
	for _, id := range slices.Sorted(maps.Keys(tunes)) {
		ss := tunes[id]
		if m, ok := named(id); ok {
			b.tieShared(ss, m, BasisID, "")
			continue
		}
		m := b.sharedModel(id, ss)
		ft := *ss[0].ft
		if org, rest, ok := strings.Cut(ft.Base, "/"); ok {
			lab, found := b.labOf(org)
			if !found {
				lab, found = b.openBrandLab(rest)
			}
			if base, ok := candidate(lab, rest); found && ok {
				if _, listed := b.out.Models[base]; listed {
					ft.Of = base
				} else if ms := byWords[looseKey(base)]; len(ms) == 1 {
					ft.Of = ms[0]
				}
			}
		}
		if ft.Of == "" {
			lab, part := SplitModelID(id)
			ft.Of, _ = b.TunedModel(lab, part)
		}
		b.out.Models[id] = m
		origin := b.report.Origins[id]
		origin.Finetuned = &ft
		b.report.Origins[id] = origin
		b.report.Inferred = append(b.report.Inferred, id)
		b.tieShared(ss, id, BasisInferred, "")
	}
}

// StepOfTied returns the model of the listing of prov that provider model
// ID id adds a free, thinking or no-thinking suffix to, which the mapping
// tied to a model, and the basis to tie id on, and reports whether there is
// one: OpenCode's "glm-5-free" is of zhipuai/glm-5, as its "glm-5" is, and
// becomes that offering's free variant; AIHubMix's "claude-opus-4-7-think"
// its thinking one; and 302.AI's "gemini-2.5-flash-nothink" an alias.
func (b *builder) StepOfTied(prov ListedProvider, id string) (gateway.ModelID, Basis, bool) {
	free := b.rules.FreeSuffixes()
	for _, s := range slices.Concat(free, b.rules.ThinkingSuffixes(), b.rules.NoThinkingSuffixes()) {
		base, ok := strings.CutSuffix(id, s)
		if !ok || base == "" {
			continue
		}
		if bo, ok := prov.Models[base]; ok && bo.Model != "" {
			if slices.Contains(free, s) {
				return bo.Model, BasisTier, true
			}
			return bo.Model, BasisID, true
		}
	}
	return "", "", false
}

// TunedModel returns the model the catalog lists that part, a model ID's
// part of lab, adds words of its own to, the longest, and reports whether
// there is one: google/gemma-4-31b-it of "gemma-4-31b-it-garnet", and
// meta/llama-3.1-8b-instruct of "meta-llama-3-1-8b-instruct-gguf".
func (b *builder) TunedModel(lab gateway.ProviderID, part string) (gateway.ModelID, bool) {
	part = CatalogID(part)
	for _, p := range slices.Compact([]string{part, strings.TrimPrefix(part, string(lab)+"-")}) {
		for i := strings.LastIndexByte(p, '-'); i > 0; i = strings.LastIndexByte(p[:i], '-') {
			id := gateway.ModelID(string(lab) + "/" + p[:i])
			if _, ok := b.out.Models[id]; ok {
				return id, true
			}
			if ms := b.byWords[looseKey(id)]; len(ms) == 1 {
				return ms[0], true
			}
		}
	}
	return "", false
}

// tieShared ties the listings of ss to model into, on basis, the first
// listing of each in tier.
func (b *builder) tieShared(ss []shared, into gateway.ModelID, basis Basis, tier gateway.Tier) {
	for _, s := range ss {
		prov := b.out.Providers[s.pid]
		for _, lid := range s.listings {
			o := prov.Models[lid]
			o.Model, o.Basis, o.Lab = into, basis, ""
			if lid == s.listings[0] && tier != "" {
				o.Tier = tier
			}
			prov.Models[lid] = o
			b.report.Bases[BasisNone]--
			b.report.Bases[basis]++
		}
	}
}

// looseKey returns model ID id's lab and its words, as Normalize reads
// them, in byte order, so that IDs that order or punctuate the same words
// otherwise compare alike, as anthropic/claude-4.5-haiku and
// anthropic/claude-haiku-4-5 do. Its numbers keep their order, so that
// gemini-2.5-pro-preview-05-06 and -06-05 do not.
func looseKey(id gateway.ModelID) string {
	lab, part := SplitModelID(id)
	var words, numbers []string
	for _, w := range strings.Split(Normalize(part), "-") {
		if w != "" && digits(w, len(w)) {
			numbers = append(numbers, w)
		} else {
			words = append(words, w)
		}
	}
	slices.Sort(words)
	return string(lab) + "/" + strings.Join(slices.Concat(words, numbers), "-")
}

// sharedModel returns the model shareUnlisted makes for model ID id, which
// ss name: named by NameOf, with the facts of the lab's own listing, or else
// of the first provider's, in byte order. It records the model's Origin, as
// inferred.
func (b *builder) sharedModel(id gateway.ModelID, ss []shared) gateway.Model {
	lab := id.Lab()
	best := slices.MinFunc(ss, func(x, y shared) int {
		return cmp.Or(cmpBool(x.pid != lab, y.pid != lab), strings.Compare(string(x.pid), string(y.pid)))
	})
	o := b.out.Providers[best.pid].Models[best.listings[0]]
	// How the lab was found, the surest way any listing names it.
	order := []LabBy{LabByCanonical, LabByBase, LabByProduct, LabByMaker, LabByPrefix, LabByProvider, LabByBrand}
	by := slices.MinFunc(ss, func(x, y shared) int { return cmp.Compare(slices.Index(order, x.by), slices.Index(order, y.by)) }).by
	b.report.Origins[id] = Origin{Inferred: &Inference{LabBy: by, Facts: best.pid}}
	return gateway.Model{
		ID: id, Lab: lab, Name: NameOf(id, b.spell), Family: o.Family, Type: o.Type,
		Released: o.Released, Updated: o.Updated, Knowledge: o.Knowledge, OpenWeights: o.OpenWeights,
		Reasoning: o.Reasoning != nil, Capabilities: o.Capabilities, Limit: o.Limit,
	}
}

// letters returns model ID id's lab and the letters and digits of the rest,
// so that IDs a dash or a dot apart compare alike: cohere/embed-v-4-0 and
// cohere/embed-v4.0.
func letters(id gateway.ModelID) string {
	lab, part := SplitModelID(id)
	return string(lab) + "/" + alnum(part)
}

// versioned matches the brand and version a model ID part starts with: the
// brand's letters, a dash or none, the major version, and a minor version of
// one digit after a dot or a dash: "qwen2.5-coder", "llama-3-1-8b",
// "claude-3-5-sonnet".
var versioned = regexp.MustCompile(`^([a-z]+)(-?)(\d+)(?:([.-])(\d))?(-|$)`)

// spelling counts how a lab's models.dev IDs write one brand's version:
// joined to the brand, as "qwen3", or after a dash, as "llama-3"; and a minor
// version after a dot, as "2.5", or a dash, as "3-5".
type spelling struct{ joined, dashed, dot, dash int }

// labSpelling returns part, a model ID's part of lab, with its brand and
// version written as lab's models.dev IDs most often write them: Alibaba's
// qwen2.5 of "qwen-2.5" and "qwen2-5", Meta's llama-3.3 of "llama3.3", and
// Anthropic's claude-3-5 of "claude-3.5". A brand the lab writes no version
// of, or writes both ways as often, stays as it is.
func (b *builder) labSpelling(lab gateway.ProviderID, part string) string {
	if b.spellings == nil {
		b.spellings = make(map[string]spelling)
		for id := range b.bundle.Models {
			l, p := SplitModelID(gateway.ModelID(id))
			m := versioned.FindStringSubmatch(p)
			if m == nil {
				continue
			}
			s := b.spellings[string(l)+"/"+m[1]]
			if m[2] == "" {
				s.joined++
			} else {
				s.dashed++
			}
			switch m[4] {
			case ".":
				s.dot++
			case "-":
				s.dash++
			}
			b.spellings[string(l)+"/"+m[1]] = s
		}
	}
	m := versioned.FindStringSubmatchIndex(part)
	if m == nil {
		return part
	}
	s, ok := b.spellings[string(lab)+"/"+part[m[2]:m[3]]]
	if !ok {
		return part
	}
	sep := part[m[4]:m[5]]
	switch {
	case s.joined > s.dashed:
		sep = ""
	case s.dashed > s.joined:
		sep = "-"
	}
	out := part[m[2]:m[3]] + sep + part[m[6]:m[7]]
	if m[8] >= 0 {
		dot := part[m[8]:m[9]]
		switch {
		case s.dot > s.dash:
			dot = "."
		case s.dash > s.dot:
			dot = "-"
		}
		out += dot + part[m[10]:m[11]]
	}
	return out + part[m[12]:]
}

// labSpelled reports whether model ID id starts with a brand and version its
// lab's models.dev IDs write, as labSpelling writes them.
func (b *builder) labSpelled(id gateway.ModelID) bool {
	lab, part := SplitModelID(id)
	m := versioned.FindStringSubmatch(part)
	if m == nil {
		return false
	}
	b.labSpelling(lab, part)
	_, ok := b.spellings[string(lab)+"/"+m[1]]
	return ok
}

// dates returns the words of model ID id that are dates, of 4, 6 or 8
// digits, in byte order: "0309" of xai/grok-4.20-0309-reasoning.
func dates(id gateway.ModelID) []string {
	_, part := SplitModelID(id)
	words := slices.DeleteFunc(strings.Split(Normalize(part), "-"), func(w string) bool {
		return !(len(w) == 4 || len(w) == 6 || len(w) == 8) || !digits(w, len(w))
	})
	slices.Sort(words)
	return words
}
