package core

import (
	"strings"
	"testing"
)

// 3.34：91 主域 91porn.com 在真机被 DNS 污染，只有它（与 www）需要
// 在拨号层固定到 Cloudflare 边缘 IP；镜像 sp2026.com 与 CDN 域名不受影响。
func TestP91DialAddresses(t *testing.T) {
	pinned, ok := p91DialAddresses("91porn.com:443")
	if !ok || len(pinned) != len(p91EdgeIPs) {
		t.Fatalf("91porn.com 应被固定到 CF 边缘 IP: pinned=%v ok=%v", pinned, ok)
	}
	for i, ip := range p91EdgeIPs {
		if pinned[i] != ip+":443" {
			t.Fatalf("第 %d 个边缘 IP 不对: got %s want %s", i, pinned[i], ip+":443")
		}
	}
	if _, ok := p91DialAddresses("www.91porn.com:443"); !ok {
		t.Fatal("www.91porn.com 同样应被固定")
	}
	for _, address := range []string{
		"91porny.com:443", "sp2026.com:443", "la.btc620.com:443",
		"ccm.91p52.com:443", "hsex.icu:443", "example.com:80",
	} {
		if _, ok := p91DialAddresses(address); ok {
			t.Fatalf("%s 不该被 91 的拨号规则改写", address)
		}
	}
	// 好色TV 与 91 的固定规则互不干扰。
	if _, ok := hsexDialAddresses("91porn.com:443"); ok {
		t.Fatal("好色TV 规则不该接管 91 主域")
	}
	if _, ok := p91DialAddresses("hsex.icu:443"); ok {
		t.Fatal("91 规则不该接管好色TV 主域")
	}
}

// 3.34：站方详情页同时给出好线（完整 st/e/k/f 签名）与坏线
// （*.91p52.com，实测 HTTP/2 PROTOCOL_ERROR）。按签名完整度排序后
// 好线必须排第一，坏线仍保留但不能进备用线路。
func TestP91MediaRank(t *testing.T) {
	good := "https://la.btc620.com//mp43/1.mp4?st=a&e=2&k=3&f=4"
	bad := "https://ccm.91p52.com/1.mp4?st=a&f=b"
	if p91MediaRank(good) >= p91MediaRank(bad) {
		t.Fatalf("完整签名的主直链应排在坏线之前: good=%d bad=%d",
			p91MediaRank(good), p91MediaRank(bad))
	}
	if !p91DeadCDNHost(bad) {
		t.Fatal("ccm.91p52.com 应被识别为不可用线路")
	}
	if p91DeadCDNHost(good) {
		t.Fatal("la.btc620.com 不该被误判")
	}
}

func TestP91MediaCandidatesOrder(t *testing.T) {
	good := "https://la.btc620.com/a.mp4?st=a&amp;e=2&amp;k=3&amp;f=4"
	bad := "https://ccm.91p52.com/b.mp4?st=a&amp;f=b"
	page := `<video><source src="` + bad + `" type="video/mp4"></video>` +
		`<video><source src="` + good + `" type="video/mp4"></video>`
	candidates, _ := p91MediaCandidates(page, "https://91porn.com/view_video.php?viewkey=x")
	if len(candidates) != 2 {
		t.Fatalf("应有 2 条候选: %v", candidates)
	}
	if !strings.Contains(candidates[0], "la.btc620.com") {
		t.Fatalf("完整签名的主直链应排第一: %v", candidates)
	}
	if !strings.Contains(candidates[1], "91p52.com") {
		t.Fatalf("坏线应被降级到末尾: %v", candidates)
	}
}

// 3.34：坏线不能作为换线目标，且跨境 443 被干扰时有一条纯 HTTP 备用。
func TestP91MediaFromCandidatesSkipsDeadCDN(t *testing.T) {
	good := "https://la.btc620.com//mp43/1.mp4?st=a&e=2&k=3&f=4"
	bad := "https://ccm.91p52.com/1.mp4?st=a&f=b"
	d := &Downloader{}
	media := d.p91MediaFromCandidates(
		[]string{good, bad},
		"https://91porn.com/view_video.php?viewkey=x",
	)
	if media.URL != good {
		t.Fatalf("主候选错误: %s", media.URL)
	}
	for _, variant := range media.Variants {
		if p91DeadCDNHost(variant.URL) {
			t.Fatalf("坏线不该进备用线路: %s", variant.URL)
		}
	}
	plain := false
	for _, variant := range media.Variants {
		if strings.HasPrefix(variant.URL, "http://la.btc620.com") {
			plain = true
		}
	}
	if !plain {
		t.Fatalf("应有一条纯 HTTP 备用线路: %+v", media.Variants)
	}
}
