package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

type apiCluster struct {
	ID                string   `json:"id"`
	OrgID             string   `json:"org_id"`
	Name              string   `json:"name"`
	Description       string   `json:"description"`
	Tags              []string `json:"tags"`
	Environment       string   `json:"environment"`
	Region            string   `json:"region"`
	HostingMode       string   `json:"hosting_mode"`
	Status            string   `json:"status"`
	Online            bool     `json:"online"`
	OperatorVersion   string   `json:"operator_version"`
	OperatorNamespace string   `json:"operator_namespace"`
	DesiredState      struct {
		Modules map[string]any `json:"modules"`
	} `json:"desired_state"`
}

func clusterPath(id string) string { return "/api/clusters/" + client.PathEscape(id) }

func getCluster(ctx context.Context, c *client.Client, workspace, id string) (*apiCluster, error) {
	var out struct {
		Cluster apiCluster `json:"cluster"`
	}
	if err := c.Get(ctx, clusterPath(id), workspace, &out); err != nil {
		return nil, err
	}
	return &out.Cluster, nil
}

func listClusters(ctx context.Context, c *client.Client, workspace string) ([]apiCluster, error) {
	var out struct {
		Clusters []apiCluster `json:"clusters"`
	}
	if err := c.Get(ctx, "/api/clusters", workspace, &out); err != nil {
		return nil, err
	}
	return out.Clusters, nil
}

func pathRoot(name string) path.Path { return path.Root(name) }

// ---------------------------------------------------------------- gezor_cluster

var _ resource.ResourceWithImportState = &clusterResource{}

func NewClusterResource() resource.Resource { return &clusterResource{} }

type clusterResource struct{ base }

type clusterModel struct {
	Workspace         types.String `tfsdk:"workspace"`
	ID                types.String `tfsdk:"id"`
	Name              types.String `tfsdk:"name"`
	OperatorNamespace types.String `tfsdk:"operator_namespace"`
	Description       types.String `tfsdk:"description"`
	Tags              types.List   `tfsdk:"tags"`
	Environment       types.String `tfsdk:"environment"`
	Region            types.String `tfsdk:"region"`
	HostingMode       types.String `tfsdk:"hosting_mode"`
	Status            types.String `tfsdk:"status"`
	Online            types.Bool   `tfsdk:"online"`
	OrgID             types.String `tfsdk:"org_id"`
	InstallToken      types.String `tfsdk:"install_token"`
	InstallExpiresAt  types.Int64  `tfsdk:"install_token_expires_at"`
	InstallCommand    types.String `tfsdk:"install_command"`
}

func (r *clusterResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (r *clusterResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Kubernetes cluster registered with Gezor. For your own cluster (`byoc`), run `install_command` on it to install the operator; use `gezor_cluster_install_token` for a fresh token later. Turn on apps with `gezor_cluster_app`.",
		Attributes: map[string]schema.Attribute{
			"workspace": workspaceAttribute(),
			"id":        idAttribute("Cluster id."),
			"name":      schema.StringAttribute{Required: true, MarkdownDescription: "Lowercase DNS label."},
			"operator_namespace": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("gzr-system"),
				MarkdownDescription: "Namespace for the operator.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"tags": schema.ListAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Lowercase labels (up to 16).",
				PlanModifiers:       []planmodifier.List{listplanmodifier.UseStateForUnknown()},
			},
			"environment": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"region":      schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"hosting_mode": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("byoc"),
				MarkdownDescription: "`byoc` (your cluster) or `gezor_hosted`.",
				Validators:          []validator.String{stringvalidator.OneOf("byoc", "gezor_hosted")},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"status": schema.StringAttribute{Computed: true},
			"online": schema.BoolAttribute{Computed: true},
			"org_id": schema.StringAttribute{Computed: true, PlanModifiers: keep},
			"install_token": schema.StringAttribute{
				Computed: true, Sensitive: true, PlanModifiers: keep,
				MarkdownDescription: "One-time operator install token, set at creation.",
			},
			"install_token_expires_at": schema.Int64Attribute{
				Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"install_command": schema.StringAttribute{
				Computed: true, Sensitive: true, PlanModifiers: keep,
				MarkdownDescription: "Helm command that installs the operator (`byoc` only). Contains secrets.",
			},
		},
	}
}

