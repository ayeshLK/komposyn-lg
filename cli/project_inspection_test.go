// Copyright 2026 Ayesh Almeida
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func TestProjectInspectionSeparatesRangeHistoryAndMetadataSessions(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	projectID := "private-project-id-marker"
	project := tokenlogger.Project{ID: projectID, DisplayName: "private-project-name-marker", Root: filepath.Join(t.TempDir(), "private-location-marker")}
	if err := store.UpsertProject(project); err != nil {
		t.Fatal(err)
	}
	firstRoot := project.Root
	if err := store.UpsertProject(tokenlogger.Project{ID: projectID, DisplayName: project.DisplayName, Root: filepath.Join(t.TempDir(), "second-private-location-marker")}); err != nil {
		t.Fatal(err)
	}
	old := cliReportEvent("private-source-record-marker", time.Date(2020, 3, 4, 5, 6, 7, 0, time.UTC))
	old.ProjectID, old.SessionID = projectID, "private-session-id-marker"
	old.Provenance.SourceID = "private-source-id-marker"
	project.Root = firstRoot
	if _, err := store.AppendWithReferences([]tokenlogger.UsageEvent{old}, []tokenlogger.Project{project}, []tokenlogger.Session{{ID: old.SessionID, ProjectID: projectID, Client: old.Client}}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(tokenlogger.Session{ID: "metadata-only-session", ProjectID: projectID, Client: tokenlogger.Client{Name: "codex-cli"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	from, to := "2026-10-01", "2026-10-21"
	var inspection tokenlogger.ProjectInspection
	var stdout, stderr bytes.Buffer
	args := []string{"projects", "inspect", projectID, "--data-dir", dataDir, "--from", from, "--to", to, "--limit", "1", "--format", "json"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("inspect exit=%d stderr=%q", code, stderr.String())
	}
	if err := json.Unmarshal(stdout.Bytes(), &inspection); err != nil {
		t.Fatalf("decode inspection JSON: %v\n%s", err, stdout.String())
	}
	if inspection.FormatVersion != tokenlogger.ProjectInspectionFormatVersion || !inspection.Exists || inspection.SelectedActivity.EventCount != 0 || inspection.HistoricalActivity.EventCount != 1 {
		t.Fatalf("inspection did not preserve range/history distinction: %+v", inspection)
	}
	if inspection.StoredSessionCount != 2 || inspection.SelectedUsageSessionCount != 0 || inspection.HistoricalUsageSessionCount != 1 {
		t.Fatalf("stored/usage-derived session counts were conflated: stored=%d selected=%d historical=%d", inspection.StoredSessionCount, inspection.SelectedUsageSessionCount, inspection.HistoricalUsageSessionCount)
	}
	if inspection.ClassificationEvidence.Status != tokenlogger.InspectionEvidenceUnavailable || inspection.NameOrigin == "" {
		t.Fatalf("unknown historical provenance was not explicit: %+v", inspection)
	}
	if !inspection.LocationsPage.HasMore || inspection.LocationsPage.NextOffset == nil || len(inspection.Locations) != 1 {
		t.Fatalf("bounded location page lacks continuation metadata: page=%+v values=%+v", inspection.LocationsPage, inspection.Locations)
	}
	if strings.Contains(stdout.String(), "private-") || strings.Contains(stdout.String(), "private-location") || strings.Contains(stdout.String(), projectID) {
		t.Fatalf("inspection JSON leaked private fixture markers: %s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	args = []string{"projects", "inspect", projectID, "--data-dir", dataDir, "--from", from, "--to", to, "--limit", "1", "--offset", "1", "--format", "json"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("second inspection page exit=%d stderr=%q", code, stderr.String())
	}
	var next tokenlogger.ProjectInspection
	if err := json.Unmarshal(stdout.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.LocationsPage.Offset != 1 || next.LocationsPage.HasMore {
		t.Fatalf("unexpected second page: %+v", next.LocationsPage)
	}

	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"projects", "inspect", projectID, "--data-dir", dataDir, "--from", from, "--to", to}, &stdout, &stderr); code != 0 {
		t.Fatalf("human inspection exit=%d stderr=%q", code, stderr.String())
	}
	for _, expected := range []string{"Selected range [", "Persisted historical activity (coverage: stored_events_only)", "stored sessions: 2; usage-bearing sessions: 1", "Location evidence: available", "not treat this output as automatically safe to publish"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("human inspection missing %q: %s", expected, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), projectID) || strings.Contains(stdout.String(), "private-location-marker") {
		t.Fatalf("human inspection leaked fixture markers: %s", stdout.String())
	}
}

func TestProjectInspectionErrorsAndRangeBounds(t *testing.T) {
	dataDir := t.TempDir()
	cases := []struct {
		name string
		args []string
		code int
		want string
	}{
		{"missing id", []string{"projects", "inspect"}, 1, "requires a project ID"},
		{"bad format", []string{"projects", "inspect", "id", "--format", "csv"}, 1, "unsupported format"},
		{"bad limit", []string{"projects", "inspect", "id", "--limit", "51"}, 1, "limit must be"},
		{"bad offset", []string{"projects", "inspect", "id", "--offset", "100001"}, 1, "offset must be"},
		{"reversed range", []string{"projects", "inspect", "id", "--from", "2026-10-02", "--to", "2026-10-01"}, 1, "range must be increasing"},
		{"missing project", []string{"projects", "inspect", "missing-id", "--data-dir", dataDir}, 2, "unavailable in the local database"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
			if code := run(test.args, stdout, stderr); code != test.code {
				t.Fatalf("exit=%d want=%d stdout=%q stderr=%q", code, test.code, stdout.String(), stderr.String())
			}
			output := stdout.String() + stderr.String()
			if !strings.Contains(output, test.want) {
				t.Fatalf("output %q missing %q", output, test.want)
			}
		})
	}
}

func TestProjectInspectionAllZeroInventoryKeepsMetadataOnlyProject(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertProject(tokenlogger.Project{ID: "zero-project", DisplayName: "Zero Usage Project"}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertSession(tokenlogger.Session{ID: "metadata-only", ProjectID: "zero-project", Client: tokenlogger.Client{Name: "codex-cli"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var listed, listErr bytes.Buffer
	if code := run([]string{"projects", "list", "--data-dir", dataDir, "--from", "2026-10-01", "--to", "2026-10-21"}, &listed, &listErr); code != 0 {
		t.Fatalf("projects list exit=%d stderr=%q", code, listErr.String())
	}
	for _, want := range []string{"All known projects", "usage-derived session counts", "WARNINGS count reconciliation only", "Zero Usage Project"} {
		if !strings.Contains(listed.String(), want) {
			t.Errorf("all-zero inventory missing %q: %s", want, listed.String())
		}
	}
	var inspected, inspectErr bytes.Buffer
	if code := run([]string{"projects", "inspect", "zero-project", "--data-dir", dataDir, "--from", "2026-10-01", "--to", "2026-10-21", "--format", "json"}, &inspected, &inspectErr); code != 0 {
		t.Fatalf("inspection exit=%d stderr=%q", code, inspectErr.String())
	}
	var result tokenlogger.ProjectInspection
	if err := json.Unmarshal(inspected.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Exists || result.StoredSessionCount != 1 || result.HistoricalActivity.EventCount != 0 || result.RecoveryClassification != tokenlogger.InspectionRecoveryRetainMetadataOnly {
		t.Fatalf("metadata-only project evidence = %+v", result)
	}
}

func TestProjectInspectionNormalCodexAndClaudeImports(t *testing.T) {
	for _, provider := range []string{"codex", "claude-code"} {
		t.Run(provider, func(t *testing.T) {
			dataDir := t.TempDir()
			fixture := filepath.Join("..", "testdata", "providers", provider, "normal.jsonl")
			var imported, importErr bytes.Buffer
			if code := run([]string{"import", fixture, "--data-dir", dataDir}, &imported, &importErr); code != 0 {
				t.Fatalf("import exit=%d stderr=%q stdout=%q", code, importErr.String(), imported.String())
			}
			var projectJSON, listErr bytes.Buffer
			if code := run([]string{"projects", "list", "--data-dir", dataDir, "--format", "json"}, &projectJSON, &listErr); code != 0 {
				t.Fatalf("list exit=%d stderr=%q", code, listErr.String())
			}
			var projects tokenlogger.ProjectResourceList
			if err := json.Unmarshal(projectJSON.Bytes(), &projects); err != nil {
				t.Fatal(err)
			}
			if len(projects.Projects) == 0 {
				t.Fatalf("%s fixture imported no project: %s", provider, projectJSON.String())
			}
			for _, project := range projects.Projects {
				var output, stderr bytes.Buffer
				if code := run([]string{"projects", "inspect", project.ID, "--data-dir", dataDir, "--from", "2000-01-01", "--to", "2100-01-01", "--format", "json"}, &output, &stderr); code != 0 {
					t.Fatalf("inspect %s project exit=%d stderr=%q", provider, code, stderr.String())
				}
				var result tokenlogger.ProjectInspection
				if err := json.Unmarshal(output.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if !result.Exists || !result.HistoricalActivity.HasMatchingEvents || result.SourceEvidence.Status != tokenlogger.InspectionEvidenceAvailable || result.AliasEvidence.Status != tokenlogger.InspectionEvidenceAvailable {
					t.Fatalf("%s inspection omitted imported evidence: %+v", provider, result)
				}
				if strings.Contains(output.String(), project.ID) || strings.Contains(output.String(), project.DisplayName) {
					t.Fatalf("%s inspection leaked project identity: %s", provider, output.String())
				}
			}
		})
	}
}

func TestProjectInspectionAuxiliaryOnlyAndLateCWDSources(t *testing.T) {
	t.Run("auxiliary-only Codex input creates no inventory rows", func(t *testing.T) {
		dataDir, source := t.TempDir(), filepath.Join(t.TempDir(), "auxiliary.jsonl")
		if err := os.WriteFile(source, []byte(`{"type":"event_msg","payload":{"type":"agent_message","text":"synthetic-only"}}`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var imported, importErr bytes.Buffer
		code := run([]string{"import", source, "--data-dir", dataDir}, &imported, &importErr)
		if code != 0 && code != 3 {
			t.Fatalf("auxiliary-only import exit=%d stderr=%q stdout=%q", code, importErr.String(), imported.String())
		}
		var listed, listErr bytes.Buffer
		if listCode := run([]string{"projects", "list", "--data-dir", dataDir, "--format", "json"}, &listed, &listErr); listCode != 0 {
			t.Fatalf("list exit=%d stderr=%q", listCode, listErr.String())
		}
		var projects tokenlogger.ProjectResourceList
		if err := json.Unmarshal(listed.Bytes(), &projects); err != nil {
			t.Fatal(err)
		}
		if len(projects.Projects) != 0 {
			t.Fatalf("auxiliary-only input created project identities: %+v", projects.Projects)
		}
	})

	t.Run("late Claude cwd remains inspectable by stable ID", func(t *testing.T) {
		dataDir, source := t.TempDir(), filepath.Join(t.TempDir(), "late-cwd.jsonl")
		initial := "{\"type\":\"user\",\"sessionId\":\"late-session\",\"message\":{\"role\":\"user\"}}\n" +
			"{\"type\":\"assistant\",\"uuid\":\"late-response\",\"sessionId\":\"late-session\",\"timestamp\":\"2026-10-11T00:00:00Z\",\"message\":{\"role\":\"assistant\",\"model\":\"claude-test\",\"usage\":{\"input_tokens\":10,\"output_tokens\":1}}}\n"
		if err := os.WriteFile(source, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, phase := range []string{"initial", "late cwd append"} {
			if phase == "late cwd append" {
				f, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0o600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := io.WriteString(f, "{\"type\":\"user\",\"sessionId\":\"late-session\",\"cwd\":\"/synthetic/private-late-cwd/Studio Ledger\",\"message\":{\"role\":\"user\"}}\n")
				closeErr := f.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatalf("append late cwd: write=%v close=%v", writeErr, closeErr)
				}
			}
			var imported, importErr bytes.Buffer
			if code := run([]string{"import", source, "--data-dir", dataDir}, &imported, &importErr); code != 0 {
				t.Fatalf("%s import exit=%d stderr=%q stdout=%q", phase, code, importErr.String(), imported.String())
			}
		}
		var listed, listErr bytes.Buffer
		if code := run([]string{"projects", "list", "--data-dir", dataDir, "--from", "2000-01-01", "--to", "2100-01-01", "--format", "json"}, &listed, &listErr); code != 0 {
			t.Fatalf("list exit=%d stderr=%q", code, listErr.String())
		}
		var projects tokenlogger.ProjectResourceList
		if err := json.Unmarshal(listed.Bytes(), &projects); err != nil {
			t.Fatal(err)
		}
		if len(projects.Projects) != 1 || projects.Projects[0].DisplayName != "Studio Ledger" {
			t.Fatalf("late cwd did not promote the provisional label: %+v", projects.Projects)
		}
		var inspected, inspectErr bytes.Buffer
		if code := run([]string{"projects", "inspect", projects.Projects[0].ID, "--data-dir", dataDir, "--from", "2000-01-01", "--to", "2100-01-01", "--format", "json"}, &inspected, &inspectErr); code != 0 {
			t.Fatalf("inspection exit=%d stderr=%q", code, inspectErr.String())
		}
		var result tokenlogger.ProjectInspection
		if err := json.Unmarshal(inspected.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.NameOrigin != "cwd" || result.HistoricalActivity.EventCount != 1 {
			t.Fatalf("late-cwd inspection = %+v", result)
		}
		if strings.Contains(inspected.String(), "Studio Ledger") || strings.Contains(inspected.String(), "private-late-cwd") {
			t.Fatalf("inspection exposed raw name/location: %s", inspected.String())
		}
	})
}

func TestProjectInspectionReportsConflictingAliasAsEvidenceOnly(t *testing.T) {
	store, err := tokenlogger.Open(filepath.Join(t.TempDir(), "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	rootA, rootB := t.TempDir(), t.TempDir()
	appendAlias := func(eventID, projectID, alias, root string, occurredAt time.Time) {
		t.Helper()
		event := cliReportEvent(eventID, occurredAt)
		event.ProjectID, event.SessionID = projectID, "session-"+eventID
		event.Client.Name = "codex-cli"
		event.Provenance.Adapter, event.Provenance.SourceID, event.Provenance.SourceRecord = "codex", "source-"+eventID, "record-"+eventID
		project := tokenlogger.Project{ID: projectID, Root: root, DisplayName: "Name-" + projectID, SourceAdapter: "codex", ProviderAlias: alias, AliasProvenance: "codex metadata"}
		session := tokenlogger.Session{ID: event.SessionID, ProjectID: projectID, Client: event.Client, SourceAdapter: "codex", ProviderAlias: event.SessionID}
		if _, err := store.AppendWithReferences([]tokenlogger.UsageEvent{event}, []tokenlogger.Project{project}, []tokenlogger.Session{session}); err != nil {
			t.Fatal(err)
		}
	}
	when := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	appendAlias("event-a", "stable-a", "stable-a", rootA, when)
	initialProjects, err := store.ListProjects(tokenlogger.ResourceFilter{From: when.Add(-time.Hour), To: when.Add(time.Hour)})
	if err != nil || len(initialProjects.Projects) != 1 {
		t.Fatalf("initial project inventory=%+v err=%v", initialProjects, err)
	}
	stableProjectID := initialProjects.Projects[0].ID
	appendAlias("event-b", "stable-b", "stable-b", rootB, when)
	appendAlias("event-a-conflict", "stable-a", "stable-a", rootB, when.Add(time.Minute))
	result, err := store.InspectProject(tokenlogger.ProjectInspectionRequest{
		ProjectID: stableProjectID, From: when.Add(-time.Hour), To: when.Add(time.Hour), Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.WarningsPage.Total == 0 || result.RecoveryClassification != tokenlogger.InspectionRecoveryReviewReconciliation {
		t.Fatalf("conflict was not retained as advisory evidence: warnings=%+v classification=%s", result.WarningsPage, result.RecoveryClassification)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"shared-provider-alias", "private-root", "stable-a", "stable-b"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("inspection exposed conflicting alias evidence %q: %s", private, encoded)
		}
	}
}

func TestInteractiveProjectInspectionUsesDisplayedRowsAndDoesNotRerender(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"row-project-a", "row-project-b"} {
		if err := store.UpsertProject(tokenlogger.Project{ID: id, DisplayName: id}); err != nil {
			t.Fatal(err)
		}
	}
	commands := "/range 2026-10-01 2026-10-10\n/projects\n/projects inspect 2\n/projects inspect 999\n/projects inspect row-project-b\n/project 2\n/quit\n"
	var stdout, stderr bytes.Buffer
	reader := &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader(commands)), output: io.Discard}
	if code := runInteractiveLoopWithClock(reader, &stdout, &stderr, store, dataDir, defaultSettings(dataDir), false, func() time.Time { return now }); code != 0 {
		t.Fatalf("interactive exit=%d stderr=%q", code, stderr.String())
	}
	output := stdout.String()
	if count := strings.Count(output, "Project inspection (format"); count != 2 {
		t.Fatalf("row and full-ID inspections rendered %d results: %s", count, output)
	}
	for _, want := range []string{"[2026-10-01T00:00:00Z, 2026-10-10T00:00:00Z)", "project ID digest:", "PROJECT: row-project-b", "row is unavailable in the currently displayed project list", projectInventoryNote} {
		if !strings.Contains(output, want) {
			t.Errorf("interactive output missing %q: %s", want, output)
		}
	}
	if strings.Contains(stderr.String(), "collector started") {
		t.Fatalf("inspection started an unexpected collector: %s", stderr.String())
	}
}

func TestInteractiveProjectInspectionRejectsUnavailableRowWithoutReload(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	commands := "/projects inspect 1\n/projects inspect 999\n/projects inspect\n/quit\n"
	var stdout, stderr bytes.Buffer
	reader := &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader(commands)), output: io.Discard}
	if code := runInteractiveLoop(reader, &stdout, &stderr, store, dataDir, defaultSettings(dataDir), false); code != 0 {
		t.Fatalf("interactive exit=%d stderr=%q", code, stderr.String())
	}
	for _, want := range []string{"currently displayed project list", "usage: /projects [inspect", "NO USAGE DATA"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("interactive output missing %q: %s", want, stdout.String())
		}
	}
}
