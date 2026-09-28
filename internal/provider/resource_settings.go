package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var _ resource.ResourceWithImportState = &workspaceSettingsResource{}

func NewWorkspaceSettingsResource() resource.Resource { return &workspaceSettingsResource{} }

type workspaceSettingsResource struct{ base }

type workspaceSettingsModel struct {
	Workspace   types.String `tfsdk:"workspace"`
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Icon        types.String `tfsdk:"icon"`
	Modules     types.Object `tfsdk:"modules"`
	Security    types.Object `tfsdk:"security"`
}

type modulesModel struct {
	Pipelines         types.Bool `tfsdk:"pipelines"`
	Fraud             types.Bool `tfsdk:"fraud"`
	AML               types.Bool `tfsdk:"aml"`
	EnterpriseLineage types.Bool `tfsdk:"enterprise_lineage"`
}

type securityModel struct {
	SessionAbsoluteSeconds types.Int64 `tfsdk:"session_absolute_seconds"`
	SessionIdleSeconds     types.Int64 `tfsdk:"session_idle_seconds"`
	RequireMFA             types.Bool  `tfsdk:"require_mfa"`
	SSOOnly                types.Bool  `tfsdk:"sso_only"`
	AllowPasswordLogin     types.Bool  `tfsdk:"allow_password_login"`
}

var modulesAttrTypes = map[string]attr.Type{
	"pipelines": types.BoolType, "fraud": types.BoolType, "aml": types.BoolType, "enterprise_lineage": types.BoolType,
}

var securityAttrTypes = map[string]attr.Type{
	"session_absolute_seconds": types.Int64Type, "session_idle_seconds": types.Int64Type,
	"require_mfa": types.BoolType, "sso_only": types.BoolType, "allow_password_login": types.BoolType,
}

type settingsSnapshot struct {
	Workspace struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		IsPrimary   bool   `json:"is_primary"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"workspace"`
	Modules  map[string]bool `json:"modules"`
	Security struct {
		SessionAbsoluteSeconds *int64 `json:"session_absolute_seconds"`
		SessionIdleSeconds     *int64 `json:"session_idle_seconds"`
		RequireMFA             *bool  `json:"require_mfa"`
		SSOOnly                *bool  `json:"sso_only"`
		AllowPasswordLogin     *bool  `json:"allow_password_login"`
	} `json:"security"`
	SSO struct {
		Providers []apiSSOProvider `json:"providers"`
	} `json:"sso"`
}

func (r *workspaceSettingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_settings"
}

func optBool(desc string) schema.BoolAttribute {
	return schema.BoolAttribute{
		Optional: true, Computed: true, MarkdownDescription: desc,
		PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
	}
}

func optInt(desc string) schema.Int64Attribute {
	return schema.Int64Attribute{
		Optional: true, Computed: true, MarkdownDescription: desc,
		PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
	}
}

func optStr(desc string) schema.StringAttribute {
	return schema.StringAttribute{
		Optional: true, Computed: true, MarkdownDescription: desc,
		PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func (r *workspaceSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "General details, enabled products and sign-in security for one workspace. There is one per workspace; destroying it only removes it from Terraform state. Attributes you leave out keep their current value.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("Workspace id."),
			"name":        optStr("Workspace name. The main workspace cannot be renamed."),
			"description": optStr(""),
			"icon":        optStr("Icon key. Empty uses the workspace initials."),
			"modules": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Products enabled in the workspace.",
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"pipelines":          optBool("Pipelines (dbt, designer, notebooks)."),
					"fraud":              optBool("Fraud detection."),
					"aml":                optBool("Anti-money laundering."),
					"enterprise_lineage": optBool("Enterprise lineage."),
				},
			},
			"security": schema.SingleNestedAttribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Sign-in and session policy.",
				PlanModifiers:       []planmodifier.Object{objectplanmodifier.UseStateForUnknown()},
				Attributes: map[string]schema.Attribute{
					"session_absolute_seconds": optInt("Maximum session length."),
					"session_idle_seconds":     optInt("Idle timeout."),
					"require_mfa":              optBool("Require multi-factor authentication."),
					"sso_only":                 optBool("Only allow single sign-on. Needs an active SSO provider."),
					"allow_password_login":     optBool("Allow email and password sign-in."),
				},
			},
		},
	}
}

func (r *workspaceSettingsResource) snapshot(ctx context.Context, workspace string) (*settingsSnapshot, error) {
	var s settingsSnapshot
	if err := r.c.Get(ctx, "/api/settings", workspace, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func boolPtrValue(b *bool) types.Bool {
	if b == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*b)
}

func intPtrValue(n *int64) types.Int64 {
	if n == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*n)
}

