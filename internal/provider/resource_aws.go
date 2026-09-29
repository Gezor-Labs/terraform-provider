package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

type awsConnectionSnapshot struct {
	Connection *struct {
		AccountID             *string `json:"account_id"`
		RoleArn               *string `json:"role_arn"`
		ExternalID            string  `json:"external_id"`
		Status                string  `json:"status"`
		AuthMode              string  `json:"auth_mode"`
		CredentialsConfigured bool    `json:"credentials_configured"`
	} `json:"connection"`
	Lake *struct {
		BronzeURI string  `json:"bronze_uri"`
		SilverURI string  `json:"silver_uri"`
		GoldURI   string  `json:"gold_uri"`
		KMSKeyArn *string `json:"kms_key_arn"`
		Prefix    string  `json:"prefix"`
		Status    string  `json:"status"`
	} `json:"lake"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---------------------------------------------------------------- gezor_aws_connection

var _ resource.ResourceWithImportState = &awsConnectionResource{}

func NewAWSConnectionResource() resource.Resource { return &awsConnectionResource{} }

type awsConnectionResource struct{ base }

type awsConnectionModel struct {
	Workspace       types.String `tfsdk:"workspace"`
	ID              types.String `tfsdk:"id"`
	AuthMode        types.String `tfsdk:"auth_mode"`
	RoleArn         types.String `tfsdk:"role_arn"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	TestPermissions types.Bool   `tfsdk:"test_permissions"`
	AccountID       types.String `tfsdk:"account_id"`
	ExternalID      types.String `tfsdk:"external_id"`
	Status          types.String `tfsdk:"status"`
}

func (r *awsConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_connection"
}

func (r *awsConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	mode := schema.StringAttribute{
		Optional: true, Computed: true, Default: stringdefault.StaticString("assume_role"),
		MarkdownDescription: "`assume_role` (recommended; deploy the template from `gezor_aws_cloudformation` first) or `credentials`.",
		Validators:          []validator.String{stringvalidator.OneOf("assume_role", "credentials")},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "Links the workspace to your AWS account. Gezor validates access on every create and update. One per workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace":         workspaceAttribute(),
			"id":                idAttribute("Always `aws`."),
			"auth_mode":         mode,
			"role_arn":          schema.StringAttribute{Optional: true, MarkdownDescription: "IAM role ARN for `assume_role`."},
			"access_key_id":     schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Access key id for `credentials`. Write-only."},
			"secret_access_key": schema.StringAttribute{Optional: true, Sensitive: true, MarkdownDescription: "Secret access key for `credentials`. Write-only."},
			"test_permissions": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(true),
				MarkdownDescription: "Also check the permissions Gezor needs.",
			},
			"account_id":  schema.StringAttribute{Computed: true},
			"external_id": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"status":      schema.StringAttribute{Computed: true},
		},
	}
}

func (r *awsConnectionResource) validate(ctx context.Context, m *awsConnectionModel) error {
	body := map[string]any{"auth_mode": m.AuthMode.ValueString(), "test_permissions": m.TestPermissions.ValueBool()}
	if known(m.RoleArn) {
		body["role_arn"] = m.RoleArn.ValueString()
	}
	if known(m.AccessKeyID) {
		body["access_key_id"] = m.AccessKeyID.ValueString()
	}
	if known(m.SecretAccessKey) {
		body["secret_access_key"] = m.SecretAccessKey.ValueString()
	}
	return r.c.Post(ctx, "/api/aws/connection/validate", m.Workspace.ValueString(), body, nil)
}

// read returns false when the workspace has no linked AWS identity.
func (r *awsConnectionResource) read(ctx context.Context, m *awsConnectionModel) (bool, error) {
	var snap awsConnectionSnapshot
	if err := r.c.Get(ctx, "/api/aws/connection", m.Workspace.ValueString(), &snap); err != nil {
		return false, err
	}
	conn := snap.Connection
	if conn == nil || (deref(conn.RoleArn) == "" && !conn.CredentialsConfigured) {
		return false, nil
	}
	m.ID = strValue("aws")
	m.AuthMode = strValue(conn.AuthMode)
	if conn.AuthMode == "assume_role" {
		m.RoleArn = optString(deref(conn.RoleArn))
	}
	m.AccountID = strValue(deref(conn.AccountID))
	m.ExternalID = strValue(conn.ExternalID)
	m.Status = strValue(conn.Status)
	return true, nil
}

