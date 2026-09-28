resource "gezor_secure_access_connector" "dc1" {
  name       = "dc1-gateway"
  cluster_id = gezor_cluster.prod.id

  allow = [
    { cidr = "10.20.0.0/16", description = "Data center databases" },
  ]
}

output "sac_install_linux" {
  value     = gezor_secure_access_connector.dc1.install["linux"]
  sensitive = true
}
