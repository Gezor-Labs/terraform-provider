package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

func sacPath(id string) string { return "/api/connectors/" + client.PathEscape(id) }

// ---------------------------------------------------------------- gezor_secure_access_connector

var _ resource.ResourceWithImportState = &sacResource{}

func NewSecureAccessConnectorResource() resource.Resource { return &sacResource{} }

type sacResource struct{ base }

type sacModel struct {
	Workspace        types.String `tfsdk:"workspace"`
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	ClusterID        types.String `tfsdk:"cluster_id"`
	Allow            types.List   `tfsdk:"allow"`
	Status           types.String `tfsdk:"status"`
	Online           types.Bool   `tfsdk:"online"`
	InstallToken     types.String `tfsdk:"install_token"`
	InstallExpiresAt types.Int64  `tfsdk:"install_token_expires_at"`
	Install          types.Map    `tfsdk:"install"`
}

var allowAttrTypes = map[string]attr.Type{"cidr": types.StringType, "description": types.StringType}

type allowModel struct {
	CIDR        types.String `tfsdk:"cidr"`
	Description types.String `tfsdk:"description"`
}

type apiSAC struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	ClusterID  string `json:"cluster_id"`
	Online     bool   `json:"online"`
	AllowCIDRs []struct {
		CIDR        string `json:"cidr"`
		Description string `json:"description"`
	} `json:"allow_cidrs"`
}

func (r *sacResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secure_access_connector"
}

func (r *sacResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Secure Access connector: a small agent you run inside your network so clusters can reach private databases over outbound HTTPS. Run one of the `install` scripts on the host.",
		Attributes: map[string]schema.Attribute{
			"workspace":  workspaceAttribute(),
			"id":         idAttribute("Connector id."),
			"name":       schema.StringAttribute{Required: true},
			"cluster_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Cluster that uses this connector."},
			"allow": schema.ListNestedAttribute{
				Optional:            true,
				MarkdownDescription: "Hosts or CIDR ranges the connector may reach.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"cidr":        schema.StringAttribute{Required: true, MarkdownDescription: "CIDR range or host name."},
					"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
				}},
			},
			"status":        schema.StringAttribute{Computed: true},
			"online":        schema.BoolAttribute{Computed: true},
			"install_token": schema.StringAttribute{Computed: true, Sensitive: true, PlanModifiers: keep, MarkdownDescription: "One-time token, set at creation."},
			"install_token_expires_at": schema.Int64Attribute{
				Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"install": schema.MapAttribute{
				Computed: true, Sensitive: true, ElementType: types.StringType,
				MarkdownDescription: "Install scripts by platform. They contain the token.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *sacResource) allowBody(ctx context.Context, m *sacModel, diags *diag.Diagnostics) []map[string]string {
	var items []allowModel
	if known(m.Allow) {
		diags.Append(m.Allow.ElementsAs(ctx, &items, false)...)
	}
	out := make([]map[string]string, 0, len(items))
	for _, it := range items {
		out = append(out, map[string]string{"cidr": it.CIDR.ValueString(), "description": it.Description.ValueString()})
	}
	return out
}

func (r *sacResource) fill(m *sacModel, c apiSAC, diags *diag.Diagnostics) {
	m.ID = strValue(c.ID)
	m.Name = strValue(c.Name)
	m.ClusterID = optString(c.ClusterID)
	m.Status = strValue(c.Status)
	m.Online = types.BoolValue(c.Online)
	if len(c.AllowCIDRs) > 0 || !m.Allow.IsNull() {
		items := make([]attr.Value, 0, len(c.AllowCIDRs))
		for _, a := range c.AllowCIDRs {
			obj, d := types.ObjectValue(allowAttrTypes, map[string]attr.Value{"cidr": strValue(a.CIDR), "description": strValue(a.Description)})
			diags.Append(d...)
			items = append(items, obj)
		}
		l, d := types.ListValue(types.ObjectType{AttrTypes: allowAttrTypes}, items)
		diags.Append(d...)
		m.Allow = l
	}
}

func (r *sacResource) get(ctx context.Context, m *sacModel) (*apiSAC, error) {
	var out struct {
		Connector apiSAC `json:"connector"`
	}
	if err := r.c.Get(ctx, sacPath(m.ID.ValueString()), m.Workspace.ValueString(), &out); err != nil {
		return nil, err
	}
	return &out.Connector, nil
}

func (r *sacResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sacModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"name": plan.Name.ValueString(), "allow_cidrs": r.allowBody(ctx, &plan, &resp.Diagnostics)}
	if known(plan.ClusterID) {
		body["cluster_id"] = plan.ClusterID.ValueString()
	}
	var out struct {
		Connector    apiSAC            `json:"connector"`
		InstallToken string            `json:"install_token"`
		ExpiresAt    int64             `json:"install_token_expires_at"`
		Install      map[string]string `json:"install"`
	}
	if err := r.c.Post(ctx, "/api/connectors", plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "create Secure Access connector", err)
		return
	}
	plan.InstallToken = strValue(out.InstallToken)
	plan.InstallExpiresAt = types.Int64Value(out.ExpiresAt)
	plan.Install = stringMap(ctx, out.Install, &resp.Diagnostics)
	r.fill(&plan, out.Connector, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sacResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sacModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := r.get(ctx, &state)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read Secure Access connector", err)
		return
	}
	r.fill(&state, *c, &resp.Diagnostics)
	if state.InstallToken.IsNull() {
		state.InstallToken = strValue("")
		state.InstallExpiresAt = types.Int64Value(0)
		state.Install = stringMap(ctx, nil, &resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *sacResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sacModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"name": plan.Name.ValueString(), "allow_cidrs": r.allowBody(ctx, &plan, &resp.Diagnostics)}
	if known(plan.ClusterID) {
		body["cluster_id"] = plan.ClusterID.ValueString()
	}
	var out struct {
		Connector apiSAC `json:"connector"`
	}
	if err := r.c.Patch(ctx, sacPath(plan.ID.ValueString()), plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update Secure Access connector", err)
		return
	}
	r.fill(&plan, out.Connector, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sacResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sacModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, sacPath(state.ID.ValueString()), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete Secure Access connector", err)
	}
}

