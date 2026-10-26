//go:build queuecompare && linux

// Copyright 2026 Ayesh Almeida
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	disruptor "github.com/ayeshLK/lib-disruptor"
)

const comparePayloadSize = 256

type compareQueue interface {
	publish([]byte) bool
	close(time.Duration) error
	drops() uint64
	written() uint64
}

type compareChannel struct {
	q        chan []byte
	mu       sync.RWMutex
	closed   bool
	dropped  atomic.Uint64
	consumed atomic.Uint64
	sink     func([][]byte) error
	done     chan struct{}
}

func newCompareChannel(sink func([][]byte) error, slow bool) *compareChannel {
	q := &compareChannel{q: make(chan []byte, logQueueCapacity), sink: sink, done: make(chan struct{})}
	go func() {
		defer close(q.done)
		batch := make([][]byte, 0, logBatchSize)
		for {
			first, ok := <-q.q
			if !ok {
				return
			}
			batch = append(batch, first)
		collect:
			for len(batch) < cap(batch) {
				select {
				case line, ok := <-q.q:
					if !ok {
						break collect
					}
					batch = append(batch, line)
				default:
					break collect
				}
			}
			q.consume(batch, slow)
			batch = batch[:0]
		}
	}()
	return q
}
func clearCompareBatch(batch [][]byte) {
	for i := range batch {
		batch[i] = nil
	}
}

func (q *compareChannel) consume(batch [][]byte, slow bool) {
	if slow {
		time.Sleep(20 * time.Microsecond)
	}
	if q.sink != nil {
		if err := q.sink(batch); err != nil {
			q.dropped.Add(uint64(len(batch)))
			clearCompareBatch(batch)
			return
		}
	}
	q.consumed.Add(uint64(len(batch)))
	clearCompareBatch(batch)
}
func (q *compareChannel) publish(data []byte) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		q.dropped.Add(1)
		return false
	}
	select {
	case q.q <- data:
		return true
	default:
		q.dropped.Add(1)
		return false
	}
}
func (q *compareChannel) close(budget time.Duration) error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.q)
	}
	q.mu.Unlock()
	timer := time.NewTimer(budget)
	defer timer.Stop()
	select {
	case <-q.done:
		return nil
	case <-timer.C:
		return context.DeadlineExceeded
	}
}
func (q *compareChannel) drops() uint64   { return q.dropped.Load() }
func (q *compareChannel) written() uint64 { return q.consumed.Load() }

type compareSlot struct {
	data []byte
	n    int
}
type compareRing struct {
	ring     *disruptor.RingBuffer[*compareSlot]
	proc     *disruptor.BatchProcessor[*compareSlot]
	mu       sync.RWMutex
	closed   bool
	dropped  atomic.Uint64
	consumed atomic.Uint64
	batch    [][]byte
	sink     func([][]byte) error
	result   chan error
	cancel   context.CancelFunc
}

