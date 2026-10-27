// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"strings"
	"testing"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

func TestInteractiveCollectorSessionDescriptionUsesCurrentRuntime(t *testing.T) {
	const thisPID = 120
	tests := []struct {
		name        string
		status      tokenlogger.CollectorRuntimeStatus
		statusErr   error
		startup     string
		want        string
		mustNotHave string
	}{
		{
			name:    "this repl owns active embedded collector",
			status:  tokenlogger.CollectorRuntimeStatus{State: "running", Mode: "embedded", PID: thisPID},
			startup: "session-owned collector started from saved auto-start preference",
			want:    "owned by this REPL session",
		},
		{
			name:    "another process owns active embedded collector",
			status:  tokenlogger.CollectorRuntimeStatus{State: "running", Mode: "embedded", PID: thisPID + 1},
			startup: "reusing the collector already active for this data directory",
			want:    "owned by another process",
		},
		{
			name:        "borrowed collector can stop after startup",
			status:      tokenlogger.CollectorRuntimeStatus{State: "stopped", Mode: "embedded", PID: thisPID + 1},
			startup:     "reusing the collector already active for this data directory",
			want:        "stopped",
			mustNotHave: "reusing the collector already active",
		},
		{
			name:   "detached collector survives repl windows",
			status: tokenlogger.CollectorRuntimeStatus{State: "running", Mode: "background", PID: thisPID + 2},
			want:   "detached background collector",
		},
		{
			name:   "stale embedded lease does not imply ownership transfer",
			status: tokenlogger.CollectorRuntimeStatus{State: "stale", Stale: true, Mode: "embedded", PID: thisPID + 1},
			want:   "will not take ownership automatically",
		},
		{
			name:    "stale background lease remains identified",
			status:  tokenlogger.CollectorRuntimeStatus{State: "running", Stale: true, Mode: "background", PID: thisPID + 2},
			want:    "detached background collector lease is stale",
			startup: "reusing the collector already active for this data directory",
		},
		{
			name:        "status failure does not repeat stale startup claim",
			statusErr:   errors.New("database unavailable"),
			startup:     "reusing the collector already active for this data directory",
			want:        "ownership cannot be confirmed",
			mustNotHave: "reusing the collector already active",
		},
		{
			name:    "off retains actionable setup failure",
			status:  tokenlogger.CollectorRuntimeStatus{State: "off"},
			startup: "collector setup failed; see the error above",
			want:    "collector setup failed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := interactiveCollectorSessionDescription(test.status, test.statusErr, test.startup, thisPID)
			if !strings.Contains(got, test.want) {
				t.Fatalf("description %q does not contain %q", got, test.want)
			}
			if test.mustNotHave != "" && strings.Contains(got, test.mustNotHave) {
				t.Fatalf("description %q must not contain stale claim %q", got, test.mustNotHave)
			}
		})
	}
}
