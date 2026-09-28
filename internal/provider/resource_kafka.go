package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider/internal/client"
)

// ---------------------------------------------------------------- gezor_kafka_topic

var _ resource.ResourceWithImportState = &kafkaTopicResource{}

func NewKafkaTopicResource() resource.Resource { return &kafkaTopicResource{} }

type kafkaTopicResource struct{ base }

type kafkaTopicModel struct {
	Workspace         types.String `tfsdk:"workspace"`
	ID                types.String `tfsdk:"id"`
	ClusterID         types.String `tfsdk:"cluster_id"`
	Name              types.String `tfsdk:"name"`
	Partitions        types.Int64  `tfsdk:"partitions"`
	ReplicationFactor types.Int64  `tfsdk:"replication_factor"`
	Configs           types.Map    `tfsdk:"configs"`
}

type topicDescription struct {
	Name              string            `json:"name"`
	Exists            bool              `json:"exists"`
	Partitions        int64             `json:"partitions"`
	ReplicationFactor int64             `json:"replicationFactor"`
	Configs           map[string]string `json:"configs"`
}

func (r *kafkaTopicResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_kafka_topic"
}

func (r *kafkaTopicResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A Kafka topic on a cluster with Event Streams (`kafka`) turned on. Changes are applied by the cluster operator, so the cluster must be online. Platform topics (names starting with `_` or `gzr`) cannot be managed.",
		Attributes: map[string]schema.Attribute{
			"workspace":  workspaceAttribute(),
			"id":         idAttribute("`<cluster_id>/<name>`."),
			"cluster_id": requiredReplaceString("Cluster id."),
			"name":       requiredReplaceString("Topic name."),
			"partitions": schema.Int64Attribute{
				Required: true, MarkdownDescription: "Partition count (1-1000). Kafka can only add partitions.",
				Validators: []validator.Int64{int64validator.Between(1, 1000)},
			},
			"replication_factor": schema.Int64Attribute{
				Optional: true, Computed: true,
				MarkdownDescription: "Copies of each partition (1-7). Defaults to the broker setting. Changing it recreates the topic.",
				Validators:          []validator.Int64{int64validator.Between(1, 7)},
				PlanModifiers:       []planmodifier.Int64{int64planmodifier.RequiresReplace(), int64planmodifier.UseStateForUnknown()},
			},
			"configs": schema.MapAttribute{
				Optional: true, ElementType: types.StringType,
				MarkdownDescription: "Topic settings that differ from the broker defaults, for example `retention.ms`. Settings not listed are reset to the default.",
			},
		},
	}
}

func (r *kafkaTopicResource) run(ctx context.Context, m *kafkaTopicModel, op string, body map[string]any) (*client.CommandResult, error) {
	cid := m.ClusterID.ValueString()
	body["topic"] = m.Name.ValueString()
	return r.c.RunCommand(ctx, "POST", clusterPath(cid)+"/kafka/topics/"+op, m.Workspace.ValueString(), body,
		func(rid string) string { return clusterPath(cid) + "/kafka/topic-actions/" + client.PathEscape(rid) })
}

func (r *kafkaTopicResource) describe(ctx context.Context, m *kafkaTopicModel) (*topicDescription, error) {
	res, err := r.run(ctx, m, "describe", map[string]any{})
	if err != nil {
		return nil, err
	}
	var d topicDescription
	if err := json.Unmarshal(res.Result, &d); err != nil {
		return nil, fmt.Errorf("decode topic description: %w", err)
	}
	return &d, nil
}

func (r *kafkaTopicResource) apply(_ context.Context, m *kafkaTopicModel, d *topicDescription) {
	m.ID = strValue(m.ClusterID.ValueString() + "/" + m.Name.ValueString())
	m.Partitions = types.Int64Value(d.Partitions)
	m.ReplicationFactor = types.Int64Value(d.ReplicationFactor)
}

