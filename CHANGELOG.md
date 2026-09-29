## 0.1.2 (September 29, 2026)

NOTES:

* The provider is now marked **Beta**. Resources and arguments may still change between minor versions before 1.0; breaking changes will be listed here.

ENHANCEMENTS:

* resource/gezor_cluster: New `wait_for_online` argument (default `true`). Hosted clusters are now waited on until they are provisioned and connected, so apps and topics can be created in the same apply.
* resource/gezor_cluster_app: New `wait_for_ready` argument (default `true`). The resource waits until the app reports Ready with the new settings before dependent resources run.

BUG FIXES:

* resource/gezor_kms_key: Creating a key without `alias_name` no longer fails with an inconsistent result; the generated alias is kept.
* resource/gezor_kms_default_key: Destroying it now switches the workspace back to the platform key, so the previous default key can be destroyed in the same run.
* resource/gezor_workspace_settings: No longer undoes a rename made by `gezor_workspace` in the same apply; name, description and icon are only sent when set in this resource.
* resource/gezor_workspace: Deleting a workspace uses its current name for confirmation, so it works after the name changed outside Terraform.
* resource/gezor_cluster_app, resource/gezor_cdc_instance, resource/gezor_cdc_connector: Changes to the same cluster are sent one at a time, so parallel applies no longer overwrite each other's settings.
* Operator-backed resources (topics, schema subjects, dbt and connector actions) no longer fail with "expired or unknown" when the command finished at the moment it was polled.
* resource/gezor_kafka_topic, resource/gezor_schema_subject: Retried for up to 3 minutes while the app refuses connections, since an app can report Ready shortly before it accepts them.
* resource/gezor_schema_subject: Imported or changed-outside-Terraform schemas are stored in `jsonencode` form, so a config using `jsonencode(...)` plans cleanly after import.

## 0.1.1 (September 29, 2026)

NOTES:

* Documentation: resources and data sources are grouped by area in the Registry navigation, the overview follows the standard provider layout, and new guides cover getting started and importing existing resources.

## 0.1.0 (September 29, 2026)

FEATURES:

* **New resources:** `gezor_workspace`, `gezor_role`, `gezor_workspace_settings`, `gezor_sso_provider`, `gezor_sso_group_mappings`, `gezor_sso_domain`, `gezor_kms_key`, `gezor_kms_default_key`, `gezor_aws_connection`, `gezor_aws_lake`, `gezor_aws_crypto_mode`, `gezor_cluster`, `gezor_cluster_install_token`, `gezor_cluster_app`, `gezor_kafka_topic`, `gezor_schema_subject`, `gezor_cdc_instance`, `gezor_cdc_connector`, `gezor_secure_access_connector`, `gezor_secure_access_endpoint`, `gezor_dbt_project`, `gezor_pipeline`, `gezor_pipeline_notebook`
* **New data sources:** `gezor_current_identity`, `gezor_workspace`, `gezor_workspaces`, `gezor_roles`, `gezor_permissions`, `gezor_cluster`, `gezor_clusters`, `gezor_kms_keys`, `gezor_aws_cloudformation`
* Authentication with service account API tokens, with the workspace chosen per provider or per resource
* Every resource supports `terraform import`
