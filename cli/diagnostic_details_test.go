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

func seedDetailedDiagnostics(t *testing.T, dataDir string) []tokenlogger.DiagnosticDetail {
	t.Helper()
	root := filepath.Join(dataDir, "private-source-directory-marker")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"private-source-one.jsonl", "private-source-two.jsonl"} {
		path := filepath.Join(root, name)
		valid := `{"type":"assistant","uuid":"fixture-` + string(rune('a'+i)) + `","sessionId":"session-` + string(rune('a'+i)) + `","cwd":"/synthetic/project","timestamp":"2026-10-11T12:00:00Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":5}}}`
		fixture := valid + "\n{private-malformed-provider-data-" + string(rune('a'+i)) + "}\n"
		if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	collector, err := tokenlogger.NewCollector(store, []string{root}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.PollOnce(); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListDiagnosticDetails(tokenlogger.DiagnosticQuery{PageSize: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) < 2 {
		t.Fatalf("got %d detailed findings; want distinct source evidence", len(page.Items))
	}
	pending, err := store.PendingDiagnostics()
	if err != nil {
		t.Fatal(err)
	}
	for _, diagnostic := range pending {
		if err := store.MarkDiagnosticReviewed(diagnostic.Fingerprint, "skipped"); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func TestDiagnosticDetailsListShowPaginationAndReview(t *testing.T) {
	dataDir := t.TempDir()
	items := seedDetailedDiagnostics(t, dataDir)
	var stdout, stderr bytes.Buffer
	args := []string{"diagnostics", "list", "--data-dir", dataDir, "--page-size", "1", "--format", "json"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("list exit=%d stderr=%q", code, stderr.String())
	}
	var first tokenlogger.DiagnosticPage
	if err := json.Unmarshal(stdout.Bytes(), &first); err != nil {
		t.Fatalf("decode first page: %v\n%s", err, stdout.String())
	}
	if first.ContractVersion != 1 || len(first.Items) != 1 || first.NextToken == "" {
		t.Fatalf("first page lost version or continuation: %+v", first)
	}
	if first.Items[0].Lifetime.Value == "" || first.Items[0].Lifetime.Availability == "" {
		t.Fatalf("exact decimal count or availability missing: %+v", first.Items[0].Lifetime)
	}
	stdout.Reset()
	stderr.Reset()
	args = []string{"diagnostics", "list", "--data-dir", dataDir, "--page-size", "1", "--continuation", first.NextToken, "--format", "json"}
	if code := run(args, &stdout, &stderr); code != 0 {
		t.Fatalf("second page exit=%d stderr=%q", code, stderr.String())
	}
	var second tokenlogger.DiagnosticPage
	if err := json.Unmarshal(stdout.Bytes(), &second); err != nil || len(second.Items) != 1 || second.Items[0].ProblemID == first.Items[0].ProblemID {
		t.Fatalf("second page did not continue: page=%+v err=%v", second, err)
	}
	problemID := items[0].ProblemID
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"diagnostics", "show", problemID, "--data-dir", dataDir, "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("show exit=%d stderr=%q", code, stderr.String())
	}
	var detail tokenlogger.DiagnosticDetail
	if err := json.Unmarshal(stdout.Bytes(), &detail); err != nil || detail.ProblemID != problemID || detail.ContractVersion != 1 {
		t.Fatalf("show response = %+v err=%v", detail, err)
	}
	stdout.Reset()
	stderr.Reset()
	revision := ""
	for _, item := range items {
		if item.ProblemID == problemID {
			revision = item.Review.EvidenceRevision
		}
	}
	if revision == "" {
		t.Fatal("seed detail missing evidence revision")
	}
	if code := run([]string{"diagnostics", "review", problemID, "--state", "reviewed", "--expected-revision", revision, "--data-dir", dataDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("review exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "marked reviewed") || !strings.Contains(stdout.String(), "does not change its operational condition") {
		t.Fatalf("review output conflates review and condition: %s", stdout.String())
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	updated, err := store.InspectDiagnosticDetail(problemID)
	if err != nil || updated.Review.State != tokenlogger.DiagnosticReviewReviewed || updated.Condition.State != detail.Condition.State {
		t.Fatalf("review changed condition or failed to persist: review=%s condition=%s/%s err=%v", updated.Review.State, detail.Condition.State, updated.Condition.State, err)
	}
	legacy, err := store.RecordDiagnostic("private legacy diagnostic message")
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"diagnostics", "show", legacy.Fingerprint, "--data-dir", dataDir}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "detail not found") {
		t.Fatalf("legacy fingerprint was accepted as detailed identity: exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDiagnosticDetailsHumanProjectionAndPrivacy(t *testing.T) {
	dataDir := t.TempDir()
	items := seedDetailedDiagnostics(t, dataDir)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"diagnostics", "show", items[0].ProblemID, "--data-dir", dataDir}, &stdout, &stderr); code != 0 {
		t.Fatalf("show exit=%d stderr=%q", code, stderr.String())
	}
	for _, expected := range []string{"condition:", "review:", "latest_poll=", "latest_observed_run=", "lifetime=", "review does not resolve"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("human detail missing %q: %s", expected, stdout.String())
		}
	}
	for _, private := range []string{"private-malformed-provider-data", "private-source-one", "private-source-two", filepath.Join(dataDir, "private-source-directory-marker")} {
		if strings.Contains(stdout.String(), private) {
			t.Errorf("diagnostic projection leaked %q: %s", private, stdout.String())
		}
	}
	if strings.Contains(stdout.String(), "token=secret") || strings.Contains(stdout.String(), "message:") {
		t.Fatalf("diagnostic projection exposed raw message data: %s", stdout.String())
	}
}

func TestDiagnosticDetailsArgumentAndTypedErrorHandling(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"diagnostics", "show"}, "requires a problem ID"},
		{[]string{"diagnostics", "review", "id", "--state", "pending"}, "review state must be reviewed or skipped"},
		{[]string{"diagnostics", "list", "--page-size", "101"}, "page size must be between"},
		{[]string{"diagnostics", "list", "--format", "csv"}, "unsupported format"},
		{[]string{"diagnostics", "list", "--continuation", strings.Repeat("x", tokenlogger.MaxDiagnosticContinuationBytes+1)}, "continuation token exceeds"},
	}
	for _, test := range cases {
		var stdout, stderr bytes.Buffer
		if code := run(test.args, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), test.want) {
			t.Errorf("args=%v exit=%d stderr=%q; want status 1 containing %q", test.args, code, stderr.String(), test.want)
		}
	}
	var stderr bytes.Buffer
	if code := writeDiagnosticCoreError(&stderr, "review diagnostic", tokenlogger.ErrDiagnosticReviewConflict); code != 2 || !strings.Contains(stderr.String(), "inspect it again") {
		t.Fatalf("review conflict mapping exit=%d message=%q", code, stderr.String())
	}
	stderr.Reset()
	if code := writeDiagnosticCoreError(&stderr, "inspect diagnostic", tokenlogger.ErrDiagnosticInspectionUnavailable); code != 2 || !strings.Contains(stderr.String(), "unavailable") {
		t.Fatalf("unavailable evidence mapping exit=%d message=%q", code, stderr.String())
	}
	dataDir := t.TempDir()
	stderr.Reset()
	if code := run([]string{"diagnostics", "list", "--condition", "private-invalid-condition", "--data-dir", dataDir}, io.Discard, &stderr); code != 2 || !strings.Contains(stderr.String(), "invalid filter") || strings.Contains(stderr.String(), "private-invalid-condition") {
		t.Fatalf("malformed core filter error was unsafe: exit=%d stderr=%q", code, stderr.String())
	}
}

