// uv_dashboard resource — declarative dashboard definitions backed by the
// /v1/dashboards REST API. Same shape as resource_connection.go: typed model,
// stub CRUD that returns ErrRESTClientTODO until the plugin framework is
// wired in.
package main

import "context"

// DashboardModel mirrors the Terraform schema for uv_dashboard.
// Maps to internal/api/openapi.yaml#/components/schemas/Dashboard.
type DashboardModel struct {
	ID          string         `tfsdk:"id"`
	Name        string         `tfsdk:"name"`
	Description string         `tfsdk:"description"`
	Layout      string         `tfsdk:"layout"`   // "grid" | "freeform"
	IsPublic    bool           `tfsdk:"is_public"`
	Tiles       []DashboardTile `tfsdk:"tiles"`
}

type DashboardTile struct {
	ID    string `tfsdk:"id"`
	Title string `tfsdk:"title"`
	SQL   string `tfsdk:"sql"`
	Viz   string `tfsdk:"viz"`   // "table" | "bar" | "line" | "kpi"
	X     int64  `tfsdk:"x"`
	Y     int64  `tfsdk:"y"`
	W     int64  `tfsdk:"w"`
	H     int64  `tfsdk:"h"`
}

type dashboardResource struct {
	apiBaseURL string
	apiKey     string
}

func NewDashboardResource() any { return &dashboardResource{} }

// SchemaDoc — see resource_connection.go for the same documentation pattern.
//
//	uv_dashboard {
//	  name        = string  // required
//	  description = string  // optional
//	  layout      = string  // optional, one of [grid, freeform], default "grid"
//	  is_public   = bool    // optional, default false
//	  tiles = [{
//	    title = string
//	    sql   = string
//	    viz   = string  // table|bar|line|kpi
//	    x, y, w, h = number
//	  }]
//	}
func (r *dashboardResource) SchemaDoc() string {
	return "uv_dashboard: dashboard with tiles (sql + viz). Mirrors POST /v1/dashboards."
}

func (r *dashboardResource) Create(ctx context.Context, plan DashboardModel) (DashboardModel, error) {
	return DashboardModel{}, ErrRESTClientTODO
}

func (r *dashboardResource) Read(ctx context.Context, id string) (DashboardModel, error) {
	return DashboardModel{}, ErrRESTClientTODO
}

func (r *dashboardResource) Update(ctx context.Context, plan DashboardModel) (DashboardModel, error) {
	return DashboardModel{}, ErrRESTClientTODO
}

func (r *dashboardResource) Delete(ctx context.Context, id string) error {
	return ErrRESTClientTODO
}
