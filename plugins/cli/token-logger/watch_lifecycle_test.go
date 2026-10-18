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
	"context"
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

const lifecycleAppendRecord = `{"type":"event_msg","id":"lifecycle-token-count-3","timestamp":"2026-10-11T10:00:06Z","payload":{"type":"token_count","session_id":"codex-session-fixture","info":{"total_token_usage":{"input_tokens":200,"cached_input_tokens":30,"output_tokens":60,"reasoning_output_tokens":15,"total_tokens":260}}}}`

type lifecycleBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (buffer *lifecycleBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.Write(value)
}

func (buffer *lifecycleBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.Buffer.String()
}

func lifecycleFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("..", "..", "library", "token-logger", "testdata", "codex", "normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "normal.jsonl")
	if err := os.WriteFile(path, fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitLifecycleStatus(t *testing.T, store *tokenlogger.Store, condition func(tokenlogger.CollectorRuntimeStatus) bool) tokenlogger.CollectorRuntimeStatus {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var status tokenlogger.CollectorRuntimeStatus
	for time.Now().Before(deadline) {
		var err error
		status, err = store.CollectorRuntimeStatus(5 * time.Second)
		if err == nil && condition(status) {
			return status
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for collector status; last status=%+v", status)
	return status
}

func runForegroundLifecycle(t *testing.T, dataDir, source string, ready func(tokenlogger.CollectorRuntimeStatus) bool) tokenlogger.CollectorRuntimeStatus {
	t.Helper()
	store, stop := startForegroundLifecycle(t, dataDir, []string{source})
	status := waitLifecycleStatus(t, store, ready)
	stop()
	status, err := store.CollectorRuntimeStatus(time.Second)
	if err != nil || status.State != "stopped" {
		t.Fatalf("foreground final status=%+v err=%v", status, err)
	}
	return status
}

func startForegroundLifecycle(t *testing.T, dataDir string, roots []string) (*tokenlogger.Store, func()) {
	t.Helper()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	// Register the store first so the collector cleanup runs before the database closes.
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close foreground lifecycle store: %v", err)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	stdout, stderr := &lifecycleBuffer{}, &lifecycleBuffer{}
	go func() {
		args := append([]string{"foreground"}, roots...)
		args = append(args, "--interval", "10ms", "--data-dir", dataDir)
		done <- runWatchWithContext(args, stdout, stderr, ctx)
	}()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			select {
			case code := <-done:
				if code != 0 {
					t.Errorf("foreground collector exit code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
				}
			case <-time.After(5 * time.Second):
				t.Errorf("foreground collector did not stop after cancellation; stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
		})
	}
	t.Cleanup(stop)
	return store, stop
}

func TestPositionalForegroundAppendPendingTailAndRestart(t *testing.T) {
	dataDir := t.TempDir()
	source := lifecycleFixture(t)
	initial := runForegroundLifecycle(t, dataDir, source, func(status tokenlogger.CollectorRuntimeStatus) bool {
		return status.Imported == 2 && status.Outcomes.UnchangedSnapshot > 0 && status.LastCompletedAt != nil
	})
	if initial.IntervalMillis != 10 || initial.Mode != "foreground" {
		t.Fatalf("foreground did not preserve explicit 10ms interval and mode: %+v", initial)
	}

	store, stop := startForegroundLifecycle(t, dataDir, []string{source})
	waitLifecycleStatus(t, store, func(status tokenlogger.CollectorRuntimeStatus) bool {
		return status.Outcomes.UnchangedSnapshot > 0 && status.LastCompletedAt != nil
	})
	file, err := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(lifecycleAppendRecord[:len(lifecycleAppendRecord)-12]); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	waitLifecycleStatus(t, store, func(status tokenlogger.CollectorRuntimeStatus) bool {
		return status.Pending && status.Outcomes.DeferredIncompleteTail > 0
	})

	file, err = os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(lifecycleAppendRecord[len(lifecycleAppendRecord)-12:] + "\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	completed := waitLifecycleStatus(t, store, func(status tokenlogger.CollectorRuntimeStatus) bool {
		return status.Imported == 1 && !status.Pending && status.LastCompletedAt != nil
	})
	stop()
	completed, err = store.CollectorRuntimeStatus(time.Second)
	if err != nil || completed.State != "stopped" {
		t.Fatalf("foreground final status=%+v err=%v", completed, err)
	}
	if completed.Outcomes.DeferredIncompleteTail == 0 {
		t.Fatalf("run history lost pending-tail outcome: %+v", completed)
	}
	restarted := runForegroundLifecycle(t, dataDir, source, func(status tokenlogger.CollectorRuntimeStatus) bool {
		return status.Outcomes.UnchangedSnapshot > 0 && status.LastCompletedAt != nil
	})
	if restarted.Imported != 0 {
		t.Fatalf("restart reimported unchanged source records: %+v", restarted)
	}

	store, err = tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if count, err := store.EventCount(); err != nil || count != 3 {
		t.Fatalf("event count after completing tail=%d err=%v, want 3", count, err)
	}
	registered, err := store.ListCollectorSources(false)
	if err != nil || len(registered) != 0 {
		t.Fatalf("positional foreground mutated the source registry: sources=%+v err=%v", registered, err)
	}
}

func TestPositionalForegroundDeduplicatesOverlappingRoots(t *testing.T) {
	dataDir := t.TempDir()
	root := t.TempDir()
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "library", "token-logger", "testdata", "codex", "normal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(child, "normal.jsonl"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	store, stop := startForegroundLifecycle(t, dataDir, []string{root, child})
	status := waitLifecycleStatus(t, store, func(status tokenlogger.CollectorRuntimeStatus) bool {
		count, countErr := store.EventCount()
		return countErr == nil && count == 2 && status.DiscoveredSources == 1
	})
	if status.SourceCount != 2 {
		t.Fatalf("overlapping roots were not retained as configured roots: %+v", status)
	}
	stop()
	registered, err := store.ListCollectorSources(false)
	if err != nil || len(registered) != 0 {
		t.Fatalf("overlapping positional roots mutated the source registry: sources=%+v err=%v", registered, err)
	}
}

func TestEmbeddedCollectorSyntheticRootKeepsRegistrySeparate(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close embedded lifecycle store: %v", err)
		}
	})
	runtime, err := startInteractiveCollector(store, []tokenlogger.CollectorRoot{{Path: filepath.Dir(lifecycleFixture(t)), Required: true}}, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(runtime.stop) }
	t.Cleanup(stop)
	waitLifecycleStatus(t, store, func(status tokenlogger.CollectorRuntimeStatus) bool {
		count, countErr := store.EventCount()
		return countErr == nil && count == 2 && status.Mode == "embedded" && status.LastCompletedAt != nil
	})
	stop()
	status, err := store.CollectorRuntimeStatus(time.Second)
	if err != nil || status.State != "stopped" || status.Mode != "embedded" {
		t.Fatalf("embedded collector final status=%+v err=%v", status, err)
	}
	sources, err := store.ListCollectorSources(false)
	if err != nil || len(sources) != 0 {
		t.Fatalf("embedded collector mutated registered roots: sources=%+v err=%v", sources, err)
	}
}

