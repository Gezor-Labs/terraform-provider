package provider

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

func cdcInstancePath(clusterID, instanceID string) string {
	return clusterPath(clusterID) + "/cdc/instances/" + client.PathEscape(instanceID)
}

// ---------------------------------------------------------------- gezor_cdc_instance

var _ resource.ResourceWithImportState = &cdcInstanceResource{}

func NewCDCInstanceResource() resource.Resource { return &cdcInstanceResource{} }

type cdcInstanceResource struct{ base }

type cdcInstanceModel struct {
	Workspace  types.String `tfsdk:"workspace"`
	ID         types.String `tfsdk:"id"`
	ClusterID  types.String `tfsdk:"cluster_id"`
	InstanceID types.String `tfsdk:"instance_id"`
	Name       types.String `tfsdk:"name"`
	Enabled    types.Bool   `tfsdk:"enabled"`
	Replicas   types.Int64  `tfsdk:"replicas"`
	GroupID    types.String `tfsdk:"group_id"`
	HeapOpts   types.String `tfsdk:"heap_opts"`
	Image      types.String `tfsdk:"image"`
	ExtraEnv   types.Map    `tfsdk:"extra_env"`
	Namespace  types.String `tfsdk:"namespace"`
}

func (r *cdcInstanceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdc_instance"
}

func (r *cdcInstanceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	iid := requiredReplaceString("Instance id such as `cdc-2`. Turning on Change Capture (`connect`) creates `cdc-1`; import it to manage it here.")
	iid.Validators = []validator.String{stringvalidator.RegexMatches(regexp.MustCompile(`^cdc-[0-9]+$`), "must look like cdc-2")}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Change Capture (Kafka Connect) worker group on a cluster. Needs the `connect` app. Remove its connectors before destroying it.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("`<cluster_id>/<instance_id>`."),
			"cluster_id":  requiredReplaceString("Cluster id."),
			"instance_id": iid,
			"name":        optStr("Deployment name."),
			"enabled":     optBool(""),
			"replicas":    optInt("Worker replicas (1-20)."),
			"group_id":    optStr("Kafka Connect group id."),
			"heap_opts":   optStr("JVM heap options."),
			"image":       optStr("Worker image override."),
			"extra_env": schema.MapAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
			"namespace": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *cdcInstanceResource) fill(ctx context.Context, m *cdcInstanceModel, inst map[string]any, diags *diag.Diagnostics) {
	m.ID = strValue(m.ClusterID.ValueString() + "/" + m.InstanceID.ValueString())
	m.Name = strValue(asString(inst["name"]))
	m.Enabled = types.BoolValue(asBool(inst["enabled"]))
	m.Replicas = types.Int64Value(asInt(inst["replicas"]))
	m.GroupID = strValue(asString(inst["groupId"]))
	m.HeapOpts = strValue(asString(inst["heapOpts"]))
	m.Image = strValue(asString(inst["image"]))
	env := map[string]string{}
	for k, v := range asMap(inst["extraEnv"]) {
		env[k] = asString(v)
	}
	m.ExtraEnv = stringMap(ctx, env, diags)
	m.Namespace = strValue(asString(inst["namespace"]))
}

func (r *cdcInstanceResource) body(ctx context.Context, m *cdcInstanceModel, diags *diag.Diagnostics) map[string]any {
	body := map[string]any{}
	for key, v := range map[string]types.String{"name": m.Name, "groupId": m.GroupID, "heapOpts": m.HeapOpts, "image": m.Image} {
		if known(v) {
			body[key] = v.ValueString()
		}
	}
	putBool(body, "enabled", m.Enabled)
	putInt(body, "replicas", m.Replicas)
	if known(m.ExtraEnv) {
		body["extraEnv"] = mapStrings(ctx, m.ExtraEnv, diags)
	}
	return body
}

