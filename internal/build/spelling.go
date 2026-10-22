package build

import (
	"cmp"
	"iter"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/unaquatic/catalog/gateway"
)

// CatalogID returns id as the catalog writes the IDs it keys models and
// unlisted offerings by: in lower case, of segments joined by "/", each of
// a-z, 0-9, "." and "-" alone, a run of any other characters a "-", with no
// "-" at either end and no empty segment, so that Kilo's
// "cohere/north-mini-code:free" is "cohere/north-mini-code-free" and
// Workers AI's "@cf/cloudflare/clef" is "cf/cloudflare/clef". The catalog's
// IDs hold ":" only between a model's ID and its variant's key.
func CatalogID(id string) string {
	var segs []string
	for seg := range strings.SplitSeq(strings.ToLower(id), "/") {
		var b strings.Builder
		dash := false
		for _, r := range seg {
			if 'a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '.' || r == '-' {
				b.WriteRune(r)
				dash = false
			} else if !dash {
				b.WriteByte('-')
				dash = true
			}
		}
		if s := strings.Trim(b.String(), "-"); s != "" {
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// SpellingsOf returns the spellings of the words of names, as NameWords
// splits them: of each word, the spelling most names give it, and of a tie,
// the first in byte order. A word of letters and the number after it also
// spell the word an ID runs them together in: "Solar Pro 3" spells the "pro3"
// of upstage/solar-pro3 "Pro 3", unless more names write "Pro3".
func SpellingsOf(names iter.Seq[string]) gateway.Spellings {
	votes := make(map[string]map[string]int)
	vote := func(k, w string) {
		if votes[k] == nil {
			votes[k] = make(map[string]int)
		}
		votes[k][w]++
	}
	for n := range names {
		words := NameWords(n)
		for i, w := range words {
			vote(strings.ToLower(w), w)
			if i > 0 && letterWord.MatchString(words[i-1]) && number.MatchString(w) {
				vote(strings.ToLower(words[i-1]+w), words[i-1]+" "+w)
			}
		}
	}
	sp := make(gateway.Spellings, len(votes))
	for k, vs := range votes {
		var best string
		for _, w := range slices.Sorted(maps.Keys(vs)) {
			if best == "" || vs[w] > vs[best] {
				best = w
			}
		}
		sp[k] = best
	}
	return sp
}

// NameWords returns the words of model name n: its runs of characters other
// than spaces, hyphens, slashes, parentheses, colons and commas, so that
// "Qwen3-Coder 480B-A35B (Instruct)" has "Qwen3", "Coder", "480B", "A35B"
// and "Instruct".
func NameWords(n string) []string {
	return strings.FieldsFunc(n, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("-/(),:", r)
	})
}

var (
	// letterWord and number are a word of letters alone, as "Pro", and a
	// version number, as "3" or "3.5".
	letterWord = regexp.MustCompile(`^[A-Za-z]+$`)
	number     = regexp.MustCompile(`^\d+(\.\d+)?$`)
	// paramCount is a word that counts parameters, as "7b".
	paramCount = regexp.MustCompile(`^\d+(\.\d+)?[bm]$`)
	// versionTag is a word that tags a version, as "v2" or "v0.1".
	versionTag = regexp.MustCompile(`^v\d+(\.\d+)*$`)
	// oSeries is a word of OpenAI's o-series, as "o3".
	oSeries = regexp.MustCompile(`^o\d+$`)
)

// IDWords returns the words of part, the part of a catalog ID that names a
// model, in lower case, as Name names them: the parts between its hyphens,
// with a date, year first, run together, and a digit after a word that ends
// in one digit joined to it with a dot. "claude-opus-4-5-20251101" has
// "claude", "opus", "4.5" and "20251101"; "qwen2-5-vl-72b-instruct" has
// "qwen2.5", "vl", "72b" and "instruct".
func IDWords(part string) []string {
	var words []string
	parts := strings.FieldsFunc(part, func(r rune) bool { return r == '-' || r == '/' })
	for len(parts) > 0 {
		p := parts[0]
		if len(parts) >= 3 && digits(p, 4) && digits(parts[1], 2) && digits(parts[2], 2) {
			words, parts = append(words, p+parts[1]+parts[2]), parts[3:]
			continue
		}
		if n := len(words); n > 0 && digits(p, 1) && endsInDigit(words[n-1]) {
			words[n-1], parts = words[n-1]+"."+p, parts[1:]
			continue
		}
		words, parts = append(words, p), parts[1:]
	}
	return words
}

// SpellName returns the name of part, the part of a catalog ID that names a
// model: IDWords(part), in their order, each spelt by Spell, with GPT's
// hyphen kept before its number. "claude-opus-4-5-20251101" is "Claude Opus
// 4.5 20251101", "minimax-m2.7" "MiniMax M2.7", and "gpt-6.1-sol" "GPT-6.1
// Sol".
func SpellName(sp gateway.Spellings, part string) string {
	var words []string
	for _, w := range IDWords(part) {
		w = Spell(sp, w)
		if n := len(words); n > 0 && strings.EqualFold(words[n-1], "GPT") && w[0] >= '0' && w[0] <= '9' {
			words[n-1] += "-" + w
			continue
		}
		words = append(words, w)
	}
	return strings.Join(words, " ")
}

// NameOf returns the name of model id: the Name of its part after its lab.
// The catalog names every model so, with its Spellings, and so a model's
// name always matches its ID: minimax/minimax-m2.7 is "MiniMax M2.7".
func NameOf(id gateway.ModelID, sp gateway.Spellings) string {
	_, own := SplitModelID(id)
	return SpellName(sp, own)
}

// UnlistedName returns the name of the unlisted offering keyed key: the
// Name of the key's last segment, which names the model where the others
// name its maker or host. "meta-llama/llama-4-scout" is "Llama 4 Scout".
func UnlistedName(key string, sp gateway.Spellings) string {
	return SpellName(sp, UnlistedPart(key))
}

// UnlistedPart returns the part of unlisted key that UnlistedName names: its
// last segment.
func UnlistedPart(key string) string {
	return key[strings.LastIndexByte(key, '/')+1:]
}

// VariantName returns the name of variant key of an offering named name:
// the offering's name and the key's words, each with a capital, as "GPT-6.1
// Sol Ultrafast" and "Claude Opus 5 Thinking Fast".
func VariantName(name, key string) string {
	words := []string{name}
	for w := range strings.SplitSeq(key, ":") {
		words = append(words, strings.ToUpper(w[:1])+w[1:])
	}
	return strings.Join(words, " ")
}

// Spell returns word w of a model ID as NameOf writes it:
//
//   - A version tag starts with a capital V: "v0.1" is "V0.1".
//   - Any other word is spelt as sp spells it, or as it is.
//   - A parameter count is in capitals: "7b" is "7B".
//   - A word in lower case then starts with a capital, but for OpenAI's
//     o-series: "mini" is "Mini", and "o3" stays "o3".
func Spell(sp gateway.Spellings, w string) string {
	if versionTag.MatchString(w) {
		return "V" + w[1:]
	}
	w = cmp.Or(sp[w], w)
	if paramCount.MatchString(w) {
		return strings.ToUpper(w)
	}
	if r, size := utf8.DecodeRuneInString(w); 'a' <= r && r <= 'z' && !oSeries.MatchString(w) {
		w = strings.ToUpper(w[:size]) + w[size:]
	}
	return w
}

// SpellingsFor returns the spellings SpellName needs to name parts as with sp: those of
// their words that sp spells otherwise than Spell would with none.
func SpellingsFor(sp gateway.Spellings, parts iter.Seq[string]) gateway.Spellings {
	out := make(gateway.Spellings)
	for part := range parts {
		for _, w := range IDWords(part) {
			if Spell(sp, w) != Spell(nil, w) {
				out[w] = sp[w]
			}
		}
	}
	return out
}

// digits reports whether s is ASCII digits alone, n of them.
func digits(s string, n int) bool {
	return len(s) == n && strings.Trim(s, "0123456789") == ""
}

// endsInDigit reports whether s ends in one digit, and not two, as "qwen2"
// and "4" do.
func endsInDigit(s string) bool {
	n := len(s)
	return n > 0 && digits(s[n-1:], 1) && (n == 1 || !digits(s[n-2:n-1], 1))
}

// SplitModelID returns the lab part of id and the model's ID within the lab,
// as Cut splits it at its first slash.
func SplitModelID(id gateway.ModelID) (lab gateway.ProviderID, model string) {
	before, after, _ := strings.Cut(string(id), "/")
	return gateway.ProviderID(before), after
}

// NameIn returns the name of model id, as catalog c names its models,
// whether or not it lists id.
func NameIn(c *gateway.Catalog, id gateway.ModelID) string {
	return NameOf(gateway.ModelID(CatalogID(string(id))), c.Spellings)
}

// UnlistedNameIn returns the name of the unlisted offering keyed key, as the
// catalog names them, whether or not it lists one.
func UnlistedNameIn(c *gateway.Catalog, key string) string {
	return UnlistedName(CatalogID(key), c.Spellings)
}

// NameParts returns the parts of the catalog's IDs that its names are made
// of: each model's ID after its lab, and each unlisted offering's
// UnlistedPart.
func NameParts(c *gateway.Catalog) iter.Seq[string] {
	return func(yield func(string) bool) {
		for id := range c.Models {
			if _, own := SplitModelID(id); !yield(own) {
				return
			}
		}
		for _, p := range c.Providers {
			for key := range p.Unlisted {
				if !yield(UnlistedPart(key)) {
					return
				}
			}
		}
	}
}