type lifecycleGatePoller struct {
	started chan struct{}
	release chan struct{}
}

func (poller *lifecycleGatePoller) PollOnceContext(ctx context.Context) (tokenlogger.ImportResult, error) {
	close(poller.started)
	select {
	case <-ctx.Done():
		return tokenlogger.ImportResult{}, ctx.Err()
	case <-poller.release:
		return tokenlogger.ImportResult{}, nil
	}
}

func TestCollectorLeaseHeartbeatSurvivesLongPoll(t *testing.T) {
	store, err := tokenlogger.Open(filepath.Join(t.TempDir(), "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close heartbeat lifecycle store: %v", err)
		}
	})
	const staleAfter = 4 * time.Second
	if _, err := store.AcquireCollectorLease("slow-poll", 1, tokenlogger.Version, "background", 100*time.Millisecond, staleAfter); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCollectorReady("slow-poll"); err != nil {
		t.Fatal(err)
	}
	before, err := store.CollectorRuntimeStatus(staleAfter)
	if err != nil || before.HeartbeatAt == nil {
		t.Fatalf("initial heartbeat=%+v err=%v", before, err)
	}
	poller := &lifecycleGatePoller{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var renewed tokenlogger.CollectorRuntimeStatus
	go func() {
		_, pollErr, heartbeatErr, stop := pollWithLeaseHeartbeat(ctx, store, "slow-poll", poller, staleAfter)
		if heartbeatErr != nil {
			done <- heartbeatErr
		} else if stop {
			done <- errors.New("unexpected stop request")
		} else {
			done <- pollErr
		}
	}()
	var releaseOnce sync.Once
	joined := false
	t.Cleanup(func() {
		if joined {
			return
		}
		releaseOnce.Do(func() { close(poller.release) })
		cancel()
		select {
		case pollErr := <-done:
			joined = true
			if pollErr != nil && !errors.Is(pollErr, context.Canceled) {
				t.Errorf("join gated heartbeat poll during cleanup: %v", pollErr)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("gated heartbeat poll did not finish during cleanup; latest status=%+v", renewed)
		}
	})
	select {
	case <-poller.started:
	case <-time.After(5 * time.Second):
		t.Fatal("gated heartbeat poll did not start")
	}
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		renewed, err = store.CollectorRuntimeStatus(staleAfter)
		if err == nil && renewed.HeartbeatAt != nil && renewed.HeartbeatAt.After(*before.HeartbeatAt) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if renewed.Stale || renewed.HeartbeatAt == nil || !renewed.HeartbeatAt.After(*before.HeartbeatAt) {
		t.Fatalf("heartbeat did not keep a long poll's lease fresh: %+v err=%v", renewed, err)
	}
	firstRenewal := *renewed.HeartbeatAt
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		renewed, err = store.CollectorRuntimeStatus(staleAfter)
		if err == nil && renewed.HeartbeatAt != nil && renewed.HeartbeatAt.After(firstRenewal) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || renewed.HeartbeatAt == nil || !renewed.HeartbeatAt.After(firstRenewal) || renewed.Stale || renewed.State != "running" {
		t.Fatalf("lease expired during long poll without repeated heartbeat: status=%+v err=%v", renewed, err)
	}
	releaseOnce.Do(func() { close(poller.release) })
	select {
	case pollErr := <-done:
		joined = true
		cancel()
		if pollErr != nil {
			t.Fatalf("long poll returned error: %v", pollErr)
		}
	case <-time.After(time.Second):
		t.Fatal("long poll did not finish after release")
	}
}

func TestWatchStopReportsTimeoutWhenLeaseOwnerDoesNotStop(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AcquireCollectorLease("unresponsive", 1, tokenlogger.Version, "background", time.Second, time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkCollectorReady("unresponsive"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"watch", "stop", "--timeout", "1ms", "--data-dir", dataDir}, &stdout, &stderr); code != 3 {
		t.Fatalf("watch stop exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "did not stop before timeout") {
		t.Fatalf("watch stop did not report timeout: %q", stderr.String())
	}
	store = openStoreWhileCollectorStarts(t, filepath.Join(dataDir, "token-logger.db"))
	defer store.Close()
	status, err := store.CollectorRuntimeStatus(time.Minute)
	if err != nil || status.State != "stopping" || status.StopRequestedAt == nil {
		t.Fatalf("timed-out stop did not preserve requested-stop state: %+v err=%v", status, err)
	}
	if err := store.ReleaseCollectorLease("unresponsive", "stopped", ""); err != nil {
		t.Fatal(err)
	}
}

func registerDetachedStopCleanup(t *testing.T, store *tokenlogger.Store, instance, closedMarker string) func() {
	t.Helper()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			deadline := time.Now().Add(15 * time.Second)
			requested := false
			var last tokenlogger.CollectorRuntimeStatus
			for time.Now().Before(deadline) {
				if _, markerErr := os.Stat(closedMarker); markerErr == nil {
					return
				} else if !os.IsNotExist(markerErr) {
					t.Errorf("check detached collector close marker during cleanup: %v", markerErr)
					return
				}
				status, err := store.CollectorRuntimeStatus(0)
				if err != nil {
					t.Errorf("read detached collector status during cleanup: %v", err)
					return
				}
				last = status
				if status.InstanceID == instance {
					switch status.State {
					case "stopped", "failed":
						for time.Now().Before(deadline) {
							if _, markerErr := os.Stat(closedMarker); markerErr == nil {
								return
							} else if !os.IsNotExist(markerErr) {
								t.Errorf("check detached collector close marker: %v", markerErr)
								return
							}
							time.Sleep(25 * time.Millisecond)
						}
						t.Errorf("detached collector reached terminal status but did not close its store before cleanup; status=%+v", status)
						return
					case "starting", "running", "degraded", "stopping":
						if !requested {
							var requestErr error
							requested, requestErr = store.RequestCollectorStop()
							if requestErr != nil {
								t.Errorf("request detached collector stop during cleanup: %v (status=%+v)", requestErr, status)
								return
							}
						}
					}
				}
				time.Sleep(25 * time.Millisecond)
			}
			t.Errorf("detached collector did not stop before cleanup timeout; last status=%+v", last)
		})
	}
	t.Cleanup(stop)
	return stop
}

