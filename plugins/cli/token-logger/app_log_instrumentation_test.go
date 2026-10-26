// Copyright 2026 Ayesh Almeida
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

type recordedAppEvent struct {
	event   appLogEvent
	code    appLogCode
	command appCommand
	exit    int
}

type recordingApplicationLog struct {
	mu        sync.Mutex
	events    []recordedAppEvent
	summaries []recordedSummary
	missing   bool
}

type recordedSummary struct {
	command    appCommand
	status     appLogCode
	durationMS int64
	latest     appLogOutcomeCounts
	cumulative appLogOutcomeCounts
}

func (logger *recordingApplicationLog) Event(event appLogEvent, code appLogCode, command appCommand, exit int) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.events = append(logger.events, recordedAppEvent{event, code, command, exit})
}

func (logger *recordingApplicationLog) CollectorSummary(command appCommand, status appLogCode, durationMS int64, latest, cumulative appLogOutcomeCounts) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.summaries = append(logger.summaries, recordedSummary{command, status, durationMS, latest, cumulative})
}

func (logger *recordingApplicationLog) Unavailable() bool { return logger.missing }
func (logger *recordingApplicationLog) Close()            {}

func (logger *recordingApplicationLog) has(event appLogEvent, code appLogCode) bool {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	for _, got := range logger.events {
		if got.event == event && got.code == code {
			return true
		}
	}
	return false
}

func (logger *recordingApplicationLog) summarySnapshot() []recordedSummary {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	return append([]recordedSummary(nil), logger.summaries...)
}

