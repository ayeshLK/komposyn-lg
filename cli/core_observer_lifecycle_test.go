// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func copyCoreObserverFixture(t *testing.T, provider, target string, malformed bool) string {
	t.Helper()
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "providers", provider, "normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if malformed {
		fixture = append(fixture, []byte(`{"type":"event_msg","payload":{"type":`+"\n")...)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target, "normal.jsonl")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func newObservedTestLogger(t *testing.T, dir string) *eventLogger {
	t.Helper()
	logger, err := newEventLogger(dir, "0.11.0-dev", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(logger.Close)
	return logger
}

func managedCoreRecords(t *testing.T, dir string) []logReadRecord {
	t.Helper()
	result, err := readManagedLogs(dir, logReadOptions{Limit: maxLogReadLimit})
	if err != nil {
		t.Fatal(err)
	}
	return result.Records
}

func requireCoreRecord(t *testing.T, records []logReadRecord, code string) logReadRecord {
	t.Helper()
	for _, record := range records {
		if record.Event == "core.diagnostic" && record.Code == code {
			return record
		}
	}
	t.Fatalf("missing core diagnostic code %q in records: %+v", code, records)
	return logReadRecord{}
}

func TestCoreObservationCoversNormalAndMalformedProviderImports(t *testing.T) {
	for _, provider := range []string{"claude-code", "codex"} {
		for _, malformed := range []bool{false, true} {
			name := "valid"
			if malformed {
				name = "malformed"
			}
			t.Run(provider+"/"+name, func(t *testing.T) {
				logDir := t.TempDir()
				logger := newObservedTestLogger(t, logDir)
				path := copyCoreObserverFixture(t, provider, t.TempDir(), malformed)
				store, err := openStoreWithLog(filepath.Join(t.TempDir(), "usage.sqlite"), logger, appCommandImport)
				if err != nil {
					t.Fatal(err)
				}
				var result tokenlogger.ImportResult
				switch provider {
				case "claude-code":
					result, err = (tokenlogger.ClaudeAdapter{}).Import(path, store)
				case "codex":
					result, err = (tokenlogger.CodexAdapter{}).Import(path, store)
				}
				if err != nil {
					_ = store.Close()
					t.Fatalf("import synthetic %s fixture: result=%+v err=%v", provider, result, err)
				}
				if !malformed && result.Receipt.Imported == 0 {
					_ = store.Close()
					t.Fatalf("valid %s fixture imported no usage: %+v", provider, result)
				}
				if malformed && result.Outcomes.MalformedRecord == 0 {
					_ = store.Close()
					t.Fatalf("malformed %s fixture produced no malformed outcome: %+v", provider, result)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
				logger.Close()
				records := managedCoreRecords(t, logDir)
				if malformed {
					core := requireCoreRecord(t, records, "core_condition_summary")
					if core.CoreDetails["core_reason"] != "invalid_json" {
						t.Fatalf("malformed fixture classification=%+v", core.CoreDetails)
					}
				} else {
					for _, record := range records {
						if record.Event == "core.diagnostic" && record.Code == "core_operation_failed" {
							t.Fatalf("successful %s import produced operation failure: %+v", provider, record)
						}
					}
				}
				encoded := strings.Builder{}
				for _, record := range records {
					encoded.WriteString(record.Event)
					encoded.WriteString(record.Code)
					for key, value := range record.CoreDetails {
						encoded.WriteString(key)
						encoded.WriteString(value)
					}
				}
				for _, private := range []string{filepath.Dir(path), "normal.jsonl", "event_msg"} {
					if strings.Contains(encoded.String(), private) {
						t.Fatalf("structured diagnostics leaked source content %q: %s", private, encoded.String())
					}
				}
			})
		}
	}
}

func waitForCoreConditionRecord(t *testing.T, logger *eventLogger, logDir string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, record := range managedCoreRecords(t, logDir) {
			if record.Event == "core.diagnostic" && record.Code == "core_condition_summary" {
				return
			}
		}
		if logger.coreObserver.Stats().Admitted > 0 {
			// Admission establishes that core observed work; give the bounded
			// drain/sink pipeline a chance to make that record readable.
			time.Sleep(2 * time.Millisecond)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("collector did not project a core condition summary; observer=%+v records=%+v", logger.coreObserver.Stats(), managedCoreRecords(t, logDir))
}

func TestCoreObservationCoversForegroundEmbeddedAndDetachedCollectors(t *testing.T) {
	t.Run("foreground", func(t *testing.T) {
		dataDir, logDir := t.TempDir(), t.TempDir()
		root := t.TempDir()
		copyCoreObserverFixture(t, "codex", root, true)
		logger := newObservedTestLogger(t, logDir)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		finished := false
		t.Cleanup(func() {
			cancel()
			if !finished {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("foreground collector did not exit during test cleanup")
				}
			}
		})
		go func() {
			done <- runForegroundWatchWithLogger([]string{root, "--data-dir", dataDir, "--interval", "1h"}, io.Discard, io.Discard, ctx, false, logger)
		}()
		waitForCoreConditionRecord(t, logger, logDir)
		cancel()
		select {
		case code := <-done:
			finished = true
			if code != 0 {
				t.Fatalf("foreground collector exit=%d", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("foreground collector did not stop after cancellation")
		}
		logger.Close()
		requireCoreRecord(t, managedCoreRecords(t, logDir), "core_condition_summary")
	})

	t.Run("embedded", func(t *testing.T) {
		dataDir, logDir := t.TempDir(), t.TempDir()
		root := t.TempDir()
		copyCoreObserverFixture(t, "codex", root, true)
		logger := newObservedTestLogger(t, logDir)
		store, err := openStoreWithLog(filepath.Join(dataDir, "usage.sqlite"), logger, appCommandInteractive)
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := startInteractiveCollectorWithLogger(store, []tokenlogger.CollectorRoot{{Path: root, Required: true}}, time.Hour, logger)
		if err != nil {
			_ = store.Close()
			t.Fatal(err)
		}
		t.Cleanup(runtime.stop)
		waitForCoreConditionRecord(t, logger, logDir)
		runtime.stop()
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		logger.Close()
		requireCoreRecord(t, managedCoreRecords(t, logDir), "core_condition_summary")
	})

	t.Run("detached worker", func(t *testing.T) {
		dataDir, logDir := t.TempDir(), t.TempDir()
		root := t.TempDir()
		copyCoreObserverFixture(t, "codex", root, true)
		setup, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := setup.RegisterCollectorSource(root, true); err != nil {
			_ = setup.Close()
			t.Fatal(err)
		}
		if err := setup.Close(); err != nil {
			t.Fatal(err)
		}
		logger := newObservedTestLogger(t, logDir)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan int, 1)
		finished := false
		t.Cleanup(func() {
			cancel()
			if !finished {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Error("detached collector did not exit during test cleanup")
				}
			}
		})
		go func() {
			done <- runWatchServeWithLogger([]string{"--data-dir", dataDir, "--interval", "1h", "--instance", "synthetic-worker"}, io.Discard, ctx, logger)
		}()
		waitForCoreConditionRecord(t, logger, logDir)
		cancel()
		select {
		case code := <-done:
			finished = true
			if code != 0 {
				t.Fatalf("detached worker exit=%d", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("detached worker did not stop after cancellation")
		}
		logger.Close()
		requireCoreRecord(t, managedCoreRecords(t, logDir), "core_condition_summary")
	})
}

const issue57DetachedDiagWorkerEnv = "KOMPOSYN_ISSUE57_DIAGNOSTIC_WORKER"

// TestIssue57DetachedDiagnosticWorkerHelper runs inside a separate test
// process. It creates its own logger before opening the collector Store, as a
// detached executable does.
func TestIssue57DetachedDiagnosticWorkerHelper(t *testing.T) {
	if os.Getenv(issue57DetachedDiagWorkerEnv) != "1" {
		return
	}
	dataDir, logDir := os.Getenv("KOMPOSYN_ISSUE57_DATA_DIR"), os.Getenv("KOMPOSYN_ISSUE57_LOG_DIR")
	logger, err := newEventLogger(logDir, "0.11.0-dev", tokenlogger.CoreVersion(), os.Getpid(), "")
	if err != nil {
		t.Fatal(err)
	}
	code := runWatchServeWithLogger([]string{"--data-dir", dataDir, "--interval", "1h", "--instance", "issue57-detached-worker"}, io.Discard, context.Background(), logger)
	logger.Close()
	if code != 0 {
		t.Fatalf("detached worker exit=%d", code)
	}
}

func TestDetachedWorkerOwnLoggerObservesCoreCollectorDiagnostics(t *testing.T) {
	dataDir, logDir := t.TempDir(), t.TempDir()
	root := t.TempDir()
	copyCoreObserverFixture(t, "codex", root, true)
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

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestIssue57DetachedDiagnosticWorkerHelper$")
	command.Env = append(os.Environ(), issue57DetachedDiagWorkerEnv+"=1", "KOMPOSYN_ISSUE57_DATA_DIR="+dataDir, "KOMPOSYN_ISSUE57_LOG_DIR="+logDir)
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	t.Cleanup(func() {
		if !waited {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if records, readErr := readManagedLogs(logDir, logReadOptions{Limit: maxLogReadLimit}); readErr == nil {
			for _, record := range records.Records {
				if record.Event == "core.diagnostic" && record.Code == "core_condition_summary" {
					goto stopWorker
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("detached process did not write core diagnostic; records=%+v", managedCoreRecords(t, logDir))

stopWorker:
	store, err = tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RequestCollectorStop(); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		waited = true
		if err != nil {
			t.Fatalf("detached helper process: %v", err)
		}
	case <-time.After(5 * time.Second):
		_ = command.Process.Kill()
		<-done
		waited = true
		t.Fatal("detached worker did not stop cooperatively")
	}
	records := managedCoreRecords(t, logDir)
	requireCoreRecord(t, records, "core_condition_summary")
	var started, stopped bool
	for _, record := range records {
		started = started || record.Event == "collector.started"
		stopped = stopped || record.Event == "collector.stopped"
	}
	if !started || !stopped {
		t.Fatalf("detached worker lifecycle evidence missing: started=%t stopped=%t records=%+v", started, stopped, records)
	}
}

func TestCoreObserverExpectedCancellationAndLeaseOwnership(t *testing.T) {
	logDir := t.TempDir()
	logger := newObservedTestLogger(t, logDir)
	store, err := openStoreWithLog(filepath.Join(t.TempDir(), "usage.sqlite"), logger, appCommandWatch)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AcquireCollectorLease("existing-owner", os.Getpid(), cliVersion, "foreground", time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := startInteractiveCollectorWithLogger(store, nil, time.Second, logger); !errors.Is(err, tokenlogger.ErrCollectorActive) {
		t.Fatalf("second collector error=%v, want active lease", err)
	}

	root := t.TempDir()
	copyCoreObserverFixture(t, "codex", root, true)
	collector, err := tokenlogger.NewCollector(store, []string{root}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, pollErr := collector.PollOnceContext(ctx)
	if pollErr == nil || !errors.Is(pollErr, context.Canceled) || result.Outcomes.FailedSource != 0 {
		t.Fatalf("expected cooperative cancellation result=%+v err=%v", result, pollErr)
	}
	active := false
	if logPollFailureEpisode(logger, appCommandWatch, ctx, pollErr, nil, false, &active) || active {
		t.Fatal("expected cancellation was logged as an unexpected collector failure")
	}
	logger.Close()
	records := managedCoreRecords(t, logDir)
	for _, record := range records {
		if record.Event == "command.failed" && record.Code == "collector_poll" {
			t.Fatalf("expected cancellation emitted generic failure: %+v", record)
		}
	}
}

func TestCoreObserverDeduplicatesEmbeddedLeaseAcquisitionFailure(t *testing.T) {
	logDir := t.TempDir()
	logger := newObservedTestLogger(t, logDir)
	dbPath := filepath.Join(t.TempDir(), "usage.sqlite")
	store, err := openStoreWithLog(dbPath, logger, appCommandInteractive)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	triggerDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := triggerDB.Exec(`CREATE TRIGGER reject_collector_runtime BEFORE UPDATE ON collector_runtime BEGIN SELECT RAISE(ABORT, 'synthetic private lease failure'); END`); err != nil {
		_ = triggerDB.Close()
		t.Fatal(err)
	}
	if err := triggerDB.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := startInteractiveCollectorWithLogger(store, []tokenlogger.CollectorRoot{{Path: t.TempDir()}}, time.Second, logger); err == nil {
		t.Fatal("embedded collector acquired a lease despite the rejecting trigger")
	}
	logger.Close()
	records := managedCoreRecords(t, logDir)
	coreFailures, genericAcquireFailures := 0, 0
	for _, record := range records {
		if record.Event == "core.diagnostic" && record.Code == "core_operation_failed" && record.CoreDetails["core_operation"] == "acquire_lease" {
			coreFailures++
		}
		if record.Event == "collector.failed" && record.Code == "collector_acquire" {
			genericAcquireFailures++
		}
	}
	if coreFailures != 1 || genericAcquireFailures != 0 {
		t.Fatalf("lease acquisition produced duplicate or missing failure representations: core=%d generic=%d records=%+v", coreFailures, genericAcquireFailures, records)
	}
}

func TestCoreObserverDeduplicatesEmbeddedPollPersistenceFailures(t *testing.T) {
	for _, tc := range []struct {
		name    string
		trigger string
	}{
		{
			name: "begin",
			trigger: `CREATE TRIGGER reject_poll_begin BEFORE UPDATE ON collector_runtime
				WHEN NEW.last_poll_at IS NOT NULL AND NEW.last_poll_at IS NOT OLD.last_poll_at
				BEGIN SELECT RAISE(ABORT, 'synthetic private poll begin failure'); END`,
		},
		{
			name: "complete",
			trigger: `CREATE TRIGGER reject_poll_complete BEFORE UPDATE ON collector_runtime
				WHEN NEW.last_completed_at IS NOT NULL AND OLD.last_completed_at IS NULL
				BEGIN SELECT RAISE(ABORT, 'synthetic private poll complete failure'); END`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logDir := t.TempDir()
			logger := newObservedTestLogger(t, logDir)
			dbPath := filepath.Join(t.TempDir(), "usage.sqlite")
			store, err := openStoreWithLog(dbPath, logger, appCommandInteractive)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			installCollectorRuntimeTrigger(t, dbPath, tc.trigger)

			if tc.name == "begin" {
				if _, err := startInteractiveCollectorWithLogger(store, nil, time.Second, logger); err == nil {
					t.Fatal("embedded collector poll began despite rejecting trigger")
				}
			} else {
				runtime, err := startInteractiveCollectorWithLogger(store, nil, time.Second, logger)
				if err != nil {
					t.Fatalf("start embedded collector before complete failure: %v", err)
				}
				runtime.stop()
			}

			logger.Close()
			records := managedCoreRecords(t, logDir)
			coreFailures, genericStoreFailures := 0, 0
			for _, record := range records {
				if record.Event == "core.diagnostic" && record.Code == "core_operation_failed" && record.CoreDetails["core_operation"] == "heartbeat" {
					coreFailures++
				}
				if record.Event == "collector.failed" && record.Code == "collector_store" {
					genericStoreFailures++
				}
			}
			if coreFailures != 1 || genericStoreFailures != 0 {
				t.Fatalf("%s poll persistence failure was duplicated or lost: core=%d generic=%d records=%+v", tc.name, coreFailures, genericStoreFailures, records)
			}
		})
	}
}

func installCollectorRuntimeTrigger(t *testing.T, dbPath, statement string) {
	t.Helper()
	triggerDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := triggerDB.Exec(statement); err != nil {
		_ = triggerDB.Close()
		t.Fatal(err)
	}
	if err := triggerDB.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestCoreObserverConcurrentFailuresRaceBoundedClose(t *testing.T) {
	logDir := t.TempDir()
	logger := newObservedTestLogger(t, logDir)
	observer := logger.CoreObserver(appCommandImport)
	const workers, operations = 8, 12
	start := make(chan struct{})
	first := make(chan struct{}, workers)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			for operation := 0; operation < operations; operation++ {
				_, err := tokenlogger.OpenWithObservation("", tokenlogger.OpenOptions{}, observer)
				if err == nil {
					t.Error("empty database path unexpectedly opened")
					return
				}
				logger.CoreFailure(err, appLogCommandFailed, appLogCodeDatabaseOpen, appCommandImport, 2)
				if operation == 0 {
					first <- struct{}{}
				}
			}
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		select {
		case <-first:
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent core operations did not start")
		}
	}
	started := time.Now()
	logger.Close()
	if elapsed := time.Since(started); elapsed > logCloseBudget+100*time.Millisecond {
		t.Fatalf("concurrent observer teardown exceeded shared close budget: %s", elapsed)
	}
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent core producers remained blocked after logger close")
	}
	if observer.Stats().Capacity > tokenlogger.MaxDiagnosticObserverCapacity || len(logger.coreFallbacks) > maxLogKeyState {
		t.Fatalf("observer or fallback state exceeded bounds: observer=%+v fallbacks=%d", observer.Stats(), len(logger.coreFallbacks))
	}
}
