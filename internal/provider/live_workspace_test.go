package provider

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

func liveClient() *client.Client {
	return client.New(firstNonEmpty(os.Getenv("GEZOR_ENDPOINT"), client.DefaultEndpoint),
		os.Getenv("GEZOR_TOKEN"), os.Getenv("GEZOR_WORKSPACE"), "terraform-provider-gezor-live-test")
}

func liveSuffix() string { return strconv.FormatInt(time.Now().Unix(), 36) }

// importAt imports a resource from the workspace named by the resource's `workspace` attribute.
func importAt(name string, attrs ...string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		id, err := importID(name, attrs...)(s)
		if err != nil {
			return "", err
		}
		return id + "@" + s.RootModule().Resources[name].Primary.Attributes["workspace"], nil
	}
}

// checkWorkspaceDeleted confirms the throwaway workspace is gone after destroy.
func checkWorkspaceDeleted(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "gezor_workspace" {
			continue
		}
		var out struct {
			Tenant struct {
				Status string `json:"status"`
			} `json:"tenant"`
		}
		err := liveClient().Get(context.Background(), "/api/tenants/"+client.PathEscape(rs.Primary.ID), "", &out)
		if err == nil && out.Tenant.Status == "active" {
			return fmt.Errorf("workspace %s still exists", rs.Primary.ID)
		}
		if err != nil && !client.IsNotFound(err) {
			return err
		}
	}
	return nil
}

