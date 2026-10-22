package gateway

import (
	"cmp"
	"slices"
	"strings"
)

// Format is the request format of an API: how a client builds a request for
// a model ID, merges option bodies into it, and reads the response. Every
// API the catalog knows takes one of these at the base URL of its Endpoint.
type Format string

// Request formats.
const (
	// FormatOpenAIChat is OpenAI Chat Completions: POST {api}/chat/completions.
	FormatOpenAIChat Format = "openai-chat"
	// FormatOpenAIResponses is OpenAI Responses: POST {api}/responses.
	FormatOpenAIResponses Format = "openai-responses"
	// FormatAnthropicMessages is Anthropic Messages: POST {api}/messages,
	// with an "anthropic-version: 2023-06-01" header.
	FormatAnthropicMessages Format = "anthropic-messages"
	// FormatAnthropicVertex is Anthropic Messages as Vertex AI takes them:
	// POST {api}/{model}:streamRawPredict, with no "model" in the body and
	// "anthropic_version": "vertex-2023-10-16" in it instead.
	FormatAnthropicVertex Format = "anthropic-vertex"
	// FormatGemini is Gemini's generateContent: POST
	// {api}/models/{model}:streamGenerateContent?alt=sse, at the Gemini API
	// and at Vertex AI alike.
	FormatGemini Format = "gemini"
)

// Formats are the request formats the catalog knows: a client that speaks
// them all can call every endpoint the catalog lists. A provider or a
// listing whose format the catalog does not know, as of an SDK package it
// cannot map, is left out of it, and the build notes it.
var Formats = []Format{
	FormatOpenAIChat, FormatOpenAIResponses, FormatAnthropicMessages,
	FormatAnthropicVertex, FormatGemini,
}

// AuthScheme is how a request proves who sends it.
type AuthScheme string

// Authentication schemes.
const (
	// AuthBearer sends the key as "Authorization: Bearer <key>".
	AuthBearer AuthScheme = "bearer"
	// AuthHeader sends the key in the header Auth.Header names, as
	// Anthropic's "x-api-key".
	AuthHeader AuthScheme = "header"
	// AuthGoogle sends a Google OAuth access token, from application
	// default credentials, as "Authorization: Bearer <token>".
	AuthGoogle AuthScheme = "google"
)

// AuthSchemes are the authentication schemes the catalog knows.
var AuthSchemes = []AuthScheme{AuthBearer, AuthHeader, AuthGoogle}

// Auth says how an API authenticates requests.
type Auth struct {
	Scheme AuthScheme `json:"scheme"`
	// Header names the header that carries the key, for AuthHeader.
	Header string `json:"header,omitempty"`
}

// Endpoint is how to call an API: its request format, its base URL, how it
// authenticates, and the environment variables models.dev lists for it.
//
// API may name environment variables as ${NAME}, which a client replaces
// with their values, as Lookup finds them. ${GOOGLE_VERTEX_ENDPOINT}, which
// models.dev uses but does not list, is aiplatform.googleapis.com when
// GOOGLE_VERTEX_LOCATION is "global" and
// {GOOGLE_VERTEX_LOCATION}-aiplatform.googleapis.com otherwise.
type Endpoint struct {
	// Format is empty when the catalog does not know the API's format,
	// which leaves its provider or listing out of the catalog.
	Format Format `json:"format,omitempty"`
	API    string `json:"api,omitempty"`
	// APIDefault reports that models.dev lists no base URL, and API is the
	// one the provider's SDK uses by default.
	APIDefault bool `json:"api_default,omitempty"`
	// Auth is zero when the catalog does not know how the API
	// authenticates.
	Auth Auth     `json:"auth,omitzero"`
	Env  []string `json:"env,omitempty"`
}

// Equal reports whether e and f are the same endpoint.
func (e Endpoint) Equal(f Endpoint) bool {
	return e.Format == f.Format && e.API == f.API && e.APIDefault == f.APIDefault && e.Auth == f.Auth &&
		slices.Equal(e.Env, f.Env)
}

// KeyEnv returns the environment variables of e that may hold an API key,
// in e's order: for a key sent as a bearer token or in a header, those its
// base URL does not name, as Azure's names its resource; and none for
// Google, whose variables say where its credentials are.
func (e Endpoint) KeyEnv() []string {
	if e.Auth.Scheme == AuthGoogle {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(e.Env), func(name string) bool { return strings.Contains(e.API, "${"+name+"}") })
}

// Lookup returns the value of the variable name that a base URL names, as
// env reads it, or, for the variables models.dev names for the clouds, as
// their SDKs find them: GOOGLE_VERTEX_PROJECT from GOOGLE_CLOUD_PROJECT, GCP_PROJECT or
// GCLOUD_PROJECT; GOOGLE_VERTEX_LOCATION from GOOGLE_CLOUD_LOCATION or
// VERTEX_LOCATION, else global, the location that serves the most models;
// and GOOGLE_VERTEX_ENDPOINT, which no SDK reads, from the location. It
// returns "" when none is set.
func Lookup(name string, env func(string) string) string {
	first := func(names ...string) string {
		for _, n := range names {
			if v := env(n); v != "" {
				return v
			}
		}
		return ""
	}
	switch name {
	case "GOOGLE_VERTEX_PROJECT":
		return first(name, "GOOGLE_CLOUD_PROJECT", "GCP_PROJECT", "GCLOUD_PROJECT")
	case "GOOGLE_VERTEX_LOCATION":
		return cmp.Or(first(name, "GOOGLE_CLOUD_LOCATION", "VERTEX_LOCATION"), "global")
	case "GOOGLE_VERTEX_ENDPOINT":
		if v := env(name); v != "" {
			return v
		}
		if loc := Lookup("GOOGLE_VERTEX_LOCATION", env); loc != "global" {
			return loc + "-aiplatform.googleapis.com"
		}
		return "aiplatform.googleapis.com"
	}
	return env(name)
}