func TestDetachedWatchServeCollectsRegisteredSourceAndStops(t *testing.T) {
	if os.Getenv("KOMPOSYN_DETACHED_WATCH_HELPER") == "1" {
		dataDir := os.Getenv("KOMPOSYN_DETACHED_WATCH_DATA_DIR")
		instance := os.Getenv("KOMPOSYN_DETACHED_WATCH_INSTANCE")
		closedMarker := os.Getenv("KOMPOSYN_DETACHED_WATCH_CLOSED_MARKER")
		if dataDir == "" || instance == "" || closedMarker == "" {
			os.Exit(2)
		}
		code := runWatchServe([]string{"--data-dir", dataDir, "--interval", "10ms", "--instance", instance}, io.Discard, context.Background())
		if err := os.WriteFile(closedMarker, []byte("closed"), 0o600); err != nil {
			os.Exit(3)
		}
		os.Exit(code)
	}

	dataDir := t.TempDir()
	root := filepath.Dir(lifecycleFixture(t))
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close detached lifecycle store: %v", err)
		}
	})
	if _, err := store.RegisterCollectorSource(root, true); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	instance, err := tokenlogger.NewCollectorInstanceID()
	if err != nil {
		t.Fatal(err)
	}
	closedMarker := filepath.Join(dataDir, "detached-closed")
	t.Setenv("KOMPOSYN_DETACHED_WATCH_HELPER", "1")
	t.Setenv("KOMPOSYN_DETACHED_WATCH_DATA_DIR", dataDir)
	t.Setenv("KOMPOSYN_DETACHED_WATCH_INSTANCE", instance)
	t.Setenv("KOMPOSYN_DETACHED_WATCH_CLOSED_MARKER", closedMarker)
	stop := registerDetachedStopCleanup(t, store, instance, closedMarker)
	if err := startDetachedCollector(executable, []string{"-test.run=^TestDetachedWatchServeCollectsRegisteredSourceAndStops$"}); err != nil {
		t.Fatal(err)
	}
	waitLifecycleStatus(t, store, func(status tokenlogger.CollectorRuntimeStatus) bool {
		count, countErr := store.EventCount()
		return countErr == nil && count == 2 && status.Mode == "background" && status.LastCompletedAt != nil
	})
	stop()
	status, err := store.CollectorRuntimeStatus(time.Second)
	if err != nil || status.State != "stopped" || status.InstanceID != instance {
		t.Fatalf("detached collector did not stop cleanly: status=%+v err=%v", status, err)
	}
	if status.Imported != 2 || status.SourceCount != 1 {
		t.Fatalf("detached collector did not persist import and source health: %+v", status)
	}
}
