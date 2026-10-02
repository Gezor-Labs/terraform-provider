package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

type apiSSOProvider struct {
	ID            string              `json:"id"`
	Kind          string              `json:"kind"`
	Status        string              `json:"status"`
	DisplayName   string              `json:"display_name"`
	Config        map[string]any      `json:"config"`
	GroupMappings []map[string]string `json:"group_mappings"`
}

func getSSOProvider(ctx context.Context, c *client.Client, workspace, id string) (*apiSSOProvider, error) {
	var out struct {
		Provider apiSSOProvider `json:"provider"`
	}
	if err := c.Get(ctx, "/api/settings/sso/providers/"+client.PathEscape(id), workspace, &out); err != nil {
		return nil, err
	}
	return &out.Provider, nil
}

// ---------------------------------------------------------------- gezor_sso_provider

var _ resource.ResourceWithImportState = &ssoProviderResource{}

func NewSSOProviderResource() resource.Resource { return &ssoProviderResource{} }

type ssoProviderResource struct{ base }

type ssoProviderModel struct {
	Workspace       types.String `tfsdk:"workspace"`
	ID              types.String `tfsdk:"id"`
	Kind            types.String `tfsdk:"kind"`
	DisplayName     types.String `tfsdk:"display_name"`
	Status          types.String `tfsdk:"status"`
	Issuer          types.String `tfsdk:"issuer"`
	ClientID        types.String `tfsdk:"client_id"`
	ClientSecret    types.String `tfsdk:"client_secret"`
	EntraTenantID   types.String `tfsdk:"entra_tenant_id"`
	Cloud           types.String `tfsdk:"cloud"`
	GroupClaim      types.String `tfsdk:"group_claim"`
	HasClientSecret types.Bool   `tfsdk:"has_client_secret"`
	RedirectURI     types.String `tfsdk:"redirect_uri"`
}

func (r *ssoProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_provider"
}

