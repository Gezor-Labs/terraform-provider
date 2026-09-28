package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider/internal/client"
)

// ---------------------------------------------------------------- gezor_current_identity

func NewCurrentIdentityDataSource() datasource.DataSource { return &currentIdentityDataSource{} }

type currentIdentityDataSource struct{ dsBase }

type currentIdentityModel struct {
	Workspace        types.String `tfsdk:"workspace"`
	UserID           types.String `tfsdk:"user_id"`
	Name             types.String `tfsdk:"name"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	OrganizationName types.String `tfsdk:"organization_name"`
	WorkspaceID      types.String `tfsdk:"workspace_id"`
	WorkspaceSlug    types.String `tfsdk:"workspace_slug"`
	WorkspaceName    types.String `tfsdk:"workspace_name"`
	Role             types.String `tfsdk:"role"`
	Permissions      types.Set    `tfsdk:"permissions"`
}

func (d *currentIdentityDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_current_identity"
}

func (d *currentIdentityDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The service account behind the API token and what it can do in a workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace":         dsWorkspaceAttribute(),
			"user_id":           schema.StringAttribute{Computed: true, MarkdownDescription: "Service account user id."},
			"name":              schema.StringAttribute{Computed: true, MarkdownDescription: "Service account name."},
			"organization_id":   schema.StringAttribute{Computed: true},
			"organization_name": schema.StringAttribute{Computed: true},
			"workspace_id":      schema.StringAttribute{Computed: true},
			"workspace_slug":    schema.StringAttribute{Computed: true},
			"workspace_name":    schema.StringAttribute{Computed: true},
			"role":              schema.StringAttribute{Computed: true},
			"permissions":       schema.SetAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *currentIdentityDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m currentIdentityModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var me struct {
		User struct {
			ID       string `json:"id"`
			FullName string `json:"full_name"`
		} `json:"user"`
		Organization struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"organization"`
		Tenant struct {
			ID   string `json:"id"`
			Slug string `json:"slug"`
			Name string `json:"name"`
		} `json:"tenant"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
	}
	if err := d.c.Get(ctx, "/api/auth/me", m.Workspace.ValueString(), &me); err != nil {
		apiError(&resp.Diagnostics, "read current identity", err)
		return
	}
	m.UserID = strValue(me.User.ID)
	m.Name = strValue(me.User.FullName)
	m.OrganizationID = strValue(me.Organization.ID)
	m.OrganizationName = strValue(me.Organization.Name)
	m.WorkspaceID = strValue(me.Tenant.ID)
	m.WorkspaceSlug = strValue(me.Tenant.Slug)
	m.WorkspaceName = strValue(me.Tenant.Name)
	m.Role = strValue(me.Role)
	m.Permissions = stringSet(ctx, me.Permissions, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

// ---------------------------------------------------------------- gezor_workspace / gezor_workspaces

var workspaceObjectTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "slug": types.StringType,
	"description": types.StringType, "is_primary": types.BoolType,
}

func workspaceObject(t apiTenant, diags *diag.Diagnostics) attr.Value {
	v, d := types.ObjectValue(workspaceObjectTypes, map[string]attr.Value{
		"id": strValue(t.ID), "name": strValue(t.Name), "slug": strValue(t.Slug),
		"description": strValue(t.Description), "is_primary": types.BoolValue(t.IsPrimary),
	})
	diags.Append(d...)
	return v
}

func workspaceAttrs(computedID bool) map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"id":          schema.StringAttribute{Optional: computedID, Computed: true, MarkdownDescription: "Workspace id or slug to look up."},
		"name":        schema.StringAttribute{Computed: true},
		"slug":        schema.StringAttribute{Computed: true},
		"description": schema.StringAttribute{Computed: true},
		"is_primary":  schema.BoolAttribute{Computed: true},
	}
}

func NewWorkspaceDataSource() datasource.DataSource { return &workspaceDataSource{} }

type workspaceDataSource struct{ dsBase }

func (d *workspaceDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace"
}

func (d *workspaceDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up one workspace by id or slug. Without `id`, returns the provider's workspace.",
		Attributes:          workspaceAttrs(true),
	}
}

func (d *workspaceDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var m workspaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := m.ID.ValueString()
	if id == "" {
		var me struct {
			Tenant struct {
				ID string `json:"id"`
			} `json:"tenant"`
		}
		if err := d.c.Get(ctx, "/api/auth/me", "", &me); err != nil {
			apiError(&resp.Diagnostics, "read current workspace", err)
			return
		}
		id = me.Tenant.ID
	}
	var out struct {
		Tenant apiTenant `json:"tenant"`
	}
	if err := d.c.Get(ctx, "/api/tenants/"+client.PathEscape(id), "", &out); err != nil {
		apiError(&resp.Diagnostics, "read workspace", err)
		return
	}
	t := out.Tenant
	m = workspaceModel{ID: strValue(t.ID), Name: strValue(t.Name), Slug: strValue(t.Slug), Description: strValue(t.Description), IsPrimary: types.BoolValue(t.IsPrimary)}
	resp.Diagnostics.Append(resp.State.Set(ctx, m)...)
}

func NewWorkspacesDataSource() datasource.DataSource { return &workspacesDataSource{} }

type workspacesDataSource struct{ dsBase }

func (d *workspacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspaces"
}

func (d *workspacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "All workspaces the token can use.",
		Attributes: map[string]schema.Attribute{
			"workspaces": schema.ListNestedAttribute{
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: workspaceAttrs(false)},
			},
		},
	}
}

func (d *workspacesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var out struct {
		Tenants []apiTenant `json:"tenants"`
	}
	if err := d.c.Get(ctx, "/api/tenants", "", &out); err != nil {
		apiError(&resp.Diagnostics, "list workspaces", err)
		return
	}
	items := make([]attr.Value, 0, len(out.Tenants))
	for _, t := range out.Tenants {
		items = append(items, workspaceObject(t, &resp.Diagnostics))
	}
	l, diags := types.ListValue(types.ObjectType{AttrTypes: workspaceObjectTypes}, items)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("workspaces"), l)...)
}

// ---------------------------------------------------------------- gezor_roles

func NewRolesDataSource() datasource.DataSource { return &rolesDataSource{} }

type rolesDataSource struct{ dsBase }

var roleObjectTypes = map[string]attr.Type{
	"id": types.StringType, "key": types.StringType, "name": types.StringType, "description": types.StringType,
	"builtin": types.BoolType, "permissions": types.SetType{ElemType: types.StringType},
}

func (d *rolesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_roles"
}

func (d *rolesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Built-in and custom roles in a workspace. `by_key` maps role keys (for example `admin`) to role ids.",
		Attributes: map[string]schema.Attribute{
			"workspace": dsWorkspaceAttribute(),
			"roles": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{Computed: true}, "key": schema.StringAttribute{Computed: true},
					"name": schema.StringAttribute{Computed: true}, "description": schema.StringAttribute{Computed: true},
					"builtin":     schema.BoolAttribute{Computed: true},
					"permissions": schema.SetAttribute{Computed: true, ElementType: types.StringType},
				}},
			},
			"by_key": schema.MapAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *rolesDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var ws types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("workspace"), &ws)...)
	var out struct {
		Roles []apiRole `json:"roles"`
	}
	if err := d.c.Get(ctx, "/api/roles", ws.ValueString(), &out); err != nil {
		apiError(&resp.Diagnostics, "list roles", err)
		return
	}
	items := make([]attr.Value, 0, len(out.Roles))
	byKey := map[string]string{}
	for _, r := range out.Roles {
		obj, diags := types.ObjectValue(roleObjectTypes, map[string]attr.Value{
			"id": strValue(r.ID), "key": strValue(r.Key), "name": strValue(r.Name), "description": strValue(r.Description),
			"builtin": types.BoolValue(r.Builtin), "permissions": stringSet(ctx, r.Permissions, &resp.Diagnostics),
		})
		resp.Diagnostics.Append(diags...)
		items = append(items, obj)
		byKey[r.Key] = r.ID
	}
	l, diags := types.ListValue(types.ObjectType{AttrTypes: roleObjectTypes}, items)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("workspace"), ws)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("roles"), l)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("by_key"), stringMap(ctx, byKey, &resp.Diagnostics))...)
}

// ---------------------------------------------------------------- gezor_permissions

func NewPermissionsDataSource() datasource.DataSource { return &permissionsDataSource{} }

type permissionsDataSource struct{ dsBase }

var permissionObjectTypes = map[string]attr.Type{"id": types.StringType, "group": types.StringType, "label": types.StringType}

func (d *permissionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permissions"
}

func (d *permissionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every permission key that can be put in a role.",
		Attributes: map[string]schema.Attribute{
			"permissions": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{Computed: true}, "group": schema.StringAttribute{Computed: true},
					"label": schema.StringAttribute{Computed: true},
				}},
			},
			"ids": schema.SetAttribute{Computed: true, ElementType: types.StringType},
		},
	}
}

func (d *permissionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	var out struct {
		Permissions []struct {
			ID    string `json:"id"`
			Group string `json:"group"`
			Label string `json:"label"`
		} `json:"permissions"`
	}
	if err := d.c.Get(ctx, "/api/roles/catalog", "", &out); err != nil {
		apiError(&resp.Diagnostics, "list permissions", err)
		return
	}
	items := make([]attr.Value, 0, len(out.Permissions))
	ids := make([]string, 0, len(out.Permissions))
	for _, p := range out.Permissions {
		obj, diags := types.ObjectValue(permissionObjectTypes, map[string]attr.Value{
			"id": strValue(p.ID), "group": strValue(p.Group), "label": strValue(p.Label),
		})
		resp.Diagnostics.Append(diags...)
		items = append(items, obj)
		ids = append(ids, p.ID)
	}
	l, diags := types.ListValue(types.ObjectType{AttrTypes: permissionObjectTypes}, items)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("permissions"), l)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("ids"), stringSet(ctx, ids, &resp.Diagnostics))...)
}

// ---------------------------------------------------------------- gezor_cluster / gezor_clusters

var clusterObjectTypes = map[string]attr.Type{
	"id": types.StringType, "name": types.StringType, "description": types.StringType,
	"environment": types.StringType, "region": types.StringType, "hosting_mode": types.StringType,
	"status": types.StringType, "online": types.BoolType, "operator_namespace": types.StringType,
	"operator_version": types.StringType, "tags": types.ListType{ElemType: types.StringType},
	"enabled_apps": types.SetType{ElemType: types.StringType},
}

func clusterObjectAttrs(lookup bool) map[string]schema.Attribute {
	a := map[string]schema.Attribute{
		"id":                 schema.StringAttribute{Computed: true},
		"name":               schema.StringAttribute{Computed: true},
		"description":        schema.StringAttribute{Computed: true},
		"environment":        schema.StringAttribute{Computed: true},
		"region":             schema.StringAttribute{Computed: true},
		"hosting_mode":       schema.StringAttribute{Computed: true},
		"status":             schema.StringAttribute{Computed: true},
		"online":             schema.BoolAttribute{Computed: true},
		"operator_namespace": schema.StringAttribute{Computed: true},
		"operator_version":   schema.StringAttribute{Computed: true},
		"tags":               schema.ListAttribute{Computed: true, ElementType: types.StringType},
		"enabled_apps":       schema.SetAttribute{Computed: true, ElementType: types.StringType, MarkdownDescription: "Apps (modules) that are turned on."},
	}
	if lookup {
		a["id"] = schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Cluster id. Set `id` or `name`."}
		a["name"] = schema.StringAttribute{Optional: true, Computed: true, MarkdownDescription: "Cluster name. Set `id` or `name`."}
		a["workspace"] = dsWorkspaceAttribute()
	}
	return a
}

func clusterValues(ctx context.Context, c apiCluster, diags *diag.Diagnostics) map[string]attr.Value {
	var apps []string
	for name, raw := range c.DesiredState.Modules {
		if m, ok := raw.(map[string]any); ok && asBool(m["enabled"]) {
			apps = append(apps, name)
		}
	}
	return map[string]attr.Value{
		"id": strValue(c.ID), "name": strValue(c.Name), "description": strValue(c.Description),
		"environment": strValue(c.Environment), "region": strValue(c.Region), "hosting_mode": strValue(c.HostingMode),
		"status": strValue(c.Status), "online": types.BoolValue(c.Online), "operator_namespace": strValue(c.OperatorNamespace),
		"operator_version": strValue(c.OperatorVersion), "tags": stringList(ctx, c.Tags, diags),
		"enabled_apps": stringSet(ctx, apps, diags),
	}
}

func NewClusterDataSource() datasource.DataSource { return &clusterDataSource{} }

type clusterDataSource struct{ dsBase }

func (d *clusterDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cluster"
}

func (d *clusterDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{MarkdownDescription: "Looks up one cluster by id or name.", Attributes: clusterObjectAttrs(true)}
}

func (d *clusterDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var ws, id, name types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("workspace"), &ws)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("id"), &id)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("name"), &name)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var found *apiCluster
	if id.ValueString() != "" {
		c, err := getCluster(ctx, d.c, ws.ValueString(), id.ValueString())
		if err != nil {
			apiError(&resp.Diagnostics, "read cluster", err)
			return
		}
		found = c
	} else if name.ValueString() != "" {
		list, err := listClusters(ctx, d.c, ws.ValueString())
		if err != nil {
			apiError(&resp.Diagnostics, "list clusters", err)
			return
		}
		for i := range list {
			if list[i].Name == name.ValueString() {
				found = &list[i]
				break
			}
		}
		if found == nil {
			resp.Diagnostics.AddError("Cluster not found", fmt.Sprintf("No cluster named %q in the workspace.", name.ValueString()))
			return
		}
	} else {
		resp.Diagnostics.AddError("Missing lookup", "Set id or name.")
		return
	}
	for k, v := range clusterValues(ctx, *found, &resp.Diagnostics) {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot(k), v)...)
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("workspace"), ws)...)
}

func NewClustersDataSource() datasource.DataSource { return &clustersDataSource{} }

type clustersDataSource struct{ dsBase }

func (d *clustersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_clusters"
}

func (d *clustersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "All clusters in a workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace": dsWorkspaceAttribute(),
			"clusters": schema.ListNestedAttribute{
				Computed:     true,
				NestedObject: schema.NestedAttributeObject{Attributes: clusterObjectAttrs(false)},
			},
		},
	}
}

func (d *clustersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var ws types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("workspace"), &ws)...)
	list, err := listClusters(ctx, d.c, ws.ValueString())
	if err != nil {
		apiError(&resp.Diagnostics, "list clusters", err)
		return
	}
	items := make([]attr.Value, 0, len(list))
	for _, c := range list {
		obj, diags := types.ObjectValue(clusterObjectTypes, clusterValues(ctx, c, &resp.Diagnostics))
		resp.Diagnostics.Append(diags...)
		items = append(items, obj)
	}
	l, diags := types.ListValue(types.ObjectType{AttrTypes: clusterObjectTypes}, items)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("workspace"), ws)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("clusters"), l)...)
}

// ---------------------------------------------------------------- gezor_kms_keys

func NewKMSKeysDataSource() datasource.DataSource { return &kmsKeysDataSource{} }

type kmsKeysDataSource struct{ dsBase }

var kmsObjectTypes = map[string]attr.Type{
	"id": types.StringType, "arn": types.StringType, "alias_name": types.StringType, "description": types.StringType,
	"key_manager": types.StringType, "key_state": types.StringType, "enabled": types.BoolType, "is_default": types.BoolType,
}

func (d *kmsKeysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kms_keys"
}

func (d *kmsKeysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	attrs := map[string]schema.Attribute{}
	for k, t := range kmsObjectTypes {
		if t == types.BoolType {
			attrs[k] = schema.BoolAttribute{Computed: true}
		} else {
			attrs[k] = schema.StringAttribute{Computed: true}
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Platform and customer encryption keys in a workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace":      dsWorkspaceAttribute(),
			"default_key_id": schema.StringAttribute{Computed: true},
			"keys": schema.ListNestedAttribute{
				Computed: true, NestedObject: schema.NestedAttributeObject{Attributes: attrs},
			},
		},
	}
}

func (d *kmsKeysDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var ws types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("workspace"), &ws)...)
	var out apiKMSList
	if err := d.c.Get(ctx, "/api/kms/keys", ws.ValueString(), &out); err != nil {
		apiError(&resp.Diagnostics, "list KMS keys", err)
		return
	}
	items := make([]attr.Value, 0, len(out.Keys))
	for _, k := range out.Keys {
		obj, diags := types.ObjectValue(kmsObjectTypes, map[string]attr.Value{
			"id": strValue(k.KeyID), "arn": strValue(k.KeyArn), "alias_name": strValue(k.AliasName),
			"description": strValue(k.Description), "key_manager": strValue(k.KeyManager), "key_state": strValue(k.KeyState),
			"enabled": types.BoolValue(k.Enabled), "is_default": types.BoolValue(k.IsDefault),
		})
		resp.Diagnostics.Append(diags...)
		items = append(items, obj)
	}
	l, diags := types.ListValue(types.ObjectType{AttrTypes: kmsObjectTypes}, items)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("workspace"), ws)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("default_key_id"), strValue(out.DefaultKeyID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("keys"), l)...)
}

// ---------------------------------------------------------------- gezor_aws_cloudformation

func NewAWSCloudFormationDataSource() datasource.DataSource { return &awsCloudFormationDataSource{} }

type awsCloudFormationDataSource struct{ dsBase }

func (d *awsCloudFormationDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_cloudformation"
}

func (d *awsCloudFormationDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The CloudFormation template that creates the IAM role Gezor assumes in your AWS account. Deploy it (for example with `aws_cloudformation_stack`) and pass its `RoleArn` output to `gezor_aws_connection`.",
		Attributes: map[string]schema.Attribute{
			"workspace":          dsWorkspaceAttribute(),
			"external_id":        schema.StringAttribute{Computed: true, Sensitive: true},
			"platform_principal": schema.StringAttribute{Computed: true},
			"template_body":      schema.StringAttribute{Computed: true, MarkdownDescription: "Template JSON."},
		},
	}
}

func (d *awsCloudFormationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var ws types.String
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, pathRoot("workspace"), &ws)...)
	var out struct {
		ExternalID        string `json:"external_id"`
		PlatformPrincipal string `json:"platform_principal"`
		Template          string `json:"template"`
	}
	if err := d.c.Get(ctx, "/api/aws/cloudformation", ws.ValueString(), &out); err != nil {
		apiError(&resp.Diagnostics, "read CloudFormation template", err)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("workspace"), ws)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("external_id"), strValue(out.ExternalID))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("platform_principal"), strValue(out.PlatformPrincipal))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, pathRoot("template_body"), strValue(out.Template))...)
}
