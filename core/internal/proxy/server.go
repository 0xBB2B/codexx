package proxy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"path"
	"strings"
	"syscall"
	"time"

	"modelbridge/internal/normalize"
)

const DefaultListenAddr = "127.0.0.1:47311"

var (
	ResponseHeaderTimeout        = 60 * time.Second
	SummaryResponseHeaderTimeout = 10 * time.Minute
)

type Config struct {
	ListenAddr         string
	OfficialAPIURL     string
	OfficialChatGPTURL string
	LogOutput          io.Writer
	DataDir            string
}

type Error struct {
	Code string
	Err  error
}

func (e *Error) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

type Server struct {
	listenAddr         string
	officialAPIURL     string
	officialChatGPTURL string
	logOutput          io.Writer
	relayState         relayState
	reasoningIDs       *normalize.ReasoningIDs
	transport          http.RoundTripper
	summaryTransport   http.RoundTripper
	httpServer         *http.Server
	listener           net.Listener
}

func New(cfg Config) *Server {
	s := &Server{
		listenAddr:         cfg.ListenAddr,
		officialAPIURL:     cfg.OfficialAPIURL,
		officialChatGPTURL: cfg.OfficialChatGPTURL,
		logOutput:          cfg.LogOutput,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = ResponseHeaderTimeout
	s.transport = transport
	summaryTransport := transport.Clone()
	summaryTransport.ResponseHeaderTimeout = SummaryResponseHeaderTimeout
	s.summaryTransport = summaryTransport
	ids, err := normalize.OpenReasoningIDs(cfg.DataDir)
	if err != nil {
		ids, _ = normalize.OpenReasoningIDs("")
	}
	s.reasoningIDs = ids
	if s.listenAddr == "" {
		s.listenAddr = DefaultListenAddr
	}
	if s.officialAPIURL == "" {
		s.officialAPIURL = defaultOfficialAPIURL
	}
	if s.officialChatGPTURL == "" {
		s.officialChatGPTURL = defaultOfficialChatGPTURL
	}
	if s.logOutput == nil {
		s.logOutput = io.Discard
	}
	return s
}

func (s *Server) SetRelay(c RelayConfig) { s.relayState.set(c) }

func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.ServeHTTP) }

func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		if errors.Is(err, syscall.EADDRINUSE) {
			return &Error{Code: "proxy_port_in_use", Err: err}
		}
		return &Error{Code: "proxy_listen_failed", Err: err}
	}
	s.listener = ln
	s.httpServer = &http.Server{Handler: s.Handler()}
	go func() { _ = s.httpServer.Serve(ln) }()
	return nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *Server) Close() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w}
	dest := "-"
	defer func() {
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		fmt.Fprintf(s.logOutput, "%s %s %d %s\n", start.Format(time.RFC3339), dest, sw.status, time.Since(start))
	}()

	if !isLoopback(r.RemoteAddr) || !isLoopbackHost(r.Host) || r.Header.Get("Origin") != "" {
		http.Error(sw, "forbidden", http.StatusForbidden)
		return
	}
	urlPath := path.Clean(r.URL.Path)
	if urlPath == "/_health" {
		sw.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(sw, `{"ok":true}`)
		return
	}
	if urlPath != "/v1" && !strings.HasPrefix(urlPath, "/v1/") {
		http.NotFound(sw, r)
		return
	}
	if isWebSocketUpgrade(r) {
		http.Error(sw, "websocket not supported", http.StatusUpgradeRequired)
		return
	}

	raw, plain, err := readBody(r)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errBodyTooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(sw, err.Error(), status)
		return
	}

	rt, err := s.decideRoute(r, urlPath, extractModel(plain))
	if err != nil {
		if rt.relay {
			dest = "relay"
			writeUnreachable(sw)
		} else {
			dest = "official"
			sw.WriteHeader(http.StatusBadGateway)
		}
		return
	}
	if rt.relay {
		dest = "relay"
		isCompact := urlPath == "/v1/responses/compact"
		if isCompact || normalize.HasCompactionTrigger(plain) {
			s.takeOverCompaction(sw, r, rt, plain, isCompact)
			return
		}
		body, tap := plain, false
		if urlPath == "/v1/responses" {
			if cleaned, err := normalize.ForRelay(plain); err == nil {
				body, tap = cleaned, true
			}
		}
		s.forward(sw, r, rt, body, forwardOpts{tapReasoning: tap})
		return
	}
	dest = "official"
	body := raw
	opts := forwardOpts{}
	if out, changed, err := normalize.ForOfficial(plain, s.reasoningIDs); err == nil && changed {
		body, opts.dropEncoding = out, true
	}
	s.forward(sw, r, rt, body, opts)
}

