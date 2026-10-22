package build

import (
	"os"
	"slices"
	"testing"

	"github.com/unaquatic/catalog/gateway"
	"github.com/unaquatic/catalog/internal/modelsdev"
)

// buildEndpoints maps testdata/endpoints.json, live models.dev entries of
// the clouds and a gateway, with every provider.
func buildEndpoints(t *testing.T) (*Listings, Report) {
	t.Helper()
	data, err := os.ReadFile("testdata/endpoints.json")
	if err != nil {
		t.Fatal(err)
	}
	b, err := modelsdev.DecodeBundle(data)
	if err != nil {
		t.Fatal(err)
	}
	c, rep, err := ListModelsDev(b, Options{})
	if err != nil {
		t.Fatal(err)
	}
	return c, rep
}

const vertexAPI = "https://${GOOGLE_VERTEX_ENDPOINT}/v1/projects/${GOOGLE_VERTEX_PROJECT}/locations/${GOOGLE_VERTEX_LOCATION}"

func TestProviderEndpoints(t *testing.T) {
	c, _ := buildEndpoints(t)
	tests := []struct {
		id   gateway.ProviderID
		want gateway.Endpoint
	}{
		{"llmgateway-providers", gateway.Endpoint{Format: gateway.FormatOpenAIChat, API: "https://api.llmgateway.io/v1", Auth: gateway.Auth{Scheme: gateway.AuthBearer}}},
		{"google-vertex", gateway.Endpoint{Format: gateway.FormatGemini, API: vertexAPI + "/publishers/google", APIDefault: true, Auth: gateway.Auth{Scheme: gateway.AuthGoogle}}},
		{"azure", gateway.Endpoint{Format: gateway.FormatOpenAIResponses, API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1", APIDefault: true,
			Auth: gateway.Auth{Scheme: gateway.AuthHeader, Header: "api-key"}}},
	}
	for _, tt := range tests {
		ep := c.Providers[tt.id].Endpoint
		ep.Env = nil
		if !ep.Equal(tt.want) {
			t.Errorf("%s endpoint = %+v, want %+v", tt.id, ep, tt.want)
		}
	}
	// Packages the catalog knows no format of, Bedrock's and GitLab's,
	// leave their providers out.
	for _, id := range []gateway.ProviderID{"amazon-bedrock", "gitlab"} {
		if _, ok := c.Providers[id]; ok {
			t.Errorf("%s is listed, whose request format the catalog does not know", id)
		}
	}
}