func (r *cdcInstanceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cdcInstanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.body(ctx, &plan, &resp.Diagnostics)
	body["id"] = plan.InstanceID.ValueString()
	var out struct {
		Instance map[string]any `json:"instance"`
	}
	if err := withClusterLock(plan.ClusterID.ValueString(), func() error {
		return r.c.Post(ctx, clusterPath(plan.ClusterID.ValueString())+"/cdc/instances", plan.Workspace.ValueString(), body, &out)
	}); err != nil {
		apiError(&resp.Diagnostics, "create CDC instance", err)
		return
	}
	r.fill(ctx, &plan, out.Instance, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *cdcInstanceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cdcInstanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Instance map[string]any `json:"instance"`
	}
	if err := r.c.Get(ctx, cdcInstancePath(state.ClusterID.ValueString(), state.InstanceID.ValueString()), state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read CDC instance", err)
		return
	}
	r.fill(ctx, &state, out.Instance, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *cdcInstanceResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan cdcInstanceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Instance map[string]any `json:"instance"`
	}
	p := cdcInstancePath(plan.ClusterID.ValueString(), plan.InstanceID.ValueString())
	body := r.body(ctx, &plan, &resp.Diagnostics)
	if err := withClusterLock(plan.ClusterID.ValueString(), func() error {
		return r.c.Put(ctx, p, plan.Workspace.ValueString(), body, &out)
	}); err != nil {
		apiError(&resp.Diagnostics, "update CDC instance", err)
		return
	}
	r.fill(ctx, &plan, out.Instance, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *cdcInstanceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cdcInstanceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := cdcInstancePath(state.ClusterID.ValueString(), state.InstanceID.ValueString())
	if err := withClusterLock(state.ClusterID.ValueString(), func() error {
		return r.c.Delete(ctx, p, state.Workspace.ValueString(), nil)
	}); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete CDC instance", err)
	}
}

func (r *cdcInstanceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<instance_id>", "cluster_id", "instance_id")
}

// ---------------------------------------------------------------- gezor_cdc_connector

var _ resource.ResourceWithImportState = &cdcConnectorResource{}

func NewCDCConnectorResource() resource.Resource { return &cdcConnectorResource{} }

type cdcConnectorResource struct{ base }

type cdcConnectorModel struct {
	Workspace             types.String `tfsdk:"workspace"`
	ID                    types.String `tfsdk:"id"`
	ClusterID             types.String `tfsdk:"cluster_id"`
	InstanceID            types.String `tfsdk:"instance_id"`
	Name                  types.String `tfsdk:"name"`
	DatabaseType          types.String `tfsdk:"database_type"`
	DatabaseHostname      types.String `tfsdk:"database_hostname"`
	DatabasePort          types.Int64  `tfsdk:"database_port"`
	DatabaseUser          types.String `tfsdk:"database_user"`
	DatabaseDbname        types.String `tfsdk:"database_dbname"`
	DatabasePassword      types.String `tfsdk:"database_password"`
	TableIncludeList      types.String `tfsdk:"table_include_list"`
	SchemaIncludeList     types.String `tfsdk:"schema_include_list"`
	DatabaseIncludeList   types.String `tfsdk:"database_include_list"`
	CollectionIncludeList types.String `tfsdk:"collection_include_list"`
	SnapshotMode          types.String `tfsdk:"snapshot_mode"`
	SlotName              types.String `tfsdk:"slot_name"`
	PublicationName       types.String `tfsdk:"publication_name"`
	SSLMode               types.String `tfsdk:"ssl_mode"`
	TasksMax              types.Int64  `tfsdk:"tasks_max"`
	DesiredRuntime        types.String `tfsdk:"desired_runtime"`
	AccessVia             types.String `tfsdk:"access_via"`
	AccessConnectorID     types.String `tfsdk:"access_connector_id"`
	AccessEndpointID      types.String `tfsdk:"access_endpoint_id"`
	AdditionalConfig      types.Map    `tfsdk:"additional_config"`
	TopicPrefix           types.String `tfsdk:"topic_prefix"`
}

func (r *cdcConnectorResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cdc_connector"
}

