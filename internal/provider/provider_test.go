package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

func providerTestClient(t *testing.T, baseURL string) *client.Client {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"exp": time.Now().Add(time.Hour).Unix(), "session_id": "provider-test-session", "principal_type": "sts_session",
	})
	if err != nil {
		t.Fatal(err)
	}
	sessionToken := "eyJhbGciOiJFUzI1NiJ9." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
	c, err := client.New(baseURL, "VAPPASIAPROVIDER", "provider-test-secret", sessionToken, "test")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestProviderContract(t *testing.T) {
	t.Parallel()
	p := New("test")()
	if _, ok := p.(provider.ProviderWithConfigValidators); !ok {
		t.Fatal("provider must implement ProviderWithConfigValidators")
	}
	var schemaResponse provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("provider schema diagnostics: %v", schemaResponse.Diagnostics)
	}
	if _, ok := schemaResponse.Schema.Attributes["token"]; ok {
		t.Fatal("legacy bearer token configuration must not be present")
	}
	for _, name := range []string{"secret_access_key", "session_token", "credential_process"} {
		attribute, ok := schemaResponse.Schema.Attributes[name]
		if !ok || !attribute.IsSensitive() {
			t.Fatalf("provider %s must be present and sensitive", name)
		}
	}
	resources := p.Resources(context.Background())
	got := map[string]bool{}
	for _, constructor := range resources {
		managed := constructor()
		var response resource.MetadataResponse
		managed.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "vappcloud"}, &response)
		got[response.TypeName] = true
		var schemaResponse resource.SchemaResponse
		managed.Schema(context.Background(), resource.SchemaRequest{}, &schemaResponse)
		if schemaResponse.Schema.Version != 0 {
			t.Errorf("%s schema version = %d, want 0", response.TypeName, schemaResponse.Schema.Version)
		}
		identityResource, ok := managed.(resource.ResourceWithIdentity)
		if !ok {
			t.Errorf("%s does not implement ResourceWithIdentity", response.TypeName)
		} else {
			var identityResponse resource.IdentitySchemaResponse
			identityResource.IdentitySchema(context.Background(), resource.IdentitySchemaRequest{}, &identityResponse)
			if identityResponse.IdentitySchema.Version != 0 || identityResponse.IdentitySchema.Attributes["id"] == nil {
				t.Errorf("%s has an invalid identity schema", response.TypeName)
			}
		}
		if _, ok := managed.(resource.ResourceWithModifyPlan); !ok {
			t.Errorf("%s does not implement ResourceWithModifyPlan", response.TypeName)
		}
	}
	for _, name := range []string{
		"vappcloud_account", "vappcloud_device", "vappcloud_compute_instance",
		"vappcloud_vmm", "vappcloud_application_instance", "vappcloud_iam_policy",
		"vappcloud_iam_policy_version", "vappcloud_iam_policy_attachment", "vappcloud_iam_group",
	} {
		if !got[name] {
			t.Errorf("missing resource %s", name)
		}
	}
	if len(p.DataSources(context.Background())) != 20 {
		t.Fatalf("expected 20 data sources, got %d", len(p.DataSources(context.Background())))
	}
}

func TestCompleteMutationRejectsFailedOperation(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(client.Operation{
			ID: "op-failed", State: "failed",
			Error: &client.APIError{Code: "PROVISIONING_FAILED", Message: "fixture failure"},
		})
	}))
	defer server.Close()
	c := providerTestClient(t, server.URL)
	result := client.Mutation[client.VMM]{
		Resource:    client.VMM{ID: "vmm-test"},
		OperationID: "op-failed",
	}
	var diagnostics diag.Diagnostics
	if completeMutation(context.Background(), c, &result, time.Second,
		func(vmm client.VMM) string { return vmm.ID },
		func(id string) string { return "/v1/vmms/" + id },
		&diagnostics,
	) {
		t.Fatal("failed operation was treated as successful")
	}
	if !diagnostics.HasError() {
		t.Fatal("failed operation did not produce diagnostics")
	}
}

func TestCompleteMutationRecoversResourceByOperationID(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/operations/op-complete":
			_ = json.NewEncoder(w).Encode(client.Operation{
				ID: "op-complete", ResourceID: "vmm-recovered", State: "succeeded",
			})
		case "/v1/vmms/vmm-recovered":
			_ = json.NewEncoder(w).Encode(client.VMM{ID: "vmm-recovered", ResourceVersion: 1})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	c := providerTestClient(t, server.URL)
	result := client.Mutation[client.VMM]{OperationID: "op-complete"}
	var diagnostics diag.Diagnostics
	if !completeMutation(context.Background(), c, &result, time.Second,
		func(vmm client.VMM) string { return vmm.ID },
		func(id string) string { return "/v1/vmms/" + id },
		&diagnostics,
	) {
		t.Fatalf("resource recovery failed: %v", diagnostics)
	}
	if result.Resource.ID != "vmm-recovered" {
		t.Fatalf("unexpected recovered resource: %+v", result.Resource)
	}
}
