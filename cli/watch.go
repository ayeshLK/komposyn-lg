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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tokenlogger "github.com/ayeshLK/komposyn/plugins/library/token-logger"
)

const watchUsage = "usage: komposyn-lg watch {add|remove|list|start|status|stop|foreground} ..."

var watchSubcommands = map[string]bool{
	"add": true, "remove": true, "list": true, "start": true,
	"status": true, "stop": true, "foreground": true, "serve": true,
}

func runWatchWithContext(args []string, stdout, stderr io.Writer, ctx context.Context) int {
	return runWatchWithContextAndLogger(args, stdout, stderr, ctx, discardApplicationLog{})
}

func runWatchWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runWatchWithContextAndLogger(args, stdout, stderr, ctx, logger)
}

func runWatchWithContextAndLogger(args []string, stdout, stderr io.Writer, ctx context.Context, logger applicationLog) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, watchUsage)
		return 1
	}
	if !watchSubcommands[args[0]] {
		return runForegroundWatchWithLogger(args, stdout, stderr, ctx, false, logger)
	}
	switch args[0] {
	case "add":
		return runWatchAddWithLogger(args[1:], stdout, stderr, logger)
	case "remove":
		return runWatchRemoveWithLogger(args[1:], stdout, stderr, logger)
	case "list":
		return runWatchListWithLogger(args[1:], stdout, stderr, logger)
	case "start":
		return runWatchStartWithLogger(args[1:], stdout, stderr, logger)
	case "status":
		return runWatchStatusWithLogger(args[1:], stdout, stderr, logger)
	case "stop":
		return runWatchStopWithLogger(args[1:], stdout, stderr, logger)
	case "foreground":
		return runForegroundWatchWithLogger(args[1:], stdout, stderr, ctx, true, logger)
	case "serve":
		return runWatchServeWithLogger(args[1:], stderr, ctx, logger)
	default:
		fmt.Fprintln(stderr, watchUsage)
		return 1
	}
}

type watchOptions struct {
	dataDir     string
	format      string
	formatSet   bool
	interval    time.Duration
	intervalSet bool
	timeout     time.Duration
	timeoutSet  bool
	defaults    bool
	instanceID  string
}

func parseWatchOptions(args []string, positional bool) ([]string, watchOptions, error) {
	options := watchOptions{format: "text", timeout: 10 * time.Second}
	values := []string{}
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if !strings.HasPrefix(argument, "--") {
			if !positional {
				return nil, options, fmt.Errorf("unexpected argument %q", argument)
			}
			values = append(values, argument)
			continue
		}
		switch argument {
		case "--defaults":
			options.defaults = true
		case "--data-dir", "--format", "--interval", "--timeout", "--instance":
			if index+1 >= len(args) {
				return nil, options, fmt.Errorf("missing value for %s", argument)
			}
			index++
			value := args[index]
			switch argument {
			case "--data-dir":
				options.dataDir = value
			case "--format":
				if value != "text" && value != "json" {
					return nil, options, errors.New("--format expects text or json")
				}
				options.format = value
				options.formatSet = true
			case "--interval":
				parsed, err := time.ParseDuration(value)
				if err != nil || parsed <= 0 {
					return nil, options, errors.New("interval must be a positive duration")
				}
				options.interval, options.intervalSet = parsed, true
			case "--timeout":
				parsed, err := time.ParseDuration(value)
				if err != nil || parsed <= 0 {
					return nil, options, errors.New("timeout must be a positive duration")
				}
				options.timeout = parsed
				options.timeoutSet = true
			case "--instance":
				options.instanceID = value
			}
		default:
			return nil, options, fmt.Errorf("unknown watch option %q", argument)
		}
	}
	return values, options, nil
}

func validateWatchOptions(options watchOptions, allowed ...string) error {
	permitted := map[string]bool{"data-dir": true}
	for _, option := range allowed {
		permitted[option] = true
	}
	for _, used := range []struct {
		name  string
		value bool
	}{
		{"format", options.formatSet}, {"interval", options.intervalSet}, {"timeout", options.timeoutSet},
		{"defaults", options.defaults}, {"instance", options.instanceID != ""},
	} {
		if used.value && !permitted[used.name] {
			return fmt.Errorf("--%s is not valid for this watch command", used.name)
		}
	}
	return nil
}

func openWatchStore(dataDir string, stderr io.Writer) (*tokenlogger.Store, string, error) {
	return openWatchStoreWithLogger(dataDir, stderr, discardApplicationLog{}, appCommandWatch)
}

