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
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func TestDiagnosticExportRequiresReviewedExplicitSelectionAndConsent(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	page, err := store.ListDiagnosticDetails(tokenlogger.DiagnosticQuery{})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("list details = %d, err=%v", len(page.Items), err)
	}
	detail := page.Items[0]
	if detail.Review.State != tokenlogger.DiagnosticReviewPending {
		t.Fatalf("synthetic diagnostic review state = %q, want pending", detail.Review.State)
	}
	if _, err := prepareDiagnosticExport(store, []string{problemID}, logReadResult{}, nil); err == nil {
		t.Fatal("pending diagnostic became exportable")
	}
	if err := store.ReviewDiagnosticDetail(tokenlogger.DiagnosticReviewRequest{
		ProblemID: detail.ProblemID, ExpectedEvidenceRevision: detail.Review.EvidenceRevision,
		State: tokenlogger.DiagnosticReviewReviewed,
	}); err != nil {
		t.Fatal(err)
	}
	preview, err := prepareDiagnosticExport(store, []string{problemID}, logReadResult{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "evidence.json")
	var shown bytes.Buffer
	err = writeReviewedDiagnosticExport(preview, destination, &shown, func() (bool, error) {
		if !bytes.Equal(shown.Bytes(), preview.Bytes()) {
			t.Fatal("consent requested before complete preview")
		}
		return false, nil
	})
	if !errors.Is(err, ErrDiagnosticExportConsent) {
		t.Fatalf("declined consent error = %v", err)
	}
	if !bytes.Equal(shown.Bytes(), preview.Bytes()) {
		t.Fatal("preview differed from output envelope")
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("declined export created destination: %v", err)
	}
	shown.Reset()
	if err := writeReviewedDiagnosticExport(preview, destination, &shown, func() (bool, error) {
		if !bytes.Equal(shown.Bytes(), preview.Bytes()) {
			t.Fatal("consent requested before complete preview")
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(shown.Bytes(), preview.Bytes()) {
		t.Fatal("confirmed preview differed from written envelope")
	}
	data, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(data, preview.Bytes()) {
		t.Fatalf("written envelope mismatch: %v", err)
	}
	if info, err := os.Stat(destination); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("export permissions = %v, err=%v; want 0600", info, err)
	}
}

func TestDiagnosticExportOmitsIdentifiersTimestampsOffsetsAndRawText(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	page, err := store.ListDiagnosticDetails(tokenlogger.DiagnosticQuery{})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("list details = %d, err=%v", len(page.Items), err)
	}
	detail := page.Items[0]
	if err := store.ReviewDiagnosticDetail(tokenlogger.DiagnosticReviewRequest{
		ProblemID: detail.ProblemID, ExpectedEvidenceRevision: detail.Review.EvidenceRevision,
		State: tokenlogger.DiagnosticReviewReviewed,
	}); err != nil {
		t.Fatal(err)
	}
	selectedLog := logReadRecord{Time: "2026-10-11T12:34:56Z", Level: "WARN", Event: "command.failed", CLI: "v1.2.3", Core: "v0.10.0", PID: 31415, RunID: "private-log-run", Command: "watch", Code: "report_query", File: "klg.log", fileRank: 0, lineOrder: 1}
	availableLogs := logReadResult{Available: true, Records: []logReadRecord{selectedLog}}
	preview, err := prepareDiagnosticExport(store, []string{problemID}, availableLogs, []logRecordRef{logRef(selectedLog)})
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(preview.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	encoded := string(preview.Bytes())
	for _, privateMarker := range []string{problemID, "diagnostic-session-marker", "diagnostic-source-marker", "/private/synthetic/path", "2026-10-11T12:34:56Z", "private-log-run", "31415", "klg.log", "byte_offset", "coverage_offset", "subject_reference", "process_id", "run_id", "message"} {
		if strings.Contains(encoded, privateMarker) {
			t.Errorf("export includes private marker/field %q: %s", privateMarker, encoded)
		}
	}
	if envelope["format_version"] != float64(1) || envelope["diagnostic_contract_version"] != float64(1) {
		t.Fatalf("export versions = %#v", envelope)
	}
	items, ok := envelope["diagnostics"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("diagnostics = %#v", envelope["diagnostics"])
	}
	logs, ok := envelope["logs"].([]any)
	if !ok || len(logs) != 1 {
		t.Fatalf("separately selected logs = %#v", envelope["logs"])
	}
}

func TestDiagnosticExportLogSelectionsResolveOnlyReaderIssuedRows(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	page, err := store.ListDiagnosticDetails(tokenlogger.DiagnosticQuery{})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("list details = %d, err=%v", len(page.Items), err)
	}
	detail := page.Items[0]
	if err := store.ReviewDiagnosticDetail(tokenlogger.DiagnosticReviewRequest{
		ProblemID: detail.ProblemID, ExpectedEvidenceRevision: detail.Review.EvidenceRevision,
		State: tokenlogger.DiagnosticReviewReviewed,
	}); err != nil {
		t.Fatal(err)
	}
	row := logReadRecord{Level: "WARN", Event: "command.failed", CLI: "v1.2.3", Core: "v0.10.0", Code: "report_query", File: "klg.log", fileRank: 0, lineOrder: 9}
	result := logReadResult{Records: []logReadRecord{row}}
	valid := logRef(row)
	spoofed := valid
	spoofed.File = "klg.log.3"
	if _, err := prepareDiagnosticExport(store, []string{problemID}, result, []logRecordRef{spoofed}); err == nil {
		t.Fatal("accepted a fabricated log record reference")
	}
	if _, err := prepareDiagnosticExport(store, []string{problemID}, result, []logRecordRef{valid, valid}); err == nil {
		t.Fatal("accepted a duplicate selected log record")
	}
}

func TestDiagnosticExportPreservesExactCountsAndRejectsUnknownValues(t *testing.T) {
	max := "18446744073709551615"
	got, err := projectExportCount(tokenlogger.DiagnosticExactCount{
		Value: max, Availability: tokenlogger.DiagnosticAvailabilityAvailable, Saturated: true, Truncated: true,
	})
	if err != nil || got.Value != max || !got.Saturated || !got.Truncated {
		t.Fatalf("exact count = %+v, err=%v", got, err)
	}
	for _, value := range []tokenlogger.DiagnosticExactCount{
		{Value: "01", Availability: tokenlogger.DiagnosticAvailabilityAvailable},
		{Value: "18446744073709551616", Availability: tokenlogger.DiagnosticAvailabilityAvailable},
		{Value: "1", Availability: tokenlogger.DiagnosticAvailabilityUnknown, Saturated: true},
		{Availability: tokenlogger.DiagnosticAvailabilityAvailable},
	} {
		if _, err := projectExportCount(value); err == nil {
			t.Errorf("accepted malformed/unavailable counter %+v", value)
		}
	}
	if _, err := projectSelectedLogForExport(logExportProjection{Event: "provider.response", Level: "INFO", Code: "secret", CLI: "v1.2.3", Core: "v0.10.0"}); err == nil {
		t.Fatal("accepted unknown log event")
	}
	if _, err := projectSelectedLogForExport(logExportProjection{Event: "command.failed", Level: "INFO", Code: "raw /private/path", CLI: "v1.2.3", Core: "v0.10.0"}); err == nil {
		t.Fatal("accepted uncontrolled log code")
	}
	if _, err := projectExportCoreLogFields(map[string]string{"core_contract_version": "1", "raw_path": "/private/path"}); err == nil {
		t.Fatal("accepted unknown core export field")
	}
	coreFields := map[string]string{
		"core_contract_version": "1", "core_kind": "operation_failed",
		"core_evidence_scope": "operation", "core_evidence_availability": "not_collected",
		"core_evidence_truncated": "false", "core_counts_scope": "operation",
		"core_counts_availability": "not_collected", "core_counts_saturated": "false", "core_counts_truncated": "false",
		"core_component": "unknown", "core_operation": "open", "core_reason": "failure_unclassified",
		"core_severity": "error", "core_category": "unknown",
		"core_retryability": "unknown", "core_impact": "unknown",
		"core_explanation_code": "explain_failure_unclassified", "core_remediation_code": "review_operation",
		"core_count_failed_source": "18446744073709551615",
	}
	selectedCoreLog := logReadRecord{Level: "ERROR", Event: "core.diagnostic", CLI: "v1.2.3", Core: "v0.10.0", Code: "core_operation_failed", File: "klg.log", fileRank: 0, lineOrder: 1, CoreDetails: coreFields}
	projection, err := projectSelectedLogForExport(mustSelectExportLog(t, selectedCoreLog))
	if err != nil || projection.Core["core_count_failed_source"] != "18446744073709551615" {
		t.Fatalf("validated core log projection = %+v, err=%v", projection, err)
	}
	coreFields["core_category"] = "security-sensitive"
	selectedCoreLog.CoreDetails = coreFields
	if _, err := projectSelectedLogForExport(mustSelectExportLog(t, selectedCoreLog)); err == nil {
		t.Fatal("security-sensitive core log evidence entered public export")
	}
	unavailable, err := projectExportCount(tokenlogger.DiagnosticExactCount{Availability: tokenlogger.DiagnosticAvailabilityUnknown, Truncated: true})
	if err != nil || unavailable.Value != "" || unavailable.Availability != string(tokenlogger.DiagnosticAvailabilityUnknown) || !unavailable.Truncated {
		t.Fatalf("unknown/truncated counter projection = %+v, err=%v", unavailable, err)
	}
}

func TestDiagnosticExportAcceptsWriterUnavailableEvidenceReason(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "0.11.0-dev", tokenlogger.CoreVersion(), os.Getpid(), "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := openStoreWithLog("", logger, appCommandImport); err == nil {
		t.Fatal("empty database path unexpectedly opened")
	}
	logger.Close()

	read, err := readManagedLogs(dir, logReadOptions{Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	var selected *logReadRecord
	for i := range read.Records {
		if read.Records[i].Event == "core.diagnostic" {
			selected = &read.Records[i]
			break
		}
	}
	if selected == nil {
		t.Fatalf("production core failure event missing: %+v", read.Records)
	}
	if got := selected.CoreDetails["core_evidence_unavailable_reason"]; got != string(tokenlogger.DiagnosticUnavailableNotCollected) {
		t.Fatalf("writer unavailable reason = %q", got)
	}

	projections, err := selectManagedLogExports(read, []logRecordRef{logRef(*selected)})
	if err != nil || len(projections) != 1 {
		t.Fatalf("managed selection failed: projections=%+v err=%v", projections, err)
	}
	exported, err := projectSelectedLogForExport(projections[0])
	if err != nil || exported.Core["core_evidence_unavailable_reason"] != string(tokenlogger.DiagnosticUnavailableNotCollected) {
		t.Fatalf("selected writer event was not exportable: %+v err=%v", exported, err)
	}

	fields := map[string]string{
		"core_contract_version": "1", "core_kind": "operation_failed",
		"core_evidence_scope": "operation", "core_evidence_availability": "not_collected",
		"core_evidence_unavailable_reason": "future_reason",
		"core_counts_scope":                "operation", "core_counts_availability": "not_collected",
	}
	if _, err := projectExportCoreLogFields(fields); err == nil {
		t.Fatal("unknown unavailable-evidence reason was accepted")
	}
}

func TestDiagnosticExportRejectsUnknownContractAndPrivateRouting(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	page, err := store.ListDiagnosticDetails(tokenlogger.DiagnosticQuery{})
	if err != nil || len(page.Items) == 0 {
		t.Fatalf("list details = %d, err=%v", len(page.Items), err)
	}
	detail := page.Items[0]
	if err := store.ReviewDiagnosticDetail(tokenlogger.DiagnosticReviewRequest{
		ProblemID: detail.ProblemID, ExpectedEvidenceRevision: detail.Review.EvidenceRevision,
		State: tokenlogger.DiagnosticReviewReviewed,
	}); err != nil {
		t.Fatal(err)
	}
	detail, err = store.InspectDiagnosticDetail(problemID)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		change func(*tokenlogger.DiagnosticDetail)
	}{
		{"unknown contract", func(d *tokenlogger.DiagnosticDetail) { d.ContractVersion = 99 }},
		{"security-sensitive", func(d *tokenlogger.DiagnosticDetail) {
			d.Classification.Category = tokenlogger.DiagnosticCategorySecuritySensitive
		}},
		{"unknown condition", func(d *tokenlogger.DiagnosticDetail) { d.Condition.State = "surprising" }},
		{"review predates evidence", func(d *tokenlogger.DiagnosticDetail) { d.Review.PredatesEvidence = true }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copy := detail
			test.change(&copy)
			if _, err := projectDiagnosticForExport(copy); err == nil {
				t.Fatal("invalid or private detail was exportable")
			}
		})
	}
	if _, err := prepareDiagnosticExport(store, []string{strings.Repeat("f", 64)}, logReadResult{}, nil); !errors.Is(err, ErrDiagnosticExportNotFound) {
		t.Fatalf("missing detail error = %v", err)
	}
}

