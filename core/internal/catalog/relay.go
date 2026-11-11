package catalog

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	RelayTimeout = 15 * time.Second
	maxRelayBody = 8 << 20
)

func FetchRelayIDs(ctx context.Context, baseURL, key string, timeout time.Duration) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, newErr("relay_unreachable", "中台地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, newErr("relay_unreachable", "无法连接中台")
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, newErr("relay_401", "中台拒绝了 Key")
	case resp.StatusCode == http.StatusForbidden:
		return nil, newErr("relay_403", "中台禁止访问")
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, newErr("relay_429", "中台限流")
	case resp.StatusCode >= 500:
		return nil, newErr("relay_5xx", "中台服务出错")
	case resp.StatusCode >= 400 && resp.StatusCode != http.StatusNotFound:
		return nil, newErr("relay_4xx", "中台未接受请求")
	default:
		return nil, newErr("relay_404", "中台地址不对或没有模型列表接口")
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRelayBody)).Decode(&body); err != nil || body.Data == nil {
		return nil, newErr("relay_404", "中台返回的不是模型列表")
	}
	ids := make([]string, len(body.Data))
	for i, d := range body.Data {
		ids[i] = d.ID
	}
	return ids, nil
}
