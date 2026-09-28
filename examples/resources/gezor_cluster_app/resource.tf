resource "gezor_cluster_app" "kafka" {
  cluster_id = gezor_cluster.prod.id
  app        = "kafka"
  config = jsonencode({
    replicas = 3
  })
}

resource "gezor_cluster_app" "schemas" {
  cluster_id = gezor_cluster.prod.id
  app        = "schemaRegistry"
  depends_on = [gezor_cluster_app.kafka]
}

resource "gezor_cluster_app" "connect" {
  cluster_id = gezor_cluster.prod.id
  app        = "connect"
  depends_on = [gezor_cluster_app.kafka]
}
