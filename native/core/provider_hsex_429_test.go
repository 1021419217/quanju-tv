package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hsexTestDownloader(server *httptest.Server) *Downloader {
	return &Downloader{
		client:        server.Client(),
		cfg:           defaultConfig(),
		providerHosts: map[string]string{},
		limiter:       newRequestLimiter(6, 0),
	}
}

// 3.34：缓存里的 m3u8 地址在独立 CDN 上，命中时不该回源 hsex.icu 详情页。
// 探针实测主域连续 3 次请求后必 429，减少回源次数是治本手段。
func TestHsexFetchPlaylistPrefersCached(t *testing.T) {
	var detailHits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cached.m3u8" {
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4,\nseg0.ts\n#EXT-X-ENDLIST\n"))
			return
		}
		detailHits++
		_, _ = w.Write([]byte(`<video><source src="https://cdn.example.com/new.m3u8"></video>`))
	}))
	defer server.Close()

	d := hsexTestDownloader(server)
	address, playlist, err := d.hsexFetchPlaylist(
		context.Background(), server.URL+"/video-1257474.htm", server.URL+"/cached.m3u8",
	)
	if err != nil {
		t.Fatalf("缓存地址应可直接起播: %v", err)
	}
	if detailHits != 0 {
		t.Fatalf("缓存命中时不应回源详情页（实际 %d 次），否则会打满 429", detailHits)
	}
	if address != server.URL+"/cached.m3u8" {
		t.Fatalf("地址应沿用缓存: %s", address)
	}
	if !strings.Contains(playlist, server.URL+"/seg0.ts") {
		t.Fatalf("相对分片应被绝对化: %s", playlist)
	}
}

// 缓存地址过期（CDN 500）时必须回源现取，保留 3.31 的自愈能力。
func TestHsexFetchPlaylistFallsBackOnStaleCache(t *testing.T) {
	var detailHits int
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stale.m3u8":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("expired"))
		case "/fresh.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:4,\nseg1.ts\n#EXT-X-ENDLIST\n"))
		default:
			detailHits++
			_, _ = w.Write([]byte(`<video><source src="` + server.URL + `/fresh.m3u8"></video>`))
		}
	}))
	defer server.Close()

	d := hsexTestDownloader(server)
	address, playlist, err := d.hsexFetchPlaylist(
		context.Background(), server.URL+"/video-1257474.htm", server.URL+"/stale.m3u8",
	)
	if err != nil {
		t.Fatalf("缓存失效应回源现取: %v", err)
	}
	if detailHits != 1 {
		t.Fatalf("应回源详情页 1 次，实际 %d 次", detailHits)
	}
	if address != server.URL+"/fresh.m3u8" {
		t.Fatalf("应换成新地址: %s", address)
	}
	if !strings.Contains(playlist, "#EXTM3U") {
		t.Fatalf("播放列表内容缺失: %s", playlist)
	}
}

// 3.34：真正收到 429 响应时也必须「等完冷却再重发」，而不是把
// 「hsex.icu HTTP 429，请在 N 秒后重试」原样抛给用户。
func TestFetchProviderTextWaitsOnFirst429(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("slow down"))
			return
		}
		_, _ = w.Write([]byte("OK-PAGE"))
	}))
	defer server.Close()

	d := hsexTestDownloader(server)
	body, err := d.fetchProviderText(context.Background(), server.URL+"/page", server.URL+"/")
	if err != nil {
		t.Fatalf("首次 429 应在冷却后自动重试，不该抛给用户: %v", err)
	}
	if body != "OK-PAGE" {
		t.Fatalf("应拿到重试后的正文: %q", body)
	}
	if hits < 2 {
		t.Fatalf("应至少重发一次请求，实际 %d 次", hits)
	}
}
