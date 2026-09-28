data "gezor_permissions" "all" {}

resource "gezor_role" "data_engineer" {
  key         = "data_engineer"
  name        = "Data engineer"
  description = "Runs pipelines and manages topics"
  permissions = [
    "clusters.read",
    "clusters.manage",
  ]
}
