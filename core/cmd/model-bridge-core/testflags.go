//go:build coretest

package main

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"net/http"
	"os"

	"modelbridge/internal/control"
)

var (
	listen             = flag.String("listen", "", "转发服务监听地址")
	codexPath          = flag.String("codex-path", "", "codex 程序路径")
	officialAPIURL     = flag.String("official-api-url", "", "官方 API 地址")
	officialChatGPTURL = flag.String("official-chatgpt-url", "", "官方 ChatGPT 后端地址")
	relayCAFile        = flag.String("relay-ca-file", "", "追加信任的 PEM 证书")
)

func init() {
	applyTestFlags = func(o *control.Options) error {
		o.ListenAddr = *listen
		o.CodexPath = *codexPath
		o.OfficialAPIURL = *officialAPIURL
		o.OfficialChatGPTURL = *officialChatGPTURL
		if *relayCAFile == "" {
			return nil
		}
		return trustCA(*relayCAFile)
	}
}

func trustCA(path string) error {
	pem, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(pem) {
		return errors.New("证书文件里没有有效的 PEM 证书")
	}
	tr := http.DefaultTransport.(*http.Transport)
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	}
	tr.TLSClientConfig.RootCAs = pool
	return nil
}
