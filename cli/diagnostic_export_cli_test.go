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

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func TestParseDiagnosticExportCLIArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr bool
	}{
		{"explicit ID and output", []string{"problem-full-id", "--output", "/tmp/share.json"}, false},
		{"multiple explicit IDs", []string{"id-one", "id-two", "--output", "share.json"}, false},
		{"requires ID", []string{"--output", "share.json"}, true},
		{"requires output", []string{"problem-id"}, true},
		{"options follow all IDs", []string{"problem-id", "--output", "share.json", "later-id"}, true},
		{"limit requires opt in", []string{"problem-id", "--output", "share.json", "--log-limit", "5"}, true},
		{"bounded log limit", []string{"problem-id", "--output", "share.json", "--include-logs", "--log-limit", "201"}, true},
		{"repeated flag", []string{"problem-id", "--output", "a", "--output", "b"}, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseDiagnosticExportCLIArgs(test.args)
			if (err != nil) != test.wantErr {
				t.Fatalf("parse error = %v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestParseDiagnosticExportREPLArgsPreservesQuotedAndWindowsPaths(t *testing.T) {
	input := `export abc --output "C:\Users\Example\Shared Files\evidence.json"`
	got, err := parseDiagnosticExportREPLArgs(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[3] != `C:\Users\Example\Shared Files\evidence.json` {
		t.Fatalf("REPL args = %#v", got)
	}
	if _, err := parseDiagnosticExportREPLArgs(`export abc --output "missing quote`); err == nil {
		t.Fatal("accepted an unclosed quoted argument")
	}
}

func TestDiagnosticExportCLIRequiresConsentAfterExactPreviewAndOmitsLogsByDefault(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	reviewExportDiagnostic(t, store, problemID)
	destination := filepath.Join(t.TempDir(), "reviewed.json")
	options := diagnosticExportCLIOptions{problemIDs: []string{problemID}, destination: destination}
	var stdout, stderr bytes.Buffer
	confirmCalls := 0
	code := runDiagnosticExportForStore(options, func(prompt string) (string, error) {
		confirmCalls++
		if !strings.Contains(prompt, "exact preview") {
			t.Fatalf("unexpected prompt: %q", prompt)
		}
		if _, err := os.Lstat(destination); !os.IsNotExist(err) {
			t.Fatalf("destination existed before affirmative consent: %v", err)
		}
		if !strings.Contains(stdout.String(), "Exact versioned export preview") || !strings.Contains(stdout.String(), `"format_version": 1`) {
			t.Fatalf("exact preview was not displayed before consent: %s", stdout.String())
		}
		return "yes", nil
	}, &stdout, &stderr, store)
	if code != 0 || confirmCalls != 1 {
		t.Fatalf("export code=%d confirmation calls=%d stderr=%s", code, confirmCalls, stderr.String())
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"diagnostics"`)) || bytes.Contains(data, []byte(`"logs"`)) {
		t.Fatalf("default export did not contain diagnostics only: %s", data)
	}
	previewStart := strings.Index(stdout.String(), "{\n")
	if previewStart < 0 || !bytes.Equal(data, []byte(stdout.String()[previewStart:])) {
		t.Fatalf("file differs from the exact preview shown on stdout: file=%s stdout=%s", data, stdout.String())
	}
	if strings.Contains(stderr.String(), problemID) || strings.Contains(stderr.String(), destination) {
		t.Fatalf("stderr leaked selected ID or path: %s", stderr.String())
	}
}

func TestDiagnosticExportCLIInputEOFDoesNotConsentOrCreateFile(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	reviewExportDiagnostic(t, store, problemID)
	destination := filepath.Join(t.TempDir(), "never-created.json")
	options := diagnosticExportCLIOptions{problemIDs: []string{problemID}, destination: destination}
	var stdout, stderr bytes.Buffer
	code := runDiagnosticExportForStore(options, inputDiagnosticExportAsk(strings.NewReader(""), &stderr), &stdout, &stderr, store)
	if code != 2 {
		t.Fatalf("EOF export code=%d, want failure without consent; stderr=%s", code, stderr.String())
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("EOF created destination: %v", err)
	}
}

func TestDiagnosticExportCLIIncludesOnlyExplicitlySelectedManagedLogRows(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	reviewExportDiagnostic(t, store, problemID)
	logDir := t.TempDir()
	t.Setenv("TOKEN_LOGGER_LOG_DIR", logDir)
	logger, err := newEventLogger(logDir, "v1.2.3", "v0.10.0", os.Getpid(), "00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatal(err)
	}
	logger.Event(appLogCommandFailed, appLogCodeReportQuery, appCommandOther, 2)
	logger.Close()
	destination := filepath.Join(t.TempDir(), "with-selected-log.json")
	options := diagnosticExportCLIOptions{problemIDs: []string{problemID}, destination: destination, includeLogs: true, logLimit: 10}
	var stdout, stderr bytes.Buffer
	answers := []string{"1", "y"}
	call := 0
	code := runDiagnosticExportForStore(options, func(string) (string, error) {
		if call >= len(answers) {
			return "", errors.New("unexpected extra prompt")
		}
		answer := answers[call]
		call++
		return answer, nil
	}, &stdout, &stderr, store)
	if code != 0 || call != 2 {
		t.Fatalf("export code=%d prompt calls=%d stderr=%s", code, call, stderr.String())
	}
	data, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	var envelope diagnosticExportEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Logs) != 1 || envelope.Logs[0].Event != "command.failed" {
		t.Fatalf("explicit selected log projection = %+v", envelope.Logs)
	}
	if !strings.Contains(stderr.String(), "Optional local log records") || !strings.Contains(stdout.String(), "Exact versioned export preview") {
		t.Fatalf("selection or exact preview was not shown: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestDiagnosticExportCLIREPLPromptsDisableHistory(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	reviewExportDiagnostic(t, store, problemID)
	destination := filepath.Join(t.TempDir(), "repl.json")
	reader := &exportPromptReader{answers: []string{"y"}}
	var stdout, stderr bytes.Buffer
	code := runDiagnosticExportForStoreArgs([]string{problemID, "--output", destination}, &stdout, &stderr, store, reader)
	if code != 0 || reader.disabled != 1 || reader.enabled != 1 || reader.reads != 1 {
		t.Fatalf("REPL export code=%d history disabled/enabled=%d/%d reads=%d stderr=%s", code, reader.disabled, reader.enabled, reader.reads, stderr.String())
	}
}

func TestDiagnosticExportLogSelectionIsExplicitAndReaderBounded(t *testing.T) {
	records := []logReadRecord{{File: "klg.log", fileRank: 0, lineOrder: 1}, {File: "klg.log.1", fileRank: 1, lineOrder: 9}}
	for _, test := range []struct {
		value string
		want  int
		bad   bool
	}{{"", 0, false}, {"2", 1, false}, {"1,2", 2, false}, {"0", 0, true}, {"3", 0, true}, {"1,1", 0, true}, {"1, nope", 0, true}} {
		refs, err := parseDiagnosticExportLogSelection(test.value, records)
		if (err != nil) != test.bad || len(refs) != test.want {
			t.Errorf("selection %q refs=%d err=%v", test.value, len(refs), err)
		}
		for _, ref := range refs {
			if ref.File == "" || ref.FileRank < 0 || ref.LineOrder == 0 {
				t.Errorf("selection created an invalid reader reference: %+v", ref)
			}
		}
	}
}

func TestDiagnosticExportCommandHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runHelp([]string{"diagnostics", "export", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("command help code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "diagnostics export <full-problem-id>") || !strings.Contains(stdout.String(), "exact bounded") {
		t.Fatalf("export help lacks command-specific consent instructions: %s", stdout.String())
	}
	var output bytes.Buffer
	_ = runHelp([]string{"help", "diagnostics", "export"}, &output, &stderr)
	if output.Len() == 0 {
		t.Fatal("explicit export help topic was empty")
	}
	store, err := tokenlogger.Open(filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reader := &exportCommandReader{lines: []string{"/help", "/diagnostics export --help", "/quit"}}
	output.Reset()
	if code := runInteractiveLoopWithLogger(reader, &output, &stderr, store, t.TempDir(), appSettings{}, false, discardApplicationLog{}); code != 0 {
		t.Fatalf("REPL export help code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(output.String(), "/diagnostics export <id>...") || !strings.Contains(output.String(), "diagnostics export <full-problem-id>") || !strings.Contains(output.String(), "exact bounded") {
		t.Fatalf("REPL /diagnostics export --help missing command-specific instructions: %s", output.String())
	}
}

func TestDiagnosticExportREPLAcceptsDestinationPathWithSpaces(t *testing.T) {
	store, problemID := seedExportDiagnostic(t)
	defer store.Close()
	reviewExportDiagnostic(t, store, problemID)
	destination := filepath.Join(t.TempDir(), "shared evidence.json")
	command := `/diagnostics export ` + problemID + ` --output "` + destination + `"`
	reader := &exportCommandReader{lines: []string{"n", command, "yes", "/quit"}}
	var stdout, stderr bytes.Buffer
	if code := runInteractiveLoopWithLogger(reader, &stdout, &stderr, store, t.TempDir(), appSettings{}, false, discardApplicationLog{}); code != 0 {
		t.Fatalf("REPL export code=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("quoted destination with spaces was not created: %v; stderr=%s", err, stderr.String())
	}
}

func reviewExportDiagnostic(t *testing.T, store *tokenlogger.Store, problemID string) {
	t.Helper()
	detail, err := store.InspectDiagnosticDetail(problemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReviewDiagnosticDetail(tokenlogger.DiagnosticReviewRequest{
		ProblemID: detail.ProblemID, ExpectedEvidenceRevision: detail.Review.EvidenceRevision,
		State: tokenlogger.DiagnosticReviewReviewed,
	}); err != nil {
		t.Fatal(err)
	}
}

type exportPromptReader struct {
	answers  []string
	reads    int
	disabled int
	enabled  int
}

type exportCommandReader struct {
	lines []string
	index int
}

func (r *exportCommandReader) ReadLine(string) (string, error) {
	if r.index >= len(r.lines) {
		return "", errors.New("unexpected extra command read")
	}
	line := r.lines[r.index]
	r.index++
	return line, nil
}
func (*exportCommandReader) DisableHistory()  {}
func (*exportCommandReader) EnableHistory()   {}
func (*exportCommandReader) IsTerminal() bool { return false }

func (r *exportPromptReader) ReadLine(string) (string, error) {
	if r.reads >= len(r.answers) {
		return "", errors.New("unexpected extra consent prompt")
	}
	answer := r.answers[r.reads]
	r.reads++
	return answer, nil
}
func (r *exportPromptReader) DisableHistory() { r.disabled++ }
func (r *exportPromptReader) EnableHistory()  { r.enabled++ }
func (*exportPromptReader) IsTerminal() bool  { return true }