// TestLiveWorkspace runs every workspace-scoped resource in a throwaway workspace.
func TestLiveWorkspace(t *testing.T) {
	liveCheck(t)
	sfx := liveSuffix()
	type v struct {
		wsName, keyAlias, defaultKey, ssoName, adminGroup string
		lineage, mfa                                      bool
		sessionSeconds                                    int
		rolePerms                                         string
	}
	cfg := func(c v) string {
		return fmt.Sprintf(`
resource "gezor_workspace" "e2e" {
  name        = %[2]q
  slug        = "tf-e2e-%[1]s"
  description = "Terraform provider end-to-end test"
}

locals { ws = gezor_workspace.e2e.slug }

resource "gezor_workspace_settings" "e2e" {
  workspace = local.ws
  icon      = "database"
  modules = {
    pipelines          = true
    fraud              = false
    aml                = false
    enterprise_lineage = %[6]t
  }
  security = {
    session_absolute_seconds = %[8]d
    session_idle_seconds     = 3600
    require_mfa              = %[7]t
    allow_password_login     = true
  }
}

data "gezor_roles" "all" {
  workspace  = local.ws
  depends_on = [gezor_role.eng]
}

resource "gezor_role" "eng" {
  workspace   = local.ws
  key         = "data_engineer"
  name        = "Data engineer"
  description = "Pipelines and clusters"
  permissions = [%[9]s]
}

resource "gezor_kms_key" "a" {
  workspace   = local.ws
  description = "E2E key A"
  alias_name  = %[3]q
}

resource "gezor_kms_key" "b" {
  workspace   = local.ws
  description = "E2E key B"
}

resource "gezor_kms_default_key" "this" {
  workspace = local.ws
  key_id    = gezor_kms_key.%[4]s.id
}

resource "gezor_sso_provider" "entra" {
  workspace       = local.ws
  kind            = "entra"
  display_name    = %[5]q
  entra_tenant_id = "0e2e0000-0000-4000-8000-%012[10]d"
  client_id       = "0e2e0000-0000-4000-8000-000000000001"
  client_secret   = "not-a-real-secret"
  status          = "ready"
}

resource "gezor_sso_group_mappings" "entra" {
  workspace   = local.ws
  provider_id = gezor_sso_provider.entra.id
  mapping = [
    { idp_group = %[11]q, role_id = data.gezor_roles.all.by_key["analyst"] },
    { idp_group = "e2e-engineers", role_id = gezor_role.eng.id },
  ]
  default_role_id = data.gezor_roles.all.by_key["viewer"]
  sync_groups     = true
}

resource "gezor_sso_domain" "e2e" {
  workspace   = local.ws
  domain      = "tf-e2e-%[1]s.example.com"
  provider_id = gezor_sso_provider.entra.id
}

data "gezor_current_identity" "me" {
  workspace  = local.ws
  depends_on = [gezor_workspace.e2e]
}
data "gezor_workspace" "e2e" { id = gezor_workspace.e2e.id }
data "gezor_workspaces" "all" { depends_on = [gezor_workspace.e2e] }
data "gezor_permissions" "all" {}
data "gezor_kms_keys" "e2e" {
  workspace  = local.ws
  depends_on = [gezor_kms_default_key.this]
}
`, sfx, c.wsName, c.keyAlias, c.defaultKey, c.ssoName, c.lineage, c.mfa, c.sessionSeconds, c.rolePerms,
			time.Now().Unix()%1_000_000_000_000, c.adminGroup)
	}
	first := v{"TF E2E " + sfx, "alias/e2e-a", "a", "E2E Entra", "e2e-analysts", true, false, 43200, `"clusters.read", "pipelines.read"`}
	second := v{"TF E2E " + sfx + " renamed", "alias/e2e-a2", "b", "E2E Entra renamed", "e2e-platform", false, true, 28800, `"clusters.read", "clusters.manage", "pipelines.read"`}
	ws := "gezor_workspace.e2e"

	imports := []resource.TestStep{
		{ResourceName: ws, ImportState: true, ImportStateVerify: true},
		{ResourceName: "gezor_workspace_settings.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_workspace_settings.e2e", "id")},
		{ResourceName: "gezor_role.eng", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_role.eng", "id")},
		{ResourceName: "gezor_kms_key.a", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_kms_key.a", "id")},
		{ResourceName: "gezor_kms_default_key.this", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_kms_default_key.this", "id")},
		{ResourceName: "gezor_sso_provider.entra", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc:       importAt("gezor_sso_provider.entra", "id"),
			ImportStateVerifyIgnore: []string{"client_secret"}},
		{ResourceName: "gezor_sso_group_mappings.entra", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_sso_group_mappings.entra", "provider_id")},
		{ResourceName: "gezor_sso_domain.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_sso_domain.e2e", "domain")},
	}

	steps := []resource.TestStep{
		{
			Config: cfg(first),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(ws, "is_primary", "false"),
				resource.TestCheckResourceAttrPair("data.gezor_current_identity.me", "workspace_id", ws, "id"),
				resource.TestMatchResourceAttr("data.gezor_current_identity.me", "name", regexp.MustCompile(`^terraform-e2e`)),
				resource.TestCheckResourceAttrPair("data.gezor_workspace.e2e", "slug", ws, "slug"),
				resource.TestCheckResourceAttr("gezor_workspace_settings.e2e", "modules.enterprise_lineage", "true"),
				resource.TestCheckResourceAttr("gezor_workspace_settings.e2e", "security.session_absolute_seconds", "43200"),
				resource.TestCheckResourceAttr("gezor_role.eng", "permissions.#", "2"),
				resource.TestCheckResourceAttrPair("gezor_kms_default_key.this", "key_id", "gezor_kms_key.a", "id"),
				resource.TestCheckResourceAttrPair("data.gezor_kms_keys.e2e", "default_key_id", "gezor_kms_key.a", "id"),
				resource.TestCheckResourceAttr("gezor_kms_key.a", "key_state", "Enabled"),
				resource.TestCheckResourceAttr("gezor_sso_provider.entra", "status", "ready"),
				resource.TestCheckResourceAttr("gezor_sso_provider.entra", "has_client_secret", "true"),
				resource.TestCheckResourceAttrSet("gezor_sso_provider.entra", "redirect_uri"),
				resource.TestCheckResourceAttr("gezor_sso_group_mappings.entra", "mapping.#", "2"),
				resource.TestCheckResourceAttr("gezor_sso_group_mappings.entra", "mapping.0.idp_group", "e2e-analysts"),
				resource.TestCheckResourceAttr("gezor_sso_domain.e2e", "verified", "false"),
				resource.TestCheckResourceAttrSet("gezor_sso_domain.e2e", "txt_record"),
			),
		},
		{
			Config: cfg(second),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(ws, "name", second.wsName),
				resource.TestCheckResourceAttr("gezor_workspace_settings.e2e", "modules.enterprise_lineage", "false"),
				resource.TestCheckResourceAttr("gezor_workspace_settings.e2e", "security.require_mfa", "true"),
				resource.TestCheckResourceAttr("gezor_workspace_settings.e2e", "security.session_absolute_seconds", "28800"),
				resource.TestCheckResourceAttr("gezor_role.eng", "permissions.#", "3"),
				resource.TestCheckResourceAttr("gezor_kms_key.a", "alias_name", "alias/e2e-a2"),
				resource.TestCheckResourceAttrPair("gezor_kms_default_key.this", "key_id", "gezor_kms_key.b", "id"),
				resource.TestCheckResourceAttr("gezor_sso_provider.entra", "display_name", "E2E Entra renamed"),
				resource.TestCheckResourceAttr("gezor_sso_group_mappings.entra", "mapping.0.idp_group", "e2e-platform"),
			),
		},
		// Moving the default key changes is_default on the other key, which Terraform only sees on refresh.
		{RefreshState: true, Check: resource.TestCheckResourceAttr("gezor_kms_key.a", "is_default", "false")},
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { liveCheck(t) },
		ProtoV6ProviderFactories: testProviderFactories,
		CheckDestroy:             checkWorkspaceDeleted,
		Steps:                    append(steps, imports...),
	})
}

// TestLiveCryptoMode switches a throwaway workspace between platform and customer managed keys.
// Customer mode creates a key and makes it the default, so it is kept apart from gezor_kms_default_key.
func TestLiveCryptoMode(t *testing.T) {
	liveCheck(t)
	sfx := liveSuffix()
	cfg := func(mode string) string {
		return fmt.Sprintf(`
resource "gezor_workspace" "e2e" {
  name = "TF E2E crypto %[1]s"
  slug = "tf-e2e-crypto-%[1]s"
}

resource "gezor_aws_crypto_mode" "this" {
  workspace = gezor_workspace.e2e.slug
  mode      = %[2]q
}
`, sfx, mode)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { liveCheck(t) },
		ProtoV6ProviderFactories: testProviderFactories,
		CheckDestroy:             checkWorkspaceDeleted,
		Steps: []resource.TestStep{
			{Config: cfg("customer"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("gezor_aws_crypto_mode.this", "mode", "customer"),
				resource.TestCheckResourceAttrSet("gezor_aws_crypto_mode.this", "customer_key_fingerprint"),
			)},
			{Config: cfg("platform"), Check: resource.TestCheckResourceAttr("gezor_aws_crypto_mode.this", "mode", "platform")},
			{ResourceName: "gezor_aws_crypto_mode.this", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importAt("gezor_aws_crypto_mode.this", "id")},
		},
	})
}
