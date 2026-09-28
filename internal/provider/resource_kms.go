package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

type apiKMSKey struct {
	KeyID        string `json:"KeyId"`
	KeyArn       string `json:"KeyArn"`
	AliasName    string `json:"AliasName"`
	Description  string `json:"Description"`
	KeyManager   string `json:"KeyManager"`
	Origin       string `json:"Origin"`
	KeyState     string `json:"KeyState"`
	Enabled      bool   `json:"Enabled"`
	IsDefault    bool   `json:"IsDefault"`
	CreationDate any    `json:"CreationDate"`
}

type apiKMSList struct {
	Keys         []apiKMSKey `json:"Keys"`
	DefaultKeyID string      `json:"DefaultKeyId"`
	KeyManager   string      `json:"KeyManager"`
}

// ---------------------------------------------------------------- gezor_kms_key

var _ resource.ResourceWithImportState = &kmsKeyResource{}

func NewKMSKeyResource() resource.Resource { return &kmsKeyResource{} }

type kmsKeyResource struct{ base }

type kmsKeyModel struct {
	Workspace   types.String `tfsdk:"workspace"`
	ID          types.String `tfsdk:"id"`
	Description types.String `tfsdk:"description"`
	AliasName   types.String `tfsdk:"alias_name"`
	Enabled     types.Bool   `tfsdk:"enabled"`
	KeyMaterial types.String `tfsdk:"key_material"`
	Arn         types.String `tfsdk:"arn"`
	KeyState    types.String `tfsdk:"key_state"`
	Origin      types.String `tfsdk:"origin"`
	IsDefault   types.Bool   `tfsdk:"is_default"`
}

func (r *kmsKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kms_key"
}

func (r *kmsKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A customer managed encryption key. Make it the workspace default with `gezor_kms_default_key`. Destroying it schedules the key for deletion; the default key must be replaced first.",
		Attributes: map[string]schema.Attribute{
			"workspace": workspaceAttribute(),
			"id":        idAttribute("Key id."),
			"description": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("Customer managed key"),
			},
			"alias_name": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString(""),
				MarkdownDescription: "Alias, for example `alias/lake`.",
			},
			"enabled": schema.BoolAttribute{Optional: true, Computed: true, Default: booldefault.StaticBool(true)},
			"key_material": schema.StringAttribute{
				Optional: true, Sensitive: true,
				MarkdownDescription: "Base64 key material to import. Leave out to have Gezor generate the key. Changing it creates a new key.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"arn":        schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"key_state":  schema.StringAttribute{Computed: true},
			"origin":     schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"is_default": schema.BoolAttribute{Computed: true},
		},
	}
}

func (r *kmsKeyResource) fill(m *kmsKeyModel, k apiKMSKey) {
	m.ID = strValue(k.KeyID)
	m.Description = strValue(k.Description)
	m.AliasName = strValue(k.AliasName)
	m.Enabled = types.BoolValue(k.Enabled)
	m.Arn = strValue(k.KeyArn)
	m.KeyState = strValue(k.KeyState)
	m.Origin = strValue(k.Origin)
	m.IsDefault = types.BoolValue(k.IsDefault)
}

func (r *kmsKeyResource) get(ctx context.Context, workspace, id string) (*apiKMSKey, error) {
	var out struct {
		KeyMetadata apiKMSKey `json:"KeyMetadata"`
	}
	if err := r.c.Get(ctx, "/api/kms/keys/"+client.PathEscape(id), workspace, &out); err != nil {
		return nil, err
	}
	return &out.KeyMetadata, nil
}

