package core

import (
	"strings"
	"testing"
)

const p91ListFixture = `<html><body>
<div id="wrapper"><div class="container container-minheight">
<div class="row"><div class="col-12">
<div class="well well-sm videos-text-align">
  <div class="video-img-box">
    <a href="/view_video.php?viewkey=abc123def456"><img src="https://img.example.com/a.jpg" /></a>
    <span class="duration">10:25</span>
  </div>
  <div class="video-info">
    <span class="video-title">第一个 标题</span>
    <span class="info">From:</span> 作者甲
  </div>
</div>
<div class="well well-sm videos-text-align">
  <div class="video-img-box">
    <a href="/view_video.php?viewkey=zzz999"><img src="//img2.example.com/b.jpg" /></a>
    <span class="duration">03:07</span>
  </div>
  <div class="video-info">
    <span class="video-title">第二个标题 &amp; 转义</span>
    <span class="info">From:</span> 作者乙
  </div>
</div>
</div></div></div></div>
</body></html>`

const p91DetailFixture = `<html><head><title>测试标题 - 91porn</title></head><body>
<div class="title-yakov"><span>2026-11-01</span></div>
<video id="vplayer" poster="https://img.example.com/p.jpg">
<script type="text/javascript">document.write(strencode2("%3Csource%20src%3D%27https%3A%2F%2Fcdn.example.com%2Fmp43%2F123456.mp4%3Fst%3Dabc%26f%3D1%27%20type%3D%27video%2Fmp4%27%3E"));</script>
</video>
</body></html>`

func TestP91ParseCards(t *testing.T) {
	items := parseP91Cards(p91ListFixture, "rf")
	if len(items) != 2 {
		t.Fatalf("期望 2 条，实际 %d", len(items))
	}
	first := items[0]
	if first.SourceID != "abc123def456" {
		t.Fatalf("viewkey 错误: %s", first.SourceID)
	}
	if first.ID != "p91:abc123def456" || first.Source != sourceP91 {
		t.Fatalf("ID/Source 错误: %s %s", first.ID, first.Source)
	}
	if first.Title != "第一个 标题" {
		t.Fatalf("标题错误: %q", first.Title)
	}
	if first.Cover != "https://img.example.com/a.jpg" {
		t.Fatalf("封面错误: %q", first.Cover)
	}
	if first.Remark != "10:25" {
		t.Fatalf("时长错误: %q", first.Remark)
	}
	if first.Desc != "作者: 作者甲" {
		t.Fatalf("作者错误: %q", first.Desc)
	}
	if first.TypeName != "最近更新" {
		t.Fatalf("分类名错误: %q", first.TypeName)
	}
	second := items[1]
	if second.Cover != "https://img2.example.com/b.jpg" {
		t.Fatalf("协议相对封面未补全: %q", second.Cover)
	}
	if second.Title != "第二个标题 & 转义" {
		t.Fatalf("HTML 实体未解码: %q", second.Title)
	}
}

func TestP91ParseDetailSource(t *testing.T) {
	address, err := p91SourceFromPage(p91DetailFixture)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	want := "https://cdn.example.com/mp43/123456.mp4?st=abc&f=1"
	if address != want {
		t.Fatalf("直链错误:\n got %s\nwant %s", address, want)
	}
	if !p91PlayableMediaURL(address) {
		t.Fatalf("直链应可播放")
	}
}

func TestP91RejectAdPlaceholder(t *testing.T) {
	if p91PlayableMediaURL("https://ccm.91p52.com/358999.mp4") {
		t.Fatalf("广告占位域名不应通过")
	}
	page := `<video><source src='https://ccm.91p52.com/358999.mp4' type='video/mp4'></video>`
	if _, err := p91SourceFromPage(page); err == nil {
		t.Fatalf("只有广告片时应报无可用地址")
	}
}

func TestP91DailyLimitAndTitle(t *testing.T) {
	if m := p91DailyRE.FindStringSubmatch("作为游客，你每天只可观看10个视频"); m == nil || m[1] != "10" {
		t.Fatalf("游客限额正则未命中")
	}
	if got := p91CleanTitle("测试标题 - 91porn"); got != "测试标题" {
		t.Fatalf("标题后缀未清理: %q", got)
	}
	if got := p91CleanTitle("测试标题Chinese homemade video"); got != "测试标题" {
		t.Fatalf("英文后缀未清理: %q", got)
	}
}

func TestP91Categories(t *testing.T) {
	if !validP91Category("rf") || !validP91Category("hot") || !validP91Category("ori") {
		t.Fatalf("站方分类应放行")
	}
	if validP91Category("nope") {
		t.Fatalf("未知分类不应放行")
	}
	if p91CategoryName("rf") != "最近更新" {
		t.Fatalf("分类名映射错误")
	}
}

func TestP91FallbackWindows(t *testing.T) {
	// 主正则不命中（假设站方换了 class），仍应按 viewkey 开窗解析出条目。
	broken := strings.ReplaceAll(p91ListFixture, `well well-sm videos-text-align`, `card card-new`)
	items := parseP91Cards(broken, "rf")
	if len(items) == 0 {
		t.Fatalf("兜底开窗未解析出任何条目")
	}
	if items[0].SourceID == "" || !strings.HasPrefix(items[0].ID, "p91:") {
		t.Fatalf("兜底条目 ID 异常: %+v", items[0])
	}
}