func openWatchStoreWithLogger(dataDir string, stderr io.Writer, logger applicationLog, command appCommand) (*tokenlogger.Store, string, error) {
	resolution, err := resolveDataDirWithLog(dataDir, logger, command)
	if err != nil {
		return nil, "", fmt.Errorf("resolve token logger data directory: %w", err)
	}
	reportDataDirResolution(stderr, resolution)
	store, err := openStoreWithLog(filepath.Join(resolution.Path, "token-logger.db"), logger, command)
	if err != nil {
		return nil, "", fmt.Errorf("open token logger database in %s: %w", resolution.Path, err)
	}
	return store, resolution.Path, nil
}

func runWatchAdd(args []string, stdout, stderr io.Writer) int {
	return runWatchAddWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runWatchAddWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	paths, options, err := parseWatchOptions(args, true)
	if err == nil {
		err = validateWatchOptions(options, "defaults")
	}
	if err != nil || (len(paths) == 0 && !options.defaults) {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, "usage: komposyn-lg watch add <source> [<source>...] [--defaults] [--data-dir <path>]")
		return 1
	}
	if options.defaults {
		for _, root := range platformDefaultCollectorRoots() {
			paths = append(paths, root.Path)
		}
	}
	store, _, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer store.Close()
	failed := false
	invalid := false
	for _, path := range paths {
		result, addErr := store.RegisterCollectorSource(path, !options.defaults || !isDefaultWatchPath(path))
		if addErr != nil {
			if !errors.Is(addErr, tokenlogger.ErrInvalidCollectorSource) {
				logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			}
			fmt.Fprintf(stderr, "add source %q: %v\n", path, addErr)
			failed = true
			invalid = invalid || errors.Is(addErr, tokenlogger.ErrInvalidCollectorSource)
			continue
		}
		state := "unchanged"
		if result.Changed {
			state = "registered"
		}
		fmt.Fprintf(stdout, "%s source id=%s path=%s\n", state, result.Source.ID, tokenlogger.FormatHumanText(result.Source.Path))
	}
	if failed && invalid {
		return 1
	}
	if failed {
		return 2
	}
	return 0
}

func isDefaultWatchPath(path string) bool {
	for _, root := range platformDefaultCollectorRoots() {
		left, _ := filepath.Abs(root.Path)
		right, _ := filepath.Abs(path)
		if filepath.Clean(left) == filepath.Clean(right) {
			return true
		}
	}
	return false
}

func runWatchRemove(args []string, stdout, stderr io.Writer) int {
	return runWatchRemoveWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runWatchRemoveWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	selectors, options, err := parseWatchOptions(args, true)
	if err == nil {
		err = validateWatchOptions(options)
	}
	if err != nil || len(selectors) == 0 {
		if err != nil {
			fmt.Fprintln(stderr, err)
		}
		fmt.Fprintln(stderr, "usage: komposyn-lg watch remove <source-or-id> [<source-or-id>...] [--data-dir <path>]")
		return 1
	}
	store, _, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer store.Close()
	for _, selector := range selectors {
		source, changed, removeErr := store.RemoveCollectorSource(selector)
		if removeErr != nil {
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			fmt.Fprintf(stderr, "remove source %q: %v\n", selector, removeErr)
			return 2
		}
		if source.ID == "" {
			fmt.Fprintf(stdout, "source not registered: %s\n", selector)
			continue
		}
		state := "already disabled"
		if changed {
			state = "disabled"
		}
		fmt.Fprintf(stdout, "%s source id=%s path=%s\n", state, source.ID, tokenlogger.FormatHumanText(source.Path))
	}
	return 0
}

type sourceListOutput struct {
	FormatVersion int                            `json:"format_version"`
	Sources       []tokenlogger.RegisteredSource `json:"sources"`
}

func runWatchList(args []string, stdout, stderr io.Writer) int {
	return runWatchListWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runWatchListWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	_, options, err := parseWatchOptions(args, false)
	if err == nil {
		err = validateWatchOptions(options, "format")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, _, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer store.Close()
	sources, err := store.ListCollectorSources(false)
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		fmt.Fprintf(stderr, "list sources: %v\n", err)
		return 2
	}
	if options.format == "json" {
		if err := json.NewEncoder(stdout).Encode(sourceListOutput{FormatVersion: 1, Sources: sources}); err != nil {
			fmt.Fprintf(stderr, "write source list: %v\n", err)
			return 2
		}
		return 0
	}
	if len(sources) == 0 {
		fmt.Fprintln(stdout, "no registered sources")
		return 0
	}
	for _, source := range sources {
		fmt.Fprintf(stdout, "id=%s enabled=%t status=%s path=%s", source.ID, source.Enabled, source.Status, tokenlogger.FormatHumanText(source.Path))
		if source.LastError != "" {
			fmt.Fprintf(stdout, " error=%s", source.LastError)
		}
		fmt.Fprintln(stdout)
	}
	return 0
}

