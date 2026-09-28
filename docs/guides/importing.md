---
page_title: "Importing existing resources"
subcategory: ""
description: |-
  Bring workspaces, clusters, apps, topics and everything else you created in the Gezor portal under Terraform management.
---

# Importing existing resources

Every Gezor resource supports import, so you can start managing an existing setup with Terraform without recreating anything.

## Generate configuration automatically

With Terraform 1.5 or later, write `import` blocks and let Terraform generate the matching configuration:

```terraform
import {
  to = gezor_cluster.prod
  id = "cls_1a2b3c"
}

import {
  to = gezor_cluster_app.kafka
  id = "cls_1a2b3c/kafka"
}

import {
  to = gezor_kafka_topic.orders
  id = "cls_1a2b3c/orders"
}
```

```shell
terraform plan -generate-config-out=generated.tf
```

Review `generated.tf`, move it into your configuration, and run `terraform plan` again. It should report no changes.

## Import ids

Find ids in the portal, or read them with data sources such as `gezor_clusters`, `gezor_roles` and `gezor_kms_keys`.

| Resource | Import id |
|---|---|
| `gezor_workspace` | `<workspace_id>` |
| `gezor_role` | `<role_id>` |
| `gezor_workspace_settings` | `<workspace_id_or_slug>` |
| `gezor_sso_provider` | `<provider_id>` |
| `gezor_sso_group_mappings` | `<provider_id>` |
| `gezor_sso_domain` | `<domain>` |
| `gezor_kms_key` | `<key_id>` |
| `gezor_kms_default_key` | `default` |
| `gezor_aws_connection` | `aws` |
| `gezor_aws_lake` | `lake` |
| `gezor_aws_crypto_mode` | `crypto` |
| `gezor_cluster` | `<cluster_id>` |
| `gezor_cluster_install_token` | `<cluster_id>/<token_id>` |
| `gezor_cluster_app` | `<cluster_id>/<app>` |
| `gezor_kafka_topic` | `<cluster_id>/<topic>` |
| `gezor_schema_subject` | `<cluster_id>/<subject>` |
| `gezor_cdc_instance` | `<cluster_id>/<instance_id>` |
| `gezor_cdc_connector` | `<cluster_id>/<instance_id>/<name>` |
| `gezor_secure_access_connector` | `<connector_id>` |
| `gezor_secure_access_endpoint` | `<connector_id>/<endpoint_id>` |
| `gezor_dbt_project` | `<cluster_id>/<project_id>` |
| `gezor_pipeline` | `<cluster_id>/<pipeline_id>` |
| `gezor_pipeline_notebook` | `<cluster_id>/<notebook_id>` |

## Other workspaces

Imports run in the provider's workspace. To import from another workspace, append `@<workspace>` to the id, using the workspace's id or slug:

```shell
terraform import gezor_role.analyst role_9f8e@analytics
```

## Secrets are not imported

Passwords, client secrets and key material are never returned by the Gezor API. After importing a resource that has one, such as `gezor_sso_provider` or `gezor_cdc_connector`, set the secret in your configuration. Terraform sends it on the next apply.