func (r *workspaceSettingsResource) fill(m *workspaceSettingsModel, s *settingsSnapshot, diags *diag.Diagnostics) {
	m.ID = strValue(s.Workspace.ID)
	m.Name = strValue(s.Workspace.Name)
	m.Description = strValue(s.Workspace.Description)
	m.Icon = strValue(s.Workspace.Icon)
	mods, d := types.ObjectValue(modulesAttrTypes, map[string]attr.Value{
		"pipelines":          types.BoolValue(s.Modules["pipelines"]),
		"fraud":              types.BoolValue(s.Modules["fraud"]),
		"aml":                types.BoolValue(s.Modules["aml"]),
		"enterprise_lineage": types.BoolValue(s.Modules["enterpriseLineage"]),
	})
	diags.Append(d...)
	m.Modules = mods
	sec, d := types.ObjectValue(securityAttrTypes, map[string]attr.Value{
		"session_absolute_seconds": intPtrValue(s.Security.SessionAbsoluteSeconds),
		"session_idle_seconds":     intPtrValue(s.Security.SessionIdleSeconds),
		"require_mfa":              boolPtrValue(s.Security.RequireMFA),
		"sso_only":                 boolPtrValue(s.Security.SSOOnly),
		"allow_password_login":     boolPtrValue(s.Security.AllowPasswordLogin),
	})
	diags.Append(d...)
	m.Security = sec
}

func putBool(body map[string]any, key string, v types.Bool) {
	if known(v) {
		body[key] = v.ValueBool()
	}
}

func putInt(body map[string]any, key string, v types.Int64) {
	if known(v) {
		body[key] = v.ValueInt64()
	}
}

// apply sends only the values that are set in the plan and differ from the server.
func (r *workspaceSettingsResource) apply(ctx context.Context, plan *workspaceSettingsModel, diags *diag.Diagnostics) {
	ws := plan.Workspace.ValueString()
	cur, err := r.snapshot(ctx, ws)
	if err != nil {
		apiError(diags, "read workspace settings", err)
		return
	}
	general := map[string]any{}
	if known(plan.Name) && plan.Name.ValueString() != cur.Workspace.Name {
		general["name"] = plan.Name.ValueString()
	}
	if known(plan.Description) && plan.Description.ValueString() != cur.Workspace.Description {
		general["description"] = plan.Description.ValueString()
	}
	if known(plan.Icon) && plan.Icon.ValueString() != cur.Workspace.Icon {
		general["icon"] = plan.Icon.ValueString()
	}
	if len(general) > 0 {
		if err := r.c.Patch(ctx, "/api/settings/general", ws, general, nil); err != nil {
			apiError(diags, "update workspace details", err)
			return
		}
	}
	if known(plan.Modules) {
		var mm modulesModel
		diags.Append(plan.Modules.As(ctx, &mm, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true, UnhandledNullAsEmpty: true})...)
		body := map[string]any{}
		putBool(body, "pipelines", mm.Pipelines)
		putBool(body, "fraud", mm.Fraud)
		putBool(body, "aml", mm.AML)
		putBool(body, "enterpriseLineage", mm.EnterpriseLineage)
		if len(body) > 0 {
			if err := r.c.Patch(ctx, "/api/settings/modules", ws, body, nil); err != nil {
				apiError(diags, "update workspace products", err)
				return
			}
		}
	}
	if known(plan.Security) {
		var sm securityModel
		diags.Append(plan.Security.As(ctx, &sm, basetypes.ObjectAsOptions{UnhandledUnknownAsEmpty: true, UnhandledNullAsEmpty: true})...)
		body := map[string]any{}
		putInt(body, "session_absolute_seconds", sm.SessionAbsoluteSeconds)
		putInt(body, "session_idle_seconds", sm.SessionIdleSeconds)
		putBool(body, "require_mfa", sm.RequireMFA)
		putBool(body, "sso_only", sm.SSOOnly)
		putBool(body, "allow_password_login", sm.AllowPasswordLogin)
		if len(body) > 0 {
			if err := r.c.Patch(ctx, "/api/settings/security", ws, body, nil); err != nil {
				apiError(diags, "update workspace security", err)
				return
			}
		}
	}
	after, err := r.snapshot(ctx, ws)
	if err != nil {
		apiError(diags, "read workspace settings", err)
		return
	}
	r.fill(plan, after, diags)
}

func (r *workspaceSettingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *workspaceSettingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceSettingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	s, err := r.snapshot(ctx, state.Workspace.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "read workspace settings", err)
		return
	}
	r.fill(&state, s, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *workspaceSettingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan workspaceSettingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.apply(ctx, &plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *workspaceSettingsResource) Delete(_ context.Context, _ resource.DeleteRequest, resp *resource.DeleteResponse) {
	resp.Diagnostics.AddWarning("Workspace settings kept", "Destroying gezor_workspace_settings only removes it from Terraform state; the workspace keeps its current settings.")
}

func (r *workspaceSettingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<workspace>", "workspace")
}
