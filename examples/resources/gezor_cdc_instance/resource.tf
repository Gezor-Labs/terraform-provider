resource "gezor_cdc_instance" "finance" {
  cluster_id  = gezor_cluster.prod.id
  instance_id = "cdc-2"
  name        = "cdc-finance"
  replicas    = 2

  depends_on = [gezor_cluster_app.connect]
}
