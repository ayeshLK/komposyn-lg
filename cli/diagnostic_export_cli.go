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
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

type diagnosticExportCLIOptions struct {
	problemIDs  []string
	destination string
	dataDir     string
	includeLogs bool
	logLimit    int
}

type diagnosticExportAsk func(string) (string, error)

// parseDiagnosticExportREPLArgs preserves quoted destination paths without
// changing the whitespace parsing used by other REPL commands. Quotes group
// whitespace; backslashes remain literal so Windows paths survive unchanged.
func parseDiagnosticExportREPLArgs(input string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	inToken := false
	for _, r := range input {
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			inToken = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			inToken = true
			continue
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			if inToken {
				args = append(args, token.String())
				token.Reset()
				inToken = false
			}
			continue
		}
		token.WriteRune(r)
		inToken = true
	}
	if quote != 0 {
		return nil, errors.New("unclosed quote")
	}
	if inToken {
		args = append(args, token.String())
	}
	return args, nil
}

func runDiagnosticExport(args []string, input io.Reader, stdout, stderr io.Writer, logger applicationLog) int {
	options, err := parseDiagnosticExportCLIArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "diagnostics export: %s\n", err)
		return 1
	}
	store, closeStore, ok := openResourceStoreWithLogger(options.dataDir, stderr, logger, appCommandOther)
	if !ok {
		return 2
	}
	defer closeStore()
	return runDiagnosticExportForStore(options, inputDiagnosticExportAsk(input, stderr), stdout, stderr, store)
}

func runDiagnosticExportForStoreArgs(args []string, stdout, stderr io.Writer, store *tokenlogger.Store, reader interactiveLineReader) int {
	for _, arg := range args {
		if arg == "--data-dir" {
			fmt.Fprintln(stderr, "diagnostics export: --data-dir is not available in the REPL; it uses the open database")
			return 1
		}
	}
	options, err := parseDiagnosticExportCLIArgs(args)
	if err != nil {
		fmt.Fprintf(stderr, "diagnostics export: %s\n", err)
		return 1
	}
	return runDiagnosticExportForStore(options, interactiveDiagnosticExportAsk(reader), stdout, stderr, store)
}

func runDiagnosticExportForStore(options diagnosticExportCLIOptions, ask diagnosticExportAsk, stdout, stderr io.Writer, store *tokenlogger.Store) int {
	if ask == nil {
		fmt.Fprintln(stderr, "diagnostics export requires an interactive consent reader")
		return 1
	}
	var availableLogs logReadResult
	var selectedRefs []logRecordRef
	if options.includeLogs {
		var err error
		availableLogs, err = readDiagnosticExportLogs(options.logLimit)
		if err != nil {
			fmt.Fprintln(stderr, "could not read the bounded local log selection")
			return 2
		}
		selectedRefs, err = selectDiagnosticExportLogRefs(availableLogs, ask, stderr)
		if err != nil {
			fmt.Fprintln(stderr, "log selection failed; no export was written")
			return 1
		}
	}
	var consentPromptErr error
	confirm := func() (bool, error) {
		choice, err := ask("Write this exact preview to the selected destination? [y/N] ")
		if err != nil {
			consentPromptErr = err
			return false, err
		}
		choice = strings.ToLower(strings.TrimSpace(choice))
		return choice == "y" || choice == "yes", nil
	}
	if _, err := fmt.Fprintln(stdout, "Exact versioned export preview (the JSON below is the file content):"); err != nil {
		fmt.Fprintln(stderr, "could not display export preview")
		return 2
	}
	err := exportDiagnosticEvidence(store, options.problemIDs, availableLogs, selectedRefs, options.destination, stdout, confirm)
	if err == nil {
		fmt.Fprintf(stderr, "diagnostic export written to the selected destination (%d diagnostic(s), %d selected log record(s)).\n", len(options.problemIDs), len(selectedRefs))
		return 0
	}
	if consentPromptErr != nil {
		fmt.Fprintln(stderr, "consent prompt unavailable; no file was written")
		return 2
	}
	switch {
	case errors.Is(err, ErrDiagnosticExportConsent):
		fmt.Fprintln(stderr, "export cancelled; no file was written")
		return 0
	case errors.Is(err, ErrDiagnosticExportSelection):
		fmt.Fprintln(stderr, "export selection is invalid or contains unsupported evidence")
	case errors.Is(err, ErrDiagnosticExportNotFound):
		fmt.Fprintln(stderr, "one or more selected diagnostic details were not found")
	case errors.Is(err, ErrDiagnosticExportUnavailable):
		fmt.Fprintln(stderr, "one or more selected diagnostic details are unavailable")
	case errors.Is(err, ErrDiagnosticExportCleanup):
		fmt.Fprintln(stderr, "an incomplete export could not be safely removed; inspect the selected destination")
	default:
		fmt.Fprintln(stderr, "export is unavailable for the selected evidence; details must be reviewed and eligible, and security-sensitive evidence requires private disclosure handling")
	}
	return 2
}