func configuredWatchInterval(dataDir string) time.Duration {
	return configuredWatchIntervalWithLogger(dataDir, discardApplicationLog{}, appCommandWatch)
}

func configuredWatchIntervalWithLogger(dataDir string, logger applicationLog, command appCommand) time.Duration {
	settings, err := loadSettings(filepath.Join(dataDir, "settings.json"), dataDir)
	if err != nil {
		logger.Event(appLogSettingsLoadFailed, appLogCodeSettingsLoad, command, 0)
		return 5 * time.Second
	}
	return settings.collectorPollInterval()
}

func persistWatchInterval(dataDir string, interval time.Duration) error {
	path := filepath.Join(dataDir, "settings.json")
	_, err := updateSettings(path, dataDir, func(settings *appSettings) error {
		settings.PollIntervalMillis = interval.Milliseconds()
		settings.PollIntervalSeconds = int((interval + time.Second - 1) / time.Second)
		return nil
	})
	return err
}

func staleWindow(interval time.Duration) time.Duration {
	window := 3 * interval
	if window < 15*time.Second {
		return 15 * time.Second
	}
	return window
}

func runWatchStart(args []string, stdout, stderr io.Writer) int {
	return runWatchStartWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runWatchStartWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	return runWatchStartWithLauncher(args, stdout, stderr, logger, startDetachedCollector, 10*time.Second, 50*time.Millisecond)
}

func runWatchStartWithLauncher(args []string, stdout, stderr io.Writer, logger applicationLog, launch func(string, []string) error, readinessTimeout, readinessInterval time.Duration) int {
	_, options, err := parseWatchOptions(args, false)
	if err == nil {
		err = validateWatchOptions(options, "interval")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, dataDir, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	interval := configuredWatchIntervalWithLogger(dataDir, logger, appCommandWatch)
	if options.intervalSet {
		interval = options.interval
	}
	sources, err := store.ListCollectorSources(true)
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		store.Close()
		fmt.Fprintf(stderr, "list enabled sources: %v\n", err)
		return 2
	}
	if len(sources) == 0 {
		store.Close()
		fmt.Fprintln(stderr, "watch start requires at least one enabled registered source")
		return 1
	}
	status, err := store.CollectorRuntimeStatus(staleWindow(interval))
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		store.Close()
		fmt.Fprintf(stderr, "read collector status: %v\n", err)
		return 2
	}
	if options.intervalSet {
		if err := persistWatchInterval(dataDir, interval); err != nil {
			logger.Event(appLogCommandFailed, appLogCodeSettingsUpdate, appCommandWatch, 2)
			store.Close()
			fmt.Fprintf(stderr, "save collector interval: %v\n", err)
			return 2
		}
	}
	if status.State == "starting" || status.State == "running" || status.State == "degraded" || status.State == "stopping" {
		store.Close()
		fmt.Fprintf(stdout, "collector already %s instance=%s pid=%d\n", status.State, status.InstanceID, status.PID)
		return 0
	}
	instanceID, err := tokenlogger.NewCollectorInstanceID()
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeCollectorAcquire, appCommandWatch, 2)
		store.Close()
		fmt.Fprintln(stderr, err)
		return 2
	}
	executable, err := os.Executable()
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeProcessLaunch, appCommandWatch, 2)
		store.Close()
		fmt.Fprintf(stderr, "resolve executable: %v\n", err)
		return 2
	}
	store.Close()
	childArgs := []string{"watch", "serve", "--data-dir", dataDir, "--interval", interval.String(), "--instance", instanceID}
	if err := launch(executable, childArgs); err != nil {
		logger.Event(appLogCommandFailed, appLogCodeProcessLaunch, appCommandWatch, 2)
		fmt.Fprintf(stderr, "start background collector: %v\n", err)
		return 2
	}
	deadline := time.Now().Add(readinessTimeout)
	for time.Now().Before(deadline) {
		time.Sleep(readinessInterval)
		check, _, openErr := openWatchStore(dataDir, io.Discard)
		if openErr != nil {
			continue
		}
		current, statusErr := check.CollectorRuntimeStatus(staleWindow(interval))
		check.Close()
		if statusErr != nil {
			continue
		}
		if (current.State == "running" || current.State == "degraded") && (current.InstanceID == instanceID || current.InstanceID != "") {
			fmt.Fprintf(stdout, "collector started state=%s instance=%s pid=%d interval=%s recovered_stale=%t\n", current.State, current.InstanceID, current.PID, current.Interval, current.RecoveredFromStale)
			return 0
		}
	}
	logger.Event(appLogCollectorFailed, appLogCodeCollectorReadiness, appCommandWatch, 2)
	fmt.Fprintf(stderr, "collector did not publish a ready heartbeat within %s\n", readinessTimeout)
	return 2
}

