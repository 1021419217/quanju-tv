package core

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"strings"
	"time"
)

// providerTransportBroken 判断错误是否属于「换一套传输参数可能就通了」。
//
// 典型场景：站方 WAF 或中间网络设备对 TLS 1.3 / HTTP2 的连接特征直接掐断，
// Go 侧看到的是 EOF、connection reset，而同一台手机上的浏览器却能打开。
// KISSAV 在部分网络下就是这种表现（目录请求直接 EOF）。
func providerTransportBroken(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	for _, marker := range []string{
		"eof",
		"connection reset",
		"broken pipe",
		"server closed idle connection",
		"tls: ",
	} {
		if strings.Contains(text, marker) {
			return true
		}
	}
	return false
}

// compatHTTPClient 是「TLS 1.2 + 强制 HTTP/1.1」的备用客户端。
// 只在直连失败且错误像传输层被掐断时用一次，不影响任何正常请求。
func (d *Downloader) compatHTTPClient() *http.Client {
	d.compatOnce.Do(func() {
		base := d.baseTransport
		if base == nil && d.client != nil {
			if candidate, ok := d.client.Transport.(*http.Transport); ok {
				base = candidate
			}
		}
		if base == nil {
			base = http.DefaultTransport.(*http.Transport)
		}
		transport := base.Clone()
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		} else {
			transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		}
		transport.TLSClientConfig.MaxVersion = tls.VersionTLS12
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
		client := &http.Client{Transport: transport}
		if d.client != nil {
			client.Timeout = d.client.Timeout
			client.CheckRedirect = d.client.CheckRedirect
		}
		d.compatClient = client
	})
	return d.compatClient
}

// doCompatCatalogRequest 用兼容客户端重发一次 GET（带请求体时不重试，
// 避免 body 已被消费）。
func (d *Downloader) doCompatCatalogRequest(request *http.Request, timeout time.Duration) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(request.Context(), timeout)
	response, err := d.compatHTTPClient().Do(request.Clone(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	response.Body = &compatResponseBody{ReadCloser: response.Body, cancel: cancel}
	return response, nil
}

type compatResponseBody struct {
	io.ReadCloser
	cancel func()
}

func (body *compatResponseBody) Close() error {
	err := body.ReadCloser.Close()
	body.cancel()
	return err
}
