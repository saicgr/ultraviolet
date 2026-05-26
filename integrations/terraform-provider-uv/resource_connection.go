// uv_connection resource — declarative warehouse connection (BigQuery,
// Snowflake, Databricks, Redshift). The Terraform plugin framework is NOT
// imported by the main Ultraviolet module to keep its transitive tree small;
// this provider lives in its own Go module (see go.mod in the same dir).
//
// Build flow (once terraform-plugin-framework is added):
//
//	go mod init github.com/ultraviolet-dev/terraform-provider-uv  // already done
//	go get github.com/hashicorp/terraform-plugin-framework@latest
//	go build -o terraform-provider-uv .
//
// Until then the imports below are commented out so `go build` does not fail.
package main

// Imports needed once the plugin SDK is wired:
//
//	"context"
//	"fmt"
//	"github.com/hashicorp/terraform-plugin-framework/path"
//	"github.com/hashicorp/terraform-plugin-framework/resource"
//	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
//	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
//	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
//	"github.com/hashicorp/terraform-plugin-framework/types"

import (
	"context"
	"errors"
)

// ErrRESTClientTODO is returned by every CRUD method until the REST client is
// wired up. Surfacing a typed error keeps the no-mock invariant: Terraform
// users will see a loud failure rather than a silent no-op apply.
var ErrRESTClientTODO = errors.New("uv-provider: REST client not yet wired — see integrations/terraform-provider-uv/README.md")

// ConnectionModel mirrors the Terraform schema for uv_connection.
// Tags map to the canonical REST API request body documented in
// internal/api/openapi.yaml#/components/schemas/Connection.
type ConnectionModel struct {
	ID                 string `tfsdk:"id"`
	Name               string `tfsdk:"name"`
	WarehouseType      string `tfsdk:"warehouse_type"`       // bigquery|snowflake|databricks|redshift
	CredentialsSecret  string `tfsdk:"credentials_secret_ref"` // e.g. "aws-sm://arn:..."
	DefaultProject     string `tfsdk:"default_project"`
	DefaultDataset     string `tfsdk:"default_dataset"`
	DefaultWarehouse   string `tfsdk:"default_warehouse"`
}

// connectionResource is the resource.Resource impl. Methods are stubs that
// return ErrRESTClientTODO until the REST client lands.
type connectionResource struct {
	apiBaseURL string
	apiKey     string
}

// NewConnectionResource is the factory the provider passes to
// resource.Resource registration. Once the SDK import lands, change the
// signature to: func() resource.Resource { return &connectionResource{} }.
func NewConnectionResource() any { return &connectionResource{} }

// SchemaDoc returns a string describing the resource schema. The real
// implementation builds a schema.Schema; we keep the documentation here so the
// shape is reviewable now.
//
//	uv_connection {
//	  name                   = string  // required, ForceNew
//	  warehouse_type         = string  // required, ForceNew, one of [bigquery, snowflake, databricks, redshift]
//	  credentials_secret_ref = string  // required, Sensitive
//	  default_project        = string  // optional
//	  default_dataset        = string  // optional
//	  default_warehouse      = string  // optional, snowflake only
//	}
//
//	Computed:
//	  id (UUID)
func (r *connectionResource) SchemaDoc() string {
	return "uv_connection: warehouse connection (name, warehouse_type, credentials_secret_ref, default_*)."
}

// Create POSTs to /v1/connections. Returns ErrRESTClientTODO until wired.
func (r *connectionResource) Create(ctx context.Context, plan ConnectionModel) (ConnectionModel, error) {
	return ConnectionModel{}, ErrRESTClientTODO
}

// Read GETs /v1/connections/{id}.
func (r *connectionResource) Read(ctx context.Context, id string) (ConnectionModel, error) {
	return ConnectionModel{}, ErrRESTClientTODO
}

// Update PATCHes /v1/connections/{id}.
func (r *connectionResource) Update(ctx context.Context, plan ConnectionModel) (ConnectionModel, error) {
	return ConnectionModel{}, ErrRESTClientTODO
}

// Delete DELETEs /v1/connections/{id}.
func (r *connectionResource) Delete(ctx context.Context, id string) error {
	return ErrRESTClientTODO
}
