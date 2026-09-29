# Terraform provider for Gezor Cloud

[![Terraform Registry](https://img.shields.io/badge/registry-gezor--labs%2Fgezor-7B42BC?logo=terraform)](https://registry.terraform.io/providers/Gezor-Labs/gezor/latest)
[![Release](https://img.shields.io/github/v/release/Gezor-Labs/terraform-provider-gezor)](https://github.com/Gezor-Labs/terraform-provider-gezor/releases)
[![Test](https://github.com/Gezor-Labs/terraform-provider-gezor/actions/workflows/test.yml/badge.svg)](https://github.com/Gezor-Labs/terraform-provider-gezor/actions/workflows/test.yml)
[![License: MPL-2.0](https://img.shields.io/badge/license-MPL--2.0-blue)](LICENSE)
![Status: Beta](https://img.shields.io/badge/status-beta-orange)

> **Beta:** resources and arguments may still change between minor versions before 1.0. Breaking changes are listed in the [changelog](CHANGELOG.md). Pin a minor version, for example `~> 0.1.0`.

Configure [Gezor Cloud](https://gezor.com) as code: workspaces, roles, workspace settings, single sign-on, encryption keys, AWS, clusters and their apps, Kafka topics, schemas, change data capture, Secure Access, dbt projects and pipelines.

The provider covers everything in the portal except adding and removing people. Invite people in the portal, or map identity provider groups to roles with `gezor_sso_group_mappings`.

Documentation: [Terraform Registry](https://registry.terraform.io/providers/Gezor-Labs/gezor/latest/docs), and [Terraform](https://gezor.com/docs/terraform/) on gezor.com.

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
      version = "~> 0.1.0"
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

1. Add the changes to `CHANGELOG.md`.
2. Push a `v*` tag, for example `git tag -a v0.2.0 -m v0.2.0 && git push origin v0.2.0`.

The Release workflow builds with GoReleaser and signs the checksums with the key in the `GPG_PRIVATE_KEY` and `PASSPHRASE` secrets (public key: [`signing-key.asc`](signing-key.asc)). The public Terraform Registry picks up each GitHub release automatically.

### HCP Terraform and Terraform Enterprise private registry

To also publish each release to your organization's private registry, add to the repository settings:

- variable `TFC_ORGANIZATION`: the HCP Terraform organization name;
- variable `TFC_HOSTNAME`: only for Terraform Enterprise, your host name;
- secret `TFC_TOKEN`: a team API token with **Manage Private Registry**.

To publish an existing release, run the Release workflow manually with its tag. Users of the private registry reference the provider as:

```hcl
gezor = {
  source = "app.terraform.io/<organization>/gezor"
}
```

`scripts/publish-private-registry.sh` does the same from a machine with the release files.