func (r *awsConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsConnectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.validate(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "link AWS", err)
		return
	}
	if _, err := r.read(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "read AWS connection", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &state)
	if err != nil {
		apiError(&resp.Diagnostics, "read AWS connection", err)
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *awsConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan awsConnectionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.validate(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "link AWS", err)
		return
	}
	if _, err := r.read(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "read AWS connection", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsConnectionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, "/api/aws/connection", state.Workspace.ValueString(), nil); err != nil {
		apiError(&resp.Diagnostics, "unlink AWS", err)
	}
}

func (r *awsConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "aws", "id")
}

// ---------------------------------------------------------------- gezor_aws_lake

var _ resource.ResourceWithImportState = &awsLakeResource{}

func NewAWSLakeResource() resource.Resource { return &awsLakeResource{} }

type awsLakeResource struct{ base }

type awsLakeModel struct {
	Workspace types.String `tfsdk:"workspace"`
	ID        types.String `tfsdk:"id"`
	BronzeURI types.String `tfsdk:"bronze_uri"`
	SilverURI types.String `tfsdk:"silver_uri"`
	GoldURI   types.String `tfsdk:"gold_uri"`
	KMSKeyArn types.String `tfsdk:"kms_key_arn"`
	Prefix    types.String `tfsdk:"prefix"`
	Status    types.String `tfsdk:"status"`
}

func (r *awsLakeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_lake"
}

func (r *awsLakeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The workspace's data lake buckets in your AWS account. Needs a validated `gezor_aws_connection`.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("Always `lake`."),
			"bronze_uri":  schema.StringAttribute{Required: true, MarkdownDescription: "`s3://` URI for raw data."},
			"silver_uri":  schema.StringAttribute{Required: true, MarkdownDescription: "`s3://` URI for cleaned data."},
			"gold_uri":    schema.StringAttribute{Required: true, MarkdownDescription: "`s3://` URI for curated data."},
			"kms_key_arn": schema.StringAttribute{Optional: true, MarkdownDescription: "AWS KMS key used for the buckets."},
			"prefix": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("gzr/"),
				MarkdownDescription: "Object key prefix inside each bucket.",
			},
			"status": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *awsLakeResource) read(ctx context.Context, m *awsLakeModel) (bool, error) {
	var snap awsConnectionSnapshot
	if err := r.c.Get(ctx, "/api/aws/connection", m.Workspace.ValueString(), &snap); err != nil {
		return false, err
	}
	if snap.Lake == nil {
		return false, nil
	}
	m.ID = strValue("lake")
	m.BronzeURI = strValue(snap.Lake.BronzeURI)
	m.SilverURI = strValue(snap.Lake.SilverURI)
	m.GoldURI = strValue(snap.Lake.GoldURI)
	m.KMSKeyArn = optString(deref(snap.Lake.KMSKeyArn))
	m.Prefix = strValue(snap.Lake.Prefix)
	m.Status = strValue(snap.Lake.Status)
	return true, nil
}

func (r *awsLakeResource) save(ctx context.Context, m *awsLakeModel) error {
	body := map[string]any{
		"bronze_uri": m.BronzeURI.ValueString(), "silver_uri": m.SilverURI.ValueString(), "gold_uri": m.GoldURI.ValueString(),
		"prefix": m.Prefix.ValueString(),
	}
	if known(m.KMSKeyArn) {
		body["kms_key_arn"] = m.KMSKeyArn.ValueString()
	}
	if err := r.c.Post(ctx, "/api/aws/lake/validate", m.Workspace.ValueString(), body, nil); err != nil {
		return err
	}
	_, err := r.read(ctx, m)
	return err
}