func TestDiagnosticProjectionRejectsUnknownVersionAndSanitizesUnknownEnums(t *testing.T) {
	if supportedDiagnosticPage(tokenlogger.DiagnosticPage{ContractVersion: 99}) {
		t.Fatal("accepted an unsupported core diagnostic contract version")
	}
	var output bytes.Buffer
	detail := tokenlogger.DiagnosticDetail{
		ContractVersion: tokenlogger.DiagnosticContractVersion,
		ProblemID:       "id\x1b[31m\n" + strings.Repeat("x", 120),
		Classification: tokenlogger.DiagnosticClassification{
			Category: tokenlogger.DiagnosticCategory("future\x1b\n" + strings.Repeat("z", 100)),
			Reason:   tokenlogger.DiagnosticReason("unknown_reason"),
		},
		Condition:                tokenlogger.DiagnosticConditionEvidence{State: tokenlogger.DiagnosticCondition("future_state")},
		Review:                   tokenlogger.DiagnosticReview{State: tokenlogger.DiagnosticReviewPending},
		LatestPoll:               tokenlogger.DiagnosticObservationCount{Count: tokenlogger.DiagnosticExactCount{Value: "1", Availability: tokenlogger.DiagnosticAvailability("bad\x1b\n" + strings.Repeat("a", 80))}},
		Lifetime:                 tokenlogger.DiagnosticExactCount{Value: "18446744073709551615", Availability: tokenlogger.DiagnosticAvailabilityAvailable, Saturated: true, Truncated: true},
		EvidenceAvailability:     tokenlogger.DiagnosticAvailability("bad-evidence\x1b"),
		LegacyDetailAvailability: tokenlogger.DiagnosticAvailability("bad-legacy\n"),
	}
	if err := writeDiagnosticDetailText(&output, detail); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "\nunknown_reason") || strings.Contains(output.String(), "bad-evidence") || strings.Contains(output.String(), "bad-legacy") || !strings.Contains(output.String(), "18446744073709551615") || !strings.Contains(output.String(), "truncated=true") {
		t.Fatalf("human projection is unsafe or lost exact qualifiers: %q", output.String())
	}
	encoded, err := json.Marshal(tokenlogger.DiagnosticExactCount{Value: "18446744073709551615", Availability: tokenlogger.DiagnosticAvailabilityAvailable})
	if err != nil || string(encoded) != `{"value":"18446744073709551615","availability":"available"}` {
		t.Fatalf("exact uint64 decimal string changed during JSON: %s err=%v", encoded, err)
	}
}