func (r *cdcConnectorResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	dbType := requiredReplaceString("`postgresql`, `mysql`, `mariadb`, `sqlserver`, `mongodb` or `oracle`.")
	dbType.Validators = []validator.String{stringvalidator.OneOf("postgresql", "mysql", "mariadb", "sqlserver", "mongodb", "oracle")}
	access := optStr("`direct` (the cluster reaches the database) or `connector` (through a Secure Access connector).")
	access.Validators = []validator.String{stringvalidator.OneOf("direct", "connector")}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Debezium change data capture connector in a CDC instance. Topics are named `<topic_prefix>.<schema>.<table>`.",
		Attributes: map[string]schema.Attribute{
			"workspace":         workspaceAttribute(),
			"id":                idAttribute("`<cluster_id>/<instance_id>/<name>`."),
			"cluster_id":        requiredReplaceString("Cluster id."),
			"instance_id":       requiredReplaceString("CDC instance id, for example `cdc-1`."),
			"name":              requiredReplaceString("Connector name (2-63 characters)."),
			"database_type":     dbType,
			"database_hostname": optStr("Database host. Filled in automatically when `access_via` is `connector`."),
			"database_port":     optInt("Database port. Defaults to the database type's port."),
			"database_user":     schema.StringAttribute{Required: true},
			"database_dbname":   optStr("Database name."),
			"database_password": schema.StringAttribute{
				Optional: true, Sensitive: true,
				MarkdownDescription: "Password, stored as a Kubernetes secret on the cluster. Required when creating. Write-only.",
			},
			"table_include_list":      optStr("Comma-separated `schema.table` list."),
			"schema_include_list":     optStr("Comma-separated schemas."),
			"database_include_list":   optStr("Comma-separated databases (MySQL, MariaDB, SQL Server, MongoDB)."),
			"collection_include_list": optStr("Comma-separated collections (MongoDB)."),
			"snapshot_mode":           optStr("Debezium snapshot mode, for example `initial`."),
			"slot_name":               optStr("Replication slot (PostgreSQL)."),
			"publication_name":        optStr("Publication (PostgreSQL)."),
			"ssl_mode":                optStr("Database SSL mode."),
			"tasks_max":               optInt("Connector tasks (1-64)."),
			"desired_runtime":         optStr("`running` or `paused`."),
			"access_via":              access,
			"access_connector_id":     schema.StringAttribute{Optional: true, MarkdownDescription: "Secure Access connector id when `access_via` is `connector`."},
			"access_endpoint_id":      schema.StringAttribute{Optional: true, MarkdownDescription: "Secure Access endpoint id when `access_via` is `connector`."},
			"additional_config": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Extra Debezium settings.",
			},
			"topic_prefix": schema.StringAttribute{Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		},
	}
}

func (r *cdcConnectorResource) connectorPath(m *cdcConnectorModel) string {
	return cdcInstancePath(m.ClusterID.ValueString(), m.InstanceID.ValueString()) + "/connectors"
}

func (r *cdcConnectorResource) body(ctx context.Context, m *cdcConnectorModel, diags *diag.Diagnostics) map[string]any {
	body := map[string]any{"name": m.Name.ValueString(), "database_type": m.DatabaseType.ValueString()}
	for key, v := range map[string]types.String{
		"database_hostname": m.DatabaseHostname, "database_user": m.DatabaseUser, "database_dbname": m.DatabaseDbname,
		"database_password": m.DatabasePassword, "table_include_list": m.TableIncludeList, "schema_include_list": m.SchemaIncludeList,
		"database_include_list": m.DatabaseIncludeList, "collection_include_list": m.CollectionIncludeList,
		"snapshot_mode": m.SnapshotMode, "slot_name": m.SlotName, "publication_name": m.PublicationName, "ssl_mode": m.SSLMode,
		"desired_runtime": m.DesiredRuntime, "access_via": m.AccessVia, "access_connector_id": m.AccessConnectorID,
		"access_endpoint_id": m.AccessEndpointID,
	} {
		if known(v) {
			body[key] = v.ValueString()
		}
	}
	putInt(body, "database_port", m.DatabasePort)
	putInt(body, "tasks_max", m.TasksMax)
	if known(m.AdditionalConfig) {
		body["additional_config"] = mapStrings(ctx, m.AdditionalConfig, diags)
	}
	return body
}