func (r *kafkaTopicResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan kafkaTopicModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := map[string]any{"partitions": plan.Partitions.ValueInt64(), "configs": mapStrings(ctx, plan.Configs, &resp.Diagnostics)}
	if known(plan.ReplicationFactor) {
		body["replication_factor"] = plan.ReplicationFactor.ValueInt64()
	}
	if _, err := r.run(ctx, &plan, "create", body); err != nil {
		apiError(&resp.Diagnostics, "create Kafka topic", err)
		return
	}
	d, err := r.describe(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "read Kafka topic", err)
		return
	}
	r.apply(ctx, &plan, d)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kafkaTopicResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state kafkaTopicModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.describe(ctx, &state)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read Kafka topic", err)
		return
	}
	if !d.Exists {
		resp.State.RemoveResource(ctx)
		return
	}
	r.apply(ctx, &state, d)
	if len(d.Configs) > 0 || !state.Configs.IsNull() {
		state.Configs = stringMap(ctx, d.Configs, &resp.Diagnostics)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *kafkaTopicResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state kafkaTopicModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if plan.Partitions.ValueInt64() < state.Partitions.ValueInt64() {
		resp.Diagnostics.AddAttributeError(pathRoot("partitions"), "Cannot remove partitions",
			fmt.Sprintf("Kafka can only add partitions (current %d). Recreate the topic to lower it.", state.Partitions.ValueInt64()))
		return
	}
	body := map[string]any{"configs": mapStrings(ctx, plan.Configs, &resp.Diagnostics)}
	if plan.Partitions.ValueInt64() != state.Partitions.ValueInt64() {
		body["partitions"] = plan.Partitions.ValueInt64()
	}
	if _, err := r.run(ctx, &plan, "alter", body); err != nil {
		apiError(&resp.Diagnostics, "update Kafka topic", err)
		return
	}
	d, err := r.describe(ctx, &plan)
	if err != nil {
		apiError(&resp.Diagnostics, "read Kafka topic", err)
		return
	}
	r.apply(ctx, &plan, d)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *kafkaTopicResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state kafkaTopicModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if _, err := r.run(ctx, &state, "delete", map[string]any{}); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete Kafka topic", err)
	}
}

func (r *kafkaTopicResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<topic>", "cluster_id", "name")
}

// ---------------------------------------------------------------- gezor_schema_subject

var _ resource.ResourceWithImportState = &schemaSubjectResource{}

func NewSchemaSubjectResource() resource.Resource { return &schemaSubjectResource{} }

type schemaSubjectResource struct{ base }

type schemaSubjectModel struct {
	Workspace       types.String `tfsdk:"workspace"`
	ID              types.String `tfsdk:"id"`
	ClusterID       types.String `tfsdk:"cluster_id"`
	Subject         types.String `tfsdk:"subject"`
	Schema          types.String `tfsdk:"schema"`
	SchemaType      types.String `tfsdk:"schema_type"`
	Compatibility   types.String `tfsdk:"compatibility"`
	PermanentDelete types.Bool   `tfsdk:"permanent_delete"`
	Version         types.Int64  `tfsdk:"version"`
	SchemaID        types.Int64  `tfsdk:"schema_id"`
}

type subjectDescription struct {
	Subject       string `json:"subject"`
	Exists        bool   `json:"exists"`
	Version       int64  `json:"version"`
	ID            int64  `json:"id"`
	SchemaType    string `json:"schemaType"`
	Schema        string `json:"schema"`
	Compatibility string `json:"compatibility"`
}

func (r *schemaSubjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_schema_subject"
}

func (r *schemaSubjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A subject in the cluster's schema registry (Data Schemas). Changing `schema` registers a new version. Applied by the cluster operator, so the cluster must be online.",
		Attributes: map[string]schema.Attribute{
			"workspace":  workspaceAttribute(),
			"id":         idAttribute("`<cluster_id>/<subject>`."),
			"cluster_id": requiredReplaceString("Cluster id."),
			"subject":    requiredReplaceString("Subject name, for example `orders-value`."),
			"schema":     schema.StringAttribute{Required: true, MarkdownDescription: "Schema text. For Avro and JSON Schema, `jsonencode(...)` works well."},
			"schema_type": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("AVRO"),
				MarkdownDescription: "`AVRO`, `JSON` or `PROTOBUF`.",
			},
			"compatibility": optStr("Subject compatibility, for example `BACKWARD` or `FULL_TRANSITIVE`. Defaults to the registry setting."),
			"permanent_delete": schema.BoolAttribute{
				Optional: true, Computed: true, Default: booldefault.StaticBool(false),
				MarkdownDescription: "Hard-delete every version on destroy instead of a soft delete.",
			},
			"version":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Latest version number."},
			"schema_id": schema.Int64Attribute{Computed: true, MarkdownDescription: "Registry id of the latest schema."},
		},
	}
}

