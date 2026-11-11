package control

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"sync"
)

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

	lines := make(chan []byte, 16)
	scanErr := make(chan error, 1)
	go func() {
		defer cancel()
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
		result, err := dispatch(ctx, svc, req)
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

func dispatch(ctx context.Context, svc *Service, req request) (any, error) {
	switch req.Method {
	case "init":
		var p InitParams
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.Init(ctx, p)
	case "status":
		return svc.Status(), nil
	case "save_connection":
		var p struct {
			BaseURL string `json:"base_url"`
			APIKey  string `json:"api_key"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.SaveConnection(ctx, p.BaseURL, p.APIKey)
	case "set_forwarding":
		var p struct {
			Enabled bool `json:"enabled"`
		}
		if err := decode(req.Params, &p); err != nil {
			return nil, err
		}
		return svc.SetForwarding(ctx, p.Enabled)
	case "refresh_models":
		return svc.RefreshModels(ctx)
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