func TestOfferingEndpoints(t *testing.T) {
	c, rep := buildEndpoints(t)
	tests := []struct {
		provider gateway.ProviderID
		id       string
		own      bool // the offering has an endpoint of its own
		want     gateway.Endpoint
	}{
		// Vertex: Anthropic's own format, at the SDK's default URL; an
		// OpenAI-compatible endpoint with a Google token. Mistral's, which no
		// format describes, is left out.
		{"google-vertex", "claude-opus-4-1@20250805", true, gateway.Endpoint{Format: gateway.FormatAnthropicVertex,
			API: vertexAPI + "/publishers/anthropic/models", APIDefault: true, Auth: gateway.Auth{Scheme: gateway.AuthGoogle}}},
		{"google-vertex", "zai-org/glm-5.2-maas", true, gateway.Endpoint{Format: gateway.FormatOpenAIChat,
			API: vertexAPI + "/endpoints/openapi", Auth: gateway.Auth{Scheme: gateway.AuthGoogle}}},
		{"google-vertex", "gemini-3.7-flash", false, gateway.Endpoint{Format: gateway.FormatGemini, API: vertexAPI + "/publishers/google", APIDefault: true,
			Auth: gateway.Auth{Scheme: gateway.AuthGoogle}}},
		// Azure: Anthropic's package sends x-api-key; a completions shape
		// keeps Azure's URL and api-key header.
		{"azure", "claude-fable-5", true, gateway.Endpoint{Format: gateway.FormatAnthropicMessages,
			API: "https://${AZURE_RESOURCE_NAME}.services.ai.azure.com/anthropic/v1", Auth: gateway.Auth{Scheme: gateway.AuthHeader, Header: "x-api-key"}}},
		{"azure", "model-router", true, gateway.Endpoint{Format: gateway.FormatOpenAIChat, API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1",
			APIDefault: true, Auth: gateway.Auth{Scheme: gateway.AuthHeader, Header: "api-key"}}},
		{"azure", "gpt-5.1", false, gateway.Endpoint{Format: gateway.FormatOpenAIResponses, API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1",
			APIDefault: true, Auth: gateway.Auth{Scheme: gateway.AuthHeader, Header: "api-key"}}},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider)+"/"+tt.id, func(t *testing.T) {
			o := mustListing(t, c, tt.provider, tt.id)
			if (o.Endpoint != nil) != tt.own {
				t.Errorf("own endpoint = %+v, want one: %v", o.Endpoint, tt.own)
			}
			ep, ok := c.EndpointOf(Ref{Provider: tt.provider, ID: tt.id})
			want := tt.want
			want.Env = c.Providers[tt.provider].Endpoint.Env
			if !ok || !ep.Equal(want) {
				t.Errorf("EndpointOf = %+v, %v; want %+v", ep, ok, want)
			}
		})
	}
	if _, ok := c.EndpointOf(Ref{Provider: "azure", ID: "nope"}); ok {
		t.Error("EndpointOf found an offering the catalog does not list")
	}
	if _, ok := c.Providers["google-vertex"].Models["codestral-2"]; ok {
		t.Error("codestral-2 is listed, whose request format the catalog does not know")
	}
	want := []string{
		`gitlab: unknown request format for npm package "gitlab-ai-provider"`,
		`gitlab: left out: the catalog does not know its request format`,
		`google-vertex/codestral-2: Mistral on Vertex AI takes chat bodies at {model}:streamRawPredict, which no format describes`,
		`google-vertex/codestral-2: left out: the catalog does not know its endpoint's request format`,
	}
	var got []string
	for _, n := range rep.Notes {
		if n.Provider == "gitlab" || n.Model == "codestral-2" {
			got = append(got, n.String())
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("notes = %q, want %q", got, want)
	}
}

func TestFormatOf(t *testing.T) {
	tests := []struct {
		npm   string
		shape modelsdev.APIShape
		want  gateway.Format
		ok    bool
	}{
		{"@ai-sdk/openai", "", gateway.FormatOpenAIResponses, true},
		{"@ai-sdk/openai", modelsdev.ShapeCompletions, gateway.FormatOpenAIChat, true},
		{"@ai-sdk/openai-compatible", "", gateway.FormatOpenAIChat, true},
		{"@ai-sdk/openai-compatible", modelsdev.ShapeResponses, gateway.FormatOpenAIResponses, true},
		{"@ai-sdk/amazon-bedrock/mantle", modelsdev.ShapeResponses, "", false},
		{"@ai-sdk/azure", "", gateway.FormatOpenAIResponses, true},
		{"@ai-sdk/anthropic", "", gateway.FormatAnthropicMessages, true},
		{"@ai-sdk/google-vertex/anthropic", "", gateway.FormatAnthropicVertex, true},
		{"@ai-sdk/google-vertex", "", gateway.FormatGemini, true},
		{"@ai-sdk/amazon-bedrock", "", "", false},
		{"@openrouter/ai-sdk-provider", "", gateway.FormatOpenAIChat, true},
		{"@ai-sdk/gateway", "", gateway.FormatOpenAIChat, true},
		{"gitlab-ai-provider", "", "", false},
	}
	for _, tt := range tests {
		if got, ok := FormatOf(tt.npm, tt.shape); got != tt.want || ok != tt.ok {
			t.Errorf("FormatOf(%q, %q) = %q, %v; want %q, %v", tt.npm, tt.shape, got, ok, tt.want, tt.ok)
		}
	}
}