func runWatchStatus(args []string, stdout, stderr io.Writer) int {
	return runWatchStatusWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runWatchStatusWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	_, options, err := parseWatchOptions(args, false)
	if err == nil {
		err = validateWatchOptions(options, "format")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, _, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer store.Close()
	status, err := store.CollectorRuntimeStatus(0)
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		fmt.Fprintf(stderr, "read collector status: %v\n", err)
		return 2
	}
	status, err = store.CollectorRuntimeStatus(staleWindow(status.Interval))
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		fmt.Fprintf(stderr, "read collector status: %v\n", err)
		return 2
	}
	if options.format == "json" {
		if err := json.NewEncoder(stdout).Encode(status); err != nil {
			logger.Event(appLogCommandFailed, appLogCodeReportRender, appCommandWatch, 2)
			fmt.Fprintf(stderr, "write collector status: %v\n", err)
			return 2
		}
		return 0
	}
	fmt.Fprintf(stdout, "collector state=%s mode=%s instance=%s pid=%d version=%s interval=%s roots=%d sources=%d imported=%d duplicates=%d pending=%t recovered_stale=%t",
		status.State, status.Mode, status.InstanceID, status.PID, status.ExecutableVersion, status.Interval, status.SourceCount, status.DiscoveredSources, status.Imported, status.Duplicates, status.Pending, status.RecoveredFromStale)
	if status.HeartbeatAt != nil {
		fmt.Fprintf(stdout, " heartbeat=%s", status.HeartbeatAt.Format(time.RFC3339Nano))
	}
	if status.LastCompletedAt != nil {
		fmt.Fprintf(stdout, " last_completed=%s", status.LastCompletedAt.Format(time.RFC3339Nano))
	}
	if status.LastError != "" {
		fmt.Fprintf(stdout, " error=%s", status.LastError)
	}
	fmt.Fprintln(stdout)
	return 0
}

func runWatchStop(args []string, stdout, stderr io.Writer) int {
	return runWatchStopWithLogger(args, stdout, stderr, discardApplicationLog{})
}

