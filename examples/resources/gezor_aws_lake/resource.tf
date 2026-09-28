resource "gezor_aws_lake" "this" {
  bronze_uri = "s3://acme-lake-bronze"
  silver_uri = "s3://acme-lake-silver"
  gold_uri   = "s3://acme-lake-gold"
  prefix     = "gzr/"

  depends_on = [gezor_aws_connection.this]
}