func (r *clusterResource) fill(ctx context.Context, m *clusterModel, c *apiCluster, diags *diag.Diagnostics) {
	m.ID = strValue(c.ID)
	m.Name = strValue(c.Name)
	m.Description = strValue(c.Description)
	m.Tags = stringList(ctx, c.Tags, diags)
	m.Environment = strValue(c.Environment)
	m.Region = strValue(c.Region)
	m.HostingMode = strValue(c.HostingMode)
	m.Status = strValue(c.Status)
	m.Online = types.BoolValue(c.Online)
	m.OrgID = strValue(c.OrgID)
	if c.OperatorNamespace != "" {
		m.OperatorNamespace = strValue(c.OperatorNamespace)
	}
}

func (r *clusterResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan clusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"name": plan.Name.ValueString(), "operator_namespace": plan.OperatorNamespace.ValueString(),
		"description": plan.Description.ValueString(), "environment": plan.Environment.ValueString(),
		"region": plan.Region.ValueString(), "hosting_mode": plan.HostingMode.ValueString(),
		"tags": listStrings(ctx, plan.Tags, &resp.Diagnostics),
	}
	var out struct {
		Cluster          apiCluster `json:"cluster"`
		InstallToken     string     `json:"install_token"`
		InstallExpiresAt int64      `json:"install_token_expires_at"`
		Install          *struct {
			Helm string `json:"helm"`
		} `json:"install"`
	}
	ws := plan.Workspace.ValueString()
	if err := r.c.Post(ctx, "/api/clusters", ws, body, &out); err != nil {
		apiError(&resp.Diagnostics, "create cluster", err)
		return
	}
	plan.InstallToken = strValue(out.InstallToken)
	plan.InstallExpiresAt = types.Int64Value(out.InstallExpiresAt)
	plan.InstallCommand = strValue("")
	if out.Install != nil {
		plan.InstallCommand = strValue(out.Install.Helm)
	}
	c, err := getCluster(ctx, r.c, ws, out.Cluster.ID)
	if err != nil {
		apiError(&resp.Diagnostics, "read cluster", err)
		return
	}
	r.fill(ctx, &plan, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *clusterResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state clusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	c, err := getCluster(ctx, r.c, state.Workspace.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read cluster", err)
		return
	}
	r.fill(ctx, &state, c, &resp.Diagnostics)
	if state.InstallToken.IsNull() {
		state.InstallToken = strValue("")
		state.InstallCommand = strValue("")
		state.InstallExpiresAt = types.Int64Value(0)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *clusterResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state clusterModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"description": plan.Description.ValueString(), "environment": plan.Environment.ValueString(),
		"region": plan.Region.ValueString(),
	}
	if plan.Name.ValueString() != state.Name.ValueString() {
		body["name"] = plan.Name.ValueString()
	}
	if known(plan.Tags) {
		body["tags"] = listStrings(ctx, plan.Tags, &resp.Diagnostics)
	}
	ws := plan.Workspace.ValueString()
	if err := r.c.Patch(ctx, clusterPath(state.ID.ValueString()), ws, body, nil); err != nil {
		apiError(&resp.Diagnostics, "update cluster", err)
		return
	}
	c, err := getCluster(ctx, r.c, ws, state.ID.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "read cluster", err)
		return
	}
	plan.ID = state.ID
	r.fill(ctx, &plan, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *clusterResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state clusterModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, clusterPath(state.ID.ValueString()), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete cluster", err)
	}
}

func (r *clusterResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>", "id")
}

// ---------------------------------------------------------------- gezor_cluster_install_token

var _ resource.ResourceWithImportState = &clusterInstallTokenResource{}

func NewClusterInstallTokenResource() resource.Resource { return &clusterInstallTokenResource{} }

type clusterInstallTokenResource struct{ base }

type clusterInstallTokenModel struct {
	Workspace         types.String `tfsdk:"workspace"`
	ID                types.String `tfsdk:"id"`
	ClusterID         types.String `tfsdk:"cluster_id"`
	TTLSeconds        types.Int64  `tfsdk:"ttl_seconds"`
	Label             types.String `tfsdk:"label"`
	OperatorNamespace types.String `tfsdk:"operator_namespace"`
	Token             types.String `tfsdk:"token"`
	ExpiresAt         types.Int64  `tfsdk:"expires_at"`
	Status            types.String `tfsdk:"status"`
	InstallCommand    types.String `tfsdk:"install_command"`
}

func (r *clusterInstallTokenResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster_install_token"
}