func newCompareRing(sink func([][]byte) error, slow bool) (*compareRing, error) {
	q := &compareRing{sink: sink, batch: make([][]byte, 0, logBatchSize), result: make(chan error, 1)}
	r, err := disruptor.New[*compareSlot](logQueueCapacity, disruptor.MultiProducer, func() *compareSlot { return &compareSlot{} }, disruptor.BlockingWait())
	if err != nil {
		return nil, err
	}
	q.ring = r
	p, err := disruptor.NewBatchProcessor(r, r.NewBarrier(), func(slot *compareSlot, _ int64, end bool) error {
		q.batch = append(q.batch, slot.data[:slot.n])
		if !end {
			return nil
		}
		if slow {
			time.Sleep(20 * time.Microsecond)
		}
		if q.sink != nil {
			if err := q.sink(q.batch); err != nil {
				q.dropped.Add(uint64(len(q.batch)))
				clearCompareBatch(q.batch)
				q.batch = q.batch[:0]
				return nil
			}
		}
		q.consumed.Add(uint64(len(q.batch)))
		clearCompareBatch(q.batch)
		q.batch = q.batch[:0]
		return nil
	}, disruptor.WithMaxBatchSize(logBatchSize))
	if err != nil {
		return nil, err
	}
	q.proc = p
	r.AddGatingSequences(p.Sequence())
	ctx, cancel := context.WithCancel(context.Background())
	q.cancel = cancel
	go func() {
		err := p.Run(ctx)
		q.result <- err
		q.mu.Lock()
		q.closed = true
		q.mu.Unlock()
	}()
	deadline := time.Now().Add(time.Second)
	for !p.Running() && time.Now().Before(deadline) {
		time.Sleep(time.Microsecond)
	}
	return q, nil
}
func (q *compareRing) publish(data []byte) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.closed {
		q.dropped.Add(1)
		return false
	}
	if len(data) > maxLogEventBytes {
		q.dropped.Add(1)
		return false
	}
	err := q.ring.TryPublish(func(slot *compareSlot, _ int64) error {
		if cap(slot.data) < len(data) {
			slot.data = make([]byte, len(data))
		}
		owned := slot.data[:cap(slot.data)]
		clear(owned)
		slot.data = owned[:len(data)]
		slot.n = copy(slot.data, data)
		return nil
	})
	if err != nil {
		q.dropped.Add(1)
		return false
	}
	return true
}
func (q *compareRing) close(budget time.Duration) error {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	err := q.ring.Shutdown(ctx)
	q.cancel()
	select {
	case runErr := <-q.result:
		if runErr != nil && !errors.Is(runErr, context.Canceled) && !errors.Is(runErr, disruptor.ErrClosed) {
			return fmt.Errorf("processor: %w", runErr)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return err
}
func (q *compareRing) drops() uint64   { return q.dropped.Load() }
func (q *compareRing) written() uint64 { return q.consumed.Load() }

func compareQueueFactory(kind string, sink func([][]byte) error, slow bool) (compareQueue, error) {
	if kind == "channel" {
		return newCompareChannel(sink, slow), nil
	}
	return newCompareRing(sink, slow)
}

func comparePayload(size int) []byte {
	data := make([]byte, size)
	prefix := []byte("{\"format_version\":1,\"msg\":\"collector.failed\",\"command\":\"watch\",\"code\":\"collector_poll\",\"payload\":\"")
	copy(data, prefix)
	for i := len(prefix); i < size-3; i++ {
		data[i] = 'x'
	}
	data[size-3], data[size-2], data[size-1] = '"', '}', '\n'
	return data
}

func BenchmarkIssue56Queue(b *testing.B) {
	for _, kind := range []string{"channel", "disruptor"} {
		for _, size := range []int{256, 8192} {
			b.Run(fmt.Sprintf("%s/payload-%d", kind, size), func(b *testing.B) {
				payload := comparePayload(size)
				q, err := compareQueueFactory(kind, nil, false)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						q.publish(payload)
					}
				})
				b.StopTimer()
				if err := q.close(time.Second); err != nil {
					b.Fatal(err)
				}
				b.ReportMetric(float64(q.drops()), "dropped")
			})
		}
	}
}
func BenchmarkIssue56FileSink(b *testing.B) {
	for _, kind := range []string{"channel", "disruptor"} {
		b.Run(kind, func(b *testing.B) {
			payload := comparePayload(comparePayloadSize)
			sink, err := newJSONLLogSink(b.TempDir())
			if err != nil {
				b.Fatal(err)
			}
			q, err := compareQueueFactory(kind, sink.appendBatch, false)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					q.publish(payload)
				}
			})
			b.StopTimer()
			if err := q.close(3 * time.Second); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(q.drops()), "dropped")
			b.ReportMetric(float64(q.written()), "written")
		})
	}
}
func BenchmarkIssue56ErrorBurst(b *testing.B) {
	payload := comparePayload(comparePayloadSize)
	for _, kind := range []string{"channel", "disruptor"} {
		b.Run(kind, func(b *testing.B) {
			q, err := compareQueueFactory(kind, nil, true)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					q.publish(payload)
				}
			})
			b.StopTimer()
			if err := q.close(3 * time.Second); err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(q.drops()), "dropped")
			b.ReportMetric(float64(q.written()), "consumed")
		})
	}
}

