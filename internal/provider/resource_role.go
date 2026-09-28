package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider/internal/client"
)

var _ resource.ResourceWithImportState = &roleResource{}

func NewRoleResource() resource.Resource { return &roleResource{} }

type roleResource struct{ base }

type roleModel struct {
	Workspace   types.String `tfsdk:"workspace"`
	ID          types.String `tfsdk:"id"`
	Key         types.String `tfsdk:"key"`
	Name        types.String `tfsdk:"name"`
	Description types.String `tfsdk:"description"`
	Permissions types.Set    `tfsdk:"permissions"`
}

type apiRole struct {
	ID          string   `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	Builtin     bool     `json:"builtin"`
}

// portalAccess is always granted by the server.
const portalAccess = "portal.access"

func (r *roleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *roleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A custom role in a workspace. Use `gezor_permissions` to list permission keys. People are assigned to roles in the portal.",
		Attributes: map[string]schema.Attribute{
			"workspace": workspaceAttribute(),
			"id":        idAttribute("Role id."),
			"key":       requiredReplaceString("Unique key, for example `data_engineer`. Cannot be changed."),
			"name":      schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
			},
			"permissions": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				MarkdownDescription: "Permission keys. `portal.access` is always granted and can be left out.",
			},
		},
	}
}

func (r *roleResource) apply(ctx context.Context, m *roleModel, role apiRole, diags *diag.Diagnostics) {
	m.ID = strValue(role.ID)
	m.Key = strValue(role.Key)
	m.Name = strValue(role.Name)
	m.Description = strValue(role.Description)
	keepPortal := containsString(setStrings(ctx, m.Permissions, diags), portalAccess)
	perms := make([]string, 0, len(role.Permissions))
	for _, p := range role.Permissions {
		if p == portalAccess && !keepPortal {
			continue
		}
		perms = append(perms, p)
	}
	m.Permissions = stringSet(ctx, perms, diags)
}

// checkDropped reports permission keys the server ignored because they do not exist.
func checkDropped(ctx context.Context, want types.Set, got *roleModel, diags *diag.Diagnostics) {
	have := setStrings(ctx, got.Permissions, diags)
	var unknown []string
	for _, p := range setStrings(ctx, want, diags) {
		if p != portalAccess && !containsString(have, p) {
			unknown = append(unknown, p)
		}
	}
	if len(unknown) > 0 {
		diags.AddAttributeError(path.Root("permissions"), "Unknown permissions",
			fmt.Sprintf("Gezor does not have these permissions: %s. See the gezor_permissions data source.", strings.Join(unknown, ", ")))
	}
}

func (r *roleResource) get(ctx context.Context, m *roleModel, diags *diag.Diagnostics) error {
	var out struct {
		Role apiRole `json:"role"`
	}
	if err := r.c.Get(ctx, "/api/roles/"+client.PathEscape(m.ID.ValueString()), m.Workspace.ValueString(), &out); err != nil {
		return err
	}
	r.apply(ctx, m, out.Role, diags)
	return nil
}

func (r *roleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"key":         plan.Key.ValueString(),
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
		"permissions": setStrings(ctx, plan.Permissions, &resp.Diagnostics),
	}
	var out struct {
		Role apiRole `json:"role"`
	}
	if err := r.c.Post(ctx, "/api/roles", plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "create role", err)
		return
	}
	want := plan.Permissions
	plan.ID = strValue(out.Role.ID)
	if err := r.get(ctx, &plan, &resp.Diagnostics); err != nil {
		apiError(&resp.Diagnostics, "read role", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	checkDropped(ctx, want, &plan, &resp.Diagnostics)
}

func (r *roleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.get(ctx, &state, &resp.Diagnostics); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read role", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *roleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state roleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	plan.ID = state.ID
	body := map[string]any{
		"name":        plan.Name.ValueString(),
		"description": plan.Description.ValueString(),
		"permissions": setStrings(ctx, plan.Permissions, &resp.Diagnostics),
	}
	if err := r.c.Patch(ctx, "/api/roles/"+client.PathEscape(state.ID.ValueString()), plan.Workspace.ValueString(), body, nil); err != nil {
		apiError(&resp.Diagnostics, "update role", err)
		return
	}
	want := plan.Permissions
	if err := r.get(ctx, &plan, &resp.Diagnostics); err != nil {
		apiError(&resp.Diagnostics, "read role", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
	checkDropped(ctx, want, &plan, &resp.Diagnostics)
}

func (r *roleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state roleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, "/api/roles/"+client.PathEscape(state.ID.ValueString()), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete role", err)
	}
}

func (r *roleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<role_id>", "id")
}