func TestDiagnosticExportNoOverwriteAndPreviewFailure(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "existing.json")
	marker := []byte("leave-existing-file-alone")
	if err := os.WriteFile(destination, marker, 0o600); err != nil {
		t.Fatal(err)
	}
	preview := DiagnosticExportPreview{bytes: []byte("{}\n")}
	var out bytes.Buffer
	if err := writeReviewedDiagnosticExport(preview, destination, &out, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("existing destination was overwritten")
	}
	got, err := os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, marker) {
		t.Fatalf("existing destination changed: %q, err=%v", got, err)
	}
	if err := writeReviewedDiagnosticExport(preview, filepath.Join(dir, "no-preview.json"), failingExportPreviewWriter{}, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("preview writer failure was ignored")
	}
	if _, err := os.Lstat(filepath.Join(dir, "no-preview.json")); !os.IsNotExist(err) {
		t.Fatalf("preview failure created output: %v", err)
	}
	link := filepath.Join(dir, "symlink.json")
	if err := os.Symlink(destination, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := writeReviewedDiagnosticExport(preview, link, &out, func() (bool, error) { return true, nil }); err == nil {
		t.Fatal("symlink destination was followed")
	}
	got, err = os.ReadFile(destination)
	if err != nil || !bytes.Equal(got, marker) {
		t.Fatalf("symlink target changed: %q, err=%v", got, err)
	}
}

