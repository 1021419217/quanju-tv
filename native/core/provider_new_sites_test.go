package core

import (
	"strings"
	"testing"
)

// packer 混淆是 KISSAV 取址的关键：解不开就拿不到重签后的代理地址。
func TestKissavUnpackRestoresDictionary(t *testing.T) {
	payload := `0(1)`
	got := kissavUnpack(payload, 62, []string{"create_player", "'/media/m3u8/?url=x'", ""})
	if !strings.Contains(got, "create_player") || !strings.Contains(got, "/media/m3u8/") {
		t.Fatalf("解包失败: %q", got)
	}
}

func TestKissavPlayerURLFromUnpackedPacker(t *testing.T) {
	// 真实页面的结构：eval(function(p,a,c,k,e,d){...}('1(...)',62,3,'...'.split('|'),0,{}))
	page := `<html><script>eval(function(p,a,c,k,e,d){e=function(c){return c};if(!''.replace(/^/,String)){while(c--){d[c]=k[c]||c}k=[function(e){return d[e]}];e=function(){return'\\w+'};c=1};while(c--){if(k[c]){p=p.replace(new RegExp('\\b'+e(c)+'\\b','g'),k[c])}}return p}('0({\'url\':\'2\',\'id\':\'mse\'})',62,3,'create_player|static|/media/m3u8/?url=https%3A%2F%2Fx.m3u8%3Fauth_key%3D1-2-0-3&exp=1794173462&token=abc'.split('|'),0,{}))</script></html>`
	address, err := kissavPlayerURL(page)
	if err != nil {
		t.Fatalf("取址失败: %v", err)
	}
	if !strings.HasPrefix(address, kissavBaseURL+"/media/m3u8/?url=") {
		t.Fatalf("地址不对: %s", address)
	}
	if !strings.Contains(address, "&exp=1794173462") {
		t.Fatalf("缺少 exp: %s", address)
	}
}

func TestKissavCardsParse(t *testing.T) {
	page := `<div class="video-img-box">
  <div class="detail"><p class="title"><a href="/video/0a77facc8f6d17edf5c7ad53907d00bf/" title="t">标题一</a></p></div>
  <div class="img"><a href="/video/0a77facc8f6d17edf5c7ad53907d00bf/"><img z-image-loader-url="https://xi.example/a.jpg" alt="标题一"></a></div>
</div>`
	items := parseKissavCards(page, "new")
	if len(items) != 1 {
		t.Fatalf("卡片数错误: %d", len(items))
	}
	if items[0].SourceID != "0a77facc8f6d17edf5c7ad53907d00bf" || items[0].Title != "标题一" {
		t.Fatalf("字段错误: %+v", items[0])
	}
	if items[0].Cover != "https://xi.example/a.jpg" {
		t.Fatalf("封面错误: %v", items[0].Cover)
	}
}

// 站方在 img 属性之间漏了空格（loading="lazy"src=），封面正则不能要求空白。
func TestViralCardsParseCoverWithoutSpace(t *testing.T) {
	page := `<div class=" vx-th item  " data-cw-id="547954">
  <a class="vx-media" href="https://viralxxxporn.com/zh/video/547954/comatozze-step/" title="ComaTozze Step">
    <img class="vx-img " loading="lazy"src="https://imgcdn.example/547000/547954/800x450/2.jpg" alt="ComaTozze Step">
    <div class="vx-duration">21:52</div>
  </a>
  <div class="vx-content">
    <a class="vx-text" href="https://viralxxxporn.com/zh/video/547954/comatozze-step/">ComaTozze Step</a>
    <ul class="vx-list"><li><a href="https://viralxxxporn.com/zh/members/213512/" class="video-owner" title="The Viral Hunter">The Viral Hunter</a></li></ul>
  </div>
</div>`
	items := parseViralCards(page, "latest")
	if len(items) != 1 {
		t.Fatalf("卡片数错误: %d", len(items))
	}
	item := items[0]
	if item.SourceID != "547954/comatozze-step" {
		t.Fatalf("ID 错误: %s", item.SourceID)
	}
	if item.Title != "ComaTozze Step" {
		t.Fatalf("标题错误: %q", item.Title)
	}
	if item.Cover != "https://imgcdn.example/547000/547954/800x450/2.jpg" {
		t.Fatalf("封面错误: %v", item.Cover)
	}
	if item.Duration != 1312 {
		t.Fatalf("时长错误: %v", item.Duration)
	}
	if item.Author != "The Viral Hunter" || item.AuthorID != "213512" {
		t.Fatalf("作者错误: %q/%q", item.Author, item.AuthorID)
	}
}

func TestViralPlayURLPrefersHD(t *testing.T) {
	page := `var flashvars = { video_url: 'https://viralxxxporn.com/zh/get_file/3/a/1/2/2.mp4/?v-acctoken=x', video_url_text: '480p', video_alt_url: 'https://viralxxxporn.com/zh/get_file/3/b/1/2/2_720p.mp4/?v-acctoken=y', video_alt_url_text: '720p' };`
	got := viralPlayURL(page)
	if !strings.Contains(got, "_720p.mp4") {
		t.Fatalf("没有优先高清: %s", got)
	}
	// 只有标清时退回 video_url。
	page480 := `var flashvars = { video_url: 'https://viralxxxporn.com/zh/get_file/3/a/1/2/2.mp4/?v-acctoken=x' };`
	if got := viralPlayURL(page480); !strings.Contains(got, "2.mp4") {
		t.Fatalf("标清回退失败: %s", got)
	}
}

func TestViralCatalogAddressPaging(t *testing.T) {
	if got := viralCatalogAddress(1, "latest", ""); got != viralBaseURL+"/zh/latest-updates/" {
		t.Fatalf("首页地址错误: %s", got)
	}
	if got := viralCatalogAddress(3, "onlyfans", ""); got != viralBaseURL+"/zh/categories/onlyfans-leaks/3/" {
		t.Fatalf("分页地址错误: %s", got)
	}
	if got := viralCatalogAddress(2, "", "sophie"); got != viralBaseURL+"/zh/search/sophie/2/" {
		t.Fatalf("搜索分页地址错误: %s", got)
	}
}