func (r *schemaSubjectResource) subjectPath(m *schemaSubjectModel) string {
	return clusterPath(m.ClusterID.ValueString()) + "/schema-registry/subjects/" + client.PathEscape(m.Subject.ValueString())
}

func (r *schemaSubjectResource) run(ctx context.Context, m *schemaSubjectModel, method, p string, body any) (*client.CommandResult, error) {
	cid := m.ClusterID.ValueString()
	return r.c.RunCommand(ctx, method, p, m.Workspace.ValueString(), body,
		func(rid string) string {
			return clusterPath(cid) + "/schema-registry/actions/" + client.PathEscape(rid)
		})
}

func (r *schemaSubjectResource) describe(ctx context.Context, m *schemaSubjectModel) (*subjectDescription, error) {
	res, err := r.run(ctx, m, "POST", r.subjectPath(m)+"/describe", map[string]any{})
	if err != nil {
		return nil, err
	}
	var d subjectDescription
	if err := json.Unmarshal(res.Result, &d); err != nil {
		return nil, fmt.Errorf("decode subject description: %w", err)
	}
	return &d, nil
}

// sameSchema compares schemas ignoring JSON formatting.
func sameSchema(a, b string) bool {
	var ja, jb any
	if json.Unmarshal([]byte(a), &ja) == nil && json.Unmarshal([]byte(b), &jb) == nil {
		return encodeJSON(ja) == encodeJSON(jb)
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func (r *schemaSubjectResource) apply(m *schemaSubjectModel, d *subjectDescription) {
	m.ID = strValue(m.ClusterID.ValueString() + "/" + m.Subject.ValueString())
	if !known(m.Schema) || !sameSchema(m.Schema.ValueString(), d.Schema) {
		m.Schema = strValue(d.Schema)
	}
	if d.SchemaType != "" {
		m.SchemaType = strValue(d.SchemaType)
	}
	m.Compatibility = strValue(d.Compatibility)
	m.Version = types.Int64Value(d.Version)
	m.SchemaID = types.Int64Value(d.ID)
}

func (r *schemaSubjectResource) save(ctx context.Context, plan *schemaSubjectModel, prior *schemaSubjectModel) error {
	if prior == nil || !sameSchema(plan.Schema.ValueString(), prior.Schema.ValueString()) || plan.SchemaType.ValueString() != prior.SchemaType.ValueString() {
		body := map[string]any{"schema": plan.Schema.ValueString(), "schemaType": plan.SchemaType.ValueString()}
		if _, err := r.run(ctx, plan, "POST", r.subjectPath(plan)+"/versions", body); err != nil {
			return fmt.Errorf("register schema: %w", err)
		}
	}
	if known(plan.Compatibility) && (prior == nil || plan.Compatibility.ValueString() != prior.Compatibility.ValueString()) {
		body := map[string]any{"compatibility": plan.Compatibility.ValueString()}
		if _, err := r.run(ctx, plan, "PUT", r.subjectPath(plan)+"/config", body); err != nil {
			return fmt.Errorf("set compatibility: %w", err)
		}
	}
	d, err := r.describe(ctx, plan)
	if err != nil {
		return err
	}
	r.apply(plan, d)
	return nil
}

func (r *schemaSubjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan schemaSubjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.save(ctx, &plan, nil); err != nil {
		apiError(&resp.Diagnostics, "create schema subject", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *schemaSubjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state schemaSubjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	d, err := r.describe(ctx, &state)
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read schema subject", err)
		return
	}
	if !d.Exists {
		resp.State.RemoveResource(ctx)
		return
	}
	r.apply(&state, d)
	if state.PermanentDelete.IsNull() {
		state.PermanentDelete = types.BoolValue(false)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *schemaSubjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state schemaSubjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.save(ctx, &plan, &state); err != nil {
		apiError(&resp.Diagnostics, "update schema subject", err)
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *schemaSubjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state schemaSubjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	p := r.subjectPath(&state) + "?permanent=" + url.QueryEscape(fmt.Sprint(state.PermanentDelete.ValueBool()))
	if _, err := r.run(ctx, &state, "DELETE", p, nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete schema subject", err)
	}
}

func (r *schemaSubjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<subject>", "cluster_id", "subject")
}