func runWatchStopWithLogger(args []string, stdout, stderr io.Writer, logger applicationLog) int {
	_, options, err := parseWatchOptions(args, false)
	if err == nil {
		err = validateWatchOptions(options, "timeout")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, _, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	status, err := store.CollectorRuntimeStatus(0)
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		store.Close()
		fmt.Fprintf(stderr, "read collector status: %v\n", err)
		return 2
	}
	status, err = store.CollectorRuntimeStatus(staleWindow(status.Interval))
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		store.Close()
		fmt.Fprintf(stderr, "read collector status: %v\n", err)
		return 2
	}
	if status.State == "stale" {
		store.Close()
		fmt.Fprintf(stdout, "collector is stale; no process was signalled (instance=%s pid=%d)\n", status.InstanceID, status.PID)
		return 0
	}
	requested, err := store.RequestCollectorStop()
	if err != nil {
		logger.Event(appLogCommandFailed, appLogCodeCollectorStop, appCommandWatch, 2)
		store.Close()
		fmt.Fprintf(stderr, "request collector stop: %v\n", err)
		return 2
	}
	if !requested {
		store.Close()
		fmt.Fprintln(stdout, "collector already stopped")
		return 0
	}
	deadline := time.Now().Add(options.timeout)
	for time.Now().Before(deadline) {
		status, statusErr := store.CollectorRuntimeStatus(0)
		if statusErr != nil {
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			store.Close()
			fmt.Fprintf(stderr, "read collector status: %v\n", statusErr)
			return 2
		}
		if status.State == "stopped" || status.State == "failed" {
			store.Close()
			fmt.Fprintf(stdout, "collector stopped instance=%s\n", status.InstanceID)
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
	store.Close()
	logger.Event(appLogCommandFailed, appLogCodeCollectorStop, appCommandWatch, 3)
	fmt.Fprintln(stderr, "collector did not stop before timeout; retry 'watch stop' after the current atomic import completes")
	return 3
}

func runForegroundWatch(args []string, stdout, stderr io.Writer, ctx context.Context, registryFallback bool) int {
	return runForegroundWatchWithLogger(args, stdout, stderr, ctx, registryFallback, discardApplicationLog{})
}

func runForegroundWatchWithLogger(args []string, stdout, stderr io.Writer, ctx context.Context, registryFallback bool, logger applicationLog) int {
	roots, options, err := parseWatchOptions(args, true)
	if err == nil {
		err = validateWatchOptions(options, "interval")
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, dataDir, err := openWatchStoreWithLogger(options.dataDir, stderr, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer store.Close()
	registryBacked := len(roots) == 0 && registryFallback
	var registeredSources []tokenlogger.RegisteredSource
	if registryBacked {
		registeredSources, roots, err = enabledRegisteredWatchRoots(store)
		listErr := err
		if listErr != nil {
			fmt.Fprintf(stderr, "list enabled sources: %v\n", listErr)
			return 2
		}
	}
	if len(roots) == 0 {
		fmt.Fprintln(stderr, "watch foreground requires a source or an enabled registered source")
		return 1
	}
	interval := configuredWatchIntervalWithLogger(dataDir, logger, appCommandWatch)
	if options.intervalSet {
		interval = options.interval
	}
	instanceID, err := tokenlogger.NewCollectorInstanceID()
	if err != nil {
		fmt.Fprintf(stderr, "create collector identity: %v\n", err)
		return 2
	}
	recovered, err := store.AcquireCollectorLease(instanceID, os.Getpid(), cliVersion, "foreground", interval, staleWindow(interval))
	if errors.Is(err, tokenlogger.ErrCollectorActive) {
		fmt.Fprintln(stderr, "another collector is already active for this data directory; stop it before starting foreground watch")
		return 2
	}
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorAcquire, appCommandWatch, 2)
		fmt.Fprintf(stderr, "acquire foreground collector lease: %v\n", err)
		return 2
	}
	terminalState, terminalError := "stopped", ""
	logger.Event(appLogCollectorStarted, appLogCodeNone, appCommandWatch, 0)
	defer func() {
		if releaseErr := store.ReleaseCollectorLease(instanceID, terminalState, terminalError); releaseErr != nil {
			logger.Event(appLogCollectorFailed, appLogCodeCollectorRelease, appCommandWatch, 2)
		}
		if terminalState != "failed" {
			logger.Event(appLogCollectorStopped, appLogCodeNone, appCommandWatch, 0)
		}
	}()
	if err := store.MarkCollectorReady(instanceID); err != nil {
		terminalState, terminalError = "failed", err.Error()
		logger.Event(appLogCollectorFailed, appLogCodeCollectorReadiness, appCommandWatch, 2)
		fmt.Fprintf(stderr, "publish foreground collector readiness: %v\n", err)
		return 2
	}
	collector, err := tokenlogger.NewCollector(store, roots, interval)
	if err != nil {
		terminalState, terminalError = "failed", err.Error()
		logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
		fmt.Fprintf(stderr, "create collector: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "watching %d source root(s) every %s instance=%s recovered_stale=%t; press Ctrl-C to stop\n", len(roots), interval, instanceID, recovered)
	completedPoll := false
	runHealth := collector.Health()
	lastHealthStatus := ""
	lastSummaryAt := time.Time{}
	pollFailureActive := false
	for {
		stop, heartbeatErr := store.HeartbeatCollector(instanceID)
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			fmt.Fprintf(stderr, "foreground collector heartbeat: %v\n", heartbeatErr)
			return 2
		}
		if stop || (ctx.Err() != nil && completedPoll) {
			writeStoppedWatchSummary(stdout, runHealth)
			return 0
		}
		if registryBacked {
			registeredSources, roots, err = enabledRegisteredWatchRoots(store)
			if err != nil {
				terminalState, terminalError = "failed", err.Error()
				logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
				fmt.Fprintf(stderr, "reload enabled sources: %v\n", err)
				return 2
			}
			collector, err = tokenlogger.NewCollector(store, roots, interval)
			if err != nil {
				terminalState, terminalError = "failed", err.Error()
				logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
				fmt.Fprintf(stderr, "create collector: %v\n", err)
				return 2
			}
		}
		if err := store.BeginCollectorPoll(instanceID, len(roots)); err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			fmt.Fprintf(stderr, "begin foreground collector poll: %v\n", err)
			return 2
		}
		stop, heartbeatErr = store.HeartbeatCollector(instanceID)
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			fmt.Fprintf(stderr, "foreground collector heartbeat: %v\n", heartbeatErr)
			return 2
		}
		if stop || (ctx.Err() != nil && completedPoll) {
			writeStoppedWatchSummary(stdout, runHealth)
			return 0
		}
		pollStarted := time.Now()
		result, pollErr, heartbeatErr, stop := pollWithLeaseHeartbeat(ctx, store, instanceID, collector, staleWindow(interval))
		pollDuration := time.Since(pollStarted).Milliseconds()
		completedPoll = true
		health := collector.Health()
		runHealth = accumulateWatchHealth(runHealth, result, health)
		if err := store.CompleteCollectorPoll(instanceID, result, health, pollErr); err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			fmt.Fprintf(stderr, "complete foreground collector poll: %v\n", err)
			return 2
		}
		if err := updateRegisteredSourceHealth(store, registeredSources, health); err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			fmt.Fprintf(stderr, "persist source health: %v\n", err)
			return 2
		}
		writeWatchPoll(stdout, result, collector.Health())
		pollFailure := logPollFailureEpisode(logger, appCommandWatch, ctx, pollErr, heartbeatErr, stop, &pollFailureActive)
		if pollFailure {
			fmt.Fprintf(stderr, "collector poll: %v\n", pollErr)
		}
		logCollectorHealthAndSummary(logger, appCommandWatch, health.Status, pollDuration, result, runHealth, &lastHealthStatus, &lastSummaryAt)
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			fmt.Fprintf(stderr, "foreground collector heartbeat: %v\n", heartbeatErr)
			return 2
		}
		if stop || ctx.Err() != nil {
			writeStoppedWatchSummary(stdout, runHealth)
			return 0
		}
		stop, heartbeatErr = waitWithLeaseHeartbeat(ctx, store, instanceID, interval, staleWindow(interval))
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			fmt.Fprintf(stderr, "foreground collector heartbeat: %v\n", heartbeatErr)
			return 2
		}
		if stop || ctx.Err() != nil {
			writeStoppedWatchSummary(stdout, runHealth)
			return 0
		}
	}
}

