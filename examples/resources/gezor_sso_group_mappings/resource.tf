data "gezor_roles" "all" {}

resource "gezor_sso_group_mappings" "entra" {
  provider_id = gezor_sso_provider.entra.id

  mapping = [
    { idp_group = "data-engineering", role_id = gezor_role.data_engineer.id },
    { idp_group = "analysts", role_id = data.gezor_roles.all.by_key["analyst"] },
  ]

  default_role_id = data.gezor_roles.all.by_key["viewer"]
  sync_groups     = true
}
