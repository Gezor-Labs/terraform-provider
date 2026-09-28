resource "gezor_pipeline" "orders" {
  cluster_id  = gezor_cluster.prod.id
  name        = "orders-bronze-to-silver"
  description = "Clean orders"
  graph       = file("${path.module}/pipelines/orders.json")
}
