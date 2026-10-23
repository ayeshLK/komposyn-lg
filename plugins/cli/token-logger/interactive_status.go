// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

// interactiveReportSnapshot keeps inspection separate from rolling-range resolution.
type interactiveReportSnapshot struct {
	from, to, queriedAt time.Time
	pinned              bool
	scope               string
}

func (state *interactiveState) recordReport(queriedAt time.Time) {
	state.reportSnapshot = &interactiveReportSnapshot{
		from: state.from, to: state.to, queriedAt: queriedAt,
		pinned: state.pinned, scope: interactiveBreadcrumb(state),
	}
}

func writeInteractiveRange(output io.Writer, state *interactiveState) {
	if !state.pinned {
		fmt.Fprint(output, "Range: last 7 days · UTC · rolling\n\n")
		return
	}
	// Date-only bounds need no time component. Preserve precision for sub-day ranges.
	format := time.RFC3339Nano
	if state.from.Equal(state.from.UTC().Truncate(24*time.Hour)) && state.to.Equal(state.to.UTC().Truncate(24*time.Hour)) {
		format = time.DateOnly
	}
	fmt.Fprintf(output, "Range: %s to %s · UTC · pinned [from, to)\n\n", state.from.UTC().Format(format), state.to.UTC().Format(format))
}

func interactiveRuntimeStatus(store *tokenlogger.Store) (tokenlogger.CollectorRuntimeStatus, error) {
	status, err := store.CollectorRuntimeStatus(0)
	if err != nil {
		return status, err
	}
	return store.CollectorRuntimeStatus(staleWindow(status.Interval))
}

// Deliberately exclude changing timestamps and counter values: normal polling must
// not repeat notices. Detailed accumulated counters remain available via /status.
func interactiveCollectorSummary(store *tokenlogger.Store) string {
	status, err := interactiveRuntimeStatus(store)
	return interactiveCollectorSummaryForStatus(status, err)
}

func interactiveCollectorSummaryForStatus(status tokenlogger.CollectorRuntimeStatus, err error) string {
	if err != nil {
		return "Collector: status unavailable; /status for details."
	}
	var notes []string
	if status.State == "starting" || (status.State == "running" && status.LastCompletedAt == nil) {
		notes = append(notes, "waiting for first scan")
	}
	if status.LastCompletedAt != nil && status.DiscoveredSources == 0 {
		notes = append(notes, "no sources found in last scan")
	}
	if status.Pending {
		notes = append(notes, "pending source data")
	}
	if status.State == "degraded" || status.LastError != "" || status.DiscoveryFailures > 0 || status.Outcomes.FailedSource > 0 || status.Outcomes.UnsupportedUsage > 0 || status.Outcomes.MalformedRecord > 0 {
		notes = append(notes, "collection warnings recorded; totals may be incomplete")
	}
	if status.State == "stopped" || status.State == "off" {
		notes = append(notes, "reports use saved data")
	}
	notes = append([]string{status.State}, notes...)
	return "Collector: " + strings.Join(notes, " · ") + "; /status for details."
}

func interactiveCollectorSessionDescription(status tokenlogger.CollectorRuntimeStatus, err error, startupDescription string, currentPID int) string {
	if err != nil {
		return "collector status unavailable; lifecycle and ownership cannot be confirmed"
	}
	if status.Stale || status.State == "stale" {
		owner := "owner unavailable"
		if status.Mode == "background" {
			owner = "detached background collector"
		} else if status.Mode == "embedded" && status.PID == currentPID {
			owner = "this REPL's embedded collector"
		} else if status.Mode == "embedded" && status.PID > 0 {
			owner = "another process's embedded collector"
		}
		return owner + " lease is stale; this REPL will not take ownership automatically"
	}
	switch status.State {
	case "starting", "running", "degraded", "stopping":
		switch status.Mode {
		case "background":
			return "detached background collector; it runs independently of REPL windows"
		case "embedded":
			if currentPID > 0 && status.PID == currentPID {
				return "collector owned by this REPL session; it stops when this session exits"
			}
			if status.PID > 0 {
				return "embedded collector owned by another process; closing this REPL will not stop it"
			}
			return "embedded collector active; owner is unavailable; this REPL will not take ownership"
		default:
			return "collector active; owner mode is unavailable; this REPL will not take ownership"
		}
	case "stopped":
		if status.Mode == "background" {
			return "detached background collector stopped; reports use committed data"
		}
		if status.Mode == "embedded" && currentPID > 0 && status.PID == currentPID {
			return "this REPL's embedded collector stopped; it will not restart automatically; reports use committed data"
		}
		if status.Mode == "embedded" && status.PID > 0 {
			return "another process's embedded collector stopped; this REPL will not take ownership automatically; reports use committed data"
		}
		return "collector stopped; reports use committed data"
	case "off":
		if strings.Contains(startupDescription, "setup failed") {
			return startupDescription
		}
		return "no collector active; reports use committed data"
	default:
		return "collector state unavailable; reports use committed data"
	}
}

