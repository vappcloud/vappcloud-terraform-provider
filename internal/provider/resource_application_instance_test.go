package provider

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestValidateApplicationPlacementConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		placements []map[string]attr.Value
		wantError  string
	}{
		{
			name: "single workload VMM",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-workload"), "replica_count": types.Int64Value(1)},
			},
		},
		{
			name: "multiple ingress VMMs and one workload VMM",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-ingress-a"), "replica_count": types.Int64Value(0)},
				{"vmm_id": types.StringValue("vmm-ingress-b"), "replica_count": types.Int64Value(0)},
				{"vmm_id": types.StringValue("vmm-workload"), "replica_count": types.Int64Value(2)},
			},
		},
		{
			name: "all ingress and no workload",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-ingress-a"), "replica_count": types.Int64Value(0)},
				{"vmm_id": types.StringValue("vmm-ingress-b"), "replica_count": types.Int64Value(0)},
			},
			wantError: "no replicas",
		},
		{
			name: "single VMM cannot have zero replicas",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-only"), "replica_count": types.Int64Value(0)},
			},
			wantError: "no replicas",
		},
		{
			name: "duplicate VMM",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-one"), "replica_count": types.Int64Value(0)},
				{"vmm_id": types.StringValue("vmm-one"), "replica_count": types.Int64Value(1)},
			},
			wantError: "Duplicate application placement",
		},
		{
			name: "negative replica count",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-invalid"), "replica_count": types.Int64Value(-1)},
			},
			wantError: "Invalid application replica count",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			values := make([]attr.Value, 0, len(test.placements))
			for _, placement := range test.placements {
				values = append(values, types.ObjectValueMust(placementAttributeTypes, placement))
			}
			list := types.ListValueMust(types.ObjectType{AttrTypes: placementAttributeTypes}, values)
			var diagnostics diag.Diagnostics
			validateApplicationPlacementConfig(list, &diagnostics)
			if test.wantError == "" {
				if diagnostics.HasError() {
					t.Fatalf("unexpected diagnostics: %v", diagnostics)
				}
				return
			}
			if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Summary(), test.wantError) {
				t.Fatalf("diagnostics = %v, want error containing %q", diagnostics, test.wantError)
			}
		})
	}
}

func TestApplicationPlanValuesPreservesZeroReplicaCount(t *testing.T) {
	t.Parallel()

	plan := applicationInstanceResourceModel{
		Source: types.ObjectValueMust(sourceAttributeTypes, map[string]attr.Value{
			"kind":                       types.StringValue("marketplace"),
			"marketplace_application_id": types.StringValue("catalog-test"),
			"marketplace_version_id":     types.StringValue("version-test"),
			"github_connection_id":       types.StringNull(),
			"repository":                 types.StringNull(),
			"ref":                        types.StringNull(),
		}),
		Placements: types.ListValueMust(types.ObjectType{AttrTypes: placementAttributeTypes}, []attr.Value{
			types.ObjectValueMust(placementAttributeTypes, map[string]attr.Value{
				"vmm_id": types.StringValue("vmm-ingress"), "replica_count": types.Int64Value(0),
			}),
			types.ObjectValueMust(placementAttributeTypes, map[string]attr.Value{
				"vmm_id": types.StringValue("vmm-workload"), "replica_count": types.Int64Value(2),
			}),
		}),
		SecretIDs: types.SetNull(types.StringType),
	}
	var diagnostics diag.Diagnostics
	_, placements, _ := applicationPlanValues(context.Background(), plan, &diagnostics)
	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
	if len(placements) != 2 || placements[0].ReplicaCount != 0 {
		t.Fatalf("zero replica count was not preserved: %+v", placements)
	}
	payload, err := json.Marshal(map[string]any{"placements": placements})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"vmmId":"vmm-ingress","replicaCount":0`) {
		t.Fatalf("zero replica count was not sent explicitly: %s", payload)
	}
}

func TestApplicationPlanValuesRejectsResolvedInvalidPlacement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		placements []map[string]attr.Value
		wantError  string
	}{
		{
			name: "resolved all-zero placement",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-ingress-a"), "replica_count": types.Int64Value(0)},
				{"vmm_id": types.StringValue("vmm-ingress-b"), "replica_count": types.Int64Value(0)},
			},
			wantError: "no replicas",
		},
		{
			name: "resolved duplicate placement",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-duplicate"), "replica_count": types.Int64Value(0)},
				{"vmm_id": types.StringValue("vmm-duplicate"), "replica_count": types.Int64Value(1)},
			},
			wantError: "Duplicate application placement",
		},
		{
			name: "resolved negative placement",
			placements: []map[string]attr.Value{
				{"vmm_id": types.StringValue("vmm-invalid"), "replica_count": types.Int64Value(-1)},
			},
			wantError: "Invalid application replica count",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			values := make([]attr.Value, 0, len(test.placements))
			for _, placement := range test.placements {
				values = append(values, types.ObjectValueMust(placementAttributeTypes, placement))
			}
			plan := applicationInstanceResourceModel{
				Source: types.ObjectValueMust(sourceAttributeTypes, map[string]attr.Value{
					"kind":                       types.StringValue("marketplace"),
					"marketplace_application_id": types.StringValue("catalog-test"),
					"marketplace_version_id":     types.StringValue("version-test"),
					"github_connection_id":       types.StringNull(),
					"repository":                 types.StringNull(),
					"ref":                        types.StringNull(),
				}),
				Placements: types.ListValueMust(types.ObjectType{AttrTypes: placementAttributeTypes}, values),
				SecretIDs:  types.SetNull(types.StringType),
			}
			var diagnostics diag.Diagnostics
			applicationPlanValues(context.Background(), plan, &diagnostics)
			if !diagnostics.HasError() || !strings.Contains(diagnostics.Errors()[0].Summary(), test.wantError) {
				t.Fatalf("diagnostics = %v, want error containing %q", diagnostics, test.wantError)
			}
		})
	}
}
