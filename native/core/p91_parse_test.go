package core

import (
	"errors"
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
	const pageURL = "https://91porn.com/view_video.php?viewkey=abc123def456"
	address, err := p91SourceFromPage(p91DetailFixture, pageURL)
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

// 站方真实直链在自家 CDN（ccm.91p52.com）上，早期按域名误杀导致永远播不了。
func TestP91AcceptSiteCDN(t *testing.T) {
	page := `<video id="player_one"><script>document.write(strencode2("%3Csource%20src%3D%22https%3A%2F%2Fccm.91p52.com%2F358999.mp4%3Fst%3D7f3a%26f%3D91%22%20type%3D%22video%2Fmp4%22%3E"));</script></video>`
	address, err := p91SourceFromPage(page, "https://91porn.com/view_video.php?viewkey=x")
	if err != nil {
		t.Fatalf("站方 CDN 直链被误杀: %v", err)
	}
	want := "https://ccm.91p52.com/358999.mp4?st=7f3a&f=91"
	if address != want {
		t.Fatalf("直链错误:\n got %s\nwant %s", address, want)
	}
}

// 站方模板有单引号、换行、以及 unescape() 的变体写法。
func TestP91SourceVariants(t *testing.T) {
	cases := []struct {
		name string
		page string
		want string
	}{
		{
			name: "单引号+空白",
			page: `<script>document.write(strencode2( '%3Csource%20src%3D%27https%3A%2F%2Fv9.91p52.com%2Fa.mp4%3Fst%3D1%27%3E' ));</script>`,
			want: "https://v9.91p52.com/a.mp4?st=1",
		},
		{
			name: "unescape写法",
			page: `<script>document.write(unescape("%3Csource%20src%3D%22https%3A%2F%2Fv9.91p52.com%2Fb.mp4%22%3E"));</script>`,
			want: "https://v9.91p52.com/b.mp4",
		},
		{
			name: "页面直出source",
			page: `<video id="player_one"><source src="https://v9.91p52.com/c.mp4?st=2" type="video/mp4"></video>`,
			want: "https://v9.91p52.com/c.mp4?st=2",
		},
		{
			name: "相对路径",
			page: `<video><source src="/mp43/1252139.mp4"></video>`,
			want: "https://91porn.com/mp43/1252139.mp4",
		},
		{
			name: "hls地址",
			page: `<video><source src="https://v9.91p52.com/d.m3u8?st=3"></video>`,
			want: "https://v9.91p52.com/d.m3u8?st=3",
		},
	}
	for _, item := range cases {
		address, err := p91SourceFromPage(item.page, "https://91porn.com/view_video.php?viewkey=x")
		if err != nil {
			t.Fatalf("%s 解析失败: %v", item.name, err)
		}
		if address != item.want {
			t.Fatalf("%s 直链错误:\n got %s\nwant %s", item.name, address, item.want)
		}
	}
}

// 加密串里可能出现孤立 % 或 %uXXXX，PathUnescape 会直接失败，这里必须容错。
func TestP91PercentDecode(t *testing.T) {
	if got := p91PercentDecode("%3Csource%20src%3D%22a.mp4%22%3E"); got != `<source src="a.mp4">` {
		t.Fatalf("常规解码错误: %q", got)
	}
	if got := p91PercentDecode("100%25%20done"); got != "100% done" {
		t.Fatalf("百分号解码错误: %q", got)
	}
	if got := p91PercentDecode("%u4e2d%u6587"); got != "中文" {
		t.Fatalf("%%u 解码错误: %q", got)
	}
	if got := p91PercentDecode("50%off"); got != "50%off" {
		t.Fatalf("孤立百分号不应报错: %q", got)
	}
}

func TestP91RejectAdPlaceholder(t *testing.T) {
	if !p91BlockedMediaHost("https://cdn.kwai.net/ads/x.mp4") {
		t.Fatalf("广告域名应被识别")
	}
	page := `<video><source src='https://cdn.kwai.net/ad/1.mp4' type='video/mp4'></video>`
	if _, err := p91SourceFromPage(page, "https://91porn.com/view_video.php?viewkey=x"); err == nil {
		t.Fatalf("只有广告片时应报无可用地址")
	}
	if p91PlayableMediaURL("https://91porn.com/images/logo.png") {
		t.Fatalf("图片不应判为可播放")
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

// 一页里可能同时有真地址、相对路径、广告片；取候选要按可靠性排序、去重并统计被拦条数。
func TestP91MediaCandidates(t *testing.T) {
	page := `<video id="player_one">
<script>document.write(strencode2("%3Csource%20src%3D%22https%3A%2F%2Fccm.91p52.com%2F1.mp4%3Fst%3Da%22%3E"));</script>
<source src="https://ccm.91p52.com/1.mp4?st=a">
<source src="/mp43/2.mp4">
<source src="https://cdn.kwai.net/ad/3.mp4">
</video>`
	list, blocked := p91MediaCandidates(page, "https://91porn.com/view_video.php?viewkey=x")
	if len(list) != 2 {
		t.Fatalf("候选数错误: %v", list)
	}
	if list[0] != "https://ccm.91p52.com/1.mp4?st=a" {
		t.Fatalf("首选项错误: %s", list[0])
	}
	if list[1] != "https://91porn.com/mp43/2.mp4" {
		t.Fatalf("次选项错误: %s", list[1])
	}
	if blocked == 0 {
		t.Fatalf("广告片应计入拦截数")
	}
}

func TestP91ProbeReason(t *testing.T) {
	if got := p91ProbeReason(errors.New("91porn 直链返回 HTTP 403")); got != "HTTP 403" {
		t.Fatalf("403 说明错误: %s", got)
	}
	if got := p91ProbeReason(errors.New("dial tcp: lookup ccm.91p52.com: no such host")); got != "域名解析失败" {
		t.Fatalf("DNS 说明错误: %s", got)
	}
}
