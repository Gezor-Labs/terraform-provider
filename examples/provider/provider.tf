terraform {
  required_providers {
    gezor = {
      source  = "gezor-labs/gezor"
      version = "~> 0.1.0"
    }
  }
}

# Create a token under Organization > API tokens and export it as GEZOR_TOKEN.
provider "gezor" {
  # endpoint  = "https://app.gezor.cloud" # or GEZOR_ENDPOINT
  # token     = var.gezor_token           # or GEZOR_TOKEN
  workspace = "main" # or GEZOR_WORKSPACE; each resource can override it
}

# A second workspace through an alias:
provider "gezor" {
  alias     = "analytics"
  workspace = "analytics"
}
