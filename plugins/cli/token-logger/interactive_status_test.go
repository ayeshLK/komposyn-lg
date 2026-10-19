// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func TestInteractiveStatusTracksLastSuccessfulReportOnly(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	event := cliReportEvent("status-snapshot", base.Add(time.Minute))
	if _, err := store.Append([]tokenlogger.UsageEvent{event}); err != nil {
		t.Fatal(err)
	}

	current := base
	commands := "/status\n/pricing\n/status\n/range 2026-10-14 2026-10-15\n/status\n/projects\n/status\n/project 1\n/status\n/total-usage\n/status\n/quit\n"
	reader := &beforeReadLineReader{
		scannerLineReader: &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader(commands)), output: io.Discard},
		before: func(command string) {
			switch command {
			case "/status":
				current = current.Add(time.Hour)
			case "/pricing":
				current = current.Add(time.Hour)
			case "/range 2026-10-14 2026-10-15":
				current = current.Add(time.Hour)
			case "/projects":
				current = current.Add(time.Hour)
			case "/project 1":
				current = current.Add(time.Hour)
			case "/total-usage":
				if err := store.Close(); err != nil {
					t.Errorf("close store to force report query failure: %v", err)
				}
			}
		},
	}
	var stdout, stderr bytes.Buffer
	if code := runInteractiveLoopWithClock(reader, &stdout, &stderr, store, dataDir, defaultSettings(dataDir), false, func() time.Time { return current }); code != 0 {
		t.Fatalf("interactive exit code=%d stderr=%q output=%q", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stderr.String(), "usage query failed") {
		t.Fatalf("expected synthetic main-report failure, stderr=%q", stderr.String())
	}

	blocks := interactiveLastReportBlocks(stdout.String())
	if len(blocks) != 6 {
		t.Fatalf("got %d /status report blocks, want 6: %q", len(blocks), stdout.String())
	}
	if blocks[0] != blocks[1] {
		t.Fatalf("/pricing or elapsed time changed the last main-report metadata:\nfirst=%q\nafter pricing=%q", blocks[0], blocks[1])
	}
	if !strings.Contains(blocks[0], "mode: rolling") || !strings.Contains(blocks[0], "2026-10-07T10:00:00Z to 2026-10-14T10:00:00Z") {
		t.Fatalf("initial /status did not retain first rolling query bounds: %q", blocks[0])
	}
	if !strings.Contains(blocks[2], "mode: pinned") || !strings.Contains(blocks[2], "2026-10-14T00:00:00Z to 2026-10-15T00:00:00Z") {
		t.Fatalf("/range report was not recorded as the last successful query: %q", blocks[2])
	}
	if !strings.Contains(blocks[3], "scope: komposyn") || strings.Contains(blocks[3], "project:") {
		t.Fatalf("project-list query scope missing from /status: %q", blocks[3])
	}
	if !strings.Contains(blocks[4], "scope: komposyn / project:Unattributed") {
		t.Fatalf("project navigation scope missing from /status: %q", blocks[4])
	}
	if blocks[5] != blocks[4] {
		t.Fatalf("failed query replaced the last successful query metadata:\nlast success=%q\nafter failure=%q", blocks[4], blocks[5])
	}
}

func interactiveLastReportBlocks(output string) []string {
	var blocks []string
	for offset := 0; offset < len(output); {
		index := strings.Index(output[offset:], "Last report query\n")
		if index < 0 {
			break
		}
		start := offset + index
		end := strings.Index(output[start:], "  Bounds are half-open")
		if end < 0 {
			break
		}
		blocks = append(blocks, output[start:start+end])
		offset = start + end
	}
	return blocks
}

func TestInteractiveCollectorNoticesTrackMeaningNotCounters(t *testing.T) {
	completed := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		status tokenlogger.CollectorRuntimeStatus
		want   []string
	}{
		{name: "first scan", status: tokenlogger.CollectorRuntimeStatus{State: "starting"}, want: []string{"starting", "waiting for first scan"}},
		{name: "no sources after completed scan", status: tokenlogger.CollectorRuntimeStatus{State: "running", LastCompletedAt: &completed}, want: []string{"no sources found in last scan"}},
		{name: "pending data", status: tokenlogger.CollectorRuntimeStatus{State: "running", LastCompletedAt: &completed, DiscoveredSources: 1, Pending: true}, want: []string{"pending source data"}},
		{name: "degraded", status: tokenlogger.CollectorRuntimeStatus{State: "degraded", DiscoveryFailures: 1, Outcomes: tokenlogger.OutcomeCounts{FailedSource: 1}}, want: []string{"degraded", "collection warnings recorded"}},
		{name: "poll error", status: tokenlogger.CollectorRuntimeStatus{State: "degraded", LastError: "poll failed"}, want: []string{"degraded", "collection warnings recorded"}},
		{name: "recovered but recorded warning remains", status: tokenlogger.CollectorRuntimeStatus{State: "running", LastCompletedAt: &completed, DiscoveredSources: 1, Outcomes: tokenlogger.OutcomeCounts{FailedSource: 1}}, want: []string{"running", "collection warnings recorded"}},
		{name: "stale", status: tokenlogger.CollectorRuntimeStatus{State: "stale", Stale: true}, want: []string{"stale"}},
		{name: "stopped", status: tokenlogger.CollectorRuntimeStatus{State: "stopped"}, want: []string{"stopped", "reports use saved data"}},
		{name: "off", status: tokenlogger.CollectorRuntimeStatus{State: "off"}, want: []string{"off", "reports use saved data"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			line := interactiveCollectorSummaryForStatus(test.status, nil)
			for _, want := range test.want {
				if !strings.Contains(line, want) {
					t.Errorf("notice %q does not contain %q", line, want)
				}
			}
		})
	}

	first := tokenlogger.CollectorRuntimeStatus{State: "running", LastCompletedAt: &completed, DiscoveredSources: 2, Imported: 3, HeartbeatAt: &completed}
	second := first
	second.Imported = 99
	changedHeartbeat := completed.Add(time.Minute)
	second.HeartbeatAt = &changedHeartbeat
	if left, right := interactiveCollectorSummaryForStatus(first, nil), interactiveCollectorSummaryForStatus(second, nil); left != right {
		t.Fatalf("routine timestamp/counter changes repeated a collector notice:\n%q\n%q", left, right)
	}
}

type interactiveWidthBuffer struct {
	bytes.Buffer
	width int
}

func (buffer *interactiveWidthBuffer) TerminalWidth() int { return buffer.width }

func TestInteractiveReportUsesNarrowTerminalLayout(t *testing.T) {
	dataDir := t.TempDir()
	store, err := tokenlogger.Open(filepath.Join(dataDir, "token-logger.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	at := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	event := cliReportEvent("narrow-terminal", at.Add(-time.Minute))
	event.ResolvedModel = "narrow-layout-model"
	if _, err := store.Append([]tokenlogger.UsageEvent{event}); err != nil {
		t.Fatal(err)
	}
	state := interactiveState{groupBy: []string{tokenlogger.GroupClient, tokenlogger.GroupProvider, tokenlogger.GroupModel}}
	output := &interactiveWidthBuffer{width: 20}
	var stderr bytes.Buffer
	reader := &scannerLineReader{scanner: bufio.NewScanner(strings.NewReader("")), output: io.Discard}
	if err := renderInteractive(output, &stderr, store, &state, reader, func() time.Time { return at }); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CLIENT:", "STATUS:", "narrow-layout-model"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("narrow REPL output missing %q: %s", expected, output.String())
		}
	}
}