func (r *awsLakeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsLakeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.save(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "register lake", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsLakeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsLakeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.read(ctx, &state)
	if err != nil {
		apiError(&resp.Diagnostics, "read lake", err)
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *awsLakeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan awsLakeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.save(ctx, &plan); err != nil {
		apiError(&resp.Diagnostics, "update lake", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsLakeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state awsLakeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, "/api/aws/lake", state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "unlink lake", err)
	}
}

func (r *awsLakeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "lake", "id")
}

// ---------------------------------------------------------------- gezor_aws_crypto_mode

var _ resource.ResourceWithImportState = &awsCryptoModeResource{}

func NewAWSCryptoModeResource() resource.Resource { return &awsCryptoModeResource{} }

type awsCryptoModeResource struct{ base }

type awsCryptoModeModel struct {
	Workspace      types.String `tfsdk:"workspace"`
	ID             types.String `tfsdk:"id"`
	Mode           types.String `tfsdk:"mode"`
	KeyMaterial    types.String `tfsdk:"key_material"`
	KeyFingerprint types.String `tfsdk:"customer_key_fingerprint"`
}

type apiCrypto struct {
	KeyMode                string  `json:"key_mode"`
	CustomerKeyConfigured  bool    `json:"customer_key_configured"`
	CustomerKeyFingerprint *string `json:"customer_key_fingerprint"`
}

func (r *awsCryptoModeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_aws_crypto_mode"
}

func (r *awsCryptoModeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Chooses who manages the key that protects the workspace's data key: Gezor (`platform`) or you (`customer`). Destroying it only removes it from Terraform state and keeps the current mode. `customer` mode creates a key and makes it the default, so use either this or `gezor_kms_key` with `gezor_kms_default_key`, not both.",
		Attributes: map[string]schema.Attribute{
			"workspace": workspaceAttribute(),
			"id":        idAttribute("Always `crypto`."),
			"mode": schema.StringAttribute{
				Required: true, Validators: []validator.String{stringvalidator.OneOf("platform", "customer")},
			},
			"key_material": schema.StringAttribute{
				Optional: true, Sensitive: true,
				MarkdownDescription: "Base64 customer key to import in `customer` mode. Leave out to have Gezor generate one. Write-only.",
			},
			"customer_key_fingerprint": schema.StringAttribute{Computed: true},
		},
	}
}

func (r *awsCryptoModeResource) fill(m *awsCryptoModeModel, c apiCrypto) {
	m.ID = strValue("crypto")
	m.Mode = strValue(c.KeyMode)
	m.KeyFingerprint = strValue(deref(c.CustomerKeyFingerprint))
}

func (r *awsCryptoModeResource) save(ctx context.Context, m *awsCryptoModeModel, prior *awsCryptoModeModel) error {
	ws := m.Workspace.ValueString()
	var out apiCrypto
	if m.Mode.ValueString() == "platform" {
		if err := r.c.Post(ctx, "/api/aws/crypto/platform-keys", ws, nil, &out); err != nil {
			return err
		}
	} else {
		material := m.KeyMaterial.ValueString()
		unchanged := prior != nil && prior.Mode.ValueString() == "customer" && prior.KeyMaterial.ValueString() == material
		if !unchanged {
			body := map[string]any{"generate": material == ""}
			if material != "" {
				body["key_material"] = material
			}
			if err := r.c.Post(ctx, "/api/aws/crypto/customer-keys", ws, body, &out); err != nil {
				return err
			}
		}
	}
	if err := r.c.Get(ctx, "/api/aws/crypto", ws, &out); err != nil {
		return err
	}
	r.fill(m, out)
	return nil
}

func (r *awsCryptoModeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan awsCryptoModeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var cur apiCrypto
	if err := r.c.Get(ctx, "/api/aws/crypto", plan.Workspace.ValueString(), &cur); err != nil {
		apiError(&resp.Diagnostics, "read key mode", err)
		return
	}
	var prior *awsCryptoModeModel
	if cur.KeyMode == plan.Mode.ValueString() && (cur.KeyMode == "platform" || (cur.CustomerKeyConfigured && !known(plan.KeyMaterial))) {
		prior = &awsCryptoModeModel{Mode: plan.Mode, KeyMaterial: plan.KeyMaterial}
	}
	if err := r.save(ctx, &plan, prior); err != nil {
		apiError(&resp.Diagnostics, "set key mode", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsCryptoModeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state awsCryptoModeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out apiCrypto
	if err := r.c.Get(ctx, "/api/aws/crypto", state.Workspace.ValueString(), &out); err != nil {
		apiError(&resp.Diagnostics, "read key mode", err)
		return
	}
	r.fill(&state, out)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *awsCryptoModeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state awsCryptoModeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.save(ctx, &plan, &state); err != nil {
		apiError(&resp.Diagnostics, "set key mode", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *awsCryptoModeResource) Delete(_ context.Context, _ resource.DeleteRequest, _ *resource.DeleteResponse) {
}

func (r *awsCryptoModeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "crypto", "id")
}
