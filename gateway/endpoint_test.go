package gateway

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/unaquatic/catalog/modelsdev"
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

const (
	awsEnv    = "AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_REGION AWS_BEARER_TOKEN_BEDROCK"
	vertexAPI = "https://${GOOGLE_VERTEX_ENDPOINT}/v1/projects/${GOOGLE_VERTEX_PROJECT}/locations/${GOOGLE_VERTEX_LOCATION}"
)

func TestProviderEndpoints(t *testing.T) {
	c, _ := buildEndpoints(t)
	tests := []struct {
		id   ProviderID
		want Endpoint
	}{
		{"amazon-bedrock", Endpoint{Format: FormatBedrockConverse, API: "https://bedrock-runtime.${AWS_REGION}.amazonaws.com",
			APIDefault: true, Auth: Auth{Scheme: AuthAWS}}},
		{"llmgateway-providers", Endpoint{Format: FormatOpenAIChat, API: "https://api.llmgateway.io/v1", Auth: Auth{Scheme: AuthBearer}}},
		{"google-vertex", Endpoint{Format: FormatGemini, API: vertexAPI + "/publishers/google", APIDefault: true, Auth: Auth{Scheme: AuthGoogle}}},
		{"azure", Endpoint{Format: FormatOpenAIResponses, API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1", APIDefault: true,
			Auth: Auth{Scheme: AuthHeader, Header: "api-key"}}},
		// A package the catalog does not know: no format, no auth.
		{"gitlab", Endpoint{}},
	}
	for _, tt := range tests {
		ep := c.Providers[tt.id].Endpoint
		ep.Env = nil
		if !ep.Equal(tt.want) {
			t.Errorf("%s endpoint = %+v, want %+v", tt.id, ep, tt.want)
		}
	}
	if env := c.Providers["amazon-bedrock"].Endpoint.Env; !slices.Equal(env, strings.Fields(awsEnv)) {
		t.Errorf("bedrock env = %q, want models.dev's", env)
	}
}

