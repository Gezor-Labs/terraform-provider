package provider

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Live tests against a real Gezor organization. Run with:
//
//	TF_ACC=1 GEZOR_TOKEN=gzr_sa_... [GEZOR_ENDPOINT=...] [GEZOR_TEST_CLUSTER_ID=cl_...] go test ./internal/provider -run TestLive
func liveCheck(t *testing.T) {
	if os.Getenv("GEZOR_TOKEN") == "" {
		t.Skip("GEZOR_TOKEN is not set")
	}
}

func TestLiveRoleAndDataSources(t *testing.T) {
	liveCheck(t)
	key := fmt.Sprintf("tf_acc_%d", time.Now().Unix())
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { liveCheck(t) },
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
data "gezor_current_identity" "me" {}
data "gezor_permissions" "all" {}
data "gezor_roles" "all" {}

resource "gezor_role" "acc" {
  key         = %q
  name        = "Terraform acceptance"
  permissions = ["clusters.read"]
}
`, key),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.gezor_current_identity.me", "workspace_id"),
					resource.TestCheckTypeSetElemAttr("data.gezor_permissions.all", "ids.*", "clusters.read"),
					resource.TestCheckResourceAttrSet("gezor_role.acc", "id"),
				),
			},
			{ResourceName: "gezor_role.acc", ImportState: true, ImportStateVerify: true},
		},
	})
}

func TestLiveKafkaTopic(t *testing.T) {
	liveCheck(t)
	cluster := os.Getenv("GEZOR_TEST_CLUSTER_ID")
	if cluster == "" {
		t.Skip("GEZOR_TEST_CLUSTER_ID is not set (an online cluster with Event Streams)")
	}
	name := fmt.Sprintf("tf-acc-%d", time.Now().Unix())
	cfg := func(partitions int) string {
		return fmt.Sprintf(`
resource "gezor_kafka_topic" "acc" {
  cluster_id = %q
  name       = %q
  partitions = %d
  configs    = { "retention.ms" = "3600000" }
}
`, cluster, name, partitions)
	}
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { liveCheck(t) },
		ProtoV6ProviderFactories: testProviderFactories,
		Steps: []resource.TestStep{
			{Config: cfg(1), Check: resource.TestCheckResourceAttr("gezor_kafka_topic.acc", "partitions", "1")},
			{Config: cfg(2), Check: resource.TestCheckResourceAttr("gezor_kafka_topic.acc", "partitions", "2")},
		},
	})
}
