resource "gezor_cluster_install_token" "reinstall" {
  cluster_id  = gezor_cluster.prod.id
  ttl_seconds = 3600
  label       = "reinstall after node pool upgrade"
}
