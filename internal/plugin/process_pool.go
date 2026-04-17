package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type pluginProcessPool struct {
	mu      sync.Mutex
	workers map[string]*pluginWorker
}

type ProcessPoolItem struct {
	Path          string     `json:"path"`
	PID           int        `json:"pid"`
	Refs          int        `json:"refs"`
	IdleReleaseAt *time.Time `json:"idle_release_at,omitempty"`
	Closed        bool       `json:"closed"`
	LastTraceID   string     `json:"last_trace_id,omitempty"`
	LastAction    string     `json:"last_action,omitempty"`
	LastStep      int        `json:"last_step"`
	LastType      string     `json:"last_type,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
	LastDuration  int64      `json:"last_duration_ms"`
	TotalCalls    int64      `json:"total_calls"`
	LastInvokeAt  *time.Time `json:"last_invoke_at,omitempty"`
}

func newPluginProcessPool() *pluginProcessPool {
	return &pluginProcessPool{workers: map[string]*pluginWorker{}}
}

func (p *pluginProcessPool) Acquire(path string, persistent bool) (pluginInvoker, func(), error) {
	if !persistent {
		return &ephemeralInvoker{path: path}, func() {}, nil
	}
	key := path
	p.mu.Lock()
	worker, ok := p.workers[key]
	if !ok {
		created, err := startPluginWorker(path)
		if err != nil {
			p.mu.Unlock()
			return nil, nil, err
		}
		worker = created
		p.workers[key] = worker
	}
	worker.refs++
	if worker.idleTimer != nil {
		worker.idleTimer.Stop()
		worker.idleTimer = nil
		worker.idleAt = nil
	}
	p.mu.Unlock()

	release := func() {
		p.release(key, worker)
	}
	return &pooledInvoker{pool: p, key: key, worker: worker}, release, nil
}

func (p *pluginProcessPool) release(key string, worker *pluginWorker) {
	p.mu.Lock()
	current, ok := p.workers[key]
	if !ok || current != worker {
		p.mu.Unlock()
		return
	}
	if worker.refs > 0 {
		worker.refs--
	}
	if worker.refs == 0 {
		releaseAt := time.Now().Add(5 * time.Second)
		worker.idleAt = &releaseAt
		worker.idleTimer = time.AfterFunc(5*time.Second, func() {
			p.closeIfIdle(key, worker)
		})
	}
	p.mu.Unlock()
}

func (p *pluginProcessPool) closeIfIdle(key string, worker *pluginWorker) {
	p.mu.Lock()
	current, ok := p.workers[key]
	if !ok || current != worker || worker.refs > 0 {
		p.mu.Unlock()
		return
	}
	delete(p.workers, key)
	worker.idleAt = nil
	p.mu.Unlock()
	_ = worker.Close()
}

func (p *pluginProcessPool) Snapshot() []ProcessPoolItem {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]ProcessPoolItem, 0, len(p.workers))
	for _, worker := range p.workers {
		worker.mu.Lock()
		pid := 0
		if worker.cmd != nil && worker.cmd.Process != nil {
			pid = worker.cmd.Process.Pid
		}
		item := ProcessPoolItem{Path: worker.path, PID: pid, Refs: worker.refs, Closed: worker.closed}
		if worker.idleAt != nil {
			t := *worker.idleAt
			item.IdleReleaseAt = &t
		}
		item.LastTraceID = worker.lastTraceID
		item.LastAction = worker.lastAction
		item.LastStep = worker.lastStep
		item.LastType = worker.lastType
		item.LastError = worker.lastError
		item.LastDuration = worker.lastDurationMS
		item.TotalCalls = worker.totalCalls
		if worker.lastInvokeAt != nil {
			t := *worker.lastInvokeAt
			item.LastInvokeAt = &t
		}
		worker.mu.Unlock()
		out = append(out, item)
	}
	return out
}

type pooledInvoker struct {
	pool   *pluginProcessPool
	key    string
	worker *pluginWorker
}

func (p *pooledInvoker) Invoke(ctx context.Context, export string, invocation Invocation) (Output, error) {
	return p.worker.Invoke(ctx, export, invocation)
}

type pluginWorker struct {
	path      string
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Reader
	stderr    *strings.Builder
	mu        sync.Mutex
	refs      int
	idleTimer *time.Timer
	idleAt    *time.Time
	closed    bool

	lastTraceID    string
	lastAction     string
	lastStep       int
	lastType       string
	lastError      string
	lastDurationMS int64
	totalCalls     int64
	lastInvokeAt   *time.Time
}

func startPluginWorker(path string) (*pluginWorker, error) {
	bin, err := exec.LookPath("wasmtime")
	if err != nil {
		return nil, fmt.Errorf("wasmtime not found in PATH")
	}
	cmd := exec.Command(bin, "--env", "WEB2API_LOOP=1", path)
	cmd.Dir = filepath.Dir(path)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	stderrBuf := &strings.Builder{}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, err
	}
	go func() {
		_, _ = io.Copy(stderrBuf, stderrPipe)
	}()
	return &pluginWorker{
		path:   path,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
		stderr: stderrBuf,
	}, nil
}

func (w *pluginWorker) Invoke(ctx context.Context, export string, invocation Invocation) (Output, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return Output{}, fmt.Errorf("plugin worker closed")
	}
	started := time.Now().UTC()
	w.lastInvokeAt = &started
	w.lastAction = invocation.Action
	w.lastStep = invocation.Step
	w.lastTraceID = traceIDFromInvocation(invocation)
	w.lastError = ""
	w.lastType = ""
	data, err := json.Marshal(invocation)
	if err != nil {
		w.lastError = err.Error()
		return Output{}, err
	}
	if _, err := io.WriteString(w.stdin, string(data)+"\n"); err != nil {
		_ = w.closeLocked()
		w.lastError = err.Error()
		w.lastDurationMS = time.Since(started).Milliseconds()
		w.totalCalls++
		return Output{}, fmt.Errorf("run action %s: %w stderr=%s", export, err, strings.TrimSpace(w.stderr.String()))
	}

	type readResult struct {
		line string
		err  error
	}
	readCh := make(chan readResult, 1)
	go func() {
		line, err := w.stdout.ReadString('\n')
		readCh <- readResult{line: line, err: err}
	}()

	var rr readResult
	stepTimer := time.NewTimer(45 * time.Second)
	defer stepTimer.Stop()
	select {
	case rr = <-readCh:
	case <-ctx.Done():
		_ = w.closeLocked()
		w.lastError = ctx.Err().Error()
		w.lastDurationMS = time.Since(started).Milliseconds()
		w.totalCalls++
		return Output{}, ctx.Err()
	case <-stepTimer.C:
		_ = w.closeLocked()
		w.lastError = "plugin step timeout"
		w.lastDurationMS = time.Since(started).Milliseconds()
		w.totalCalls++
		return Output{}, fmt.Errorf("plugin step timeout action=%s stderr=%s", export, strings.TrimSpace(w.stderr.String()))
	}

	if rr.err != nil {
		_ = w.closeLocked()
		w.lastError = rr.err.Error()
		w.lastDurationMS = time.Since(started).Milliseconds()
		w.totalCalls++
		return Output{}, fmt.Errorf("run action %s: %w stderr=%s", export, rr.err, strings.TrimSpace(w.stderr.String()))
	}
	raw := strings.TrimSpace(rr.line)
	if raw == "" {
		w.lastError = "plugin returned empty stdout"
		w.lastDurationMS = time.Since(started).Milliseconds()
		w.totalCalls++
		return Output{}, fmt.Errorf("plugin returned empty stdout")
	}
	var out Output
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		w.lastError = err.Error()
		w.lastDurationMS = time.Since(started).Milliseconds()
		w.totalCalls++
		return Output{}, fmt.Errorf("decode plugin output: %w raw=%s", err, raw)
	}
	w.lastType = out.Type
	w.lastDurationMS = time.Since(started).Milliseconds()
	w.totalCalls++
	return out, nil
}

func traceIDFromInvocation(inv Invocation) string {
	if inv.Input == nil {
		return ""
	}
	return strings.TrimSpace(inv.Input.Request.Metadata["request_trace_id"])
}

func (w *pluginWorker) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closeLocked()
}

func (w *pluginWorker) closeLocked() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
	}
	return nil
}
