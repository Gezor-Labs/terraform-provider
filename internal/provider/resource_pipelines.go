package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
)

// ---------------------------------------------------------------- gezor_dbt_project

var _ resource.ResourceWithImportState = &dbtProjectResource{}

func NewDbtProjectResource() resource.Resource { return &dbtProjectResource{} }

type dbtProjectResource struct{ base }

type dbtProjectModel struct {
	Workspace       types.String `tfsdk:"workspace"`
	ID              types.String `tfsdk:"id"`
	ClusterID       types.String `tfsdk:"cluster_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Template        types.String `tfsdk:"template"`
	Target          types.String `tfsdk:"target"`
	Threads         types.Int64  `tfsdk:"threads"`
	Catalog         types.String `tfsdk:"catalog"`
	SchemaName      types.String `tfsdk:"schema_name"`
	Materialization types.String `tfsdk:"materialization"`
	TrinoHost       types.String `tfsdk:"trino_host"`
	TrinoPort       types.Int64  `tfsdk:"trino_port"`
	TrinoUser       types.String `tfsdk:"trino_user"`
	HTTPScheme      types.String `tfsdk:"http_scheme"`
	Selector        types.String `tfsdk:"selector"`
	Command         types.String `tfsdk:"command"`
	Files           types.Map    `tfsdk:"files"`
}

func (r *dbtProjectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dbt_project"
}

func (r *dbtProjectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A dbt project that runs against the cluster's Data Explorer (Trino). Needs the Pipelines product in the workspace.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("Project id."),
			"cluster_id":  requiredReplaceString("Cluster id."),
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"template": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("cdc_customers"),
				MarkdownDescription: "Starter template used when the project is created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"target":          optStr("dbt target."),
			"threads":         optInt("dbt threads (1-16)."),
			"catalog":         optStr("Trino catalog."),
			"schema_name":     optStr("Target schema."),
			"materialization": optStr("Default materialization, for example `table` or `view`."),
			"trino_host":      optStr("Trino host. Empty uses the cluster's Data Explorer."),
			"trino_port":      optInt(""),
			"trino_user":      optStr(""),
			"http_scheme":     optStr("`http` or `https`."),
			"selector":        optStr("Default model selector."),
			"command":         optStr("Default dbt command, for example `build`."),
			"files": schema.MapAttribute{
				Optional: true, Computed: true, ElementType: types.StringType,
				MarkdownDescription: "Project files by path (for example `models/orders.sql`). When set, this is the full file list.",
				PlanModifiers:       []planmodifier.Map{mapplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func dbtProjectPath(clusterID, id string) string {
	return clusterPath(clusterID) + "/dbt/projects/" + client.PathEscape(id)
}

func (r *dbtProjectResource) fill(ctx context.Context, m *dbtProjectModel, p map[string]any, diags *diag.Diagnostics) {
	m.ID = strValue(asString(p["id"]))
	m.ClusterID = strValue(asString(p["cluster_id"]))
	m.Name = strValue(asString(p["name"]))
	m.Description = strValue(asString(p["description"]))
	m.Target = strValue(asString(p["target"]))
	m.Threads = types.Int64Value(asInt(p["threads"]))
	m.Catalog = strValue(asString(p["catalog"]))
	m.SchemaName = strValue(asString(p["schema_name"]))
	m.Materialization = strValue(asString(p["materialization"]))
	m.TrinoHost = strValue(asString(p["trino_host"]))
	m.TrinoPort = types.Int64Value(asInt(p["trino_port"]))
	m.TrinoUser = strValue(asString(p["trino_user"]))
	m.HTTPScheme = strValue(asString(p["http_scheme"]))
	m.Selector = strValue(asString(p["selector"]))
	m.Command = strValue(asString(p["command"]))
	files := map[string]string{}
	for k, v := range asMap(p["files"]) {
		files[k] = asString(v)
	}
	m.Files = stringMap(ctx, files, diags)
	if m.Template.IsNull() || m.Template.IsUnknown() {
		m.Template = strValue("cdc_customers")
	}
}

func (r *dbtProjectResource) settings(m *dbtProjectModel) map[string]any {
	body := map[string]any{"name": m.Name.ValueString(), "description": m.Description.ValueString()}
	for key, v := range map[string]types.String{
		"target": m.Target, "catalog": m.Catalog, "schema_name": m.SchemaName, "materialization": m.Materialization,
		"trino_host": m.TrinoHost, "trino_user": m.TrinoUser, "http_scheme": m.HTTPScheme, "selector": m.Selector, "command": m.Command,
	} {
		if known(v) {
			body[key] = v.ValueString()
		}
	}
	putInt(body, "threads", m.Threads)
	putInt(body, "trino_port", m.TrinoPort)
	return body
}

func (r *dbtProjectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dbtProjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.settings(&plan)
	body["template"] = plan.Template.ValueString()
	ws := plan.Workspace.ValueString()
	var out struct {
		Project map[string]any `json:"project"`
	}
	if err := r.c.Post(ctx, clusterPath(plan.ClusterID.ValueString())+"/dbt/projects", ws, body, &out); err != nil {
		apiError(&resp.Diagnostics, "create dbt project", err)
		return
	}
	if known(plan.Files) {
		id := asString(out.Project["id"])
		files := map[string]any{"files": mapStrings(ctx, plan.Files, &resp.Diagnostics)}
		if err := r.c.Put(ctx, dbtProjectPath(plan.ClusterID.ValueString(), id), ws, files, &out); err != nil {
			apiError(&resp.Diagnostics, "upload dbt project files", err)
			plan.ID = strValue(id)
		}
	}
	r.fill(ctx, &plan, out.Project, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dbtProjectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dbtProjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Project map[string]any `json:"project"`
	}
	if err := r.c.Get(ctx, dbtProjectPath(state.ClusterID.ValueString(), state.ID.ValueString()), state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read dbt project", err)
		return
	}
	r.fill(ctx, &state, out.Project, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *dbtProjectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dbtProjectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body := r.settings(&plan)
	if known(plan.Files) {
		body["files"] = mapStrings(ctx, plan.Files, &resp.Diagnostics)
	}
	var out struct {
		Project map[string]any `json:"project"`
	}
	if err := r.c.Put(ctx, dbtProjectPath(plan.ClusterID.ValueString(), plan.ID.ValueString()), plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update dbt project", err)
		return
	}
	r.fill(ctx, &plan, out.Project, &resp.Diagnostics)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *dbtProjectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dbtProjectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, dbtProjectPath(state.ClusterID.ValueString(), state.ID.ValueString()), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete dbt project", err)
	}
}

func (r *dbtProjectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<project_id>", "cluster_id", "id")
}

// ---------------------------------------------------------------- gezor_pipeline

var _ resource.ResourceWithImportState = &pipelineResource{}

func NewPipelineResource() resource.Resource { return &pipelineResource{} }

type pipelineResource struct{ base }

type pipelineModel struct {
	Workspace   types.String         `tfsdk:"workspace"`
	ID          types.String         `tfsdk:"id"`
	ClusterID   types.String         `tfsdk:"cluster_id"`
	Name        types.String         `tfsdk:"name"`
	Description types.String         `tfsdk:"description"`
	Graph       jsontypes.Normalized `tfsdk:"graph"`
	Status      types.String         `tfsdk:"status"`
}

func (r *pipelineResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (r *pipelineResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A pipeline from the Pipeline Designer. `graph` is the designer's node and edge JSON; export it from the portal or build it with `jsonencode`. Deploying and running pipelines is done in the portal.",
		Attributes: map[string]schema.Attribute{
			"workspace":   workspaceAttribute(),
			"id":          idAttribute("Pipeline id."),
			"cluster_id":  requiredReplaceString("Cluster id."),
			"name":        schema.StringAttribute{Required: true},
			"description": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
			"graph": schema.StringAttribute{
				CustomType: jsontypes.NormalizedType{}, Optional: true, Computed: true,
				MarkdownDescription: "Designer graph as JSON with `nodes` and `edges`. Defaults to an empty graph.",
			},
			"status": schema.StringAttribute{Computed: true},
		},
	}
}

func pipelinePath(clusterID, id string) string {
	return clusterPath(clusterID) + "/designer-pipelines/" + client.PathEscape(id)
}

func (r *pipelineResource) fill(m *pipelineModel, p map[string]any) {
	m.ID = strValue(asString(p["id"]))
	m.ClusterID = strValue(asString(p["cluster_id"]))
	m.Name = strValue(asString(p["name"]))
	m.Description = strValue(asString(p["description"]))
	m.Status = strValue(asString(p["status"]))
	graph := encodeJSON(p["graph"])
	if !known(m.Graph) {
		m.Graph = jsontypes.NewNormalizedValue(graph)
		return
	}
	if eq, _ := m.Graph.StringSemanticEquals(context.Background(), jsontypes.NewNormalizedValue(graph)); !eq {
		m.Graph = jsontypes.NewNormalizedValue(graph)
	}
}

func (r *pipelineResource) body(m *pipelineModel) (map[string]any, error) {
	raw := `{"nodes":[],"edges":[]}`
	if known(m.Graph) {
		raw = m.Graph.ValueString()
	}
	graph, err := decodeJSONObject(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": m.Name.ValueString(), "description": m.Description.ValueString(), "graph": graph}, nil
}

func (r *pipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan pipelineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := r.body(&plan)
	if err != nil {
		resp.Diagnostics.AddAttributeError(pathRoot("graph"), "Invalid graph", err.Error())
		return
	}
	var out struct {
		Pipeline map[string]any `json:"pipeline"`
	}
	if err := r.c.Post(ctx, clusterPath(plan.ClusterID.ValueString())+"/designer-pipelines", plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "create pipeline", err)
		return
	}
	r.fill(&plan, out.Pipeline)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *pipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state pipelineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Pipeline map[string]any `json:"pipeline"`
	}
	if err := r.c.Get(ctx, pipelinePath(state.ClusterID.ValueString(), state.ID.ValueString()), state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read pipeline", err)
		return
	}
	r.fill(&state, out.Pipeline)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *pipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan pipelineModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	body, err := r.body(&plan)
	if err != nil {
		resp.Diagnostics.AddAttributeError(pathRoot("graph"), "Invalid graph", err.Error())
		return
	}
	var out struct {
		Pipeline map[string]any `json:"pipeline"`
	}
	if err := r.c.Put(ctx, pipelinePath(plan.ClusterID.ValueString(), plan.ID.ValueString()), plan.Workspace.ValueString(), body, &out); err != nil {
		apiError(&resp.Diagnostics, "update pipeline", err)
		return
	}
	r.fill(&plan, out.Pipeline)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *pipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state pipelineModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, pipelinePath(state.ClusterID.ValueString(), state.ID.ValueString()), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete pipeline", err)
	}
}

func (r *pipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<pipeline_id>", "cluster_id", "id")
}

// ---------------------------------------------------------------- gezor_pipeline_notebook

var _ resource.ResourceWithImportState = &notebookResource{}

func NewPipelineNotebookResource() resource.Resource { return &notebookResource{} }

type notebookResource struct{ base }

type notebookModel struct {
	Workspace types.String `tfsdk:"workspace"`
	ID        types.String `tfsdk:"id"`
	ClusterID types.String `tfsdk:"cluster_id"`
	Path      types.String `tfsdk:"path"`
	Language  types.String `tfsdk:"language"`
	Content   types.String `tfsdk:"content"`
}

func (r *notebookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline_notebook"
}

func (r *notebookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A notebook used by Pipeline Designer steps.",
		Attributes: map[string]schema.Attribute{
			"workspace":  workspaceAttribute(),
			"id":         idAttribute("Notebook id."),
			"cluster_id": requiredReplaceString("Cluster id."),
			"path":       schema.StringAttribute{Required: true, MarkdownDescription: "Notebook path, for example `bronze/orders.sql`."},
			"language": schema.StringAttribute{
				Optional: true, Computed: true, Default: stringdefault.StaticString("sql"),
				MarkdownDescription: "`sql` or `python`.",
			},
			"content": schema.StringAttribute{Optional: true, Computed: true, Default: stringdefault.StaticString("")},
		},
	}
}

func notebookPath(clusterID, id string) string {
	return clusterPath(clusterID) + "/pipeline-notebooks/" + client.PathEscape(id)
}

func (r *notebookResource) fill(m *notebookModel, n map[string]any) {
	m.ID = strValue(asString(n["id"]))
	m.ClusterID = strValue(asString(n["cluster_id"]))
	m.Path = strValue(asString(n["path"]))
	m.Language = strValue(asString(n["language"]))
	m.Content = strValue(asString(n["content"]))
}

func (r *notebookResource) body(m *notebookModel) map[string]any {
	return map[string]any{"path": m.Path.ValueString(), "language": m.Language.ValueString(), "content": m.Content.ValueString()}
}

func (r *notebookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan notebookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Notebook map[string]any `json:"notebook"`
	}
	if err := r.c.Post(ctx, clusterPath(plan.ClusterID.ValueString())+"/pipeline-notebooks", plan.Workspace.ValueString(), r.body(&plan), &out); err != nil {
		apiError(&resp.Diagnostics, "create notebook", err)
		return
	}
	r.fill(&plan, out.Notebook)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *notebookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state notebookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Notebook map[string]any `json:"notebook"`
	}
	if err := r.c.Get(ctx, notebookPath(state.ClusterID.ValueString(), state.ID.ValueString()), state.Workspace.ValueString(), &out); err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		apiError(&resp.Diagnostics, "read notebook", err)
		return
	}
	r.fill(&state, out.Notebook)
	resp.Diagnostics.Append(resp.State.Set(ctx, state)...)
}

func (r *notebookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan notebookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var out struct {
		Notebook map[string]any `json:"notebook"`
	}
	if err := r.c.Put(ctx, notebookPath(plan.ClusterID.ValueString(), plan.ID.ValueString()), plan.Workspace.ValueString(), r.body(&plan), &out); err != nil {
		apiError(&resp.Diagnostics, "update notebook", err)
		return
	}
	r.fill(&plan, out.Notebook)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *notebookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state notebookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.c.Delete(ctx, notebookPath(state.ClusterID.ValueString(), state.ID.ValueString()), state.Workspace.ValueString(), nil); err != nil && !client.IsNotFound(err) {
		apiError(&resp.Diagnostics, "delete notebook", err)
	}
}

func (r *notebookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importState(ctx, req, resp, "<cluster_id>/<notebook_id>", "cluster_id", "id")
}
