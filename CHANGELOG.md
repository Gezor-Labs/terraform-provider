## 0.1.1 (September 29, 2026)

NOTES:

* Documentation: resources and data sources are grouped by area in the Registry navigation, the overview follows the standard provider layout, and new guides cover getting started and importing existing resources.

## 0.1.0 (September 29, 2026)

FEATURES:

* **New resources:** `gezor_workspace`, `gezor_role`, `gezor_workspace_settings`, `gezor_sso_provider`, `gezor_sso_group_mappings`, `gezor_sso_domain`, `gezor_kms_key`, `gezor_kms_default_key`, `gezor_aws_connection`, `gezor_aws_lake`, `gezor_aws_crypto_mode`, `gezor_cluster`, `gezor_cluster_install_token`, `gezor_cluster_app`, `gezor_kafka_topic`, `gezor_schema_subject`, `gezor_cdc_instance`, `gezor_cdc_connector`, `gezor_secure_access_connector`, `gezor_secure_access_endpoint`, `gezor_dbt_project`, `gezor_pipeline`, `gezor_pipeline_notebook`
* **New data sources:** `gezor_current_identity`, `gezor_workspace`, `gezor_workspaces`, `gezor_roles`, `gezor_permissions`, `gezor_cluster`, `gezor_clusters`, `gezor_kms_keys`, `gezor_aws_cloudformation`
* Authentication with service account API tokens, with the workspace chosen per provider or per resource
* Every resource supports `terraform import`