func (r *sacResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<connector_id>", "id")
}

// ---------------------------------------------------------------- gezor_secure_access_endpoint

var _ resource.ResourceWithImportState = &sacEndpointResource{}

func NewSecureAccessEndpointResource() resource.Resource { return &sacEndpointResource{} }

type sacEndpointResource struct{ base }

type sacEndpointModel struct {
	Workspace    types.String `tfsdk:"workspace"`
	ID           types.String `tfsdk:"id"`
	ConnectorID  types.String `tfsdk:"connector_id"`
	Name         types.String `tfsdk:"name"`
	Host         types.String `tfsdk:"host"`
	Port         types.Int64  `tfsdk:"port"`
	DatabaseType types.String `tfsdk:"database_type"`
	ListenPort   types.Int64  `tfsdk:"listen_port"`
	TunnelHost   types.String `tfsdk:"tunnel_host"`
	Status       types.String `tfsdk:"status"`
}

type apiEndpoint struct {
	ID           string `json:"id"`
	ConnectorID  string `json:"connector_id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int64  `json:"port"`
	DatabaseType string `json:"database_type"`
	ListenPort   int64  `json:"listen_port"`
	TunnelHost   string `json:"tunnel_host"`
	Status       string `json:"status"`
}

func (r *sacEndpointResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secure_access_endpoint"
}

func (r *sacEndpointResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A private database reachable through a Secure Access connector. Point a `gezor_cdc_connector` at it with `access_via = \"connector\"`.",
		Attributes: map[string]schema.Attribute{
			"workspace":    workspaceAttribute(),
			"id":           idAttribute("Endpoint id."),
			"connector_id": requiredReplaceString("Secure Access connector id."),
			"name":         schema.StringAttribute{Required: true},
			"host":         schema.StringAttribute{Required: true, MarkdownDescription: "Database host as seen from the connector."},
			"port": schema.Int64Attribute{
				Required: true, Validators: []validator.Int64{int64validator.Between(1, 65535)},
			},
			"database_type": schema.StringAttribute{
				Required:   true,
				Validators: []validator.String{stringvalidator.OneOf("postgresql", "mysql", "mariadb", "sqlserver", "mongodb", "oracle")},
			},
			"listen_port": schema.Int64Attribute{Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"tunnel_host": schema.StringAttribute{Computed: true, MarkdownDescription: "Host clusters use to reach the database.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"status": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *sacEndpointResource) fill(m *sacEndpointModel, e apiEndpoint) {
	m.ID = strValue(e.ID)
	m.ConnectorID = strValue(e.ConnectorID)
	m.Name = strValue(e.Name)
	m.Host = strValue(e.Host)
	m.Port = types.Int64Value(e.Port)
	m.DatabaseType = strValue(e.DatabaseType)
	m.ListenPort = types.Int64Value(e.ListenPort)
	m.TunnelHost = strValue(e.TunnelHost)
	m.Status = strValue(e.Status)
}

func (r *sacEndpointResource) body(m *sacEndpointModel) map[string]any {
	return map[string]any{
		"name": m.Name.ValueString(), "host": m.Host.ValueString(), "port": m.Port.ValueInt64(),
		"database_type": m.DatabaseType.ValueString(),
	}
}

func (r *sacEndpointResource) endpointPath(m *sacEndpointModel) string {
	return sacPath(m.ConnectorID.ValueString()) + "/endpoints/" + client.PathEscape(m.ID.ValueString())
}

func (r *sacEndpointResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan sacEndpointModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Endpoint apiEndpoint `json:"endpoint"`
	}
	if err := r.c.Post(ctx, sacPath(plan.ConnectorID.ValueString())+"/endpoints", plan.Workspace.ValueString(), r.body(&plan), &out); err != nil {
		apiError(&resp.Diagnostics, "create Secure Access endpoint", err)
		return
	}
	r.fill(&plan, out.Endpoint)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sacEndpointResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state sacEndpointModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Endpoint apiEndpoint `json:"endpoint"`
	}
	if err := r.c.Get(ctx, r.endpointPath(&state), state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read Secure Access endpoint", err)
		return
	}
	r.fill(&state, out.Endpoint)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *sacEndpointResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan sacEndpointModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Endpoint apiEndpoint `json:"endpoint"`
	}
	if err := r.c.Patch(ctx, r.endpointPath(&plan), plan.Workspace.ValueString(), r.body(&plan), &out); err != nil {
		apiError(&resp.Diagnostics, "update Secure Access endpoint", err)
		return
	}
	r.fill(&plan, out.Endpoint)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *sacEndpointResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state sacEndpointModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, r.endpointPath(&state), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete Secure Access endpoint", err)
	}
}

func (r *sacEndpointResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<connector_id>/<endpoint_id>", "connector_id", "id")
}
