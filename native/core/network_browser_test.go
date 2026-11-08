package core

import (
	"net/http"
	"testing"
)

// 91porn 的片源在 91p52.com 的随机子域上，站方按 TLS 指纹拒绝普通客户端，
// 必须整体走 Chrome 指纹；主站 91porn.com 保持原样，避免列表页被验证页拦住。
func TestBrowserFingerprintCoversP91CDN(t *testing.T) {
	for _, host := range []string{"v9.91p52.com", "ccm.91p52.com", "91p52.com", "a.b.91p52.com"} {
		if !browserSuffixHost(host) {
			t.Fatalf("%s 应该走浏览器指纹", host)
		}
	}
	for _, host := range []string{"91porn.com", "www.91porn.com", "not91p52.com", "91p52.com.evil.net"} {
		if browserSuffixHost(host) {
			t.Fatalf("%s 不该走浏览器指纹", host)
		}
	}
}

func TestBrowserFingerprintMatchesMediaRequest(t *testing.T) {
	request, err := http.NewRequest(http.MethodGet, "https://v9.91p52.com/video/abc.mp4?sign=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &huangguoBrowserTransport{hosts: map[string]bool{}, upgraded: map[string]bool{}}
	if !transport.matches(request) {
		t.Fatal("91 CDN 的媒体请求应该被指纹传输接管")
	}
	// POST 不参与指纹转发（FormData/上传类请求没有对应能力）。
	post, _ := http.NewRequest(http.MethodPost, "https://v9.91p52.com/video/abc.mp4", nil)
	if transport.matches(post) {
		t.Fatal("POST 不该走指纹传输")
	}
	main, _ := http.NewRequest(http.MethodGet, "https://91porn.com/view_video.php?viewkey=x", nil)
	if transport.matches(main) {
		t.Fatal("主站不该被指纹传输接管")
	}
}

// 带 Range 的请求要装成 video 元素拉流，而不是「导航到网页」。
func TestMediaFingerprintHeadersLookLikeVideoElement(t *testing.T) {
	request, _ := http.NewRequest(http.MethodGet, "https://v9.91p52.com/v.mp4", nil)
	request.Header.Set("Range", "bytes=0-")
	request.Header.Set("Referer", "https://91porn.com/view_video.php?viewkey=x")
	headers := huangguoBrowserHeaders(request)
	if got := firstHeader(headers, "accept"); got != "*/*" {
		t.Fatalf("媒体请求 Accept 应为 */*，实际 %q", got)
	}
	if got := firstHeader(headers, "sec-fetch-dest"); got != "video" {
		t.Fatalf("媒体请求 Sec-Fetch-Dest 应为 video，实际 %q", got)
	}
	if got := firstHeader(headers, "range"); got != "bytes=0-" {
		t.Fatalf("Range 必须原样透传，实际 %q", got)
	}
	if got := firstHeader(headers, "referer"); got != "https://91porn.com/view_video.php?viewkey=x" {
		t.Fatalf("Referer 必须原样透传，实际 %q", got)
	}
	// 普通导航请求仍保持网页指纹。
	page, _ := http.NewRequest(http.MethodGet, "https://91porn.com/", nil)
	pageHeaders := huangguoBrowserHeaders(page)
	if got := firstHeader(pageHeaders, "sec-fetch-dest"); got != "document" {
		t.Fatalf("导航请求 Sec-Fetch-Dest 应为 document，实际 %q", got)
	}
}

func firstHeader(headers map[string][]string, key string) string {
	if values := headers[key]; len(values) > 0 {
		return values[0]
	}
	return ""
}