func (r *ssoProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	kind := requiredReplaceString("`entra`, `okta` or `google`. One provider of each kind per workspace.")
	kind.Validators = []validator.String{stringvalidator.OneOf("entra", "okta", "google")}
	status := optStr("`disabled`, `ready` or `active`. `active` needs issuer, client id and client secret.")
	status.Validators = []validator.String{stringvalidator.OneOf("disabled", "ready", "active")}
	resp.Schema = schema.Schema{
		MarkdownDescription: "An OpenID Connect single sign-on provider for a workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace":    workspaceAttribute(),
			"id":           idAttribute("Provider id."),
			"kind":         kind,
			"display_name": optStr("Name on the sign-in button."),
			"status":       status,
			"issuer":       optStr("OIDC issuer URL. Derived for Google and for Entra when `entra_tenant_id` is set."),
			"client_id":    schema.StringAttribute{Optional: true},
			"client_secret": schema.StringAttribute{
				Optional: true, Sensitive: true,
				MarkdownDescription: "Client secret. Write-only: it is never read back, so changes made outside Terraform are not detected.",
			},
			"entra_tenant_id":   schema.StringAttribute{Optional: true, MarkdownDescription: "Microsoft Entra tenant id."},
			"cloud":             optStr("Entra cloud: `global`, `usgov`, `china` or `germany`."),
			"group_claim":       optStr("ID token claim that lists groups."),
			"has_client_secret": schema.BoolAttribute{Computed: true},
			"redirect_uri": schema.StringAttribute{
				Computed: true, MarkdownDescription: "Callback URL to register with the identity provider.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *ssoProviderResource) fill(m *ssoProviderModel, p *apiSSOProvider) {
	m.ID = strValue(p.ID)
	m.Kind = strValue(p.Kind)
	m.DisplayName = strValue(p.DisplayName)
	m.Status = strValue(p.Status)
	cfg := p.Config
	m.Issuer = strValue(asString(cfg["issuer"]))
	m.ClientID = optString(asString(cfg["client_id"]))
	tid := asString(cfg["entra_tenant_id"])
	if tid == "" {
		tid = asString(cfg["tenant_id"])
	}
	m.EntraTenantID = optString(tid)
	m.Cloud = strValue(asString(cfg["cloud"]))
	m.GroupClaim = strValue(asString(cfg["group_claim"]))
	m.HasClientSecret = types.BoolValue(asBool(cfg["has_client_secret"]))
	m.RedirectURI = strValue(asString(cfg["redirect_uri"]))
}

func (r *ssoProviderResource) put(ctx context.Context, plan *ssoProviderModel) (*apiSSOProvider, error) {
	cfg := map[string]any{}
	for key, v := range map[string]types.String{
		"issuer": plan.Issuer, "client_id": plan.ClientID, "client_secret": plan.ClientSecret,
		"entra_tenant_id": plan.EntraTenantID, "cloud": plan.Cloud, "group_claim": plan.GroupClaim,
	} {
		if known(v) && v.ValueString() != "" {
			cfg[key] = v.ValueString()
		}
	}
	body := map[string]any{"kind": plan.Kind.ValueString(), "config": cfg}
	if known(plan.DisplayName) {
		body["display_name"] = plan.DisplayName.ValueString()
	}
	if known(plan.Status) {
		body["status"] = plan.Status.ValueString()
	}
	var out struct {
		Provider apiSSOProvider `json:"provider"`
	}
	if err := r.c.Put(ctx, "/api/settings/sso/providers", plan.Workspace.ValueString(), body, &out); err != nil {
		return nil, err
	}
	return &out.Provider, nil
}

func (r *ssoProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ssoProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var snap settingsSnapshot
	if err := r.c.Get(ctx, "/api/settings", plan.Workspace.ValueString(), &snap); err != nil {
		apiError(&resp.Diagnostics, "read SSO providers", err)
		return
	}
	for _, p := range snap.SSO.Providers {
		if p.Kind == plan.Kind.ValueString() {
			resp.Diagnostics.AddError("SSO provider already exists",
				fmt.Sprintf("This workspace already has a %s provider (%s). Import it with: terraform import <address> %s", p.Kind, p.ID, p.ID))
			return
		}
	}
	p, err := r.put(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "create SSO provider", err)
		return
	}
	r.fill(&plan, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ssoProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := getSSOProvider(ctx, r.c, state.Workspace.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read SSO provider", err)
		return
	}
	r.fill(&state, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ssoProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ssoProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.put(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "update SSO provider", err)
		return
	}
	r.fill(&plan, p)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ssoProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.c.Delete(ctx, "/api/settings/sso/providers/"+client.PathEscape(state.ID.ValueString()), state.Workspace.ValueString(), nil)
	if err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete SSO provider", err)
	}
}

func (r *ssoProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<provider_id>", "id")
}

// ---------------------------------------------------------------- gezor_sso_group_mappings

var _ resource.ResourceWithImportState = &ssoGroupMappingsResource{}

func NewSSOGroupMappingsResource() resource.Resource { return &ssoGroupMappingsResource{} }

type ssoGroupMappingsResource struct{ base }

type ssoGroupMappingsModel struct {
	Workspace     types.String `tfsdk:"workspace"`
	ID            types.String `tfsdk:"id"`
	ProviderID    types.String `tfsdk:"provider_id"`
	Mappings      types.List   `tfsdk:"mapping"`
	DefaultRoleID types.String `tfsdk:"default_role_id"`
	SyncGroups    types.Bool   `tfsdk:"sync_groups"`
}

var mappingAttrTypes = map[string]attr.Type{"idp_group": types.StringType, "role_id": types.StringType}

type mappingModel struct {
	IdpGroup types.String `tfsdk:"idp_group"`
	RoleID   types.String `tfsdk:"role_id"`
}

func (r *ssoGroupMappingsResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_group_mappings"
}

func (r *ssoGroupMappingsResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Maps identity provider groups to workspace roles for one SSO provider. Manages the full list; destroying it clears the mappings. Every mapped role and the default role must be within the token's own permissions, so roles that manage people, such as Admin, can only be mapped in the portal.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("Same as `provider_id`."),
			"provider_id": requiredReplaceString("SSO provider id."),
			"mapping": schema.ListNestedAttribute{
				Optional:            true,
				MarkdownDescription: "Group to role mappings, checked in order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"idp_group": schema.StringAttribute{Required: true, MarkdownDescription: "Group name or id sent by the identity provider."},
						"role_id":   schema.StringAttribute{Required: true, MarkdownDescription: "Workspace role id."},
					},
				},
			},
			"default_role_id": schema.StringAttribute{Optional: true, MarkdownDescription: "Role for people who match no group."},
			"sync_groups": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Update a person's role from their groups at every sign-in.",
			},
		},
	}
}

