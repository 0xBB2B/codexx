package catalog

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

const (
	RelayTimeout = 15 * time.Second
	maxRelayBody = 8 << 20
)

// Efforts 为 nil 表示接口没给，空切片表示接口给了但为空
type RelayModel struct {
	ID              string   `json:"id"`
	ContextLength   int      `json:"context_length"`
	InputModalities []string `json:"input_modalities"`
	Efforts         []string `json:"reasoning_efforts"`
}

func FetchRelayModels(ctx context.Context, baseURL, key string, timeout time.Duration) ([]RelayModel, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, newErr("relay_unreachable", "中转地址无效")
	}
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, newErr("relay_unreachable", "无法连接中转")
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, newErr("relay_401", "中转拒绝了 Key")
	case resp.StatusCode == http.StatusForbidden:
		return nil, newErr("relay_403", "中转禁止访问")
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, newErr("relay_429", "中转限流")
	case resp.StatusCode == http.StatusNotFound:
		return nil, newErr("relay_404", "中转地址不对或没有模型列表接口")
	case resp.StatusCode >= 500:
		return nil, newStatusErr("relay_5xx", "中转服务出错", resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, newStatusErr("relay_4xx", "中转未接受请求", resp.StatusCode)
	default:
		return nil, newStatusErr("relay_bad_response", "中转返回的不是模型列表", resp.StatusCode)
	}

	var body struct {
		Data []struct {
			ID            string          `json:"id"`
			ContextLength json.RawMessage `json:"context_length"`
			Architecture  json.RawMessage `json:"architecture"`
			Reasoning     json.RawMessage `json:"reasoning"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxRelayBody)).Decode(&body); err != nil || body.Data == nil {
		return nil, newStatusErr("relay_bad_response", "中转返回的不是模型列表", resp.StatusCode)
	}
	models := make([]RelayModel, 0, len(body.Data))
	for _, d := range body.Data {
		if d.ID == "" {
			continue
		}
		arch := decodeOrZero[struct {
			InputModalities json.RawMessage `json:"input_modalities"`
		}](d.Architecture)
		reasoning := decodeOrZero[struct {
			SupportedEfforts json.RawMessage `json:"supported_efforts"`
		}](d.Reasoning)
		models = append(models, RelayModel{
			ID:              d.ID,
			ContextLength:   positiveInt(decodeOrZero[float64](d.ContextLength)),
			InputModalities: decodeOrZero[[]string](arch.InputModalities),
			Efforts:         decodeOrZero[[]string](reasoning.SupportedEfforts),
		})
	}
	return models, nil
}

// 单个字段类型不对只当这一项没给，不让整张模型列表失败
func decodeOrZero[T any](raw json.RawMessage) T {
	var v T
	if json.Unmarshal(raw, &v) != nil {
		var zero T
		return zero
	}
	return v
}

func positiveInt(f float64) int {
	if f < 1 || f > math.MaxInt32 || f != math.Trunc(f) {
		return 0
	}
	return int(f)
}
