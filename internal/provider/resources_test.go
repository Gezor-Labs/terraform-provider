package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func providerBlock(url string) string {
	return fmt.Sprintf(`
provider "gezor" {
  endpoint = %q
  token    = "gzr_sa_0123456789abcdef_0123456789012345678901234567890123456789"
  command_timeout_seconds = 10
}
`, url)
}

func TestAccRole(t *testing.T) {
	_, srv := newFakeAPI(t)
	cfg := func(name string, perms string) string {
		return providerBlock(srv.URL) + fmt.Sprintf(`
resource "gezor_role" "eng" {
  key         = "data_engineer"
  name        = %q
  permissions = [%s]
}
`, name, perms)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("Data engineer", `"clusters.read"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("gezor_role.eng", "id"),
					resource.TestCheckResourceAttr("gezor_role.eng", "permissions.#", "1"),
					resource.TestCheckTypeSetElemAttr("gezor_role.eng", "permissions.*", "clusters.read"),
				),
			},
			{
				Config: cfg("Engineers", `"clusters.read", "clusters.manage"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gezor_role.eng", "name", "Engineers"),
					resource.TestCheckResourceAttr("gezor_role.eng", "permissions.#", "2"),
				),
			},
			{ResourceName: "gezor_role.eng", ImportState: true, ImportStateVerify: true},
			{
				Config:      cfg("Engineers", `"clusters.read", "bogus.permission"`),
				ExpectError: regexp.MustCompile(`bogus.permission`),
			},
		},
	})
}

func TestAccWorkspaceAndSettings(t *testing.T) {
	_, srv := newFakeAPI(t)
	cfg := func(desc string, fraud bool) string {
		return providerBlock(srv.URL) + fmt.Sprintf(`
resource "gezor_workspace" "analytics" {
  name        = "Analytics"
  slug        = "analytics"
  description = %q
}

resource "gezor_workspace_settings" "main" {
  modules = {
    fraud = %t
  }
  security = {
    session_idle_seconds = 1800
  }
}

data "gezor_current_identity" "me" {}
data "gezor_workspaces" "all" { depends_on = [gezor_workspace.analytics] }
`, desc, fraud)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg("Team analytics", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gezor_workspace.analytics", "is_primary", "false"),
					resource.TestCheckResourceAttr("gezor_workspace_settings.main", "modules.fraud", "true"),
					resource.TestCheckResourceAttr("gezor_workspace_settings.main", "modules.pipelines", "true"),
					resource.TestCheckResourceAttr("gezor_workspace_settings.main", "security.session_idle_seconds", "1800"),
					resource.TestCheckResourceAttr("gezor_workspace_settings.main", "security.allow_password_login", "true"),
					resource.TestCheckResourceAttr("data.gezor_current_identity.me", "name", "terraform"),
					resource.TestCheckResourceAttr("data.gezor_workspaces.all", "workspaces.#", "2"),
				),
			},
			{
				Config: cfg("Renamed", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gezor_workspace.analytics", "description", "Renamed"),
					resource.TestCheckResourceAttr("gezor_workspace_settings.main", "modules.fraud", "false"),
				),
			},
			{ResourceName: "gezor_workspace.analytics", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestAccClusterAppsTopicsSchemas(t *testing.T) {
	_, srv := newFakeAPI(t)
	cfg := func(partitions int, retention string, protection bool, schema string) string {
		return providerBlock(srv.URL) + fmt.Sprintf(`
resource "gezor_cluster" "prod" {
  name = "prod"
  tags = ["prod"]
}

resource "gezor_cluster_app" "kafka" {
  cluster_id          = gezor_cluster.prod.id
  app                 = "kafka"
  deletion_protection = %t
  config              = jsonencode({ replicas = 3 })
}

resource "gezor_kafka_topic" "orders" {
  cluster_id = gezor_cluster.prod.id
  name       = "orders"
  partitions = %d
  configs    = { "retention.ms" = %q }
  depends_on = [gezor_cluster_app.kafka]
}

resource "gezor_schema_subject" "orders" {
  cluster_id    = gezor_cluster.prod.id
  subject       = "orders-value"
  schema        = jsonencode(%s)
  compatibility = "FULL"
}

resource "gezor_pipeline_notebook" "bronze" {
  cluster_id = gezor_cluster.prod.id
  path       = "bronze/orders.sql"
  content    = "select 1"
}
`, protection, partitions, retention, schema)
	}
	schemaV1 := `{ type = "record", name = "Order", fields = [{ name = "id", type = "string" }] }`
	schemaV2 := `{ type = "record", name = "Order", fields = [{ name = "id", type = "string" }, { name = "total", type = ["null", "double"], default = null }] }`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: cfg(3, "86400000", true, schemaV1),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gezor_cluster.prod", "install_token", "gzr_it_secret"),
					resource.TestCheckResourceAttr("gezor_cluster_app.kafka", "enabled", "true"),
					resource.TestMatchResourceAttr("gezor_cluster_app.kafka", "effective_config", regexp.MustCompile(`"storage"`)),
					resource.TestCheckResourceAttr("gezor_kafka_topic.orders", "replication_factor", "3"),
					resource.TestCheckResourceAttr("gezor_schema_subject.orders", "version", "1"),
					resource.TestCheckResourceAttr("gezor_schema_subject.orders", "compatibility", "FULL"),
				),
			},
			{
				Config: cfg(6, "3600000", false, schemaV2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("gezor_kafka_topic.orders", "partitions", "6"),
					resource.TestCheckResourceAttr("gezor_kafka_topic.orders", "configs.retention.ms", "3600000"),
					resource.TestCheckResourceAttr("gezor_schema_subject.orders", "version", "2"),
				),
			},
			{
				ResourceName: "gezor_kafka_topic.orders", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: importID("gezor_kafka_topic.orders", "cluster_id", "name"),
			},
			{
				ResourceName: "gezor_cluster_app.kafka", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc:       importID("gezor_cluster_app.kafka", "cluster_id", "app"),
				ImportStateVerifyIgnore: []string{"config"},
			},
			{
				Config:      cfg(3, "3600000", false, schemaV2),
				ExpectError: regexp.MustCompile(`Cannot remove partitions`),
			},
		},
	})
}

func TestAccClusterAppDeletionProtection(t *testing.T) {
	_, srv := newFakeAPI(t)
	base := providerBlock(srv.URL) + `
resource "gezor_cluster" "c" { name = "c" }
`
	app := `
resource "gezor_cluster_app" "flink" {
  cluster_id = gezor_cluster.c.id
  app        = "flink"
}
`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: base + app},
			{Config: base, ExpectError: regexp.MustCompile(`deletion_protection = false`)},
			{Config: base + `
resource "gezor_cluster_app" "flink" {
  cluster_id          = gezor_cluster.c.id
  app                 = "flink"
  deletion_protection = false
}
`},
			{Config: base},
		},
	})
}

func importID(name string, attrs ...string) resource.ImportStateIdFunc {
	return func(s *terraform.State) (string, error) {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return "", fmt.Errorf("%s not in state", name)
		}
		id := ""
		for i, a := range attrs {
			if i > 0 {
				id += "/"
			}
			id += rs.Primary.Attributes[a]
		}
		return id, nil
	}
}
