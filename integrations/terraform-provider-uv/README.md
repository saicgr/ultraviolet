# terraform-provider-uv

Terraform provider exposing Ultraviolet resources (`uv_connection`,
`uv_dashboard`, `uv_synced_table`, `uv_alert_rule`, `uv_workspace`) on top of
the REST control plane (`internal/api/`).

## Status

Skeleton: resource models + CRUD method stubs live in `resource_*.go`. CRUD
methods currently return `ErrRESTClientTODO` so users see a typed error rather
than a silent no-op apply (per the project's no-fallback invariant).

## Setup (when wiring up)

This is a **separate Go module** from the main repo to avoid pulling the
~30MB `terraform-plugin-framework` transitive tree into the proxy / api / sync
binaries. From this directory:

```bash
go get github.com/hashicorp/terraform-plugin-framework@latest
go get github.com/hashicorp/terraform-plugin-framework-validators@latest
go build -o terraform-provider-uv .
```

Then register the provider:

```hcl
terraform {
  required_providers {
    uv = { source = "ultraviolet-dev/uv" }
  }
}

provider "uv" {
  api_base_url = "https://api.ultraviolet.dev"
  api_key      = var.uv_api_key
}
```

## REST client

A minimal `net/http` client targeting `internal/api/openapi.yaml` lives at
`client.go` (to be added). The provider configuration block plumbs
`apiBaseURL` + `apiKey` into each resource's struct.

## Why the SDK is not in main go.mod

`terraform-plugin-framework` pulls in `hashicorp/go-hclog`,
`hashicorp/terraform-plugin-go`, gRPC, and several others. Keeping this in a
sibling module keeps the proxy binary lean and avoids dependency churn each
time the plugin framework cuts a release.
