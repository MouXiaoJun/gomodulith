package modulith

import (
	"encoding/json"
	"testing"
)

func TestExportSARIF(t *testing.T) {
	writeFixture(t, defaultFixture()) // has a cross-module violation
	app := mustLoad(t, "./...")

	data, err := app.ExportSARIF()
	if err != nil {
		t.Fatalf("ExportSARIF: %v", err)
	}
	var doc struct {
		Schema  string `json:"$schema"`
		Version string `json:"version"`
		Runs    []struct {
			Tool struct {
				Driver struct {
					Name  string `json:"name"`
					Rules []struct {
						ID string `json:"id"`
					} `json:"rules"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string                `json:"ruleId"`
				Level   string                `json:"level"`
				Message struct{ Text string } `json:"message"`
			} `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal sarif: %v", err)
	}
	if doc.Schema == "" || doc.Version != "2.1.0" {
		t.Errorf("schema=%q version=%q", doc.Schema, doc.Version)
	}
	if len(doc.Runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(doc.Runs))
	}
	run := doc.Runs[0]
	if run.Tool.Driver.Name != "gomodulith" {
		t.Errorf("driver = %q", run.Tool.Driver.Name)
	}
	if len(run.Results) == 0 {
		t.Fatal("expected results (fixture has a violation)")
	}
	found := false
	for _, r := range run.Results {
		if r.RuleID == "cross-module-private-access" {
			found = true
			if r.Level != "error" {
				t.Errorf("level = %q, want error", r.Level)
			}
			if r.Message.Text == "" {
				t.Error("empty message")
			}
		}
	}
	if !found {
		t.Errorf("cross-module-private-access result not found: %s", data)
	}
}

func TestExportSARIFHealthy(t *testing.T) {
	files := defaultFixture()
	files["internal/order/domain/dom.go"] = goFile("domain", []string{fixtureModule + "/internal/user/api"})
	writeFixture(t, files)
	app := mustLoad(t, "./...")

	data, err := app.ExportSARIF()
	if err != nil {
		t.Fatalf("ExportSARIF: %v", err)
	}
	var doc struct {
		Runs []struct {
			Results []any `json:"results"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.Runs[0].Results) != 0 {
		t.Fatalf("expected no results, got %d", len(doc.Runs[0].Results))
	}
}
