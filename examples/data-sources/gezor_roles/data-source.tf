data "gezor_roles" "all" {}

output "admin_role_id" {
  value = data.gezor_roles.all.by_key["admin"]
}