func (r *clusterInstallTokenResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	keep := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A new operator install token for a cluster. Destroying it revokes the token.",
		Attributes: map[string]schema.Attribute{
			"workspace":  workspaceAttribute(),
			"id":         idAttribute("Token id."),
			"cluster_id": requiredReplaceString("Cluster id."),
			"ttl_seconds": schema.Int64Attribute{
				Optional: true, Computed: true, Default: int64default.StaticInt64(86400),
				MarkdownDescription: "Lifetime, 300 to 604800 seconds.",
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace()},
			},
			"label": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"operator_namespace": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("gzr-system"),
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"token":           schema.StringAttribute{Computed: true, Sensitive: true, PlanModifiers: keep},
			"expires_at":      schema.Int64Attribute{Computed: true, PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}},
			"status":          schema.StringAttribute{Computed: true, MarkdownDescription: "`active`, `used` or `expired`."},
			"install_command": schema.StringAttribute{Computed: true, Sensitive: true, PlanModifiers: keep},
		},
	}
}

func (r *clusterInstallTokenResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan clusterInstallTokenModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"ttl_seconds": plan.TTLSeconds.ValueInt64(), "label": plan.Label.ValueString(),
		"operator_namespace": plan.OperatorNamespace.ValueString(),
	}
	var out struct {
		InstallToken string `json:"install_token"`
		TokenID      string `json:"token_id"`
		ExpiresAt    int64  `json:"expires_at"`
		Install      *struct {
			Helm string `json:"helm"`
		} `json:"install"`
	}
	if err := r.c.Post(ctx, clusterPath(plan.ClusterID.ValueString())+"/tokens", plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "create install token", err)
		return
	}
	plan.ID = strValue(out.TokenID)
	plan.Token = strValue(out.InstallToken)
	plan.ExpiresAt = types.Int64Value(out.ExpiresAt)
	plan.Status = strValue("active")
	plan.InstallCommand = strValue("")
	if out.Install != nil {
		plan.InstallCommand = strValue(out.Install.Helm)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *clusterInstallTokenResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state clusterInstallTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Tokens []struct {
			ID        string `json:"id"`
			Label     string `json:"label"`
			ExpiresAt int64  `json:"expires_at"`
			Status    string `json:"status"`
		} `json:"tokens"`
	}
	if err := r.c.Get(ctx, clusterPath(state.ClusterID.ValueString())+"/tokens", state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "list install tokens", err)
		return
	}
	for _, t := range out.Tokens {
		if t.ID == state.ID.ValueString() {
			state.Label = strValue(t.Label)
			state.ExpiresAt = types.Int64Value(t.ExpiresAt)
			state.Status = strValue(t.Status)
			if state.Token.IsNull() {
				state.Token = strValue("")
				state.InstallCommand = strValue("")
			}
			resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
			return
		}
	}
	resp.State.RemoveResource(ctx)
}

func (r *clusterInstallTokenResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unexpected update", "Every argument of gezor_cluster_install_token forces a new token.")
}

func (r *clusterInstallTokenResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state clusterInstallTokenModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := clusterPath(state.ClusterID.ValueString()) + "/tokens/" + client.PathEscape(state.ID.ValueString())
	if err := r.c.Delete(ctx, p, state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "revoke install token", err)
	}
}

func (r *clusterInstallTokenResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<token_id>", "cluster_id", "id")
}

// ---------------------------------------------------------------- gezor_cluster_app

var clusterApps = []string{"kafka", "schemaRegistry", "connect", "flink", "trino", "spark", "garage", "iceberg"}

// Module keys the server manages; they are never part of `config`.
var managedModuleKeys = []string{"enabled", "deletionProtection", "deletionUnprotectedAt"}

var _ resource.ResourceWithImportState = &clusterAppResource{}

func NewClusterAppResource() resource.Resource { return &clusterAppResource{} }

type clusterAppResource struct{ base }

type clusterAppModel struct {
	Workspace          types.String         `tfsdk:"workspace"`
	ID                 types.String         `tfsdk:"id"`
	ClusterID          types.String         `tfsdk:"cluster_id"`
	App                types.String         `tfsdk:"app"`
	Enabled            types.Bool           `tfsdk:"enabled"`
	DeletionProtection types.Bool           `tfsdk:"deletion_protection"`
	Config             jsontypes.Normalized `tfsdk:"config"`
	EffectiveConfig    jsontypes.Normalized `tfsdk:"effective_config"`
}

func (r *clusterAppResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster_app"
}

