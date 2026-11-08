package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// 站方掐断连接时（客户端看到 EOF），应换兼容客户端补一次再报错。
func TestProviderTextRetriesWithCompatTransportAfterEOF(t *testing.T) {
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			if hijacked, ok := writer.(http.Hijacker); ok {
				if conn, _, err := hijacked.Hijack(); err == nil {
					_ = conn.Close()
					return
				}
			}
			return
		}
		_, _ = io.WriteString(writer, "second")
	}))
	defer server.Close()

	cfg := defaultConfig()
	cfg.Retries = 1
	d := &Downloader{cfg: cfg, providerHosts: map[string]string{}, limiter: newRequestLimiter(6, 0)}
	d.client = server.Client()
	if transport, ok := server.Client().Transport.(*http.Transport); ok {
		d.baseTransport = transport
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	body, err := d.fetchProviderText(ctx, server.URL+"/page", "")
	if err != nil {
		t.Fatalf("兼容重试后仍失败: %v", err)
	}
	if body != "second" {
		t.Fatalf("内容错误: %q", body)
	}
	if calls != 2 {
		t.Fatalf("请求次数应为 2，实际 %d", calls)
	}
}

func TestProviderTransportBrokenClassifiesEOF(t *testing.T) {
	if !providerTransportBroken(io.ErrUnexpectedEOF) {
		t.Fatal("EOF 应判为传输层被掐断")
	}
	if providerTransportBroken(context.Canceled) {
		t.Fatal("取消不应触发兼容重试")
	}
}