func TestDiagnosticListHumanOutputIsBoundedAndSanitized(t *testing.T) {
	page := tokenlogger.DiagnosticPage{
		ContractVersion:      tokenlogger.DiagnosticContractVersion,
		EvidenceAvailability: tokenlogger.DiagnosticAvailability("private\x1b\n" + strings.Repeat("x", 200)),
		OverflowCount:        tokenlogger.DiagnosticExactCount{Value: strings.Repeat("9", 200), Availability: tokenlogger.DiagnosticAvailability("bad\x1b"), Saturated: true},
		NextToken:            strings.Repeat("\x1b\n", tokenlogger.MaxDiagnosticContinuationBytes/2),
		Items:                make([]tokenlogger.DiagnosticDetail, 100),
	}
	for i := range page.Items {
		page.Items[i] = tokenlogger.DiagnosticDetail{
			ContractVersion: tokenlogger.DiagnosticContractVersion,
			ProblemID:       strings.Repeat("i", 400),
			Classification:  tokenlogger.DiagnosticClassification{Category: tokenlogger.DiagnosticCategory(strings.Repeat("c", 200)), Reason: tokenlogger.DiagnosticReason(strings.Repeat("r", 200))},
			Condition:       tokenlogger.DiagnosticConditionEvidence{State: tokenlogger.DiagnosticCondition(strings.Repeat("s", 100))},
			Review:          tokenlogger.DiagnosticReview{State: tokenlogger.DiagnosticReviewState(strings.Repeat("v", 100))},
			Lifetime:        tokenlogger.DiagnosticExactCount{Value: strings.Repeat("7", 200), Availability: tokenlogger.DiagnosticAvailability("private\x1b")},
		}
	}
	var output bytes.Buffer
	if err := writeDiagnosticPageText(&output, page); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b") || strings.Contains(output.String(), "private") || len(output.Bytes()) > 32*1024 {
		t.Fatalf("page output was not bounded/sanitized: bytes=%d output=%q", output.Len(), output.String())
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if len(line) > 2048 {
			t.Fatalf("human line exceeds bound (%d bytes)", len(line))
		}
	}
	if strings.Count(output.String(), "condition=") != maxHumanDiagnosticListRows {
		t.Fatalf("human row cap not applied: %d rows", strings.Count(output.String(), "condition="))
	}
}

