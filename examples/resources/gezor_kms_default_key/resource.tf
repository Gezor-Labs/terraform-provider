resource "gezor_kms_default_key" "this" {
  key_id = gezor_kms_key.lake.id
}
