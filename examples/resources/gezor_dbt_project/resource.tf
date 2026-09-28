resource "gezor_dbt_project" "marts" {
  cluster_id  = gezor_cluster.prod.id
  name        = "marts"
  description = "Finance marts"
  schema_name = "gold"

  files = {
    "dbt_project.yml"   = file("${path.module}/dbt/dbt_project.yml")
    "models/orders.sql" = file("${path.module}/dbt/models/orders.sql")
  }
}