func compareProcTicks() uint64 {
	data, err := os.ReadFile("/proc/self/stat")
	if err != nil {
		return 0
	}
	end := bytes.LastIndexByte(data, ')')
	if end < 0 || end+1 >= len(data) {
		return 0
	}
	fields := strings.Fields(string(data[end+1:]))
	if len(fields) < 13 {
		return 0
	}
	user, _ := strconv.ParseUint(fields[11], 10, 64)
	system, _ := strconv.ParseUint(fields[12], 10, 64)
	return user + system
}

func TestIssue56QueueIdleMetrics(t *testing.T) {
	for _, kind := range []string{"channel", "disruptor"} {
		t.Run(kind, func(t *testing.T) {
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			goroutines := runtime.NumGoroutine()
			q, err := compareQueueFactory(kind, nil, false)
			if err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			cpuStart := compareProcTicks()
			start := time.Now()
			time.Sleep(time.Second)
			idle := time.Since(start)
			cpuTicks := compareProcTicks() - cpuStart
			idleGoroutines := runtime.NumGoroutine() - goroutines
			closeStart := time.Now()
			if err := q.close(time.Second); err != nil {
				t.Fatal(err)
			}
			t.Logf("idle=%s goroutine_delta=%d heap_delta=%d bytes process_cpu_ticks=%d shutdown=%s", idle, idleGoroutines, int64(after.HeapAlloc)-int64(before.HeapAlloc), cpuTicks, time.Since(closeStart))
		})
	}
}

func TestIssue56QueueRecoveryAfterPressure(t *testing.T) {
	for _, kind := range []string{"channel", "disruptor"} {
		t.Run(kind, func(t *testing.T) {
			payload := comparePayload(comparePayloadSize)
			q, err := compareQueueFactory(kind, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			const burst = 5000
			accepted := 0
			for range burst {
				if q.publish(payload) {
					accepted++
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for q.written() < uint64(accepted) && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if q.written() != uint64(accepted) {
				t.Fatalf("pressure queue did not drain: accepted=%d consumed=%d", accepted, q.written())
			}
			recovered := 0
			for range 100 {
				if q.publish(payload) {
					recovered++
				}
				time.Sleep(100 * time.Microsecond)
			}
			if err := q.close(time.Second); err != nil {
				t.Fatal(err)
			}
			if recovered != 100 {
				t.Fatalf("queue failed to recover after pressure: accepted=%d", recovered)
			}
			t.Logf("burst accepted=%d dropped=%d; recovered paced events=%d; drained=%d", accepted, q.drops()-uint64(100-recovered), recovered, q.written())
		})
	}
}

func TestIssue56QueuePacedFileSinkAndRetainedPayload(t *testing.T) {
	for _, kind := range []string{"channel", "disruptor"} {
		t.Run(kind, func(t *testing.T) {
			sink, err := newJSONLLogSink(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			q, err := compareQueueFactory(kind, sink.appendBatch, false)
			if err != nil {
				t.Fatal(err)
			}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			latencies := make([]time.Duration, 1024)
			accepted := 0
			for i := range latencies {
				payload := comparePayload(maxLogEventBytes)
				start := time.Now()
				if q.publish(payload) {
					accepted++
				}
				latencies[i] = time.Since(start)
				payload = nil
				time.Sleep(100 * time.Microsecond)
			}
			if err := q.close(time.Second); err != nil {
				t.Fatal(err)
			}
			if accepted != len(latencies) || q.written() != uint64(len(latencies)) || q.drops() != 0 {
				t.Fatalf("paced workload lost events: accepted=%d written=%d dropped=%d", accepted, q.written(), q.drops())
			}
			logRecords := 0
			for _, path := range append([]string{sink.active}, sink.archivePath(1), sink.archivePath(2), sink.archivePath(3)) {
				data, readErr := os.ReadFile(path)
				if os.IsNotExist(readErr) {
					continue
				}
				if readErr != nil {
					t.Fatal(readErr)
				}
				lines := bytes.Split(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'})
				for index, line := range lines {
					if !json.Valid(line) {
						t.Fatalf("invalid JSONL file=%s line=%d len=%d tail=%q", filepath.Base(path), index, len(line), line[max(0, len(line)-12):])
					}
				}
				logRecords += len(lines)
			}
			if logRecords != len(latencies) {
				t.Fatalf("valid rotated JSONL records=%d; want %d", logRecords, len(latencies))
			}
			runtime.GC()
			runtime.ReadMemStats(&after)
			sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
			t.Logf("paced 8KiB records=%d accepted=%d dropped=%d written=%d enqueue p50=%s p95=%s p99=%s retained_heap_delta=%d bytes", len(latencies), accepted, q.drops(), q.written(), latencies[511], latencies[972], latencies[1013], int64(after.HeapAlloc)-int64(before.HeapAlloc))
		})
	}
}

func TestIssue56QueueFailedWriterAcknowledgesAndRecovers(t *testing.T) {
	for _, kind := range []string{"channel", "disruptor"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Uint64
			sink := func(batch [][]byte) error {
				if calls.Add(1) <= 5 {
					return os.ErrPermission
				}
				return nil
			}
			q, err := compareQueueFactory(kind, sink, false)
			if err != nil {
				t.Fatal(err)
			}
			payload := comparePayload(comparePayloadSize)
			for range 200 {
				if !q.publish(payload) {
					t.Fatal("paced publication unexpectedly dropped")
				}
				time.Sleep(100 * time.Microsecond)
			}
			if err := q.close(time.Second); err != nil {
				t.Fatal(err)
			}
			if q.drops() == 0 || q.written() == 0 || calls.Load() <= 5 {
				t.Fatalf("writer did not count failures and resume: calls=%d dropped=%d written=%d", calls.Load(), q.drops(), q.written())
			}
			t.Logf("sink calls=%d failed events=%d successful events=%d", calls.Load(), q.drops(), q.written())
		})
	}
}

func TestIssue56QueueShutdownBudgetAndJoin(t *testing.T) {
	for _, kind := range []string{"channel", "disruptor"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			sink := func([][]byte) error { once.Do(func() { close(entered) }); <-release; return nil }
			q, err := compareQueueFactory(kind, sink, false)
			if err != nil {
				t.Fatal(err)
			}
			if !q.publish(comparePayload(comparePayloadSize)) {
				t.Fatal("initial publication dropped")
			}
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("writer did not enter blocked sink")
			}
			start := time.Now()
			err = q.close(20 * time.Millisecond)
			elapsed := time.Since(start)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("close error=%v; want deadline", err)
			}
			if elapsed > 100*time.Millisecond {
				t.Fatalf("close exceeded bounded wait: %s", elapsed)
			}
			if q.publish(comparePayload(comparePayloadSize)) {
				t.Fatal("publication succeeded after close began")
			}
			close(release)
			switch typed := q.(type) {
			case *compareChannel:
				select {
				case <-typed.done:
				case <-time.After(time.Second):
					t.Fatal("channel writer did not finish after sink release")
				}
			case *compareRing:
				select {
				case <-typed.result:
				case <-time.After(time.Second):
					t.Fatal("ring processor did not finish after sink release")
				}
			}
			t.Logf("bounded close returned in %s and worker exited after sink release", elapsed)
		})
	}
}

