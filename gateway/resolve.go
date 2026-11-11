package gateway

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

// Resolved is a call as a client sends it: an offering's own, or one of its
// variants combined with it, with its provider and model, and its endpoint
// always set.
type Resolved struct {
	Provider     ProviderID `json:"provider"`
	ProviderName string     `json:"provider_name"`
	// Model is the model called.
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
}

// Variants returns every call of provider p, resolved: offering by offering
// in model ID order, each offering's own call followed by its variants, in
// VariantKeys order.
func (c *Catalog) Variants(p ProviderID) iter.Seq[Resolved] {
	return func(yield func(Resolved) bool) {
		prov, ok := c.Providers[p]
		if !ok {
			return
		}
		for _, m := range slices.Sorted(maps.Keys(prov.Models)) {
			off := prov.Models[m]
			if !yield(resolve(prov, m, off, "")) {
				return
			}
			for _, k := range off.VariantKeys() {
				if !yield(resolve(prov, m, off, k)) {
					return
				}
			}
		}
	}
}

// resolve returns the call of variant k, or the offering's own for "", of
// offering off of model at provider prov.
func resolve(prov Provider, model ModelID, off Offering, k string) Resolved {
	r := Resolved{Provider: prov.ID, ProviderName: prov.Name, Model: model, ID: VariantID(string(model), k), Name: off.Name,
		ProviderOptions: off.ProviderOptions, Price: off.Price, Status: off.Status}
	r.ProviderOptions.Body = mergeBody(nil, off.ProviderOptions.Body)
	if v, ok := off.Variants[k]; ok {
		r.Name, r.Tier, r.Mode = cmp.Or(v.Name, off.Name), v.Tier, v.Mode
		r.ProviderOptions = ProviderOptions{
			ID:       cmp.Or(v.ProviderOptions.ID, off.ProviderOptions.ID),
			Name:     cmp.Or(v.ProviderOptions.Name, off.ProviderOptions.Name),
			Endpoint: cmp.Or(v.ProviderOptions.Endpoint, off.ProviderOptions.Endpoint),
			Body:     mergeBody(off.ProviderOptions.Body, v.ProviderOptions.Body),
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

// Lookup returns the model of the offering that provider p's variant id
// calls, and the variant's key, "" for the offering's own call. It reports
// whether the catalog lists the variant. It ignores case, as the catalog's
// IDs are all lower case.
func (c *Catalog) Lookup(p ProviderID, id string) (model ModelID, variant string, ok bool) {
	id = strings.ToLower(id)
	prov, ok := c.Providers[p]
	if !ok {
		return "", "", false
	}
	if _, ok := prov.Models[ModelID(id)]; ok {
		return ModelID(id), "", true
	}
	// A variant's key holds a mode, a tier, or both: one or two segments.
	for cut, segs := id, 0; segs < 2; segs++ {
		i := strings.LastIndexByte(cut, ':')
		if i < 0 {
			break
		}
		cut = cut[:i]
		if o, ok := prov.Models[ModelID(cut)]; ok {
			if _, has := o.Variants[id[i+1:]]; has {
				return ModelID(cut), id[i+1:], true
			}
		}
	}
	return "", "", false
}

// Resolve returns provider p's variant id, resolved, and reports whether
// the catalog lists it.
func (c *Catalog) Resolve(p ProviderID, id string) (Resolved, bool) {
	m, k, ok := c.Lookup(p, id)
	if !ok {
		return Resolved{}, false
	}
	prov := c.Providers[p]
	return resolve(prov, m, prov.Models[m], k), true
}

// mergeBody returns request fields over merged into base deeply: an object
// in both merges key by key, and any other value of over replaces base's.
// It returns nil when both are empty, and copies what it returns.
func mergeBody(base, over map[string]any) map[string]any {
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
				out[k] = mergeBody(bm, om)
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
