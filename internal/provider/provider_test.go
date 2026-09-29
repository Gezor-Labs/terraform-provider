package provider

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Gezor-Labs/terraform-provider-gezor/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

var testProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"gezor": providerserver.NewProtocol6WithError(New("test")()),
}

func TestProviderSchemas(t *testing.T) {
	ctx := context.Background()
	server, err := testProviderFactories["gezor"]()
	if err != nil {
		t.Fatal(err)
	}
	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range resp.Diagnostics {
		t.Errorf("%s: %s: %s", d.Severity, d.Summary, d.Detail)
	}
	if len(resp.ResourceSchemas) != 23 {
		t.Errorf("expected 23 resources, got %d", len(resp.ResourceSchemas))
	}
	if len(resp.DataSourceSchemas) != 9 {
		t.Errorf("expected 9 data sources, got %d", len(resp.DataSourceSchemas))
	}
}

func TestSchemasValidate(t *testing.T) {
	ctx := context.Background()
	p := New("test")().(*gezorProvider)
	for _, f := range p.Resources(ctx) {
		r := f()
		var meta resource.MetadataResponse
		r.Metadata(ctx, resource.MetadataRequest{ProviderTypeName: "gezor"}, &meta)
		var s resource.SchemaResponse
		r.Schema(ctx, resource.SchemaRequest{}, &s)
		if s.Diagnostics.HasError() {
			t.Errorf("%s schema: %v", meta.TypeName, s.Diagnostics)
		}
		if d := s.Schema.ValidateImplementation(ctx); d.HasError() {
			t.Errorf("%s: %v", meta.TypeName, d)
		}
		if _, ok := s.Schema.Attributes["id"]; !ok {
			t.Errorf("%s has no id attribute", meta.TypeName)
		}
		if _, ok := r.(resource.ResourceWithImportState); !ok {
			t.Errorf("%s is not importable", meta.TypeName)
		}
	}
	for _, f := range p.DataSources(ctx) {
		d := f()
		var meta datasource.MetadataResponse
		d.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "gezor"}, &meta)
		var s datasource.SchemaResponse
		d.Schema(ctx, datasource.SchemaRequest{}, &s)
		if diags := s.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Errorf("%s: %v", meta.TypeName, diags)
		}
	}
}

func TestParseImportID(t *testing.T) {
	cases := []struct {
		raw   string
		n     int
		parts []string
		ws    string
		err   bool
	}{
		{"role_1", 1, []string{"role_1"}, "", false},
		{"role_1@analytics", 1, []string{"role_1"}, "analytics", false},
		{"cl_1/orders", 2, []string{"cl_1", "orders"}, "", false},
		{"cl_1/cdc-1/pg@ws_2", 3, []string{"cl_1", "cdc-1", "pg"}, "ws_2", false},
		{"cl_1/a/b", 2, []string{"cl_1", "a/b"}, "", false},
		{"cl_1", 2, nil, "", true},
		{"cl_1/", 2, nil, "", true},
	}
	for _, c := range cases {
		parts, ws, err := parseImportID(c.raw, c.n, "x")
		if c.err {
			if err == nil {
				t.Errorf("%q: expected error", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", c.raw, err)
			continue
		}
		if ws != c.ws || len(parts) != len(c.parts) {
			t.Errorf("%q: got %v %q", c.raw, parts, ws)
			continue
		}
		for i := range parts {
			if parts[i] != c.parts[i] {
				t.Errorf("%q: got %v", c.raw, parts)
			}
		}
	}
}

func TestSubsetEqual(t *testing.T) {
	have := map[string]any{"replicas": float64(3), "storage": map[string]any{"size": "10Gi", "class": "gp3"}, "list": []any{"a", "b"}}
	yes := []map[string]any{
		{},
		{"replicas": float64(3)},
		{"storage": map[string]any{"size": "10Gi"}},
		{"list": []any{"a", "b"}},
		{"missing": nil},
	}
	no := []map[string]any{
		{"replicas": float64(2)},
		{"storage": map[string]any{"size": "20Gi"}},
		{"list": []any{"a"}},
		{"missing": "x"},
	}
	for _, w := range yes {
		if !subsetEqual(w, have) {
			t.Errorf("expected %v to be a subset", w)
		}
	}
	for _, w := range no {
		if subsetEqual(w, have) {
			t.Errorf("expected %v not to be a subset", w)
		}
	}
}

func TestSameSchema(t *testing.T) {
	if !sameSchema(`{"type":"record","name":"a"}`, "{\n  \"name\": \"a\",\n  \"type\": \"record\"\n}") {
		t.Error("formatted JSON schemas should match")
	}
	if sameSchema(`{"type":"record","name":"a"}`, `{"type":"record","name":"b"}`) {
		t.Error("different schemas should not match")
	}
	if !sameSchema("syntax = \"proto3\";\n", "syntax = \"proto3\";") {
		t.Error("protobuf text should compare trimmed")
	}
}

func TestCanonicalSchema(t *testing.T) {
	registry := `{"type":"record","name":"Order","fields":[{"name":"id","type":"string"},{"name":"n","type":"long","default":12345678901234567890}]}`
	want := `{"fields":[{"name":"id","type":"string"},{"default":12345678901234567890,"name":"n","type":"long"}],"name":"Order","type":"record"}`
	if got := canonicalSchema(registry); got != want {
		t.Fatalf("got %s", got)
	}
	if !sameSchema(registry, want) || sameSchema(registry, `{"type":"record"}`) {
		t.Fatal("sameSchema mismatch")
	}
	proto := "  syntax = \"proto3\";\nmessage Order { string id = 1; }\n"
	if got := canonicalSchema(proto); got != strings.TrimSpace(proto) {
		t.Fatalf("got %q", got)
	}
}

func TestRetryWhileAppStarts(t *testing.T) {
	defer func(d time.Duration) { waitPollInterval = d }(waitPollInterval)
	waitPollInterval = time.Millisecond
	calls := 0
	_, err := retryWhileAppStarts(context.Background(), func() (*client.CommandResult, error) {
		calls++
		if calls < 3 {
			return nil, errors.New(`register schema: Post "http://gzr-schema-registry:8081/subjects/x/versions": dial tcp 10.43.0.1:8081: connect: connection refused`)
		}
		return &client.CommandResult{Status: "ready"}, nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("got %v after %d calls", err, calls)
	}
	calls = 0
	_, err = retryWhileAppStarts(context.Background(), func() (*client.CommandResult, error) {
		calls++
		return nil, errors.New("topic already exists")
	})
	if err == nil || calls != 1 {
		t.Fatalf("got %v after %d calls", err, calls)
	}
}