func TestInteractiveDiagnosticCommandsUseOpenStore(t *testing.T) {
	dataDir := t.TempDir()
	items := seedDetailedDiagnostics(t, dataDir)
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var stdout, stderr bytes.Buffer
	code := runDiagnosticDetailsForStore([]string{"show", items[0].ProblemID}, &stdout, &stderr, discardApplicationLog{}, store)
	if code != 0 || !strings.Contains(stdout.String(), "Diagnostic detail") || stderr.Len() != 0 {
		t.Fatalf("interactive detail exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	settings := defaultSettings(dataDir)
	settings.OnboardingComplete = true
	code = runInteractiveScanner(bufio.NewScanner(strings.NewReader("/diagnostics list --page-size 1\n/quit\n")), &stdout, &stderr, store, dataDir, settings, false)
	if code != 0 || !strings.Contains(stdout.String(), "Diagnostic details") {
		t.Fatalf("interactive list exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	code = runInteractiveScanner(bufio.NewScanner(strings.NewReader("/diagnostics review "+items[0].ProblemID+" reviewed --expected-revision "+items[0].Review.EvidenceRevision+"\n/quit\n")), &stdout, &stderr, store, dataDir, settings, false)
	if code != 0 || !strings.Contains(stdout.String(), "marked reviewed") {
		t.Fatalf("interactive review exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestDiagnosticReviewRejectsEvidenceChangedSinceInspection(t *testing.T) {
	dataDir := t.TempDir()
	items := seedDetailedDiagnostics(t, dataDir)
	item := items[0]
	root := filepath.Join(dataDir, "private-source-directory-marker")
	path := filepath.Join(root, "private-source-one.jsonl")
	valid := `{"type":"assistant","uuid":"fixture-a","sessionId":"session-a","cwd":"/synthetic/project","timestamp":"2026-10-11T12:00:00Z","message":{"role":"assistant","model":"claude-sonnet-5","usage":{"input_tokens":10,"output_tokens":5}}}`
	if err := os.WriteFile(path, []byte(valid+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	collector, err := tokenlogger.NewCollector(store, []string{root}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.PollOnce(); err != nil {
		t.Fatal(err)
	}
	current, err := store.InspectDiagnosticDetail(item.ProblemID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Review.EvidenceRevision == item.Review.EvidenceRevision {
		t.Fatalf("fixture did not advance material evidence revision: old=%q current=%q", item.Review.EvidenceRevision, current.Review.EvidenceRevision)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagnostics", "review", item.ProblemID, "--state", "reviewed", "--expected-revision", item.Review.EvidenceRevision, "--data-dir", dataDir}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "evidence changed during review") {
		t.Fatalf("stale inspected revision exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	store, err = tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	current, err = store.InspectDiagnosticDetail(item.ProblemID)
	if err != nil || current.Review.State != tokenlogger.DiagnosticReviewPending {
		t.Fatalf("stale revision changed review state: review=%s err=%v", current.Review.State, err)
	}
}

func TestDiagnosticReviewAllowsCountOnlyRecurrenceAtInspectedRevision(t *testing.T) {
	dataDir := t.TempDir()
	items := seedDetailedDiagnostics(t, dataDir)
	item := items[0]
	root := filepath.Join(dataDir, "private-source-directory-marker")
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	collector, err := tokenlogger.NewCollector(store, []string{root}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collector.PollOnce(); err != nil {
		t.Fatal(err)
	}
	current, err := store.InspectDiagnosticDetail(item.ProblemID)
	if err != nil || current.Review.EvidenceRevision != item.Review.EvidenceRevision {
		t.Fatalf("count-only recurrence changed material revision: old=%q current=%q err=%v", item.Review.EvidenceRevision, current.Review.EvidenceRevision, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"diagnostics", "review", item.ProblemID, "--state", "reviewed", "--expected-revision", item.Review.EvidenceRevision, "--data-dir", dataDir}, &stdout, &stderr)
	if code != 0 || !strings.Contains(stdout.String(), "marked reviewed") {
		t.Fatalf("count-only recurrence review exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestInteractiveDiagnosticCommandsRejectDataDirOverride(t *testing.T) {
	dataDir := t.TempDir()
	items := seedDetailedDiagnostics(t, dataDir)
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var stdout, stderr bytes.Buffer
	code := runDiagnosticDetailsForStore([]string{"review", items[0].ProblemID, "--state", "reviewed", "--expected-revision", items[0].Review.EvidenceRevision, "--data-dir", filepath.Join(dataDir, "other")}, &stdout, &stderr, discardApplicationLog{}, store)
	if code != 1 || !strings.Contains(stderr.String(), "cannot be used from the REPL") {
		t.Fatalf("data-dir override exit=%d stderr=%q", code, stderr.String())
	}
	detail, err := store.InspectDiagnosticDetail(items[0].ProblemID)
	if err != nil || detail.Review.State != tokenlogger.DiagnosticReviewPending {
		t.Fatalf("rejected override changed review state: review=%s err=%v", detail.Review.State, err)
	}
}

func TestDiagnosticCommandHelpAndCompletion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help", "diagnostics"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "diagnostics list") || !strings.Contains(stdout.String(), "next_token") {
		t.Fatalf("diagnostics help exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"completion", "bash"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "diagnostics:list") || !strings.Contains(stdout.String(), "--review-state") {
		t.Fatalf("bash diagnostics completion exit=%d stderr=%q", code, stderr.String())
	}
	stdout.Reset()
	if code := run([]string{"completion", "fish"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "__fish_seen_subcommand_from diagnostics") {
		t.Fatalf("fish diagnostics completion exit=%d stderr=%q", code, stderr.String())
	}
}
