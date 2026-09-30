package provider

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// Live tests against a dedicated Gezor test organization. They create and delete workspaces and
// make requests that fail on purpose, so they refuse to run anywhere else. Run with:
//
//	TF_ACC=1 GEZOR_TOKEN=gzr_sa_... GEZOR_TEST_ORG_ID=org_... [GEZOR_ENDPOINT=...] [GEZOR_TEST_CLUSTER_ID=cl_...] go test ./internal/provider -run TestLive
func liveCheck(t *testing.T) {
	if os.Getenv("GEZOR_TOKEN") == "" {
		t.Skip("GEZOR_TOKEN is not set")
	}
	want := os.Getenv("GEZOR_TEST_ORG_ID")
	if want == "" {
		t.Skip("GEZOR_TEST_ORG_ID is not set (the id of the dedicated test organization)")
	}
	if got, err := liveOrgID(); err != nil {
		t.Fatalf("could not read the token's organization: %v", err)
	} else if got != want {
		t.Fatalf("GEZOR_TOKEN belongs to %s, not the test organization %s; refusing to run live tests", got, want)
	}
}

var liveOrg struct {
	once sync.Once
	id   string
	err  error
}

func liveOrgID() (string, error) {
	liveOrg.once.Do(func() {
		var me struct {
			Organization struct {
				ID string `json:"id"`
			} `json:"organization"`
		}
		liveOrg.err = liveClient().Get(context.Background(), "/api/auth/me", "", &me)
		liveOrg.id = me.Organization.ID
	})
	return liveOrg.id, liveOrg.err
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
