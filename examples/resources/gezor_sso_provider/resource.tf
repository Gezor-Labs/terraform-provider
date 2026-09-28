resource "gezor_sso_provider" "entra" {
  kind            = "entra"
  display_name    = "Acme Entra ID"
  entra_tenant_id = "00000000-0000-0000-0000-000000000000"
  client_id       = var.entra_client_id
  client_secret   = var.entra_client_secret
  status          = "active"
}