func (r *ssoGroupMappingsResource) fill(ctx context.Context, m *ssoGroupMappingsModel, p *apiSSOProvider, diags *diag.Diagnostics) {
	m.ID = strValue(p.ID)
	m.ProviderID = strValue(p.ID)
	if len(p.GroupMappings) == 0 && m.Mappings.IsNull() {
		// keep null
	} else {
		items := make([]attr.Value, 0, len(p.GroupMappings))
		for _, gm := range p.GroupMappings {
			obj, d := types.ObjectValue(mappingAttrTypes, map[string]attr.Value{
				"idp_group": strValue(gm["idp_group"]), "role_id": strValue(gm["role_id"]),
			})
			diags.Append(d...)
			items = append(items, obj)
		}
		l, d := types.ListValue(types.ObjectType{AttrTypes: mappingAttrTypes}, items)
		diags.Append(d...)
		m.Mappings = l
	}
	m.DefaultRoleID = optString(asString(p.Config["default_role_id"]))
	sync, ok := p.Config["sync_groups"].(bool)
	if !ok {
		sync = true
	}
	m.SyncGroups = types.BoolValue(sync)
}

func (r *ssoGroupMappingsResource) put(ctx context.Context, m *ssoGroupMappingsModel, diags *diag.Diagnostics) (*apiSSOProvider, error) {
	var items []mappingModel
	if known(m.Mappings) {
		diags.Append(m.Mappings.ElementsAs(ctx, &items, false)...)
	}
	mappings := make([]map[string]string, 0, len(items))
	for _, it := range items {
		mappings = append(mappings, map[string]string{"idp_group": it.IdpGroup.ValueString(), "role_id": it.RoleID.ValueString()})
	}
	body := map[string]any{
		"group_mappings":  mappings,
		"default_role_id": m.DefaultRoleID.ValueString(),
		"sync_groups":     m.SyncGroups.IsNull() || m.SyncGroups.IsUnknown() || m.SyncGroups.ValueBool(),
	}
	var out struct {
		Provider apiSSOProvider `json:"provider"`
	}
	p := "/api/settings/sso/providers/" + client.PathEscape(m.ProviderID.ValueString()) + "/mappings"
	if err := r.c.Put(ctx, p, m.Workspace.ValueString(), body, &out); err != nil {
		return nil, err
	}
	return &out.Provider, nil
}

func (r *ssoGroupMappingsResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ssoGroupMappingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.put(ctx, &plan, &resp.Diagnostics)
	if err != nil {
		apiError(&resp.Diagnostics, "set SSO group mappings", err)
		return
	}
	r.fill(ctx, &plan, p, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoGroupMappingsResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ssoGroupMappingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ProviderID.ValueString()
	if id == "" {
		id = state.ID.ValueString()
	}
	p, err := getSSOProvider(ctx, r.c, state.Workspace.ValueString(), id)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read SSO group mappings", err)
		return
	}
	r.fill(ctx, &state, p, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ssoGroupMappingsResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ssoGroupMappingsModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p, err := r.put(ctx, &plan, &resp.Diagnostics)
	if err != nil {
		apiError(&resp.Diagnostics, "set SSO group mappings", err)
		return
	}
	r.fill(ctx, &plan, p, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoGroupMappingsResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ssoGroupMappingsModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	cleared := ssoGroupMappingsModel{
		Workspace: state.Workspace, ProviderID: state.ProviderID,
		Mappings: types.ListNull(types.ObjectType{AttrTypes: mappingAttrTypes}), DefaultRoleID: types.StringNull(), SyncGroups: types.BoolValue(true),
	}
	if _, err := r.put(ctx, &cleared, &resp.Diagnostics); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "clear SSO group mappings", err)
	}
}

