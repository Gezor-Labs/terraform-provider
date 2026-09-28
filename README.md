# Terraform provider for Gezor Cloud

Configure [Gezor Cloud](https://gezor.com) as code: workspaces, roles, workspace settings, single sign-on, encryption keys, AWS, clusters and their apps, Kafka topics, schemas, change data capture, Secure Access, dbt projects and pipelines.

The provider covers everything in the portal except adding and removing people. Invite people in the portal, or map identity provider groups to roles with `gezor_sso_group_mappings`.

Documentation: [`docs/`](docs/index.md), and [Terraform](https://gezor.com/docs/terraform/) on gezor.com.

## Quick start

1. In the portal, open **Organization → API tokens**, create a service account with a role, then create a token.
2. Export the token and apply:

```sh
export GEZOR_TOKEN=gzr_sa_...
terraform init
terraform apply
```

```hcl
terraform {
  required_providers {
    gezor = {
      source  = "gezor-labs/gezor"
      version = "~> 0.1"
    }
  }
}

provider "gezor" {
  workspace = "main"
}

resource "gezor_cluster" "prod" {
  name   = "prod-eu"
  region = "eu-central-1"
}

resource "gezor_cluster_app" "kafka" {
  cluster_id = gezor_cluster.prod.id
  app        = "kafka"
}

resource "gezor_kafka_topic" "orders" {
  cluster_id = gezor_cluster.prod.id
  name       = "orders"
  partitions = 6
  depends_on = [gezor_cluster_app.kafka]
}
```

More examples are in [`examples/`](examples/).

## Development

Requirements: Go (see `go.mod`) and Terraform 1.9 or later.

```sh
go build ./...
go test ./...                     # unit tests and the fake-API tests (needs terraform on PATH)
go generate ./...                 # regenerate docs/ from the schemas, examples/ and templates/
```

Live tests run against a real organization and create and delete resources:

```sh
TF_ACC=1 GEZOR_TOKEN=gzr_sa_... GEZOR_ENDPOINT=https://app.gezor.cloud \
  GEZOR_TEST_CLUSTER_ID=cl_... go test ./internal/provider -run TestLive -v
```

To use a local build, point Terraform at it with `dev_overrides`:

```hcl
# ~/.terraformrc
provider_installation {
  dev_overrides {
    "gezor-labs/gezor" = "/path/to/go/bin"
  }
  direct {}
}
```

then `go install .`.

## Releasing

Push a `v*` tag. The Release workflow builds with GoReleaser and signs the checksums with the GPG key in the `GPG_PRIVATE_KEY` and `PASSPHRASE` repository secrets. The Terraform Registry picks up new GitHub releases automatically.