func TestOfferingEndpoints(t *testing.T) {
	c, rep := buildEndpoints(t)
	tests := []struct {
		provider ProviderID
		id       string
		own      bool // the offering has an endpoint of its own
		want     Endpoint
	}{
		// Bedrock's Converse entries take the provider's endpoint; Mantle
		// takes OpenAI Responses at a URL of its own, with AWS auth.
		{"amazon-bedrock", "global.openai.gpt-6.1-sol", false, Endpoint{Format: FormatBedrockConverse,
			API: "https://bedrock-runtime.${AWS_REGION}.amazonaws.com", APIDefault: true, Auth: Auth{Scheme: AuthAWS}}},
		{"amazon-bedrock", "openai.gpt-6.1-sol", true, Endpoint{Format: FormatOpenAIResponses,
			API: "https://bedrock-mantle.${AWS_REGION}.api.aws/openai/v1", Auth: Auth{Scheme: AuthAWS}}},
		// Vertex: Anthropic's own format, at the SDK's default URL; an
		// OpenAI-compatible endpoint with a Google token; Mistral, which no
		// format describes, keeps its URL and auth.
		{"google-vertex", "claude-opus-4-1@20250805", true, Endpoint{Format: FormatAnthropicVertex,
			API: vertexAPI + "/publishers/anthropic/models", APIDefault: true, Auth: Auth{Scheme: AuthGoogle}}},
		{"google-vertex", "zai-org/glm-5.2-maas", true, Endpoint{Format: FormatOpenAIChat,
			API: vertexAPI + "/endpoints/openapi", Auth: Auth{Scheme: AuthGoogle}}},
		{"google-vertex", "codestral-2", true, Endpoint{API: vertexAPI + "/publishers/mistralai/models", Auth: Auth{Scheme: AuthGoogle}}},
		{"google-vertex", "gemini-3.7-flash", false, Endpoint{Format: FormatGemini, API: vertexAPI + "/publishers/google", APIDefault: true,
			Auth: Auth{Scheme: AuthGoogle}}},
		// Azure: Anthropic's package sends x-api-key; a completions shape
		// keeps Azure's URL and api-key header.
		{"azure", "claude-fable-5", true, Endpoint{Format: FormatAnthropicMessages,
			API: "https://${AZURE_RESOURCE_NAME}.services.ai.azure.com/anthropic/v1", Auth: Auth{Scheme: AuthHeader, Header: "x-api-key"}}},
		{"azure", "model-router", true, Endpoint{Format: FormatOpenAIChat, API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1",
			APIDefault: true, Auth: Auth{Scheme: AuthHeader, Header: "api-key"}}},
		{"azure", "gpt-5.1", false, Endpoint{Format: FormatOpenAIResponses, API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1",
			APIDefault: true, Auth: Auth{Scheme: AuthHeader, Header: "api-key"}}},
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
	want := []string{
		`gitlab: unknown request format for npm package "gitlab-ai-provider"`,
		`google-vertex/codestral-2: Mistral on Vertex AI takes chat bodies at {model}:streamRawPredict, which no format describes`,
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
		want  Format
		ok    bool
	}{
		{"@ai-sdk/openai", "", FormatOpenAIResponses, true},
		{"@ai-sdk/openai", modelsdev.ShapeCompletions, FormatOpenAIChat, true},
		{"@ai-sdk/openai-compatible", "", FormatOpenAIChat, true},
		{"@ai-sdk/openai-compatible", modelsdev.ShapeResponses, FormatOpenAIResponses, true},
		{"@ai-sdk/amazon-bedrock/mantle", modelsdev.ShapeResponses, FormatOpenAIResponses, true},
		{"@ai-sdk/azure", "", FormatOpenAIResponses, true},
		{"@ai-sdk/anthropic", "", FormatAnthropicMessages, true},
		{"@ai-sdk/google-vertex/anthropic", "", FormatAnthropicVertex, true},
		{"@ai-sdk/google-vertex", "", FormatGemini, true},
		{"@ai-sdk/amazon-bedrock", "", FormatBedrockConverse, true},
		{"@openrouter/ai-sdk-provider", "", FormatOpenAIChat, true},
		{"@ai-sdk/gateway", "", FormatOpenAIChat, true},
		{"gitlab-ai-provider", "", "", false},
	}
	for _, tt := range tests {
		if got, ok := FormatOf(tt.npm, tt.shape); got != tt.want || ok != tt.ok {
			t.Errorf("FormatOf(%q, %q) = %q, %v; want %q, %v", tt.npm, tt.shape, got, ok, tt.want, tt.ok)
		}
	}
}

// TestLookup checks the clouds' variables: from the variables their SDKs
// read too, with their defaults, and Vertex AI's host from its location.
func TestLookup(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"AWS_REGION", map[string]string{"AWS_REGION": "eu-west-1", "AWS_DEFAULT_REGION": "us-west-2"}, "eu-west-1"},
		{"AWS_REGION", map[string]string{"AWS_DEFAULT_REGION": "us-west-2"}, "us-west-2"},
		{"AWS_REGION", nil, "us-east-1"},
		{"GOOGLE_VERTEX_PROJECT", map[string]string{"GOOGLE_CLOUD_PROJECT": "p"}, "p"},
		{"GOOGLE_VERTEX_PROJECT", nil, ""},
		{"GOOGLE_VERTEX_LOCATION", nil, "global"},
		{"GOOGLE_VERTEX_ENDPOINT", nil, "aiplatform.googleapis.com"},
		{"GOOGLE_VERTEX_ENDPOINT", map[string]string{"GOOGLE_CLOUD_LOCATION": "us-central1"}, "us-central1-aiplatform.googleapis.com"},
		{"GOOGLE_VERTEX_ENDPOINT", map[string]string{"GOOGLE_VERTEX_ENDPOINT": "private.example"}, "private.example"},
		{"OTHER", map[string]string{"OTHER": "v"}, "v"},
	}
	for _, tt := range tests {
		if got := Lookup(tt.name, func(k string) string { return tt.env[k] }); got != tt.want {
			t.Errorf("Lookup(%s) with %v = %q, want %q", tt.name, tt.env, got, tt.want)
		}
	}
}

// TestKeyEnv checks which variables hold an API key: not a base URL's, not
// AWS's or Google's credentials, but Bedrock's API key.
func TestKeyEnv(t *testing.T) {
	tests := []struct {
		ep   Endpoint
		want []string
	}{
		{Endpoint{API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1", Auth: Auth{Scheme: AuthHeader, Header: "api-key"},
			Env: []string{"AZURE_RESOURCE_NAME", "AZURE_API_KEY"}}, []string{"AZURE_API_KEY"}},
		{Endpoint{Auth: Auth{Scheme: AuthAWS}, Env: []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION", BedrockKeyEnv}}, []string{BedrockKeyEnv}},
		{Endpoint{Auth: Auth{Scheme: AuthGoogle}, Env: []string{"GOOGLE_VERTEX_PROJECT", "GOOGLE_APPLICATION_CREDENTIALS"}}, nil},
		{Endpoint{Auth: Auth{Scheme: AuthBearer}, Env: []string{"OPENROUTER_API_KEY"}}, []string{"OPENROUTER_API_KEY"}},
	}
	for _, tt := range tests {
		if got := tt.ep.KeyEnv(); !slices.Equal(got, tt.want) {
			t.Errorf("KeyEnv(%+v) = %v, want %v", tt.ep, got, tt.want)
		}
	}
}
