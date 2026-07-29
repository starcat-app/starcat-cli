// Package globalsearchcontract 校验 Launcher 共享 fixture 的基础不变量。
//
// 这里故意不引入 JSON Schema 第三方库：正式 schema 供外部工具消费，Go 测试只负责
// 防止示例与 CLI 已承诺的 v1 字段、来源和数量语义发生明显漂移。
package globalsearchcontract

import (
	"encoding/json"
	"os"
	"testing"
)

type searchFixture struct {
	SchemaVersion int `json:"schema_version"`
	ReturnedCount int `json:"returned_count"`
	Items         []struct {
		FullName      string   `json:"full_name"`
		PrimarySource string   `json:"primary_source"`
		Sources       []string `json:"sources"`
		OpenURL       string   `json:"open_url"`
	} `json:"items"`
}

func TestSuccessFixturesMatchV1Contract(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"success-all.json",
		"success-local-warning.json",
		"empty.json",
	} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var fixture searchFixture
			decodeJSONFile(t, name, &fixture)
			if fixture.SchemaVersion != 1 {
				t.Fatalf("schema_version = %d, want 1", fixture.SchemaVersion)
			}
			if fixture.ReturnedCount != len(fixture.Items) {
				t.Fatalf(
					"returned_count = %d, items = %d",
					fixture.ReturnedCount,
					len(fixture.Items),
				)
			}
			for _, item := range fixture.Items {
				if item.FullName == "" || item.OpenURL == "" {
					t.Fatalf("fixture item is missing full_name or open_url: %+v", item)
				}
				if item.PrimarySource != "local" && item.PrimarySource != "github" {
					t.Fatalf("primary_source = %q", item.PrimarySource)
				}
				if len(item.Sources) == 0 {
					t.Fatalf("sources must not be empty for %q", item.FullName)
				}
			}
		})
	}
}

func TestSchemaAndErrorCatalogAreValidJSON(t *testing.T) {
	t.Parallel()

	var schema map[string]any
	decodeJSONFile(t, "schema-v1.json", &schema)
	if schema["title"] == nil || schema["$defs"] == nil {
		t.Fatal("schema-v1.json is missing title or $defs")
	}

	var catalog struct {
		SchemaVersion int `json:"schema_version"`
		Errors        []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	decodeJSONFile(t, "error-codes.json", &catalog)
	if catalog.SchemaVersion != 1 {
		t.Fatalf("error schema_version = %d, want 1", catalog.SchemaVersion)
	}
	if len(catalog.Errors) != 7 {
		t.Fatalf("error code count = %d, want 7", len(catalog.Errors))
	}
}

func decodeJSONFile(t *testing.T, path string, value any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
}