func TestIssue56RingReuseClearsPayloadTail(t *testing.T) {
	q, err := newCompareRing(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	large := comparePayload(maxLogEventBytes)
	for range logQueueCapacity {
		if !q.publish(large) {
			t.Fatal("large-event fill unexpectedly dropped")
		}
	}
	deadline := time.Now().Add(time.Second)
	for q.written() < logQueueCapacity && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if q.written() != logQueueCapacity {
		t.Fatalf("large-event ring did not drain: %d", q.written())
	}
	short := []byte("{\\\"format_version\\\":1,\\\"msg\\\":\\\"short\\\"}\\n")
	for range logQueueCapacity {
		if !q.publish(short) {
			t.Fatal("short-event wrap unexpectedly dropped")
		}
	}
	if err := q.close(time.Second); err != nil {
		t.Fatal(err)
	}
	slot := q.ring.Get(logQueueCapacity)
	if slot.n != len(short) || !bytes.Equal(slot.data[:slot.n], short) {
		t.Fatalf("reused slot payload=%q len=%d", slot.data[:slot.n], slot.n)
	}
	for index, value := range slot.data[slot.n:cap(slot.data)] {
		if value != 0 {
			t.Fatalf("reused slot retained stale payload byte at %d", slot.n+index)
		}
	}
}