func accumulateWatchHealth(run tokenlogger.CollectorHealth, result tokenlogger.ImportResult, latest tokenlogger.CollectorHealth) tokenlogger.CollectorHealth {
	run.Status = latest.Status
	run.Lifecycle = latest.Lifecycle
	run.LastPollAt = latest.LastPollAt
	run.LastCompletedAt = latest.LastCompletedAt
	if !latest.LastSuccessAt.IsZero() {
		run.LastSuccessAt = latest.LastSuccessAt
	}
	run.LastError = latest.LastError
	run.Imported += result.Receipt.Imported
	run.Duplicates += result.Receipt.Duplicates
	run.Outcomes.Merge(result.Outcomes)
	run.Pending = latest.Pending
	run.Interrupted = run.Interrupted || latest.LatestInterrupted
	run.LatestInterrupted = latest.LatestInterrupted
	run.Sources = latest.Sources
	run.DiscoveryFailures = latest.DiscoveryFailures
	run.Roots = latest.Roots
	return run
}

func collectorHealthCode(status string) appLogCode {
	switch status {
	case "healthy", "running":
		return appLogCodeCollectorHealthRunning
	case "degraded":
		return appLogCodeCollectorHealthDegraded
	case "stale":
		return appLogCodeCollectorHealthStale
	default:
		return appLogCodeNone
	}
}

func expectedCollectorCancellation(ctx context.Context, err error) bool {
	return err != nil && ctx != nil && ctx.Err() != nil && errors.Is(err, context.Canceled)
}

func expectedCollectorPollCancellation(ctx context.Context, err error, cooperativeStop bool) bool {
	return expectedCollectorCancellation(ctx, err) || cooperativeStop && err != nil && errors.Is(err, context.Canceled)
}

func logPollFailureEpisode(logger applicationLog, command appCommand, ctx context.Context, pollErr, heartbeatErr error, cooperativeStop bool, active *bool) bool {
	unexpected := pollErr != nil && heartbeatErr == nil && !expectedCollectorPollCancellation(ctx, pollErr, cooperativeStop)
	if unexpected && !*active {
		logger.CoreFailure(pollErr, appLogCollectorFailed, appLogCodeCollectorPoll, command, 2)
	}
	*active = unexpected
	return unexpected
}

func outcomeCounts(result tokenlogger.ImportResult) appLogOutcomeCounts {
	return appLogOutcomeCounts{
		Imported: int64(result.Receipt.Imported), Duplicates: int64(result.Receipt.Duplicates),
		IgnoredNonUsage: int64(result.Outcomes.IgnoredNonUsage), UnchangedSnapshot: int64(result.Outcomes.UnchangedSnapshot),
		DeferredIncompleteTail: int64(result.Outcomes.DeferredIncompleteTail), UnsupportedUsage: int64(result.Outcomes.UnsupportedUsage),
		MalformedRecord: int64(result.Outcomes.MalformedRecord), FailedSource: int64(result.Outcomes.FailedSource),
	}
}

