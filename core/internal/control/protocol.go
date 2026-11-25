package control

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// stdin 关闭后，还在处理的请求最多再跑这么久；排在后面的请求各自同样计时
const eofGrace = 2 * time.Second

const maxLineBytes = 1 << 20

type request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	ID     int64  `json:"id"`
	Result any    `json:"result,omitempty"`
	Error  *Error `json:"error,omitempty"`
}

type event struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lineWriter) send(v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.w.Write(append(raw, '\n'))
}

func Serve(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w := &lineWriter{w: out}
	opts.OnStatus = func(s Status) { w.send(event{Event: "status", Data: s}) }
	svc := NewService(opts)
	defer func() { _ = svc.Shutdown(context.Background(), false) }()

	loopCtx, stopLoop := context.WithCancel(ctx)
	loopDone := make(chan struct{})
	go func() {
		defer close(loopDone)
		svc.runUpdateLoop(loopCtx)
	}()
	defer func() {
		stopLoop()
		<-loopDone
	}()

	lines := make(chan []byte, 16)
	scanErr := make(chan error, 1)
	eof := make(chan struct{})
	go func() {
		defer close(eof)
		defer close(lines)
		sc := bufio.NewScanner(in)
		sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
		for sc.Scan() {
			select {
			case lines <- append([]byte(nil), sc.Bytes()...):
			case <-ctx.Done():
				return
			}
		}
		scanErr <- sc.Err()
	}()

	for line := range lines {
		if ctx.Err() != nil {
			break
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.Method == "shutdown" {
			stopLoop()
			<-loopDone
		}
		result, err := dispatchUntilEOFGrace(ctx, eof, svc, req)
		if err != nil {
			w.send(response{ID: req.ID, Error: toError(err)})
		} else {
			w.send(response{ID: req.ID, Result: result})
		}
		if req.Method == "shutdown" {
			return nil
		}
	}
	select {
	case err := <-scanErr:
		return err
	default:
		return nil
	}
}

func dispatchUntilEOFGrace(ctx context.Context, eof <-chan struct{}, svc *Service, req request) (any, error) {
	reqCtx, cancelReq := context.WithCancel(ctx)
	done := make(chan struct{})
	defer func() {
		close(done)
		cancelReq()
	}()
	go func() {
		select {
		case <-eof:
		case <-done:
			return
		}
		t := time.NewTimer(eofGrace)
		defer t.Stop()
		select {
		case <-t.C:
			cancelReq()
		case <-done:
		}
	}()
	return dispatch(reqCtx, svc, req)
}

func dispatch(ctx context.Context, svc *Service, req request) (any, error) {
	switch req.Method {
	case "init":
		var p InitParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.Init(ctx, p)
	case "status":
		return svc.StatusContext(ctx), nil
	case "save_relay":
		var p SaveRelayParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.SaveRelay(ctx, p)
	case "delete_relay":
		var p struct {
			ID string `json:"id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.DeleteRelay(ctx, p.ID)
	case "set_forwarding":
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.SetForwarding(ctx, p.Enabled)
	case "set_relay_only":
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.SetRelayOnly(ctx, p.Enabled)
	case "refresh_models":
		var p struct {
			ID *string `json:"id"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.RefreshModels(ctx, p.ID)
	case "detect_access":
		return svc.DetectAccess(ctx)
	case "apply_access":
		return svc.ApplyAccess(ctx)
	case "check_update":
		var p struct {
			Manual bool `json:"manual"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.CheckUpdate(ctx, p.Manual)
	case "prepare_uninstall":
		if err := svc.PrepareUninstall(ctx); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	case "shutdown":
		var p struct {
			Restore bool `json:"restore"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		if err := svc.Shutdown(ctx, p.Restore); err != nil {
			return nil, err
		}
		return struct{}{}, nil
	}
	return nil, &Error{Code: "unknown_method"}
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return &Error{Code: "bad_params"}
	}
	return nil
}
