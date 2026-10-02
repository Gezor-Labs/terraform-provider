---
page_title: "Get started with the Gezor provider"
subcategory: ""
description: |-
  Go from an empty workspace to a cluster with Kafka topics, schemas and single sign-on, all managed by Terraform.
---

# Get started with the Gezor provider

This guide takes you from an empty workspace to a registered cluster with Event Streams, a Kafka topic, a schema, a custom role and single sign-on, all managed by Terraform.

## Before you begin

- A Gezor Cloud organization. Sign up at [gezor.com](https://gezor.com).
- Terraform 1.9 or later.
- A Kubernetes cluster where you can run `helm`, to install the Gezor operator.

## 1. Create an API token

1. In the Gezor portal, open **Organization → API tokens**.
2. Under **Create service account**, name it `terraform`, give it the **Admin** role in the workspace you want to manage, and select **Create service account**.
3. Select **New token**, choose an expiry, and copy the token. It starts with `gzr_sa_` and is shown once.

```shell
export GEZOR_TOKEN="gzr_sa_..."
export GEZOR_WORKSPACE="production"
```

## 2. Configure the provider

Create `main.tf`:

```terraform
terraform {
  required_providers {
    gezor = {
      source  = "gezor-labs/gezor"
      version = "~> 0.1.0"
    }
  }
}

provider "gezor" {}

data "gezor_current_identity" "me" {}

output "workspace" {
  value = data.gezor_current_identity.me.workspace_id
}
```

Run `terraform init` and `terraform apply`. The output shows the workspace the token is using.

## 3. Register a cluster

```terraform
resource "gezor_cluster" "prod" {
  name        = "prod-eu"
  environment = "production"
  region      = "eu-central-1"
}

output "install_command" {
  value     = gezor_cluster.prod.install_command
  sensitive = true
}
```

Apply, then print the install command and run it against your Kubernetes cluster:

```shell
terraform output -raw install_command
```

The cluster shows as online in the portal within a minute.

## 4. Turn on apps

```terraform
resource "gezor_cluster_app" "kafka" {
  cluster_id = gezor_cluster.prod.id
  app        = "kafka"
}

resource "gezor_cluster_app" "schemas" {
  cluster_id = gezor_cluster.prod.id
  app        = "schemaRegistry"
  depends_on = [gezor_cluster_app.kafka]
}
```

Apps are protected from accidental removal: to remove one, set `deletion_protection = false`, apply, then delete the resource.

## 5. Create a topic and a schema

```terraform
resource "gezor_kafka_topic" "orders" {
  cluster_id = gezor_cluster.prod.id
  name       = "orders"
  partitions = 6
  configs    = { "retention.ms" = "604800000" }
  depends_on = [gezor_cluster_app.kafka]
}

resource "gezor_schema_subject" "orders_value" {
  cluster_id    = gezor_cluster.prod.id
  subject       = "orders-value"
  compatibility = "BACKWARD"
  schema = jsonencode({
    type   = "record"
    name   = "Order"
    fields = [
      { name = "id", type = "string" },
      { name = "total", type = "double" },
    ]
  })
  depends_on = [gezor_cluster_app.schemas]
}
```

The operator on the cluster makes these changes. Terraform waits for it to confirm each one.

## 6. Control access

```terraform
data "gezor_roles" "all" {}

resource "gezor_role" "data_engineer" {
  key         = "data_engineer"
  name        = "Data engineer"
  permissions = ["clusters.read", "clusters.manage", "pipelines.read", "pipelines.manage"]
}

resource "gezor_sso_provider" "entra" {
  kind            = "entra"
  display_name    = "Acme Entra ID"
  entra_tenant_id = var.entra_tenant_id
  client_id       = var.entra_client_id
  client_secret   = var.entra_client_secret
  status          = "active"
}

resource "gezor_sso_group_mappings" "entra" {
  provider_id = gezor_sso_provider.entra.id
  mapping = [
    { idp_group = "data-engineering", role_id = gezor_role.data_engineer.id },
    { idp_group = "analysts", role_id = data.gezor_roles.all.by_key["analyst"] },
  ]
  default_role_id = data.gezor_roles.all.by_key["viewer"]
}
```

People who sign in through SSO get the role mapped to their group. Terraform does not invite or remove people.

## Next steps

- Bring what you already set up in the portal under Terraform: [Importing existing resources](importing).
- Capture changes from your databases with `gezor_cdc_instance` and `gezor_cdc_connector`.
- Reach private databases with `gezor_secure_access_connector` and `gezor_secure_access_endpoint`.
- Build pipelines with `gezor_dbt_project`, `gezor_pipeline` and `gezor_pipeline_notebook`.
