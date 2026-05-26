// Terraform provider for Ultraviolet — `uv_workspace`, `uv_connection`,
// `uv_synced_table`, `uv_dashboard`, `uv_alert_rule` resources.
//
// Skeleton only — the resource schemas + REST glue plug into the canonical
// hashicorp/terraform-plugin-framework. Build with:
//
//   go build -o terraform-provider-uv .
//
// Use:
//
//   terraform {
//     required_providers {
//       uv = { source = "ultraviolet-dev/uv" }
//     }
//   }
//
//   provider "uv" { api_key = var.api_key }
//   resource "uv_connection" "bq" { ... }
package main

import "fmt"

// Real wiring requires `github.com/hashicorp/terraform-plugin-framework`
// (heavy transitive tree). We avoid that import in the proxy module and ship
// this provider from a sibling repo. See AUDIT.md dev-2.
func main() {
	fmt.Println("terraform-provider-uv: build from a sibling module that imports terraform-plugin-framework")
}
