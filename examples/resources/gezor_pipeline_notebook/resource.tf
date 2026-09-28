resource "gezor_pipeline_notebook" "clean_orders" {
  cluster_id = gezor_cluster.prod.id
  path       = "silver/clean_orders.sql"
  language   = "sql"
  content    = file("${path.module}/notebooks/clean_orders.sql")
}
