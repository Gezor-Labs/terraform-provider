package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/Gezor-Labs/terraform-provider/internal/client"
)

func secondsDuration(n int64) time.Duration { return time.Duration(n) * time.Second }

// base is embedded by every resource for the configured API client.
type base struct {
	c *client.Client
}

func (b *base) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	b.c = c
}

// dsBase is embedded by every data source.
type dsBase struct {
	c *client.Client
}

func (b *dsBase) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := req.ProviderData.(*client.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("got %T", req.ProviderData))
		return
	}
	b.c = c
}

const workspaceDescription = "Workspace id or slug. Defaults to the provider's `workspace`."

func workspaceAttribute() rschema.StringAttribute {
	return rschema.StringAttribute{
		Optional:            true,
		MarkdownDescription: workspaceDescription + " Changing it recreates the resource.",
		PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

func dsWorkspaceAttribute() dschema.StringAttribute {
	return dschema.StringAttribute{Optional: true, MarkdownDescription: workspaceDescription}
}

func idAttribute(desc string) rschema.StringAttribute {
	return rschema.StringAttribute{
		Computed:            true,
		MarkdownDescription: desc,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
	}
}

func requiredReplaceString(desc string) rschema.StringAttribute {
	return rschema.StringAttribute{
		Required:            true,
		MarkdownDescription: desc,
		PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
	}
}

// parseImportID splits "<a>/<b>...[@<workspace>]" into exactly n parts; the last part
// keeps any remaining slashes.
func parseImportID(raw string, n int, format string) ([]string, string, error) {
	rest, workspace := raw, ""
	if i := strings.LastIndex(raw, "@"); i >= 0 {
		rest, workspace = raw[:i], raw[i+1:]
	}
	parts := strings.SplitN(rest, "/", n)
	if len(parts) != n {
		return nil, "", fmt.Errorf("expected import ID %q (optionally followed by @<workspace>), got %q", format, raw)
	}
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return nil, "", fmt.Errorf("expected import ID %q (optionally followed by @<workspace>), got %q", format, raw)
		}
	}
	return parts, workspace, nil
}

// importState sets the given attributes (and workspace) from an import ID.
func importState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse, format string, attrs ...string) {
	parts, workspace, err := parseImportID(req.ID, len(attrs), format)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	for i, a := range attrs {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(a), parts[i])...)
	}
	if workspace != "" {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("workspace"), workspace)...)
	}
}

func apiError(diags *diag.Diagnostics, action string, err error) {
	diags.AddError("Gezor API error", fmt.Sprintf("Unable to %s: %s", action, err))
}

func strValue(s string) types.String { return types.StringValue(s) }

// optString is null when s is empty.
func optString(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

func known(v interface {
	IsNull() bool
	IsUnknown() bool
}) bool {
	return !v.IsNull() && !v.IsUnknown()
}

func stringSet(ctx context.Context, values []string, diags *diag.Diagnostics) types.Set {
	if values == nil {
		values = []string{}
	}
	v, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return v
}

func stringList(ctx context.Context, values []string, diags *diag.Diagnostics) types.List {
	if values == nil {
		values = []string{}
	}
	v, d := types.ListValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return v
}

func setStrings(ctx context.Context, s types.Set, diags *diag.Diagnostics) []string {
	out := []string{}
	if !known(s) {
		return out
	}
	diags.Append(s.ElementsAs(ctx, &out, false)...)
	sort.Strings(out)
	return out
}

func listStrings(ctx context.Context, l types.List, diags *diag.Diagnostics) []string {
	out := []string{}
	if !known(l) {
		return out
	}
	diags.Append(l.ElementsAs(ctx, &out, false)...)
	return out
}

func stringMap(ctx context.Context, values map[string]string, diags *diag.Diagnostics) types.Map {
	if values == nil {
		values = map[string]string{}
	}
	v, d := types.MapValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return v
}

func mapStrings(ctx context.Context, m types.Map, diags *diag.Diagnostics) map[string]string {
	out := map[string]string{}
	if !known(m) {
		return out
	}
	diags.Append(m.ElementsAs(ctx, &out, false)...)
	return out
}

// subsetEqual reports whether every value in want is present and equal in have.
// Objects may have extra keys in have; arrays must match element by element.
func subsetEqual(want, have any) bool {
	switch w := want.(type) {
	case map[string]any:
		h, ok := have.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			hv, ok := h[k]
			if !ok {
				if wv == nil {
					continue
				}
				return false
			}
			if !subsetEqual(wv, hv) {
				return false
			}
		}
		return true
	case []any:
		h, ok := have.([]any)
		if !ok || len(h) != len(w) {
			return false
		}
		for i := range w {
			if !subsetEqual(w[i], h[i]) {
				return false
			}
		}
		return true
	case float64:
		h, ok := have.(float64)
		return ok && math.Abs(h-w) < 1e-9
	default:
		return reflect.DeepEqual(want, have)
	}
}

func decodeJSONObject(s string) (map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(s) == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, fmt.Errorf("must be a JSON object: %w", err)
	}
	return out, nil
}

func encodeJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	case float64:
		if t == math.Trunc(t) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprint(t)
	default:
		return fmt.Sprint(t)
	}
}

func asInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int:
		return int64(t)
	case int64:
		return t
	case json.Number:
		n, _ := t.Int64()
		return n
	}
	return 0
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