func (r *ssoGroupMappingsResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<provider_id>", "provider_id")
}

// ---------------------------------------------------------------- gezor_sso_domain

var _ resource.ResourceWithImportState = &ssoDomainResource{}

func NewSSODomainResource() resource.Resource { return &ssoDomainResource{} }

type ssoDomainResource struct{ base }

type ssoDomainModel struct {
	Workspace  types.String `tfsdk:"workspace"`
	ID         types.String `tfsdk:"id"`
	Domain     types.String `tfsdk:"domain"`
	ProviderID types.String `tfsdk:"provider_id"`
	Verified   types.Bool   `tfsdk:"verified"`
	TxtHost    types.String `tfsdk:"txt_host"`
	TxtRecord  types.String `tfsdk:"txt_record"`
}

type apiDomain struct {
	Domain     string  `json:"domain"`
	ProviderID *string `json:"provider_id"`
	Verified   bool    `json:"verified"`
	TxtHost    string  `json:"txt_host"`
	TxtRecord  string  `json:"txt_record"`
}

func (r *ssoDomainResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_sso_domain"
}

func (r *ssoDomainResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "An email domain that signs in through SSO. Publish `txt_record` at `txt_host` in DNS, then verify the domain in the portal.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("Same as `domain`."),
			"domain":      requiredReplaceString("Email domain, for example `example.com`."),
			"provider_id": schema.StringAttribute{Optional: true, MarkdownDescription: "SSO provider for this domain."},
			"verified":    schema.BoolAttribute{Computed: true},
			"txt_host": schema.StringAttribute{Computed: true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"txt_record": schema.StringAttribute{Computed: true, MarkdownDescription: "DNS TXT value proving ownership.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *ssoDomainResource) fill(m *ssoDomainModel, d apiDomain) {
	m.ID = strValue(d.Domain)
	m.Domain = strValue(d.Domain)
	if d.ProviderID != nil && *d.ProviderID != "" {
		m.ProviderID = strValue(*d.ProviderID)
	} else {
		m.ProviderID = types.StringNull()
	}
	m.Verified = types.BoolValue(d.Verified)
	m.TxtHost = strValue(d.TxtHost)
	m.TxtRecord = strValue(d.TxtRecord)
}

func (r *ssoDomainResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ssoDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"domain": plan.Domain.ValueString()}
	if known(plan.ProviderID) {
		body["provider_id"] = plan.ProviderID.ValueString()
	}
	var out struct {
		Domain apiDomain `json:"domain"`
	}
	if err := r.c.Post(ctx, "/api/settings/sso/domains", plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "add SSO domain", err)
		return
	}
	r.fill(&plan, out.Domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoDomainResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ssoDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	domain := state.Domain.ValueString()
	if domain == "" {
		domain = state.ID.ValueString()
	}
	var out struct {
		Domain apiDomain `json:"domain"`
	}
	if err := r.c.Get(ctx, "/api/settings/sso/domains/"+client.PathEscape(domain), state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read SSO domain", err)
		return
	}
	r.fill(&state, out.Domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *ssoDomainResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ssoDomainModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Domain apiDomain `json:"domain"`
	}
	body := map[string]any{"provider_id": plan.ProviderID.ValueString()}
	if err := r.c.Patch(ctx, "/api/settings/sso/domains/"+client.PathEscape(plan.Domain.ValueString()), plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update SSO domain", err)
		return
	}
	r.fill(&plan, out.Domain)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ssoDomainResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ssoDomainModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.c.Delete(ctx, "/api/settings/sso/domains/"+client.PathEscape(state.Domain.ValueString()), state.Workspace.ValueString(), nil)
	if err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete SSO domain", err)
	}
}

func (r *ssoDomainResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<domain>", "domain")
}
