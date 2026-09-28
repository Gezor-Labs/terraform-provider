data "gezor_roles" "all" {}

resource "gezor_sso_group_mappings" "entra" {
  provider_id = gezor_sso_provider.entra.id

  mapping = [
    { idp_group = "gezor-admins", role_id = data.gezor_roles.all.by_key["admin"] },
    { idp_group = "data-engineering", role_id = gezor_role.data_engineer.id },
  ]

  default_role_id = data.gezor_roles.all.by_key["viewer"]
  sync_groups     = true
}
