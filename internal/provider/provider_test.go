package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderContract(t *testing.T) {
	t.Parallel()
	p := New("test")()
	var schemaResponse provider.SchemaResponse
	p.Schema(context.Background(), provider.SchemaRequest{}, &schemaResponse)
	if schemaResponse.Diagnostics.HasError() {
		t.Fatalf("provider schema diagnostics: %v", schemaResponse.Diagnostics)
	}
	token, ok := schemaResponse.Schema.Attributes["token"]
	if !ok || !token.IsSensitive() {
		t.Fatal("provider token must be present and sensitive")
	}
	resources := p.Resources(context.Background())
	got := map[string]bool{}
	for _, constructor := range resources {
		var response resource.MetadataResponse
		constructor().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "vappcloud"}, &response)
		got[response.TypeName] = true
	}
	for _, name := range []string{
		"vappcloud_project", "vappcloud_device", "vappcloud_compute_instance",
		"vappcloud_vmm", "vappcloud_application_instance",
	} {
		if !got[name] {
			t.Errorf("missing resource %s", name)
		}
	}
	if len(p.DataSources(context.Background())) != 20 {
		t.Fatalf("expected 20 data sources, got %d", len(p.DataSources(context.Background())))
	}
}
