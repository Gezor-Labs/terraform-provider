resource "gezor_secure_access_endpoint" "billing" {
  connector_id  = gezor_secure_access_connector.dc1.id
  name          = "billing"
  host          = "10.20.4.15"
  port          = 5432
  database_type = "postgresql"
}
