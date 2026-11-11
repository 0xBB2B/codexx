package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/klauspost/compress/zstd"
)

const maxBodySize = 64 << 20

var (
	errBodyTooLarge = errors.New("request body too large")
	errBadBody      = errors.New("bad request body")
)

func readBody(r *http.Request) (wire, decoded []byte, err error) {
	if r.Body == nil {
		return nil, nil, nil
	}
	wire, err = io.ReadAll(io.LimitReader(r.Body, maxBodySize+1))
	if err != nil {
		return nil, nil, errBadBody
	}
	if len(wire) > maxBodySize {
		return nil, nil, errBodyTooLarge
	}
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get("Content-Encoding")), "zstd") {
		return wire, wire, nil
	}
	dec, err := zstd.NewReader(bytes.NewReader(wire))
	if err != nil {
		return wire, nil, nil
	}
	defer dec.Close()
	decoded, err = io.ReadAll(io.LimitReader(dec, maxBodySize+1))
	if err != nil {
		return wire, nil, nil
	}
	if len(decoded) > maxBodySize {
		return nil, nil, errBodyTooLarge
	}
	return wire, decoded, nil
}

func extractModel(body []byte) string {
	var v struct {
		Model string `json:"model"`
	}
	if json.Unmarshal(body, &v) != nil {
		return ""
	}
	return v.Model
}