type forwardOpts struct {
	tapReasoning bool
	dropEncoding bool
}

func (s *Server) takeOverCompaction(w http.ResponseWriter, r *http.Request, rt route, plain []byte, isCompact bool) {
	reqBody, err := normalize.SummaryRequest(plain)
	if err != nil {
		http.Error(w, errBadBody.Error(), http.StatusBadRequest)
		return
	}
	u := *rt.target
	u.Path = strings.TrimSuffix(u.Path, "/compact")
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, u.String(), bytes.NewReader(reqBody))
	if err != nil {
		writeUnreachable(w)
		return
	}
	req.Header = r.Header.Clone()
	rewriteRelayHeaders(req.Header, rt.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := s.summaryTransport.RoundTrip(req)
	if err != nil {
		writeUnreachable(w)
		return
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		writeUnreachable(w)
		return
	}
	status := resp.StatusCode
	var summary string
	if status < 400 {
		if summary, err = normalize.SummaryFromResponse(respBody); err != nil {
			status = http.StatusBadGateway
		}
	}
	if status >= 400 {
		code, msg := neutralRelayError(status)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(relayErrorBody(code, msg))
		return
	}
	if isCompact {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(normalize.CompactionOutput(summary))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = w.Write(normalize.CompactionStream(summary))
}

func (s *Server) forward(w http.ResponseWriter, r *http.Request, rt route, body []byte, opts forwardOpts) {
	rp := &httputil.ReverseProxy{
		Transport:     s.transport,
		FlushInterval: -1,
		ErrorLog:      log.New(s.logOutput, "", 0),
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL = rt.target
			pr.Out.Host = ""
			pr.Out.Body = io.NopCloser(bytes.NewReader(body))
			pr.Out.ContentLength = int64(len(body))
			if opts.dropEncoding {
				pr.Out.Header.Del("Content-Encoding")
			}
			if rt.relay {
				rewriteRelayHeaders(pr.Out.Header, rt.key)
			}
		},
	}
	if rt.relay {
		rp.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode < 400 {
				if opts.tapReasoning {
					resp.Body = struct {
						io.Reader
						io.Closer
					}{normalize.TapReasoningIDs(resp.Body, s.reasoningIDs), resp.Body}
				}
				return nil
			}
			code, msg := neutralRelayError(resp.StatusCode)
			writeNeutral(resp, resp.StatusCode, code, msg)
			return nil
		}
		rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) { writeUnreachable(w) }
	} else {
		rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
			w.WriteHeader(http.StatusBadGateway)
		}
	}
	rp.ServeHTTP(w, r)
}

func writeUnreachable(w http.ResponseWriter) {
	code, msg := unreachableRelayError()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write(relayErrorBody(code, msg))
}

func writeNeutral(resp *http.Response, status int, code, msg string) {
	_ = resp.Body.Close()
	b := relayErrorBody(code, msg)
	resp.Header = http.Header{"Content-Type": {"application/json"}}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	resp.StatusCode = status
	resp.Status = fmt.Sprintf("%d %s", status, http.StatusText(status))
	resp.TransferEncoding = nil
	resp.Trailer = nil
	resp.Uncompressed = true
}

func isLoopback(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func isLoopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.Trim(hostport, "[]")
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), "websocket") {
		return false
	}
	for _, v := range r.Header.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(tok), "upgrade") {
				return true
			}
		}
	}
	return false
}