func TestDiagnosticExportPartialWriteIsRemoved(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "partial.json")
	file, err := openPrivateDiagnosticExportFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	created, err := file.Stat()
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := writeDiagnosticExportPayload(partialExportFile{File: file}, []byte("complete payload")); err == nil {
		t.Fatal("partial write succeeded")
	}
	if err := removeCreatedDiagnosticExportFile(destination, created); err != nil {
		t.Fatalf("remove incomplete output: %v", err)
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("partial destination remains: %v", err)
	}
}

type failingExportPreviewWriter struct{}

func (failingExportPreviewWriter) Write([]byte) (int, error) {
	return 0, errors.New("synthetic preview failure")
}

type partialExportFile struct{ *os.File }

func (partialExportFile) Write(data []byte) (int, error) {
	if len(data) > 4 {
		return 4, errors.New("synthetic disk write failure")
	}
	return len(data), nil
}

func mustSelectExportLog(t *testing.T, record logReadRecord) logExportProjection {
	t.Helper()
	projections, err := selectManagedLogExports(logReadResult{Records: []logReadRecord{record}}, []logRecordRef{logRef(record)})
	if err != nil || len(projections) != 1 {
		t.Fatalf("select actual reader record: projections=%d err=%v", len(projections), err)
	}
	return projections[0]
}

func seedExportDiagnostic(t *testing.T) (*tokenlogger.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "diagnostic-source-marker.jsonl")
	fixture := `{"type":"assistant","uuid":"diagnostic-session-marker","sessionId":"diagnostic-session-marker","cwd":"/private/synthetic/path","timestamp":"2026-10-11T12:34:56Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":5}}}` + "\n{not-json}\n"
	if err := os.WriteFile(source, []byte(fixture), 0o600); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	collector, err := tokenlogger.NewCollector(store, []string{dir}, time.Second)
	if err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if _, err := collector.PollOnce(); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	page, err := store.ListDiagnosticDetails(tokenlogger.DiagnosticQuery{})
	if err != nil || len(page.Items) == 0 {
		_ = store.Close()
		t.Fatalf("synthetic detail page = %+v, err=%v", page, err)
	}
	return store, page.Items[0].ProblemID
}