func (r *clusterAppResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	app := requiredReplaceString("One of `kafka` (Event Streams), `schemaRegistry` (Data Schemas), `connect` (Change Capture), `flink` (Stream Processing), `trino` (Data Explorer), `spark` (Batch Processing), `garage` (Object Storage) or `iceberg` (Iceberg Catalog).")
	app.Validators = []validator.String{stringvalidator.OneOf(clusterApps...)}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Turns on and configures one app on a cluster. `config` uses the same fields as the portal's app settings; leave a field out to use its default. Destroying the resource turns the app off, which fails while `deletion_protection` is on.",
		Attributes: map[string]schema.Attribute{
			"workspace":  workspaceAttribute(),
			"id":         idAttribute("`<cluster_id>/<app>`."),
			"cluster_id": requiredReplaceString("Cluster id."),
			"app":        app,
			"enabled":    schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"deletion_protection": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Block turning the app off. Set to `false` and apply before destroying. Gezor turns it back on after an hour.",
			},
			"config": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{}, Optional: true,
				MarkdownDescription: "App settings as JSON, for example `jsonencode({ replicas = 3 })`.",
			},
			"effective_config": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{}, Computed: true,
				MarkdownDescription: "All settings as stored, including defaults.",
			},
		},
	}
}

func moduleConfig(mod map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range mod {
		if !containsString(managedModuleKeys, k) {
			out[k] = v
		}
	}
	return out
}

func (r *clusterAppResource) read(ctx context.Context, m *clusterAppModel) (bool, error) {
	c, err := getCluster(ctx, r.c, m.Workspace.ValueString(), m.ClusterID.ValueString())
	if err != nil {
		return false, err
	}
	raw, ok := c.DesiredState.Modules[m.App.ValueString()]
	if !ok {
		return false, nil
	}
	mod := asMap(raw)
	cfg := moduleConfig(mod)
	m.ID = strValue(m.ClusterID.ValueString() + "/" + m.App.ValueString())
	m.Enabled = types.BoolValue(asBool(mod["enabled"]))
	if v, ok := mod["deletionProtection"].(bool); ok {
		m.DeletionProtection = types.BoolValue(v)
	} else {
		m.DeletionProtection = types.BoolValue(true)
	}
	m.EffectiveConfig = jsontypes.NewNormalizedValue(encodeJSON(cfg))
	if known(m.Config) {
		want, err := decodeJSONObject(m.Config.ValueString())
		if err != nil || !subsetEqual(want, cfg) {
			m.Config = jsontypes.NewNormalizedValue(encodeJSON(cfg))
		}
	}
	return true, nil
}

func (r *clusterAppResource) put(ctx context.Context, m *clusterAppModel, enabled bool) error {
	body := map[string]any{}
	if known(m.Config) {
		cfg, err := decodeJSONObject(m.Config.ValueString())
		if err != nil {
			return fmt.Errorf("config %w", err)
		}
		body = cfg
	}
	body["enabled"] = enabled
	if known(m.DeletionProtection) {
		body["deletionProtection"] = m.DeletionProtection.ValueBool()
	}
	p := clusterPath(m.ClusterID.ValueString()) + "/modules/" + client.PathEscape(m.App.ValueString())
	return r.c.Put(ctx, p, m.Workspace.ValueString(), body, nil)
}

func (r *clusterAppResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan clusterAppModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &plan, plan.Enabled.ValueBool()); err != nil {
		apiError(&resp.Diagnostics, "configure "+plan.App.ValueString(), err)
		return
	}
	if _, err := r.read(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "read "+plan.App.ValueString(), err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *clusterAppResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state clusterAppModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &state)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read "+state.App.ValueString(), err)
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *clusterAppResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan clusterAppModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.put(ctx, &plan, plan.Enabled.ValueBool()); err != nil {
		apiError(&resp.Diagnostics, "configure "+plan.App.ValueString(), err)
		return
	}
	if _, err := r.read(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "read "+plan.App.ValueString(), err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *clusterAppResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state clusterAppModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !state.Enabled.ValueBool() {
		return
	}
	if err := r.put(ctx, &state, false); err != nil && !client.IsNotFound(err) {
		if client.IsConflict(err) {
			resp.Diagnostics.AddError("Cannot turn off "+state.App.ValueString(),
				fmt.Sprintf("%s\n\nSet deletion_protection = false and apply, then destroy.", err))
			return
		}
		apiError(&resp.Diagnostics, "turn off "+state.App.ValueString(), err)
	}
}

func (r *clusterAppResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<app>", "cluster_id", "app")
}
