// Package build builds flow's model catalog, a gateway.Catalog, from the
// models.dev catalog, and keeps our own: the rules of the mapping
// (rules.json), the names and reasoning facts they give, and the trimmed
// copies of a catalog that Keep makes.
//
// models.dev keeps two lists. Its models (the site's /models page) are the
// labs' models, one per canonical ID such as "anthropic/claude-opus-5-5".
// Its providers each list the models they serve under their own IDs, such
// as Vercel's "anthropic/claude-opus-5.5", which names the model it serves
// in canonical_model_id; one without it, such as OpenCode's "space-bunny",
// is provider-specific.
//
// FromModelsDev maps the providers' lists to Listings first, one Listing
// per models.dev entry, tied to a model by models.dev or by rules of the
// mapping's own: by ID, as LLM Gateway's "gpt-6-astra" is
// openai/gpt-6-astra; as a tier, as Vercel's "openai/gpt-6-astra-fast";
// to a model derived for a mode, as GPT-6 Astra Pro; or to one derived for
// a "-latest" alias, as GPT Sol Latest. Each Listing's Basis says which.
//
// It then groups each provider's listings into one Offering per model,
// keyed by the model's ID. An Offering is one way to call the model, with
// the ProviderOptions to send: the provider's model ID, request fields, and
// an Endpoint when it is not the provider's. Its Variants are the tiers and
// modes it offers on top, by name, such as "fast"; its Aliases are the
// provider's other model IDs of the model. A provider or a listing whose
// request format the catalog does not know is left out, as no client could
// call it.
package build
