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
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const coreObserverDrainBudget = 100 * time.Millisecond

// A projection owns only the approved, contract-v1 diagnostic fields. It never
// includes IDs, subject references, positions, errors, paths, or timestamps.
type coreDiagnosticProjection struct {
	contractVersion int
	kind            tokenlogger.DiagnosticEventKind
	classification  tokenlogger.DiagnosticClassification
	evidenceScope   tokenlogger.DiagnosticEvidenceScope
	evidenceAvail   tokenlogger.DiagnosticAvailability
	evidenceReason  tokenlogger.DiagnosticUnavailableReason
	evidenceLost    bool
	countsScope     tokenlogger.DiagnosticCountScope
	countsAvail     tokenlogger.DiagnosticAvailability
	countsSaturated bool
	countsTruncated bool
	counts          tokenlogger.DiagnosticOutcomeCounts
}

func projectCoreDiagnostic(event tokenlogger.DiagnosticEvent) (coreDiagnosticProjection, bool) {
	if event.Validate() != nil || event.ContractVersion != tokenlogger.DiagnosticContractVersion {
		return coreDiagnosticProjection{}, false
	}
	return coreDiagnosticProjection{
		contractVersion: event.ContractVersion,
		kind:            event.Kind,
		classification:  event.Classification,
		evidenceScope:   event.Evidence.Scope,
		evidenceAvail:   event.Evidence.Availability,
		evidenceReason:  event.Evidence.UnavailableReason,
		evidenceLost:    event.Evidence.Truncated,
		countsScope:     event.Counts.Scope,
		countsAvail:     event.Counts.Availability,
		countsSaturated: event.Counts.Saturated,
		countsTruncated: event.Counts.Truncated,
		counts:          event.Counts,
	}, true
}

func coreDiagnosticCode(kind tokenlogger.DiagnosticEventKind) appLogCode {
	switch kind {
	case tokenlogger.DiagnosticEventOperationFailed:
		return appLogCodeCoreOperationFailed
	case tokenlogger.DiagnosticEventConditionSummary:
		return appLogCodeCoreConditionSummary
	case tokenlogger.DiagnosticEventPollSummary:
		return appLogCodeCorePollSummary
	case tokenlogger.DiagnosticEventOperationRecovered:
		return appLogCodeCoreOperationRecovered
	default:
		return appLogCodeNone
	}
}
