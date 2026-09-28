resource "gezor_kms_key" "lake" {
  description = "Lake encryption key"
  alias_name  = "alias/lake"
}
