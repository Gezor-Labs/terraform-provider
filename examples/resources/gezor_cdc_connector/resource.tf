resource "gezor_cdc_connector" "orders_db" {
  cluster_id         = gezor_cluster.prod.id
  instance_id        = gezor_cdc_instance.finance.instance_id
  name               = "orders-db"
  database_type      = "postgresql"
  database_hostname  = "orders.internal.acme.com"
  database_port      = 5432
  database_user      = "debezium"
  database_password  = var.orders_db_password
  database_dbname    = "orders"
  table_include_list = "public.orders,public.order_items"
}

# Through a Secure Access connector instead of a direct connection:
resource "gezor_cdc_connector" "billing_db" {
  cluster_id          = gezor_cluster.prod.id
  instance_id         = "cdc-1"
  name                = "billing-db"
  database_type       = "postgresql"
  database_user       = "debezium"
  database_password   = var.billing_db_password
  database_dbname     = "billing"
  access_via          = "connector"
  access_connector_id = gezor_secure_access_connector.dc1.id
  access_endpoint_id  = gezor_secure_access_endpoint.billing.id
}