func printInteractiveStatus(output io.Writer, store *tokenlogger.Store, state *interactiveState, dataDir string, settings appSettings, session string) {
	runtime, runtimeErr := interactiveRuntimeStatus(store)
	fmt.Fprintf(output, "\nStatus\n  %s\n  data directory: %s\n", versionOutput(), tokenlogger.FormatHumanLabel(dataDir))
	fmt.Fprintf(output, "\nCollection\n  session: %s\n", interactiveCollectorSessionDescription(runtime, runtimeErr, session, os.Getpid()))
	fmt.Fprintf(output, "  saved collector enabled preference: %t\n  saved collector auto-start on REPL launch: %t\n  saved collector poll interval: %s\n", settings.CollectorEnabled, settings.AutoStart, settings.collectorPollInterval())
	if err := runtimeErr; err == nil {
		fmt.Fprintf(output, "  collector lifecycle: state=%s; mode=%s; stale=%t\n", runtime.State, runtime.Mode, runtime.Stale)
		fmt.Fprintf(output, "  source scan: last_poll=%s; last_completed=%s; discovered=%d; registered=%d; discovery_failures=%d; pending=%t\n", collectorTimePointer(runtime.LastPollAt), collectorTimePointer(runtime.LastCompletedAt), runtime.DiscoveredSources, runtime.SourceCount, runtime.DiscoveryFailures, runtime.Pending)
		fmt.Fprintf(output, "  runtime: interval=%s; started=%s; heartbeat=%s; stopped=%s; stop_requested=%s\n", runtime.Interval, collectorTimePointer(runtime.StartedAt), collectorTimePointer(runtime.HeartbeatAt), collectorTimePointer(runtime.StoppedAt), collectorTimePointer(runtime.StopRequestedAt))
		fmt.Fprintf(output, "  accumulated outcomes: imported=%d; duplicates=%d; ignored_non_usage=%d; unchanged_snapshot=%d; deferred_incomplete_tail=%d; unsupported_usage=%d; malformed_record=%d; failed_source=%d\n", runtime.Imported, runtime.Duplicates, runtime.Outcomes.IgnoredNonUsage, runtime.Outcomes.UnchangedSnapshot, runtime.Outcomes.DeferredIncompleteTail, runtime.Outcomes.UnsupportedUsage, runtime.Outcomes.MalformedRecord, runtime.Outcomes.FailedSource)
	} else {
		fmt.Fprintln(output, "  collector lifecycle and source scan: unavailable")
	}
	fmt.Fprintln(output, "  Counters are accumulated for this collector run; lifecycle does not prove a source scan completed.")
	fmt.Fprintln(output, "\nLast report query")
	if report := state.reportSnapshot; report != nil {
		mode := "rolling"
		if report.pinned {
			mode = "pinned"
		}
		fmt.Fprintf(output, "  scope: %s\n  mode: %s\n  report range: %s to %s (UTC)\n  report queried_at: %s\n", report.scope, mode, report.from.UTC().Format(time.RFC3339Nano), report.to.UTC().Format(time.RFC3339Nano), report.queriedAt.Format(time.RFC3339Nano))
	} else {
		fmt.Fprintln(output, "  unavailable: no successful report query in this session")
	}
	fmt.Fprintln(output, "  Bounds are half-open [from, to). Reports query saved database data; source scanning is separate.")
	fmt.Fprintln(output, "  /refresh redraws committed data without starting or waiting for a scan.")
	fmt.Fprint(output, "\nPrivacy\n  diagnostic reporting: ask before any external action\n\n")
}
