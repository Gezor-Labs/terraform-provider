data "gezor_current_identity" "me" {}

output "token_permissions" {
  value = data.gezor_current_identity.me.permissions
}
