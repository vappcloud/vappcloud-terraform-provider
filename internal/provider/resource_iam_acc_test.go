package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/vappcloud/vappcloud-terraform-provider/internal/client"
)

type iamAcceptanceAPI struct {
	mu          sync.Mutex
	policies    map[string]client.IAMPolicy
	versions    map[string][]client.IAMPolicyVersion
	attachments map[string]client.IAMPolicyAttachment
	groups      map[string]client.IAMGroup
	members     map[string]map[string]struct{}
	clock       int64
	versionSeq  int
}

func newIAMAcceptanceServer(t *testing.T) (*httptest.Server, *iamAcceptanceAPI) {
	t.Helper()
	api := &iamAcceptanceAPI{
		policies: map[string]client.IAMPolicy{}, versions: map[string][]client.IAMPolicyVersion{},
		attachments: map[string]client.IAMPolicyAttachment{}, groups: map[string]client.IAMGroup{},
		members: map[string]map[string]struct{}{},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.mu.Lock()
		defer api.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer header.payload.signature" {
			http.Error(w, `{"code":"UNAUTHENTICATED","message":"missing token"}`, http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodGet && !requireIdempotency(w, r) {
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/v1/iam/policies" && r.Method == http.MethodGet:
			items := make([]client.IAMPolicy, 0, len(api.policies))
			for _, item := range api.policies {
				items = append(items, item)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.IAMPolicy]{Items: items})
		case r.URL.Path == "/v1/iam/policies" && r.Method == http.MethodPost:
			var in struct {
				Name         string `json:"name"`
				Description  string `json:"description"`
				DocumentJSON string `json:"document_json"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			policy := client.IAMPolicy{
				ID: "pol-test", OrganizationID: 42, Name: in.Name, Description: in.Description,
				ARN: "arn:vapp:iam::42:policy/" + in.Name, DefaultVersion: "v1", DocumentJSON: in.DocumentJSON,
				CreatedAt: now, UpdatedAt: now,
			}
			api.policies[policy.ID] = policy
			api.versionSeq = 1
			api.versions[policy.ID] = []client.IAMPolicyVersion{{
				PolicyID: policy.ID, Version: "v1", DocumentJSON: in.DocumentJSON, IsDefault: true, CreatedAt: now,
			}}
			_ = json.NewEncoder(w).Encode(policy)
		case len(parts) == 4 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "policies":
			api.handlePolicy(w, r, parts[3])
		case len(parts) == 5 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "policies" && parts[4] == "versions":
			api.handlePolicyVersions(w, r, parts[3])
		case len(parts) == 6 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "policies" && parts[4] == "versions":
			api.handlePolicyVersion(w, r, parts[3], parts[5])
		case len(parts) == 7 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "policies" && parts[4] == "versions" && parts[6] == "default":
			api.setDefaultPolicyVersion(w, parts[3], parts[5])
		case r.URL.Path == "/v1/iam/attachments" && r.Method == http.MethodGet:
			items := make([]client.IAMPolicyAttachment, 0, len(api.attachments))
			for _, item := range api.attachments {
				items = append(items, item)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.IAMPolicyAttachment]{Items: items})
		case r.URL.Path == "/v1/iam/attachments" && r.Method == http.MethodPost:
			var in struct {
				PolicyID   string `json:"policy_id"`
				TargetType string `json:"target_type"`
				TargetID   string `json:"target_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			policy, ok := api.policies[in.PolicyID]
			if !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"policy not found"}`, http.StatusNotFound)
				return
			}
			attachment := client.IAMPolicyAttachment{
				PolicyID: policy.ID, PolicyARN: policy.ARN, PolicyName: policy.Name,
				TargetType: in.TargetType, TargetID: in.TargetID, CreatedBy: "principal-test", CreatedAt: api.now(),
			}
			api.attachments[iamPolicyAttachmentID(in.PolicyID, in.TargetType, in.TargetID)] = attachment
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case len(parts) == 6 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "attachments" && r.Method == http.MethodDelete:
			delete(api.attachments, iamPolicyAttachmentID(parts[3], parts[4], parts[5]))
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case r.URL.Path == "/v1/iam/groups" && r.Method == http.MethodGet:
			items := make([]client.IAMGroup, 0, len(api.groups))
			for _, item := range api.groups {
				item.MemberCount = int64(len(api.members[item.ID]))
				items = append(items, item)
			}
			_ = json.NewEncoder(w).Encode(client.Page[client.IAMGroup]{Items: items})
		case r.URL.Path == "/v1/iam/groups" && r.Method == http.MethodPost:
			var in struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&in)
			now := api.now()
			group := client.IAMGroup{
				ID: "grp-test", OrganizationID: 42, Name: in.Name,
				ARN: "arn:vapp:iam::42:group/" + in.Name, CreatedAt: now, UpdatedAt: now,
			}
			api.groups[group.ID] = group
			api.members[group.ID] = map[string]struct{}{}
			_ = json.NewEncoder(w).Encode(group)
		case len(parts) == 4 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "groups" && r.Method == http.MethodDelete:
			delete(api.groups, parts[3])
			delete(api.members, parts[3])
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		case len(parts) == 5 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "groups" && parts[4] == "members" && r.Method == http.MethodGet:
			members := make([]string, 0, len(api.members[parts[3]]))
			for id := range api.members[parts[3]] {
				members = append(members, id)
			}
			sort.Strings(members)
			_ = json.NewEncoder(w).Encode(client.IAMGroupMembers{PrincipalIDs: members})
		case len(parts) == 6 && parts[0] == "v1" && parts[1] == "iam" && parts[2] == "groups" && parts[4] == "members":
			if _, ok := api.groups[parts[3]]; !ok {
				http.Error(w, `{"code":"NOT_FOUND","message":"group not found"}`, http.StatusNotFound)
				return
			}
			switch r.Method {
			case http.MethodPut:
				api.members[parts[3]][parts[5]] = struct{}{}
			case http.MethodDelete:
				delete(api.members[parts[3]], parts[5])
			}
			_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	return server, api
}

func (a *iamAcceptanceAPI) now() time.Time {
	a.clock++
	return time.Unix(1_750_000_000+a.clock, 0).UTC()
}

func (a *iamAcceptanceAPI) handlePolicy(w http.ResponseWriter, r *http.Request, id string) {
	policy, ok := a.policies[id]
	if !ok {
		http.Error(w, `{"code":"NOT_FOUND","message":"policy not found"}`, http.StatusNotFound)
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(policy)
		return
	}
	if r.Method == http.MethodDelete {
		for _, attachment := range a.attachments {
			if attachment.PolicyID == id {
				http.Error(w, `{"code":"FAILED_PRECONDITION","message":"policy is attached"}`, http.StatusConflict)
				return
			}
		}
		delete(a.policies, id)
		delete(a.versions, id)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	http.Error(w, `{"code":"INVALID_ARGUMENT","message":"unsupported policy operation"}`, http.StatusBadRequest)
}

func (a *iamAcceptanceAPI) handlePolicyVersions(w http.ResponseWriter, r *http.Request, policyID string) {
	policy, ok := a.policies[policyID]
	if !ok {
		http.Error(w, `{"code":"NOT_FOUND","message":"policy not found"}`, http.StatusNotFound)
		return
	}
	if r.Method == http.MethodGet {
		_ = json.NewEncoder(w).Encode(client.Page[client.IAMPolicyVersion]{Items: a.versions[policyID]})
		return
	}
	var in struct {
		DocumentJSON string `json:"document_json"`
		SetDefault   bool   `json:"set_default"`
	}
	_ = json.NewDecoder(r.Body).Decode(&in)
	a.versionSeq++
	version := client.IAMPolicyVersion{
		PolicyID: policyID, Version: fmt.Sprintf("v%d", a.versionSeq), DocumentJSON: in.DocumentJSON,
		IsDefault: in.SetDefault, CreatedAt: a.now(),
	}
	if in.SetDefault {
		for index := range a.versions[policyID] {
			a.versions[policyID][index].IsDefault = false
		}
		policy.DefaultVersion = version.Version
		policy.DocumentJSON = version.DocumentJSON
		policy.UpdatedAt = version.CreatedAt
		a.policies[policyID] = policy
	}
	a.versions[policyID] = append(a.versions[policyID], version)
	_ = json.NewEncoder(w).Encode(version)
}

func (a *iamAcceptanceAPI) handlePolicyVersion(w http.ResponseWriter, r *http.Request, policyID, version string) {
	versions, ok := a.versions[policyID]
	if !ok {
		http.Error(w, `{"code":"NOT_FOUND","message":"policy not found"}`, http.StatusNotFound)
		return
	}
	for index, item := range versions {
		if item.Version != version {
			continue
		}
		if item.IsDefault {
			http.Error(w, `{"code":"FAILED_PRECONDITION","message":"default policy version cannot be deleted"}`, http.StatusConflict)
			return
		}
		a.versions[policyID] = append(versions[:index], versions[index+1:]...)
		_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		return
	}
	http.Error(w, `{"code":"NOT_FOUND","message":"policy version not found"}`, http.StatusNotFound)
}

func (a *iamAcceptanceAPI) setDefaultPolicyVersion(w http.ResponseWriter, policyID, version string) {
	policy, ok := a.policies[policyID]
	if !ok {
		http.Error(w, `{"code":"NOT_FOUND","message":"policy not found"}`, http.StatusNotFound)
		return
	}
	found := false
	for index := range a.versions[policyID] {
		isDefault := a.versions[policyID][index].Version == version
		a.versions[policyID][index].IsDefault = isDefault
		if isDefault {
			found = true
			policy.DefaultVersion = version
			policy.DocumentJSON = a.versions[policyID][index].DocumentJSON
			policy.UpdatedAt = a.now()
		}
	}
	if !found {
		http.Error(w, `{"code":"NOT_FOUND","message":"policy version not found"}`, http.StatusNotFound)
		return
	}
	a.policies[policyID] = policy
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (a *iamAcceptanceAPI) empty() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.policies) == 0 && len(a.attachments) == 0 && len(a.groups) == 0
}

func TestAccIAMResources(t *testing.T) {
	server, api := newIAMAcceptanceServer(t)
	defer server.Close()
	config := func(action string, members []string) string {
		membersJSON, _ := json.Marshal(members)
		return fmt.Sprintf(`
provider "vappcloud" {
  token   = "header.payload.signature"
  api_url = %q
}

resource "vappcloud_iam_policy" "operator" {
  name        = "TerraformOperator"
  description = "Managed by Terraform"
  document = jsonencode({
    Version = "2026-08-01"
    Statement = [{
      Effect   = "Allow"
      Action   = [%q]
      Resource = "*"
    }]
  })
}

resource "vappcloud_iam_policy_version" "audit" {
  policy_id     = vappcloud_iam_policy.operator.id
  set_as_default = false
  document = jsonencode({
    Version = "2026-08-01"
    Statement = [{
      Effect   = "Deny"
      Action   = ["vmm:DeleteVm"]
      Resource = "*"
    }]
  })
}

resource "vappcloud_iam_group" "operators" {
  name       = "Operators"
  member_ids = %s
}

resource "vappcloud_iam_policy_attachment" "operators" {
  policy_id  = vappcloud_iam_policy.operator.id
  target_type = "group"
  target_id   = vappcloud_iam_group.operators.id
}
`, server.URL, action, string(membersJSON))
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: providerFactories(),
		CheckDestroy: func(_ *terraform.State) error {
			if !api.empty() {
				return fmt.Errorf("IAM acceptance resources remain")
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config("vmm:GetVm", []string{"principal-user", "principal-service"}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_iam_policy.operator", "default_version", "v1"),
					resource.TestCheckResourceAttr("vappcloud_iam_policy_version.audit", "version", "v2"),
					resource.TestCheckResourceAttr("vappcloud_iam_group.operators", "member_count", "2"),
					resource.TestCheckResourceAttr("vappcloud_iam_policy_attachment.operators", "target_type", "group"),
				),
			},
			{
				Config: config("vmm:ListVms", []string{"principal-service", "principal-auditor"}),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PostApplyPostRefresh: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("vappcloud_iam_policy.operator", "default_version", "v3"),
					resource.TestCheckResourceAttr("vappcloud_iam_group.operators", "member_count", "2"),
					resource.TestCheckResourceAttr("vappcloud_iam_group.operators", "member_ids.#", "2"),
				),
			},
			{ResourceName: "vappcloud_iam_policy.operator", ImportState: true, ImportStateVerify: true},
			{
				ResourceName: "vappcloud_iam_policy_version.audit", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"set_as_default"},
			},
			{ResourceName: "vappcloud_iam_group.operators", ImportState: true, ImportStateVerify: true},
			{ResourceName: "vappcloud_iam_policy_attachment.operators", ImportState: true, ImportStateVerify: true},
		},
	})
}
