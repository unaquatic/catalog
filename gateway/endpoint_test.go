package gateway

import (
	"slices"
	"testing"
)

// TestLookup checks the clouds' variables: from the variables their SDKs
// read too, with their defaults, and Vertex AI's host from its location.
func TestLookup(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
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
// Google's credentials.
func TestKeyEnv(t *testing.T) {
	tests := []struct {
		ep   Endpoint
		want []string
	}{
		{Endpoint{API: "https://${AZURE_RESOURCE_NAME}.openai.azure.com/openai/v1", Auth: Auth{Scheme: AuthHeader, Header: "api-key"},
			Env: []string{"AZURE_RESOURCE_NAME", "AZURE_API_KEY"}}, []string{"AZURE_API_KEY"}},
		{Endpoint{Auth: Auth{Scheme: AuthGoogle}, Env: []string{"GOOGLE_VERTEX_PROJECT", "GOOGLE_APPLICATION_CREDENTIALS"}}, nil},
		{Endpoint{Auth: Auth{Scheme: AuthBearer}, Env: []string{"OPENROUTER_API_KEY"}}, []string{"OPENROUTER_API_KEY"}},
	}
	for _, tt := range tests {
		if got := tt.ep.KeyEnv(); !slices.Equal(got, tt.want) {
			t.Errorf("KeyEnv(%+v) = %v, want %v", tt.ep, got, tt.want)
		}
	}
}