func TestInteractiveDatabaseOpenFailureIsLoggedBeforeStoreExists(t *testing.T) {
	const marker = "PRIVATE_DATA_DIR_SENTINEL"
	root := filepath.Join(t.TempDir(), marker)
	if err := os.MkdirAll(filepath.Join(root, "token-logger.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	t.Setenv("TOKEN_LOGGER_DATA_DIR", root)
	t.Setenv("TOKEN_LOGGER_LOG_DIR", logDir)
	logger, err := newEventLogger(logDir, "v0.11.0", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := runWithInputAndLogger(nil, strings.NewReader(""), &stdout, &stderr, logger); code != 2 {
		t.Fatalf("exit code = %d; want 2", code)
	}
	logger.Close()
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"code":"database_open"`) || !strings.Contains(string(contents), `"msg":"data_directory.resolved"`) {
		t.Fatalf("log missing safe resolution/open evidence: %s", contents)
	}
	if strings.Contains(string(contents), marker) || strings.Contains(string(contents), root) || strings.Contains(string(contents), "database is a directory") {
		t.Fatalf("log contains uncontrolled input or path: %s", contents)
	}
}

func TestSettingsPersistenceFailureUsesControlledCode(t *testing.T) {
	logger := &recordingApplicationLog{}
	dataDir := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(dataDir, []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := runSettingsWithLogger([]string{"--data-dir", dataDir, "--collector", "on"}, io.Discard, io.Discard, logger); code != 2 {
		t.Fatalf("exit code = %d; want 2", code)
	}
	if !logger.has(appLogCommandFailed, appLogCodeSettingsUpdate) {
		t.Fatalf("settings update failure was not classified: %+v", logger.events)
	}
}

func TestWatchSettingsFallbackIsLoggedWithoutClaimingCommandFailure(t *testing.T) {
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, "settings.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	logger := &recordingApplicationLog{}
	if interval := configuredWatchIntervalWithLogger(dataDir, logger, appCommandWatch); interval != 5*time.Second {
		t.Fatalf("fallback interval = %s; want 5s", interval)
	}
	if !logger.has(appLogSettingsLoadFailed, appLogCodeSettingsLoad) || logger.has(appLogCommandFailed, appLogCodeSettingsLoad) {
		t.Fatalf("non-fatal settings fallback was misclassified: %+v", logger.events)
	}
}

func TestInteractiveSettingsLoadAndOnboardingPersistenceFailuresAreLogged(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("TOKEN_LOGGER_DATA_DIR", dataDir)
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dataDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	loadLog := &recordingApplicationLog{}
	if code := runWithInputAndLogger(nil, strings.NewReader(""), io.Discard, io.Discard, loadLog); code != 2 {
		t.Fatalf("settings load exit code = %d; want 2", code)
	}
	if !loadLog.has(appLogCommandFailed, appLogCodeSettingsLoad) {
		t.Fatalf("settings load failure was not classified: %+v", loadLog.events)
	}

	if err := os.Remove(settingsPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settingsPath+".lock", 0o700); err != nil {
		t.Fatal(err)
	}
	onboardingLog := &recordingApplicationLog{}
	if code := runWithInputAndLogger(nil, strings.NewReader("y\nn\n"), io.Discard, io.Discard, onboardingLog); code != 2 {
		t.Fatalf("onboarding persistence exit code = %d; want 2", code)
	}
	if !onboardingLog.has(appLogCommandFailed, appLogCodeOnboardingSave) {
		t.Fatalf("onboarding persistence failure was not classified: %+v", onboardingLog.events)
	}
}

func TestHarnessDatabaseOpenFailurePreservesProtocolOutputAndLogsSafely(t *testing.T) {
	const marker = "PRIVATE_HARNESS_PATH_SENTINEL"
	dataDir := filepath.Join(t.TempDir(), marker)
	if err := os.MkdirAll(filepath.Join(dataDir, "token-logger.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(initializeParams{DataDir: dataDir})
	if err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(protocolRequest{
		Protocol: "komposyn.token-logger", Version: protocolVersion, ID: "init", Method: "initialize", Params: params,
	})
	if err != nil {
		t.Fatal(err)
	}
	input := strings.NewReader(string(request) + "\n")
	logDir := t.TempDir()
	logger, err := newEventLogger(logDir, "v0.11.0", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if code := runProtocolWithLogger(input, &stdout, &stderr, logger); code != 0 {
		t.Fatalf("harness exit code = %d; stderr=%s", code, stderr.String())
	}
	logger.Close()
	if !strings.Contains(stdout.String(), `"code":"initialize_failed"`) || !strings.Contains(stdout.String(), `"version":1`) {
		t.Fatalf("protocol-v1 failure response changed: %s", stdout.String())
	}
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"code":"database_open"`) || strings.Contains(string(contents), marker) || strings.Contains(string(contents), dataDir) {
		t.Fatalf("harness log missing safe open classification or contains path: %s", contents)
	}
}

func TestImportFailureLogsOneControlledEventWithoutSourceContent(t *testing.T) {
	const marker = "PRIVATE_IMPORT_SOURCE_SENTINEL"
	root := t.TempDir()
	source := filepath.Join(root, marker+".jsonl")
	if err := os.WriteFile(source, []byte(`{"type":"PRIVATE_PROVIDER_RECORD_SENTINEL"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	logDir := t.TempDir()
	logger, err := newEventLogger(logDir, "v0.11.0", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	code := runImportWithDetectorAndLogger([]string{source, "--data-dir", filepath.Join(root, "data"), "--format", "json"}, &stdout, &stderr,
		func(path string) (string, error) {
			// Reproduce a source disappearing after discovery/detection but before
			// the adapter opens it, without relying on host filesystem timing.
			if err := os.Remove(path); err != nil {
				return "", err
			}
			return "claude", nil
		}, logger)
	logger.Close()
	if code != 3 {
		t.Fatalf("import exit code = %d; want 3; stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(contents), `"code":"import_source"`); got != 1 {
		t.Fatalf("want one aggregate import-source failure event, got %d: %s", got, contents)
	}
	if strings.Contains(string(contents), marker) || strings.Contains(string(contents), "PRIVATE_PROVIDER_RECORD_SENTINEL") || strings.Contains(string(contents), source) {
		t.Fatalf("log contains source path or content: %s", contents)
	}
	if !strings.Contains(stdout.String(), `"failed_sources":1`) || !strings.Contains(stdout.String(), `"format_version":1`) {
		t.Fatalf("import protocol output changed: %s", stdout.String())
	}
}

func TestReportQueryAndRenderFailuresUseDistinctCodes(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	queryLog := &recordingApplicationLog{}
	if code := runReportWithLogger("usage", []string{"--data-dir", dataDir, "--group-by", "PRIVATE_FILTER_SENTINEL"}, io.Discard, io.Discard, queryLog); code != 2 {
		t.Fatalf("query exit code = %d; want 2", code)
	}
	if !queryLog.has(appLogCommandFailed, appLogCodeReportQuery) {
		t.Fatalf("query failure not recorded: %+v", queryLog.events)
	}
	const filterMarker = "PRIVATE_FILTER_SENTINEL"
	logDir := t.TempDir()
	fileLogger, err := newEventLogger(logDir, "v0.11.0", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = runReportWithLogger("usage", []string{"--data-dir", dataDir, "--group-by", filterMarker}, io.Discard, io.Discard, fileLogger)
	fileLogger.Close()
	logBytes, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logBytes), filterMarker) {
		t.Fatalf("raw filter value escaped into local log: %s", logBytes)
	}

	renderLog := &recordingApplicationLog{}
	if code := runReportWithLogger("usage", []string{"--data-dir", dataDir}, failingWriter{}, io.Discard, renderLog); code != 2 {
		t.Fatalf("render exit code = %d; want 2", code)
	}
	if !renderLog.has(appLogCommandFailed, appLogCodeReportRender) {
		t.Fatalf("render failure not recorded: %+v", renderLog.events)
	}
	privacyLogDir := t.TempDir()
	privacyLogger, err := newEventLogger(privacyLogDir, "v0.11.0", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = runReportWithLogger("usage", []string{"--data-dir", dataDir}, failingWriter{}, io.Discard, privacyLogger)
	privacyLogger.Close()
	logBytes, err = os.ReadFile(filepath.Join(privacyLogDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logBytes), "PRIVATE_RENDER_ERROR") || strings.Contains(string(logBytes), dataDir) {
		t.Fatalf("renderer error or data path escaped into local log: %s", logBytes)
	}
}

func TestInteractiveReportWriterFailureIsLoggedSafely(t *testing.T) {
	const marker = "PRIVATE_INTERACTIVE_RENDER_SENTINEL"
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append([]tokenlogger.UsageEvent{cliReportEvent("interactive-render", time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))}); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	defer store.Close()
	logDir := t.TempDir()
	logger, err := newEventLogger(logDir, "v0.11.0", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	reader := &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader("/quit\n")), output: io.Discard}
	var stderr strings.Builder
	code := runInteractiveLoopWithClockAndRootsAndLogger(reader, failingWriter{message: marker}, &stderr, store, dataDir, defaultSettings(dataDir), false,
		func() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }, nil, logger)
	logger.Close()
	if code != 0 {
		t.Fatalf("writer failure changed REPL exit code: %d stderr=%q", code, stderr.String())
	}
	contents, err := os.ReadFile(filepath.Join(logDir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), `"code":"report_render"`) || strings.Contains(string(contents), `"exit_code"`) || strings.Contains(string(contents), marker) {
		t.Fatalf("interactive renderer failure was not safely classified: %s", contents)
	}
}

type failingWriter struct{ message string }

func (writer failingWriter) Write([]byte) (int, error) {
	if writer.message != "" {
		return 0, errors.New(writer.message)
	}
	return 0, errors.New("PRIVATE_RENDER_ERROR")
}

func TestDetachedLaunchAndReadinessFailuresAreLogged(t *testing.T) {
	dataDir := t.TempDir()
	root := filepath.Join(dataDir, "synthetic-root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RegisterCollectorSource(root, true); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	args := []string{"--data-dir", dataDir}
	launchLog := &recordingApplicationLog{}
	if code := runWatchStartWithLauncher(args, io.Discard, io.Discard, launchLog, func(string, []string) error { return errors.New("PRIVATE_LAUNCH_ERROR") }, time.Second, time.Millisecond); code != 2 {
		t.Fatalf("launch exit code = %d; want 2", code)
	}
	if !launchLog.has(appLogCommandFailed, appLogCodeProcessLaunch) {
		t.Fatalf("launch failure not recorded: %+v", launchLog.events)
	}

	readinessLog := &recordingApplicationLog{}
	if code := runWatchStartWithLauncher(args, io.Discard, io.Discard, readinessLog, func(string, []string) error { return nil }, 2*time.Millisecond, time.Millisecond); code != 2 {
		t.Fatalf("readiness exit code = %d; want 2", code)
	}
	if !readinessLog.has(appLogCollectorFailed, appLogCodeCollectorReadiness) {
		t.Fatalf("readiness failure not recorded: %+v", readinessLog.events)
	}
}

func TestCollectorSummarySeparatesLatestAndCumulativeScopes(t *testing.T) {
	logger := &recordingApplicationLog{}
	latest := tokenlogger.ImportResult{Receipt: tokenlogger.Receipt{Imported: 2, Duplicates: 3}}
	latest.Outcomes.MalformedRecord = 7
	cumulative := tokenlogger.CollectorHealth{Imported: 8, Duplicates: 11}
	cumulative.Outcomes.MalformedRecord = 21
	lastStatus := ""
	lastSummary := time.Time{}
	logCollectorHealthAndSummary(logger, appCommandWatch, "degraded", 19, latest, cumulative, &lastStatus, &lastSummary)
	if !logger.has(appLogCollectorHealthChanged, appLogCodeCollectorHealthDegraded) {
		t.Fatalf("health transition missing: %+v", logger.events)
	}
	if len(logger.summaries) != 1 {
		t.Fatalf("summary count = %d; want one", len(logger.summaries))
	}
	got := logger.summaries[0]
	if got.latest.Imported != 2 || got.latest.Duplicates != 3 || got.latest.MalformedRecord != 7 || got.cumulative.Imported != 8 || got.cumulative.Duplicates != 11 || got.cumulative.MalformedRecord != 21 {
		t.Fatalf("summary scopes conflated or lost: %+v", got)
	}
	logCollectorHealthAndSummary(logger, appCommandWatch, "degraded", 19, latest, cumulative, &lastStatus, &lastSummary)
	if len(logger.events) != 1 || len(logger.summaries) != 1 {
		t.Fatalf("unchanged health generated repeated event: events=%+v summaries=%+v", logger.events, logger.summaries)
	}
}

func TestEmbeddedCollectorSummaryOmitsUnmeasuredDuration(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	logger := &recordingApplicationLog{}
	runtime, err := startInteractiveCollectorWithLogger(store, nil, 5*time.Second, logger)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for len(logger.summarySnapshot()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	runtime.stop()
	summaries := logger.summarySnapshot()
	if len(summaries) == 0 {
		t.Fatal("embedded collector did not emit its first completed-poll summary")
	}
	if summaries[0].durationMS >= 0 {
		t.Fatalf("embedded collector claimed an unmeasured poll duration: %+v", summaries[0])
	}
}

func TestCollectorSummaryEncodingIsBoundedAndOmitsUnknownDuration(t *testing.T) {
	dir := t.TempDir()
	logger, err := newEventLogger(dir, "v0.11.0", tokenlogger.CoreVersion(), 4321, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	logger.CollectorSummary(appCommandWatch, appLogCodeCollectorHealthDegraded, -1,
		appLogOutcomeCounts{Imported: -2, FailedSource: maxLoggedOutcomeCount + 99},
		appLogOutcomeCounts{Imported: 12, MalformedRecord: 4})
	logger.Close()
	contents, err := os.ReadFile(filepath.Join(dir, "klg.log"))
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := json.Unmarshal(contents, &event); err != nil {
		t.Fatalf("decode summary: %v (%s)", err, contents)
	}
	if event["msg"] != "collector.summary" || event["code"] != "collector_health_degraded" || event["level"] != "WARN" {
		t.Fatalf("unexpected summary envelope: %#v", event)
	}
	if _, ok := event["poll_duration_ms"]; ok {
		t.Fatalf("unknown duration serialized as a known value: %#v", event)
	}
	if event["latest_poll_imported"] != float64(0) || event["latest_poll_failed_source"] != float64(maxLoggedOutcomeCount) || event["run_cumulative_imported"] != float64(12) || event["run_cumulative_malformed_record"] != float64(4) {
		t.Fatalf("summary counters missing, unbounded, or mis-scoped: %#v", event)
	}
}

func TestUnexpectedDeadlineIsNotSuppressedAsCancellation(t *testing.T) {
	active := context.Background()
	if expectedCollectorCancellation(active, context.DeadlineExceeded) {
		t.Fatal("bare deadline was treated as normal cancellation")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if !expectedCollectorCancellation(cancelled, context.Canceled) {
		t.Fatal("cancellation from the operation context was not treated as expected")
	}
	if expectedCollectorCancellation(cancelled, context.DeadlineExceeded) {
		t.Fatal("deadline error was hidden by an unrelated context cancellation")
	}
	if !expectedCollectorPollCancellation(active, context.Canceled, true) {
		t.Fatal("cooperative stop cancellation was not recognized")
	}
	if expectedCollectorPollCancellation(active, context.Canceled, false) {
		t.Fatal("cancellation without operation or cooperative-stop evidence was treated as expected")
	}
	if expectedCollectorPollCancellation(active, context.DeadlineExceeded, true) {
		t.Fatal("cooperative stop hid an unrelated poll deadline")
	}
}

func TestPollFailureEpisodeLogsOnceUntilRecovery(t *testing.T) {
	logger := &recordingApplicationLog{}
	active := false
	for range 5 {
		logPollFailureEpisode(logger, appCommandWatch, context.Background(), context.DeadlineExceeded, nil, false, &active)
	}
	if count := len(logger.events); count != 1 || logger.events[0].code != appLogCodeCollectorPoll {
		t.Fatalf("continuous failure generated %d poll events: %+v", count, logger.events)
	}
	logPollFailureEpisode(logger, appCommandWatch, context.Background(), nil, nil, false, &active)
	logPollFailureEpisode(logger, appCommandWatch, context.Background(), context.DeadlineExceeded, nil, false, &active)
	if count := len(logger.events); count != 2 {
		t.Fatalf("recovered failure episode was not logged once: %+v", logger.events)
	}
}
