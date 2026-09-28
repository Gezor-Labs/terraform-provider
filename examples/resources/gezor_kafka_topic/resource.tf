resource "gezor_kafka_topic" "orders" {
  cluster_id = gezor_cluster.prod.id
  name       = "orders"
  partitions = 6

  configs = {
    "retention.ms"   = "604800000"
    "cleanup.policy" = "delete"
  }

  depends_on = [gezor_cluster_app.kafka]
}
