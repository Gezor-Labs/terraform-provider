package provider

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

// checkConnectorRunning waits until the operator reports the CDC connector RUNNING.
func checkConnectorRunning(name string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[name]
		if !ok {
			return fmt.Errorf("%s not in state", name)
		}
		a := rs.Primary.Attributes
		p := cdcInstancePath(a["cluster_id"], a["instance_id"]) + "/status"
		last := "not reported yet"
		for deadline := time.Now().Add(10 * time.Minute); time.Now().Before(deadline); time.Sleep(15 * time.Second) {
			var out struct {
				Status struct {
					Connectors []map[string]any `json:"connectors"`
				} `json:"status"`
			}
			if err := liveClient().Get(context.Background(), p, a["workspace"], &out); err != nil {
				return err
			}
			for _, c := range out.Status.Connectors {
				if asString(c["name"]) != a["name"] {
					continue
				}
				if asString(c["state"]) == "RUNNING" && asInt(c["tasks"]) > 0 {
					return nil
				}
				last = fmt.Sprintf("state %q, %d tasks running: %s", asString(c["state"]), asInt(c["tasks"]), asString(c["message"]))
			}
		}
		return fmt.Errorf("connector %s is not running: %s", a["name"], last)
	}
}

// TestLiveHostedCluster creates a throwaway workspace with a Gezor hosted cluster and runs every
// cluster resource on it. It takes 15 to 30 minutes, so it only runs with GEZOR_TEST_HOSTED=1.
//
// Set GEZOR_TEST_PG_HOST and GEZOR_TEST_PG_PASSWORD to also test a CDC connector against a
// PostgreSQL server (wal_level=logical) that the new cluster can reach, with database
// GEZOR_TEST_PG_DB (default "shop") holding tables public.orders and public.customers.
func TestLiveHostedCluster(t *testing.T) {
	liveCheck(t)
	if os.Getenv("GEZOR_TEST_HOSTED") != "1" {
		t.Skip("GEZOR_TEST_HOSTED is not 1")
	}
	sfx := liveSuffix()
	type v struct {
		clusterDesc, heap, sacDesc, dbtDesc, pipelineGraph, notebookSQL, schema string
		partitions, endpointPort                                                int
	}
	cfg := func(c v) string {
		return fmt.Sprintf(`
resource "gezor_workspace" "e2e" {
  name = "TF E2E cluster %[1]s"
  slug = "tf-e2e-cl-%[1]s"
}

locals { ws = gezor_workspace.e2e.slug }

resource "gezor_workspace_settings" "e2e" {
  workspace = local.ws
  modules   = { pipelines = true }
}

resource "gezor_cluster" "e2e" {
  workspace    = local.ws
  name         = "e2e"
  description  = %[2]q
  environment  = "development"
  region       = "eu-central-1"
  hosting_mode = "gezor_hosted"
  tags         = ["e2e", "terraform"]
}

resource "gezor_cluster_install_token" "e2e" {
  workspace   = local.ws
  cluster_id  = gezor_cluster.e2e.id
  ttl_seconds = 3600
  label       = "e2e"
}

resource "gezor_cluster_app" "kafka" {
  workspace           = local.ws
  cluster_id          = gezor_cluster.e2e.id
  app                 = "kafka"
  deletion_protection = false
}

resource "gezor_cluster_app" "schemas" {
  workspace           = local.ws
  cluster_id          = gezor_cluster.e2e.id
  app                 = "schemaRegistry"
  deletion_protection = false
  depends_on          = [gezor_cluster_app.kafka]
}

resource "gezor_cluster_app" "connect" {
  workspace           = local.ws
  cluster_id          = gezor_cluster.e2e.id
  app                 = "connect"
  deletion_protection = false
  depends_on          = [gezor_cluster_app.kafka]
}

resource "gezor_cluster_app" "garage" {
  workspace           = local.ws
  cluster_id          = gezor_cluster.e2e.id
  app                 = "garage"
  deletion_protection = false
}

resource "gezor_cluster_app" "iceberg" {
  workspace           = local.ws
  cluster_id          = gezor_cluster.e2e.id
  app                 = "iceberg"
  deletion_protection = false
  depends_on          = [gezor_cluster_app.garage]
}

resource "gezor_cluster_app" "trino" {
  workspace           = local.ws
  cluster_id          = gezor_cluster.e2e.id
  app                 = "trino"
  deletion_protection = false
  depends_on          = [gezor_cluster_app.iceberg]
}

resource "gezor_kafka_topic" "orders" {
  workspace  = local.ws
  cluster_id = gezor_cluster.e2e.id
  name       = "e2e.orders"
  partitions = %[8]d
  configs    = { "retention.ms" = "3600000" }
  depends_on = [gezor_cluster_app.kafka]
}

resource "gezor_schema_subject" "orders" {
  workspace     = local.ws
  cluster_id    = gezor_cluster.e2e.id
  subject       = "e2e.orders-value"
  schema_type   = "AVRO"
  compatibility = "BACKWARD"
  schema        = %[7]s
  depends_on    = [gezor_cluster_app.schemas]
}

resource "gezor_cdc_instance" "e2e" {
  workspace   = local.ws
  cluster_id  = gezor_cluster.e2e.id
  instance_id = "cdc-2"
  name        = "gzr-cdc-e2e"
  replicas    = 1
  heap_opts   = %[3]q
  depends_on  = [gezor_cluster_app.connect]
}

resource "gezor_secure_access_connector" "e2e" {
  workspace  = local.ws
  name       = "e2e-gateway"
  cluster_id = gezor_cluster.e2e.id
  allow = [
    { cidr = "10.20.0.0/16", description = %[4]q },
  ]
}

resource "gezor_secure_access_endpoint" "e2e" {
  workspace     = local.ws
  connector_id  = gezor_secure_access_connector.e2e.id
  name          = "billing"
  host          = "10.20.4.15"
  port          = %[9]d
  database_type = "postgresql"
}

resource "gezor_dbt_project" "e2e" {
  workspace   = local.ws
  cluster_id  = gezor_cluster.e2e.id
  name        = "e2e-marts"
  description = %[5]q
  schema_name = "gold"
  files = {
    "dbt_project.yml"   = "name: e2e_marts\nversion: '1.0'\nprofile: gzr\n"
    "models/orders.sql" = "select 1 as id"
  }
  depends_on = [gezor_workspace_settings.e2e, gezor_cluster_app.trino]
}

resource "gezor_pipeline" "e2e" {
  workspace   = local.ws
  cluster_id  = gezor_cluster.e2e.id
  name        = "e2e-orders"
  description = "Clean orders"
  graph       = %[6]s
  depends_on  = [gezor_workspace_settings.e2e]
}

resource "gezor_pipeline_notebook" "e2e" {
  workspace  = local.ws
  cluster_id = gezor_cluster.e2e.id
  path       = "silver/e2e_orders.sql"
  language   = "sql"
  content    = %[10]q
  depends_on = [gezor_workspace_settings.e2e]
}

data "gezor_cluster" "e2e" {
  workspace  = local.ws
  name       = gezor_cluster.e2e.name
  depends_on = [gezor_cluster_app.kafka, gezor_cluster_app.schemas, gezor_cluster_app.connect, gezor_cluster_app.trino]
}

data "gezor_clusters" "all" {
  workspace  = local.ws
  depends_on = [gezor_cluster.e2e]
}
`, sfx, c.clusterDesc, c.heap, c.sacDesc, c.dbtDesc, c.pipelineGraph, c.schema, c.partitions, c.endpointPort, c.notebookSQL)
	}

	schemaV1 := `jsonencode({ type = "record", name = "Order", fields = [{ name = "id", type = "string" }] })`
	schemaV2 := `jsonencode({ type = "record", name = "Order", fields = [{ name = "id", type = "string" }, { name = "note", type = ["null", "string"], default = null }] })`
	first := v{"End-to-end test", "-Xms256m -Xmx768m", "Data center databases", "Finance marts",
		`jsonencode({ nodes = [], edges = [] })`, "select 1", schemaV1, 1, 5432}
	second := v{"End-to-end test, updated", "-Xms256m -Xmx1g", "Data center databases (updated)", "Finance marts v2",
		`jsonencode({ nodes = [{ id = "src", type = "source", data = { table = "orders" } }], edges = [] })`, "select 2", schemaV2, 2, 5433}

	cl := "gezor_cluster.e2e"
	steps := []resource.TestStep{
		{
			Config: cfg(first),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(cl, "hosting_mode", "gezor_hosted"),
				resource.TestCheckResourceAttr(cl, "online", "true"),
				resource.TestCheckResourceAttr("gezor_cluster_install_token.e2e", "status", "active"),
				resource.TestCheckResourceAttrSet("gezor_cluster_install_token.e2e", "token"),
				resource.TestCheckTypeSetElemAttr("data.gezor_cluster.e2e", "enabled_apps.*", "kafka"),
				resource.TestCheckTypeSetElemAttr("data.gezor_cluster.e2e", "enabled_apps.*", "schemaRegistry"),
				resource.TestCheckTypeSetElemAttr("data.gezor_cluster.e2e", "enabled_apps.*", "connect"),
				resource.TestCheckTypeSetElemAttr("data.gezor_cluster.e2e", "enabled_apps.*", "trino"),
				resource.TestCheckResourceAttr("data.gezor_clusters.all", "clusters.#", "1"),
				resource.TestCheckResourceAttr("gezor_kafka_topic.orders", "partitions", "1"),
				resource.TestCheckResourceAttr("gezor_schema_subject.orders", "version", "1"),
				resource.TestCheckResourceAttr("gezor_cdc_instance.e2e", "replicas", "1"),
				resource.TestCheckResourceAttrSet("gezor_secure_access_connector.e2e", "install_token"),
				resource.TestCheckResourceAttrSet("gezor_secure_access_endpoint.e2e", "tunnel_host"),
				resource.TestCheckResourceAttr("gezor_dbt_project.e2e", "files.%", "2"),
				resource.TestCheckResourceAttrSet("gezor_pipeline.e2e", "id"),
				resource.TestCheckResourceAttrSet("gezor_pipeline_notebook.e2e", "id"),
			),
		},
		{
			Config: cfg(second),
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr(cl, "description", second.clusterDesc),
				resource.TestCheckResourceAttr("gezor_kafka_topic.orders", "partitions", "2"),
				resource.TestCheckResourceAttr("gezor_schema_subject.orders", "version", "2"),
				resource.TestCheckResourceAttr("gezor_cdc_instance.e2e", "heap_opts", second.heap),
				resource.TestCheckResourceAttr("gezor_secure_access_connector.e2e", "allow.0.description", second.sacDesc),
				resource.TestCheckResourceAttr("gezor_secure_access_endpoint.e2e", "port", "5433"),
				resource.TestCheckResourceAttr("gezor_dbt_project.e2e", "description", second.dbtDesc),
				resource.TestCheckResourceAttr("gezor_pipeline_notebook.e2e", "content", "select 2"),
			),
		},
		{ResourceName: cl, ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc:       importAt(cl, "id"),
			ImportStateVerifyIgnore: []string{"install_token", "install_token_expires_at", "install_command"}},
		{ResourceName: "gezor_cluster_install_token.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc:       importAt("gezor_cluster_install_token.e2e", "cluster_id", "id"),
			ImportStateVerifyIgnore: []string{"token", "install_command", "ttl_seconds", "operator_namespace"}},
		{ResourceName: "gezor_cluster_app.kafka", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_cluster_app.kafka", "cluster_id", "app")},
		{ResourceName: "gezor_kafka_topic.orders", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_kafka_topic.orders", "cluster_id", "name")},
		{ResourceName: "gezor_schema_subject.orders", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc:       importAt("gezor_schema_subject.orders", "cluster_id", "subject"),
			ImportStateVerifyIgnore: []string{"permanent_delete"}},
		{ResourceName: "gezor_cdc_instance.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_cdc_instance.e2e", "cluster_id", "instance_id")},
		{ResourceName: "gezor_secure_access_connector.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc:       importAt("gezor_secure_access_connector.e2e", "id"),
			ImportStateVerifyIgnore: []string{"install", "install_token", "install_token_expires_at"}},
		{ResourceName: "gezor_secure_access_endpoint.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_secure_access_endpoint.e2e", "connector_id", "id")},
		{ResourceName: "gezor_dbt_project.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_dbt_project.e2e", "cluster_id", "id")},
		{ResourceName: "gezor_pipeline.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_pipeline.e2e", "cluster_id", "id")},
		{ResourceName: "gezor_pipeline_notebook.e2e", ImportState: true, ImportStateVerify: true,
			ImportStateIdFunc: importAt("gezor_pipeline_notebook.e2e", "cluster_id", "id")},
	}

	if host := os.Getenv("GEZOR_TEST_PG_HOST"); host != "" {
		db := firstNonEmpty(os.Getenv("GEZOR_TEST_PG_DB"), "shop")
		connector := func(tables string) string {
			return cfg(second) + fmt.Sprintf(`
resource "gezor_cdc_connector" "pg" {
  workspace          = local.ws
  cluster_id         = gezor_cluster.e2e.id
  instance_id        = gezor_cdc_instance.e2e.instance_id
  name               = "e2e-shop"
  database_type      = "postgresql"
  database_hostname  = %q
  database_port      = 5432
  database_user      = %q
  database_password  = %q
  database_dbname    = %q
  table_include_list = %q
}
`, host, firstNonEmpty(os.Getenv("GEZOR_TEST_PG_USER"), "postgres"), os.Getenv("GEZOR_TEST_PG_PASSWORD"), db, tables)
		}
		steps = append(steps,
			resource.TestStep{Config: connector("public.orders"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttrSet("gezor_cdc_connector.pg", "topic_prefix"),
				checkConnectorRunning("gezor_cdc_connector.pg"),
			)},
			resource.TestStep{Config: connector("public.orders,public.customers"), Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("gezor_cdc_connector.pg", "table_include_list", "public.orders,public.customers"),
				checkConnectorRunning("gezor_cdc_connector.pg"),
			)},
			resource.TestStep{ResourceName: "gezor_cdc_connector.pg", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc:       importAt("gezor_cdc_connector.pg", "cluster_id", "instance_id", "name"),
				ImportStateVerifyIgnore: []string{"database_password"}},
		)
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { liveCheck(t) },
		ProtoV6ProviderFactories: testProviderFactories,
		CheckDestroy:             checkWorkspaceDeleted,
		Steps:                    steps,
	})
}
