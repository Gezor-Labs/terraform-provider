resource "gezor_workspace_settings" "main" {
  description = "Production data platform"

  modules = {
    pipelines          = true
    fraud              = true
    aml                = false
    enterprise_lineage = true
  }

  security = {
    session_absolute_seconds = 43200
    session_idle_seconds     = 3600
    require_mfa              = true
    allow_password_login     = true
  }
}