func cumulativeOutcomeCounts(health tokenlogger.CollectorHealth) appLogOutcomeCounts {
	return appLogOutcomeCounts{
		Imported: int64(health.Imported), Duplicates: int64(health.Duplicates),
		IgnoredNonUsage: int64(health.Outcomes.IgnoredNonUsage), UnchangedSnapshot: int64(health.Outcomes.UnchangedSnapshot),
		DeferredIncompleteTail: int64(health.Outcomes.DeferredIncompleteTail), UnsupportedUsage: int64(health.Outcomes.UnsupportedUsage),
		MalformedRecord: int64(health.Outcomes.MalformedRecord), FailedSource: int64(health.Outcomes.FailedSource),
	}
}

func logCollectorHealthAndSummary(logger applicationLog, command appCommand, status string, durationMillis int64, latest tokenlogger.ImportResult, cumulative tokenlogger.CollectorHealth, lastStatus *string, lastSummary *time.Time) {
	code := collectorHealthCode(status)
	if code == appLogCodeNone {
		return
	}
	if *lastStatus != status {
		logger.Event(appLogCollectorHealthChanged, code, command, 0)
		*lastStatus = status
	}
	now := time.Now()
	if lastSummary.IsZero() || now.Sub(*lastSummary) >= time.Minute {
		logger.CollectorSummary(command, code, durationMillis, outcomeCounts(latest), cumulativeOutcomeCounts(cumulative))
		*lastSummary = now
	}
}

func enabledRegisteredWatchRoots(store *tokenlogger.Store) ([]tokenlogger.RegisteredSource, []string, error) {
	sources, err := store.ListCollectorSources(true)
	if err != nil {
		return nil, nil, err
	}
	roots := make([]string, 0, len(sources))
	for _, source := range sources {
		roots = append(roots, source.Path)
	}
	return sources, roots, nil
}

func writeStoppedWatchSummary(output io.Writer, health tokenlogger.CollectorHealth) {
	health.Lifecycle = "stopped"
	writeWatchSummary(output, health)
}

func runWatchServe(args []string, stderr io.Writer, ctx context.Context) int {
	return runWatchServeWithLogger(args, stderr, ctx, discardApplicationLog{})
}

