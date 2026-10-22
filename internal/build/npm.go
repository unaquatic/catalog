package build

import (
	"cmp"
	"slices"
	"strings"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/modelsdev"
)

// FormatOf returns the format of the AI SDK package npm that models.dev
// names, with API shape shape, and reports whether the catalog knows it.
// Gateways and labs whose packages speak OpenAI Chat Completions, such as
// OpenRouter, xAI and Mistral, take FormatOpenAIChat; Vercel's AI Gateway
// and Cohere take it at their OpenAI-compatible base URLs.
func FormatOf(npm string, shape modelsdev.APIShape) (gateway.Format, bool) {
	switch npm {
	case "@ai-sdk/openai", "@ai-sdk/azure":
		if shape == modelsdev.ShapeCompletions {
			return gateway.FormatOpenAIChat, true
		}
		return gateway.FormatOpenAIResponses, true
	case "@ai-sdk/openai-compatible":
		if shape == modelsdev.ShapeResponses {
			return gateway.FormatOpenAIResponses, true
		}
		return gateway.FormatOpenAIChat, true
	case "@ai-sdk/anthropic":
		return gateway.FormatAnthropicMessages, true
	case "@ai-sdk/google-vertex/anthropic":
		return gateway.FormatAnthropicVertex, true
	case "@ai-sdk/google", "@ai-sdk/google-vertex":
		return gateway.FormatGemini, true
	case "@ai-sdk/gateway", "@openrouter/ai-sdk-provider", "@ai-sdk/xai", "@ai-sdk/mistral", "@ai-sdk/groq",
		"@ai-sdk/cerebras", "@ai-sdk/togetherai", "@ai-sdk/deepinfra", "@ai-sdk/perplexity", "@ai-sdk/cohere":
		return gateway.FormatOpenAIChat, true
	}
	return "", false
}

// vertexBase is the base URL of a Vertex AI project's location.
const vertexBase = "https://${GOOGLE_VERTEX_ENDPOINT}/v1/projects/${GOOGLE_VERTEX_PROJECT}/locations/${GOOGLE_VERTEX_LOCATION}"

// DefaultAPIs are the base URLs that AI SDK packages use when models.dev
// lists none, by package.
var DefaultAPIs = map[string]string{
	"@ai-sdk/openai":                  "https://api.openai.com/v1",
	"@ai-sdk/anthropic":               "https://api.anthropic.com/v1",
	"@ai-sdk/google":                  "https://generativelanguage.googleapis.com/v1beta",
	"@ai-sdk/google-vertex":           vertexBase + "/publishers/google",
	"@ai-sdk/google-vertex/anthropic": vertexBase + "/publishers/anthropic/models",
	"@ai-sdk/gateway":                 "https://ai-gateway.vercel.sh/v1",
	"@openrouter/ai-sdk-provider":     "https://openrouter.ai/api/v1",
	"@ai-sdk/xai":                     "https://api.x.ai/v1",
	"@ai-sdk/mistral":                 "https://api.mistral.ai/v1",
	"@ai-sdk/groq":                    "https://api.groq.com/openai/v1",
	"@ai-sdk/cerebras":                "https://api.cerebras.ai/v1",
	"@ai-sdk/togetherai":              "https://api.together.xyz/v1",
	"@ai-sdk/deepinfra":               "https://api.deepinfra.com/v1/openai",
	"@ai-sdk/perplexity":              "https://api.perplexity.ai",
	"@ai-sdk/cohere":                  "https://api.cohere.ai/compatibility/v1",
}

// AzureAPIs are the default base URLs of the Azure providers, whose
// resource, named in an environment variable, is part of the URL.
var AzureAPIs = map[gateway.ProviderID]string{
	"azure":                    "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1",
	"azure-cognitive-services": "https://${AZURE_COGNITIVE_SERVICES_RESOURCE_NAME}.cognitiveservices.azure.com/openai/v1",
}

// ProviderAuths are the providers that authenticate the same way at every
// endpoint, whatever package speaks to it: GitHub Copilot takes its token
// as a bearer token at its Anthropic endpoint too.
var ProviderAuths = map[gateway.ProviderID]gateway.Auth{
	"github-copilot": {Scheme: gateway.AuthBearer},
}

// AuthOf returns how a request authenticates with package npm at provider
// pid, whose own package is providerNPM. A cloud's own credentials go with
// its host, whatever package speaks to it: Vertex AI's OpenAI-compatible
// endpoint takes a Google token; so do ProviderAuths. Otherwise the package decides, as Anthropic's sends
// "x-api-key". It reports false for a package the catalog does not know.
func AuthOf(pid gateway.ProviderID, providerNPM, npm string) (gateway.Auth, bool) {
	if a, ok := ProviderAuths[pid]; ok {
		return a, true
	}
	switch {
	case strings.HasPrefix(providerNPM, "@ai-sdk/google-vertex"):
		return gateway.Auth{Scheme: gateway.AuthGoogle}, true
	}
	switch npm {
	case "@ai-sdk/anthropic":
		return gateway.Auth{Scheme: gateway.AuthHeader, Header: "x-api-key"}, true
	case "@ai-sdk/google":
		return gateway.Auth{Scheme: gateway.AuthHeader, Header: "x-goog-api-key"}, true
	case "@ai-sdk/azure":
		return gateway.Auth{Scheme: gateway.AuthHeader, Header: "api-key"}, true
	}
	if _, ok := FormatOf(npm, ""); ok {
		return gateway.Auth{Scheme: gateway.AuthBearer}, true
	}
	return gateway.Auth{}, false
}

// endpoint returns the endpoint of provider pid, with package npm, API shape
// shape and base URL api, which models.dev may leave empty. It notes an
// endpoint whose format the catalog does not know, for model when it is
// not the provider's own.
func (b *builder) endpoint(pid gateway.ProviderID, model, npm string, shape modelsdev.APIShape, api string) gateway.Endpoint {
	p := b.bundle.Providers[modelsdev.ProviderID(pid)]
	ep := gateway.Endpoint{API: api, Env: slices.Clone(p.Env)}
	ep.Auth, _ = AuthOf(pid, p.NPM, npm)
	format, ok := FormatOf(npm, shape)
	switch {
	case !ok:
		b.note(pid, model, "unknown request format for npm package %q", npm)
		return ep
	case npm == "@ai-sdk/mistral" && strings.HasPrefix(p.NPM, "@ai-sdk/google-vertex"):
		// Vertex AI takes Mistral's chat bodies, model included, at
		// {model}:streamRawPredict, which no format describes.
		b.note(pid, model, "Mistral on Vertex AI takes chat bodies at {model}:streamRawPredict, which no format describes")
		return ep
	}
	ep.Format = format
	if ep.API == "" {
		ep.API = cmp.Or(AzureAPIs[pid], DefaultAPIs[npm])
		ep.APIDefault = ep.API != ""
	}
	return ep
}
