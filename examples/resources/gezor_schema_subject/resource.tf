resource "gezor_schema_subject" "orders_value" {
  cluster_id    = gezor_cluster.prod.id
  subject       = "orders-value"
  compatibility = "BACKWARD"
  schema = jsonencode({
    type = "record"
    name = "Order"
    fields = [
      { name = "id", type = "string" },
      { name = "total", type = "double" },
    ]
  })

  depends_on = [gezor_cluster_app.schemas]
}