func parseDiagnosticExportCLIArgs(args []string) (diagnosticExportCLIOptions, error) {
	options := diagnosticExportCLIOptions{logLimit: defaultLogReadLimit}
	seen := make(map[string]bool)
	flagsStarted := false
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") {
			if flagsStarted {
				return options, errors.New("list all full problem IDs before options")
			}
			options.problemIDs = append(options.problemIDs, arg)
			if len(options.problemIDs) > maxExportDiagnostics {
				return options, fmt.Errorf("select at most %d problem IDs", maxExportDiagnostics)
			}
			continue
		}
		flagsStarted = true
		if seen[arg] {
			return options, fmt.Errorf("repeated option %s", boundedTerminalText(arg, 48))
		}
		seen[arg] = true
		switch arg {
		case "--include-logs":
			options.includeLogs = true
		case "--output", "--data-dir", "--log-limit":
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return options, fmt.Errorf("%s requires a value", arg)
			}
			value := args[index+1]
			index++
			switch arg {
			case "--output":
				options.destination = value
			case "--data-dir":
				options.dataDir = value
			case "--log-limit":
				limit, err := strconv.Atoi(value)
				if err != nil || limit < 1 || limit > maxLogReadLimit {
					return options, fmt.Errorf("log limit must be between 1 and %d", maxLogReadLimit)
				}
				options.logLimit = limit
			}
		default:
			return options, fmt.Errorf("unknown option %q", boundedTerminalText(arg, 48))
		}
	}
	if len(options.problemIDs) == 0 {
		return options, errors.New("select at least one full diagnostic problem ID")
	}
	if options.destination == "" {
		return options, errors.New("--output <path> is required")
	}
	if seen["--log-limit"] && !options.includeLogs {
		return options, errors.New("--log-limit requires explicit --include-logs")
	}
	return options, nil
}

func inputDiagnosticExportAsk(input io.Reader, promptOutput io.Writer) diagnosticExportAsk {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 256), 1024)
	return func(prompt string) (string, error) {
		if _, err := fmt.Fprint(promptOutput, prompt); err != nil {
			return "", err
		}
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return "", err
			}
			return "", io.EOF
		}
		return scanner.Text(), nil
	}
}

func interactiveDiagnosticExportAsk(reader interactiveLineReader) diagnosticExportAsk {
	return func(prompt string) (string, error) {
		reader.DisableHistory()
		defer reader.EnableHistory()
		return reader.ReadLine(prompt)
	}
}

func readDiagnosticExportLogs(limit int) (logReadResult, error) {
	directory, err := resolveLogDirectory(os.Getenv("TOKEN_LOGGER_LOG_DIR"), os.UserHomeDir)
	if err != nil {
		return logReadResult{}, err
	}
	return readManagedLogs(directory, logReadOptions{Limit: limit, Deadline: time.Now().Add(logReadTimeBudget)})
}

func selectDiagnosticExportLogRefs(result logReadResult, ask diagnosticExportAsk, output io.Writer) ([]logRecordRef, error) {
	if result.ReadFailure {
		return nil, errors.New("managed log read failed")
	}
	if !result.Available || len(result.Records) == 0 {
		fmt.Fprintln(output, "No readable managed log records are available; no logs will be selected.")
		return nil, nil
	}
	fmt.Fprintln(output, "Optional local log records (only numbered rows you select are included):")
	for index, record := range result.Records {
		line := fmt.Sprintf("  %d  %s %-5s %-28s command=%s", index+1, record.Time, record.Level, record.Event, record.Command)
		if record.Code != "" {
			line += " code=" + record.Code
		}
		line += " file=" + record.File
		fmt.Fprintln(output, escapeLogText(line))
	}
	if result.Truncated || result.TimedOut || result.InvalidEntries > 0 || result.Unsupported > 0 || result.RetentionGap {
		fmt.Fprintf(output, "Log selection is incomplete: scanned=%d bytes entries=%d invalid=%d unsupported=%d truncated=%t timed_out=%t retention_gap=%t. Unshown records are not selected.\n", result.BytesScanned, result.EntriesScanned, result.InvalidEntries, result.Unsupported, result.Truncated, result.TimedOut, result.RetentionGap)
	}
	selection, err := ask("Select log row numbers separated by commas, or blank for no logs: ")
	if err != nil {
		return nil, err
	}
	return parseDiagnosticExportLogSelection(selection, result.Records)
}

func parseDiagnosticExportLogSelection(value string, records []logReadRecord) ([]logRecordRef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > maxExportLogRecords {
		return nil, fmt.Errorf("select no more than %d log rows", maxExportLogRecords)
	}
	refs := make([]logRecordRef, 0, len(parts))
	seen := make(map[int]struct{}, len(parts))
	for _, part := range parts {
		index, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || index < 1 || index > len(records) {
			return nil, errors.New("log selection must contain available numbered rows")
		}
		if _, ok := seen[index]; ok {
			return nil, errors.New("log selection contains a duplicate row")
		}
		seen[index] = struct{}{}
		refs = append(refs, logRef(records[index-1]))
	}
	return refs, nil
}
