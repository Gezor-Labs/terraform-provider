resource "gezor_cluster" "prod" {
  name        = "prod-eu"
  description = "Production, Frankfurt"
  environment = "production"
  region      = "eu-central-1"
  tags        = ["prod", "eu"]
}

# Run this once on the Kubernetes cluster to install the operator.
output "install_command" {
  value     = gezor_cluster.prod.install_command
  sensitive = true
}