func (r *kmsKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan kmsKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"Description":         plan.Description.ValueString(),
		"AliasName":           plan.AliasName.ValueString(),
		"SetAsDefault":        false,
		"GenerateKeyMaterial": !known(plan.KeyMaterial) || plan.KeyMaterial.ValueString() == "",
	}
	if known(plan.KeyMaterial) && plan.KeyMaterial.ValueString() != "" {
		body["KeyMaterial"] = plan.KeyMaterial.ValueString()
		body["Origin"] = "IMPORTED"
	}
	var out struct {
		KeyMetadata apiKMSKey `json:"KeyMetadata"`
	}
	ws := plan.Workspace.ValueString()
	if err := r.c.Post(ctx, "/api/kms/keys", ws, body, &out); err != nil {
		apiError(&resp.Diagnostics, "create KMS key", err)
		return
	}
	key := out.KeyMetadata
	if !plan.Enabled.ValueBool() {
		if err := r.c.Patch(ctx, "/api/kms/keys/"+client.PathEscape(key.KeyID), ws, map[string]any{"Enabled": false}, &out); err != nil {
			apiError(&resp.Diagnostics, "disable KMS key", err)
		} else {
			key = out.KeyMetadata
		}
	}
	r.fill(&plan, key)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kmsKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state kmsKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.get(ctx, state.Workspace.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read KMS key", err)
		return
	}
	if key.KeyState == "PendingDeletion" {
		resp.State.RemoveResource(ctx)
		return
	}
	r.fill(&state, *key)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *kmsKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan kmsKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{
		"Description": plan.Description.ValueString(),
		"AliasName":   plan.AliasName.ValueString(),
		"Enabled":     plan.Enabled.ValueBool(),
	}
	var out struct {
		KeyMetadata apiKMSKey `json:"KeyMetadata"`
	}
	if err := r.c.Patch(ctx, "/api/kms/keys/"+client.PathEscape(plan.ID.ValueString()), plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update KMS key", err)
		return
	}
	r.fill(&plan, out.KeyMetadata)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kmsKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state kmsKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.c.Delete(ctx, "/api/kms/keys/"+client.PathEscape(state.ID.ValueString()), state.Workspace.ValueString(), nil)
	if err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "schedule KMS key deletion", err)
	}
}

func (r *kmsKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<key_id>", "id")
}

// ---------------------------------------------------------------- gezor_kms_default_key

var _ resource.ResourceWithImportState = &kmsDefaultKeyResource{}

func NewKMSDefaultKeyResource() resource.Resource { return &kmsDefaultKeyResource{} }

type kmsDefaultKeyResource struct{ base }

type kmsDefaultKeyModel struct {
	Workspace types.String `tfsdk:"workspace"`
	ID        types.String `tfsdk:"id"`
	KeyID     types.String `tfsdk:"key_id"`
}

func (r *kmsDefaultKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kms_default_key"
}

func (r *kmsDefaultKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The workspace's default encryption key. Destroying it only removes it from Terraform state.",
		Attributes: map[string]schema.Attribute{
			"workspace": workspaceAttribute(),
			"id":        idAttribute("Always `default`."),
			"key_id":    schema.StringAttribute{Required: true, MarkdownDescription: "Key id to use by default."},
		},
	}
}

func (r *kmsDefaultKeyResource) set(ctx context.Context, m *kmsDefaultKeyModel) error {
	var out apiKMSList
	if err := r.c.Post(ctx, "/api/kms/keys/default", m.Workspace.ValueString(), map[string]any{"KeyId": m.KeyID.ValueString()}, &out); err != nil {
		return err
	}
	m.ID = strValue("default")
	m.KeyID = strValue(out.DefaultKeyID)
	return nil
}

func (r *kmsDefaultKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan kmsDefaultKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "set default KMS key", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kmsDefaultKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state kmsDefaultKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out apiKMSList
	if err := r.c.Get(ctx, "/api/kms/keys", state.Workspace.ValueString(), &out); err != nil {
		apiError(&resp.Diagnostics, "list KMS keys", err)
		return
	}
	state.ID = strValue("default")
	state.KeyID = strValue(out.DefaultKeyID)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *kmsDefaultKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan kmsDefaultKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.set(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "set default KMS key", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kmsDefaultKeyResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *kmsDefaultKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "default", "id")
}
