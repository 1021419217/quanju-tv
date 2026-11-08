package core

import "testing"

// 站方 2026 年把作者链接从 uvideos.php 改成了 uprofile.php，
// 只认旧路径会让 AuthorID 永远为空 —— 作者页于是整页空白。
func TestP91AuthorUIDFromProfileLink(t *testing.T) {
	fragment := `<div><span class="info">From: </span>
<span class="title-yakov">
<a href="https://1024.sol148.com/uprofile.php?UID=kBBKslfHMfbdECL6Gb-NpMDCPhcw7ovm2WT6Qn6M6imv"><span class="title">kaixinchaoren44<a href="javascript:void(0);"><i class="my_icon daren" title="Original Author"></i></a></span></a>
</span></div>`
	name, uid := p91AuthorOf(fragment)
	if uid != "kBBKslfHMfbdECL6Gb-NpMDCPhcw7ovm2WT6Qn6M6imv" {
		t.Fatalf("UID 提取失败：%q", uid)
	}
	if name != "kaixinchaoren44" {
		t.Fatalf("作者名提取失败：%q", name)
	}
	if !p91ValidAuthorID(uid) {
		t.Fatal("合法 UID 被判成无效")
	}
}

// 旧路径仍要能用：老页面、收藏夹、搜索结果里还是 uvideos.php。
func TestP91AuthorUIDFromLegacyLink(t *testing.T) {
	fragment := `<a href="uvideos.php?UID=abc123def&type=public">olduser</a>`
	name, uid := p91AuthorOf(fragment)
	if uid != "abc123def" || name != "olduser" {
		t.Fatalf("旧格式解析失败：name=%q uid=%q", name, uid)
	}
}

// 作者页卡片只有 .well，没有 videos-text-align，必须靠 well 兜底切出来。
func TestP91AuthorPageCardsUseWellLayout(t *testing.T) {
	page := `<div class="well well-filters"><h4>Public Videos (Videos 1-2 of 21)</h4></div>
<div class="row"><div class="col-sm-12"><div class="row">
<div class="col-xs-12 col-sm-4 col-md-3 col-lg-3"><div class="well well-sm">
<a href="https://example/view_video.php?own=1&viewkey=aaaaaaaaaaaaaaaaaaaa"><span class="video-title">标题一</span></a>
<span class="duration">00:01:02</span></div></div>
<div class="col-xs-12 col-sm-4 col-md-3 col-lg-3"><div class="well well-sm">
<a href="https://example/view_video.php?own=1&viewkey=bbbbbbbbbbbbbbbbbbbb"><span class="video-title">标题二</span></a>
<span class="duration">00:02:03</span></div></div>
</div></div></div>`
	items := parseP91Cards(page, "")
	if len(items) != 2 {
		t.Fatalf("作者页应解析出 2 条，实际 %d", len(items))
	}
}

// 91 的入口域名只做跳板，必须能认出 meta refresh 才能跟到落地域名。
func TestP91MetaRefreshTarget(t *testing.T) {
	page := `<META HTTP-EQUIV=Refresh CONTENT="0; URL=https://a.26702.app/1.php">`
	if got := p91MetaRefreshTarget(page); got != "https://a.26702.app/1.php" {
		t.Fatalf("meta refresh 提取失败：%q", got)
	}
	if got := p91MetaRefreshTarget(`<html><body>no jump</body></html>`); got != "" {
		t.Fatalf("无跳转页面不该返回地址：%q", got)
	}
}