func (r *cdcConnectorResource) fill(ctx context.Context, m *cdcConnectorModel, c map[string]any, diags *diag.Diagnostics) {
	m.ID = strValue(m.ClusterID.ValueString() + "/" + m.InstanceID.ValueString() + "/" + asString(c["name"]))
	m.Name = strValue(asString(c["name"]))
	m.DatabaseType = strValue(asString(c["databaseType"]))
	m.AccessVia = strValue(asString(c["accessVia"]))
	if m.AccessVia.ValueString() == "" {
		m.AccessVia = strValue("direct")
	}
	if m.AccessVia.ValueString() != "connector" || !known(m.DatabaseHostname) {
		m.DatabaseHostname = strValue(asString(c["databaseHostname"]))
	}
	if m.AccessVia.ValueString() != "connector" || !known(m.DatabasePort) {
		m.DatabasePort = types.Int64Value(asInt(c["databasePort"]))
	}
	m.DatabaseUser = strValue(asString(c["databaseUser"]))
	m.DatabaseDbname = strValue(asString(c["databaseDbname"]))
	m.TableIncludeList = strValue(asString(c["tableIncludeList"]))
	m.SchemaIncludeList = strValue(asString(c["schemaIncludeList"]))
	m.DatabaseIncludeList = strValue(asString(c["databaseIncludeList"]))
	m.CollectionIncludeList = strValue(asString(c["collectionIncludeList"]))
	m.SnapshotMode = strValue(asString(c["snapshotMode"]))
	m.SlotName = strValue(asString(c["slotName"]))
	m.PublicationName = strValue(asString(c["publicationName"]))
	m.SSLMode = strValue(asString(c["sslMode"]))
	m.TasksMax = types.Int64Value(asInt(c["tasksMax"]))
	m.DesiredRuntime = strValue(asString(c["desiredRuntime"]))
	m.AccessConnectorID = optString(asString(c["accessConnectorId"]))
	m.AccessEndpointID = optString(asString(c["accessEndpointId"]))
	extra := map[string]string{}
	for k, v := range asMap(c["additionalConfig"]) {
		extra[k] = asString(v)
	}
	if len(extra) > 0 || !m.AdditionalConfig.IsNull() {
		m.AdditionalConfig = stringMap(ctx, extra, diags)
	}
	m.TopicPrefix = strValue(asString(c["topicPrefix"]))
}

func (r *cdcConnectorResource) get(ctx context.Context, m *cdcConnectorModel) (map[string]any, error) {
	var out struct {
		Connector map[string]any `json:"connector"`
	}
	if err := r.c.Get(ctx, r.connectorPath(m)+"/"+client.PathEscape(m.Name.ValueString()), m.Workspace.ValueString(), &out); err != nil {
		return nil, err
	}
	return out.Connector, nil
}

func (r *cdcConnectorResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cdcConnectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.body(ctx, &plan, &resp.Diagnostics)
	if err := withClusterLock(plan.ClusterID.ValueString(), func() error {
		return r.c.Post(ctx, r.connectorPath(&plan), plan.Workspace.ValueString(), body, nil)
	}); err != nil {
		apiError(&resp.Diagnostics, "create CDC connector", err)
		return
	}
	c, err := r.get(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "read CDC connector", err)
		return
	}
	r.fill(ctx, &plan, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *cdcConnectorResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cdcConnectorModel
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
		apiError(&resp.Diagnostics, "read CDC connector", err)
		return
	}
	r.fill(ctx, &state, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *cdcConnectorResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan cdcConnectorModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := r.connectorPath(&plan) + "/" + client.PathEscape(plan.Name.ValueString())
	body := r.body(ctx, &plan, &resp.Diagnostics)
	if err := withClusterLock(plan.ClusterID.ValueString(), func() error {
		return r.c.Put(ctx, p, plan.Workspace.ValueString(), body, nil)
	}); err != nil {
		apiError(&resp.Diagnostics, "update CDC connector", err)
		return
	}
	c, err := r.get(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "read CDC connector", err)
		return
	}
	r.fill(ctx, &plan, c, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *cdcConnectorResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cdcConnectorModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := r.connectorPath(&state) + "/" + client.PathEscape(state.Name.ValueString())
	if err := withClusterLock(state.ClusterID.ValueString(), func() error {
		return r.c.Delete(ctx, p, state.Workspace.ValueString(), nil)
	}); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete CDC connector", err)
	}
}

func (r *cdcConnectorResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<instance_id>/<name>", "cluster_id", "instance_id", "name")
}