func runWatchServeWithLogger(args []string, stderr io.Writer, ctx context.Context, logger applicationLog) int {
	_, options, err := parseWatchOptions(args, false)
	if err == nil {
		err = validateWatchOptions(options, "interval", "instance")
	}
	if err != nil || options.instanceID == "" || !options.intervalSet {
		fmt.Fprintln(stderr, "invalid internal watch serve invocation")
		return 1
	}
	store, _, err := openWatchStoreWithLogger(options.dataDir, io.Discard, logger, appCommandWatch)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	defer store.Close()
	logFailureRecorded := false
	recordLogFailure := func() {
		if !logFailureRecorded && logger.Unavailable() {
			_, _ = recordDiagnostic(store, "reason=local_log_sink_unavailable", tokenlogger.DiagnosticCategoryCLI)
			logFailureRecorded = true
		}
	}
	recordLogFailure()
	recovered, err := store.AcquireCollectorLease(options.instanceID, os.Getpid(), cliVersion, "background", options.interval, staleWindow(options.interval))
	if errors.Is(err, tokenlogger.ErrCollectorActive) {
		return 0
	}
	if err != nil {
		logger.Event(appLogCollectorFailed, appLogCodeCollectorAcquire, appCommandWatch, 2)
		fmt.Fprintf(stderr, "acquire collector lease: %v\n", err)
		return 2
	}
	terminalState, terminalError := "stopped", ""
	logger.Event(appLogCollectorStarted, appLogCodeNone, appCommandWatch, 0)
	defer func() {
		if releaseErr := store.ReleaseCollectorLease(options.instanceID, terminalState, terminalError); releaseErr != nil {
			logger.Event(appLogCollectorFailed, appLogCodeCollectorRelease, appCommandWatch, 2)
		}
		if terminalState != "failed" {
			logger.Event(appLogCollectorStopped, appLogCodeNone, appCommandWatch, 0)
		}
	}()
	if err := store.MarkCollectorReady(options.instanceID); err != nil {
		terminalState, terminalError = "failed", err.Error()
		logger.Event(appLogCollectorFailed, appLogCodeCollectorReadiness, appCommandWatch, 2)
		return 2
	}
	if recovered {
		_, _ = recordDiagnostic(store, "reason=stale_collector_recovered", tokenlogger.DiagnosticCategoryCLI)
	}
	lastHealthStatus := ""
	lastSummaryAt := time.Time{}
	pollFailureActive := false
	var runHealth tokenlogger.CollectorHealth
	for {
		stop, heartbeatErr := store.HeartbeatCollector(options.instanceID)
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			return 2
		}
		if stop || ctx.Err() != nil {
			return 0
		}
		sources, listErr := store.ListCollectorSources(true)
		if listErr != nil {
			terminalState, terminalError = "failed", listErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			return 2
		}
		roots := make([]string, 0, len(sources))
		for _, source := range sources {
			roots = append(roots, source.Path)
		}
		if err := store.BeginCollectorPoll(options.instanceID, len(sources)); err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			return 2
		}
		collector, err := tokenlogger.NewCollector(store, roots, options.interval)
		if err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			return 2
		}
		pollStarted := time.Now()
		result, pollErr, heartbeatErr, stop := pollWithLeaseHeartbeat(ctx, store, options.instanceID, collector, staleWindow(options.interval))
		pollDuration := time.Since(pollStarted).Milliseconds()
		logPollFailureEpisode(logger, appCommandWatch, ctx, pollErr, heartbeatErr, stop, &pollFailureActive)
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			return 2
		}
		health := collector.Health()
		runHealth = accumulateWatchHealth(runHealth, result, health)
		if err := store.CompleteCollectorPoll(options.instanceID, result, health, pollErr); err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			return 2
		}
		if err := updateRegisteredSourceHealth(store, sources, health); err != nil {
			terminalState, terminalError = "failed", err.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorStore, appCommandWatch, 2)
			return 2
		}
		recordLogFailure()
		logCollectorHealthAndSummary(logger, appCommandWatch, health.Status, pollDuration, result, runHealth, &lastHealthStatus, &lastSummaryAt)
		if stop || ctx.Err() != nil {
			return 0
		}
		stop, heartbeatErr = waitWithLeaseHeartbeat(ctx, store, options.instanceID, options.interval, staleWindow(options.interval))
		if heartbeatErr != nil {
			terminalState, terminalError = "failed", heartbeatErr.Error()
			logger.Event(appLogCollectorFailed, appLogCodeCollectorHeartbeat, appCommandWatch, 2)
			return 2
		}
		if stop || ctx.Err() != nil {
			return 0
		}
	}
}

type watchPoller interface {
	PollOnceContext(context.Context) (tokenlogger.ImportResult, error)
}

type leaseHeartbeatResult struct {
	stop bool
	err  error
}

func pollWithLeaseHeartbeat(parent context.Context, store *tokenlogger.Store, instanceID string, collector watchPoller, staleAfter time.Duration) (tokenlogger.ImportResult, error, error, bool) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	interval := leaseHeartbeatInterval(staleAfter)
	done := make(chan leaseHeartbeatResult, 1)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- leaseHeartbeatResult{}
				return
			case <-ticker.C:
				stop, err := store.HeartbeatCollector(instanceID)
				if err != nil || stop {
					cancel()
					done <- leaseHeartbeatResult{stop: stop, err: err}
					return
				}
			}
		}
	}()
	result, pollErr := collector.PollOnceContext(ctx)
	cancel()
	heartbeat := <-done
	return result, pollErr, heartbeat.err, heartbeat.stop
}

func waitWithLeaseHeartbeat(ctx context.Context, store *tokenlogger.Store, instanceID string, wait, staleAfter time.Duration) (bool, error) {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	ticker := time.NewTicker(leaseHeartbeatInterval(staleAfter))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return false, nil
		case <-timer.C:
			return false, nil
		case <-ticker.C:
			stop, err := store.HeartbeatCollector(instanceID)
			if err != nil || stop {
				return stop, err
			}
		}
	}
}

func updateRegisteredSourceHealth(store *tokenlogger.Store, sources []tokenlogger.RegisteredSource, health tokenlogger.CollectorHealth) error {
	for _, source := range sources {
		root, ok := health.Roots[source.Path]
		if !ok {
			continue
		}
		if err := store.UpdateCollectorSourceHealth(source.ID, root.Status, root.LastError); err != nil {
			return err
		}
	}
	return nil
}

func leaseHeartbeatInterval(staleAfter time.Duration) time.Duration {
	interval := staleAfter / 3
	// Keep cooperative stop latency comfortably below the default ten-second
	// CLI timeout even when the stale window is large.
	if interval > time.Second {
		interval = time.Second
	}
	if interval < time.Second {
		interval = time.Second
	}
	return interval
}
