package core

import (
	"strings"
	"testing"
)

// 3.32：主域名在部分网络被掐（EOF），站方发布页下发的 Cloudflare 直连线路
// 内容逐字节一致，必须作为候选逐个尝试。
func TestKissavURLCandidates(t *testing.T) {
	raw := "https://kissavs.com/av/updates/"
	candidates := kissavURLCandidates(raw)
	if len(candidates) != 1+len(kissavMirrorHosts) {
		t.Fatalf("候选数 = %d，期望 %d", len(candidates), 1+len(kissavMirrorHosts))
	}
	if candidates[0] != raw {
		t.Fatalf("第一个候选必须是主站：%s", candidates[0])
	}
	for _, candidate := range candidates[1:] {
		if !strings.Contains(candidate, ".npb6frvj.cc") {
			t.Fatalf("备用线路域名异常：%s", candidate)
		}
		if !strings.HasSuffix(candidate, "/av/updates/") {
			t.Fatalf("备用线路必须保留原路径与查询：%s", candidate)
		}
	}
	// 带签名的播放列表代理地址也要能换线路。
	proxy := "https://kissavs.com/media/m3u8/?url=https%3A%2F%2Fx.example%2Fa.m3u8&token=abc"
	got := kissavURLCandidates(proxy)
	if len(got) != 1+len(kissavMirrorHosts) || !strings.Contains(got[1], "token=abc") {
		t.Fatalf("代理地址候选异常：%v", got)
	}
	// 已经是备用线路时保持单候选，避免套娃。
	mirror := "https://abandon.npb6frvj.cc/av/updates/"
	if got := kissavURLCandidates(mirror); len(got) != 1 || got[0] != mirror {
		t.Fatalf("备用线路不应再展开：%v", got)
	}
}

func TestKissavMirrorHostRecognition(t *testing.T) {
	for _, ok := range []string{"abandon.npb6frvj.cc", "ABILITY.npb6frvj.cc"} {
		if !isKissavMirrorHost(ok) {
			t.Fatalf("%s 应识别为备用线路", ok)
		}
	}
	for _, bad := range []string{"npb6frvj.cc", "evil.com", "kissavs.com", ".npb6frvj.cc"} {
		if isKissavMirrorHost(bad) {
			t.Fatalf("%q 不应识别为备用线路", bad)
		}
	}
	if got := providerSourceForURL("https://apple.npb6frvj.cc/av/updates/"); got != sourceKissav {
		t.Fatalf("备用线路应归属 KISSAV 源，实际 %q", got)
	}
}
