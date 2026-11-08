package seku

import "testing"

// SupJav 免代兜底接线回归（离线确定性部分）：
//   - CF 新版验证页签名 "Performing security verification" 须能被公共
//     挑战识别命中（jinaFallback 的触发条件之一）；
//   - supjavJinaRetry 对不带挑战签名的正文必须拒绝触发兜底（不浪费
//     代抓、不把正常页误送重抓）。
//
// 活体口径（2026-10-24 本机出口实测）：直连 supjav.com 回 403，
// r.jina.ai 代抓回的也是挑战页、被兜底验收逻辑正确拒绝——本地出口
// 双路皆挡，兜底的实际放行待用户侧网络验，本测试不冒充活体已通。
func TestSupjavFallbackWiring(t *testing.T) {
	challenge := "<html><head><title>Performing security verification</title></head>" +
		"<body>Attention Required! Cloudflare</body></html>"
	if !isChallengePage(challenge) {
		t.Fatal("isChallengePage missed the security-verification interstitial")
	}
	if !supjavIsCFBlock(challenge) {
		t.Fatal("supjavIsCFBlock missed its own block page")
	}
	if got := supjavJinaRetry("https://supjav.com/", "", "<html><body>normal page</body></html>"); got != "" {
		t.Fatalf("jina retry must decline non-challenge bodies, got %q", got)
	}
}

// SupJav 已按用户 2026-10-24 二轮口径撤注册（fix1 接全壳免代兜底后真机
// 仍打不开）；sites_g2.go 的实现与上面的兜底接线测试保留存档，本测试
// 锁定槽位不得再被注册回来。
func TestSupjavSlotsUnregistered(t *testing.T) {
	if implForSlot("supjav") != nil {
		t.Fatal("slot supjav must stay unregistered (removed in fix2)")
	}
	if _, ok := keyToSlot["supjav"]; ok {
		t.Fatal("key supjav must not resolve to a slot")
	}
	for _, s := range allSlots() {
		if s == "supjav" {
			t.Fatal("supjav must not appear in allSlots")
		}
	}
}
