package provider

import (
	"context"
	"net/url"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

var (
	_ resource.ResourceWithImportState = &workspaceResource{}
)

func NewWorkspaceResource() resource.Resource { return &workspaceResource{} }

type workspaceResource struct{ base }

type workspaceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Slug        types.String `tfsdk:"slug"`
	Description types.String `tfsdk:"description"`
	IsPrimary   types.Bool   `tfsdk:"is_primary"`
}

type apiTenant struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	IsPrimary   bool   `json:"is_primary"`
	Icon        string `json:"icon"`
	Role        string `json:"role"`
	RoleID      string `json:"role_id"`
}

func (r *workspaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (r *workspaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A workspace in the organization. The token's service account must have access to every workspace (or be granted the new one) to manage resources inside it. The main workspace cannot be deleted, and a workspace can only be deleted from another workspace after its clusters and apps are removed.",
		Attributes: map[string]schema.Attribute{
			"id":   idAttribute("Workspace id."),
			"name": schema.StringAttribute{Required: true, MarkdownDescription: "Display name."},
			"slug": requiredReplaceString("URL-safe identifier (3-48 characters). Cannot be changed."),
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			},
			"is_primary": schema.BoolAttribute{
				Computed:      true,
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *workspaceResource) read(ctx context.Context, id string, m *workspaceModel) error {
	var out struct {
		Tenant apiTenant `json:"tenant"`
	}
	if err := r.c.Get(ctx, "/api/tenants/"+client.PathEscape(id), "", &out); err != nil {
		return err
	}
	m.ID = strValue(out.Tenant.ID)
	m.Name = strValue(out.Tenant.Name)
	m.Slug = strValue(out.Tenant.Slug)
	m.Description = strValue(out.Tenant.Description)
	m.IsPrimary = types.BoolValue(out.Tenant.IsPrimary)
	return nil
}

func (r *workspaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		TenantID string `json:"tenant_id"`
	}
	body := map[string]any{"name": plan.Name.ValueString(), "slug": plan.Slug.ValueString(), "description": plan.Description.ValueString()}
	if err := r.c.Post(ctx, "/api/tenants", "", body, &out); err != nil {
		apiError(&resp.Diagnostics, "create workspace", err)
		return
	}
	if err := r.read(ctx, out.TenantID, &plan); err != nil {
		apiError(&resp.Diagnostics, "read workspace", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *workspaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.read(ctx, state.ID.ValueString(), &state); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read workspace", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *workspaceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state workspaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"description": plan.Description.ValueString()}
	if plan.Name.ValueString() != state.Name.ValueString() {
		body["name"] = plan.Name.ValueString()
	}
	if err := r.c.Patch(ctx, "/api/tenants/"+client.PathEscape(state.ID.ValueString()), "", body, nil); err != nil {
		apiError(&resp.Diagnostics, "update workspace", err)
		return
	}
	if err := r.read(ctx, state.ID.ValueString(), &plan); err != nil {
		apiError(&resp.Diagnostics, "read workspace", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *workspaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state workspaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := "/api/tenants/" + client.PathEscape(state.ID.ValueString()) + "?confirm_name=" + url.QueryEscape(state.Name.ValueString())
	if err := r.c.Delete(ctx, p, "", nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete workspace", err)
	}
}

func (r *workspaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
