package core

import (
	"strings"
	"testing"
)

const hsexListFixture = `
<div class="row">
        <div class="col-xs-6 col-md-3">
        <div class="thumbnail">
            <a target="_self" href="video-1257475.htm">
                <div class="image" style="background-image: url('https://i.hdcdn.store/thumb/1257475.webp')" title= "[原创] 沈阳有娇小的母狗想被爆操吗？私信">
                    <div class="marker-overlays">
                        <var class="duration">00:19</var>                    </div>
                </div>
            </a>
            <div class="caption title">
                <h5><a href="video-1257475.htm">[原创] 沈阳有娇小的母狗想被爆操吗？私信</a></h5></div>
            <div class="info">
                <p>&nbsp;&nbsp;<a href="user.htm?author=gxd89697031">&nbsp;gxd89697031</a><br/>
                &nbsp;&nbsp;6.4k次观看&nbsp;&nbsp;4天前</p>
            </div>
        </div>
    </div>
        <div class="col-xs-6 col-md-3">
        <div class="thumbnail">
            <a target="_self" href="video-1257474.htm">
                <div class="image" style="background-image: url('https://i.hdcdn.store/thumb/1257474.webp')" title= "[原创] 无广-贵州遵义母狗人妻续集">
                    <div class="marker-overlays">
                        <var class="duration">01:52</var><span class="hd-thumbnail">HD</span>                    </div>
                </div>
            </a>
            <div class="caption title">
                <h5><a href="video-1257474.htm">[原创] 无广-贵州遵义母狗人妻续集</a></h5></div>
            <div class="info">
                <p>&nbsp;&nbsp;<a href="user.htm?author=guizhou9991">&nbsp;guizhou9991</a><br/>
                &nbsp;&nbsp;13.9k次观看&nbsp;&nbsp;4天前</p>
            </div>
        </div>
    </div>
</div>
<h2><a href="list-1.htm">最新</a></h2>
`

func TestParseHsexCards(t *testing.T) {
	items := parseHsexCards(hsexListFixture, "new")
	if len(items) != 2 {
		t.Fatalf("解析出 %d 条，期望 2 条", len(items))
	}
	if items[0].SourceID != "1257475" || items[0].ID != "hsex:1257475" {
		t.Fatalf("首条 ID 异常: %+v", items[0])
	}
	if items[0].Title != "[原创] 沈阳有娇小的母狗想被爆操吗？私信" {
		t.Fatalf("标题异常: %q", items[0].Title)
	}
	if items[0].Cover != "https://i.hdcdn.store/thumb/1257475.webp" {
		t.Fatalf("封面异常: %v", items[0].Cover)
	}
	if items[0].Duration != 19 || items[1].Duration != 112 {
		t.Fatalf("时长异常: %v %v", items[0].Duration, items[1].Duration)
	}
	if items[0].Source != sourceHsex || items[0].ChannelName != "好色TV" {
		t.Fatalf("来源字段异常: %+v", items[0])
	}
}

func TestHsexCatalogAddress(t *testing.T) {
	if got := hsexCatalogAddress(1, "", ""); got != "https://hsex.icu/list-1.htm" {
		t.Fatalf("默认分类地址 = %s", got)
	}
	if got := hsexCatalogAddress(3, "weekly", ""); got != "https://hsex.icu/top7_list-3.htm" {
		t.Fatalf("周榜地址 = %s", got)
	}
	if got := hsexCatalogAddress(2, "5min", ""); got != "https://hsex.icu/5min_list-2.htm" {
		t.Fatalf("5分钟地址 = %s", got)
	}
	search := hsexCatalogAddress(1, "", "原创")
	if !strings.HasPrefix(search, "https://hsex.icu/search.htm?search=") ||
		!strings.HasSuffix(search, "&sort=new") {
		t.Fatalf("搜索地址异常: %s", search)
	}
}

func TestHsexPlayAddress(t *testing.T) {
	page := `<video id="video-play" poster="https://i.hdcdn.store/thumb/1257475.webp">
        <source id="video-source" src="https://cdn.hdcdn.store/1794183362/gM9/hls/1257475/index.m3u8" type="application/x-mpegURL" />
    </video>`
	got := hsexPlayAddress(page)
	want := "https://cdn.hdcdn.store/1794183362/gM9/hls/1257475/index.m3u8"
	if got != want {
		t.Fatalf("播放地址 = %q，期望 %q", got, want)
	}
	if hsexPlayAddress("<video></video>") != "" {
		t.Fatal("没有 source 时应返回空")
	}
}

func TestHsexDetailTitleAndValidation(t *testing.T) {
	page := `<h3 class="panel-title">[原创] 测试标题</h3><title>[原创] 测试标题 - 好色™ Tv</title>
	<div class="col-md-3 col-xs-6">作者：<a href="user.htm?author=gxd89697031">gxd89697031</a></div>
	<div class="col-md-3 col-xs-6">时长：01:52</div>`
	if got := hsexDetailTitle(page); got != "[原创] 测试标题" {
		t.Fatalf("标题 = %q", got)
	}
	if got := hsexDetailTitle("<title>只有标题 - 好色™ Tv</title>"); got != "只有标题" {
		t.Fatalf("标题回退 = %q", got)
	}
	for _, ok := range []string{"1", "1257475"} {
		if !validHsexID(ok) {
			t.Fatalf("%s 应合法", ok)
		}
	}
	for _, bad := range []string{"", "12a", "../1", strings.Repeat("9", 13)} {
		if validHsexID(bad) {
			t.Fatalf("%q 应被拒", bad)
		}
	}
}

// 3.32：播放列表里的相对分片必须改写成绝对地址，否则刷新列表时
// 时间戳目录一变，中转按 host+path 就再也匹配不上旧分片。
func TestHsexAbsolutePlaylist(t *testing.T) {
	body := "#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:6.3,\nindex0.ts\n#EXTINF:5.6,\nindex1.ts\n#EXT-X-ENDLIST\n"
	base := "https://cdn.hdcdn.store/1794185829/TK/hls/1257475/index.m3u8"
	got := hsexAbsolutePlaylist(body, base)
	if strings.Contains(got, "\nindex0.ts") || strings.Contains(got, "\nindex1.ts") {
		t.Fatalf("相对分片未绝对化：\n%s", got)
	}
	want := "https://cdn.hdcdn.store/1794185829/TK/hls/1257475/index0.ts"
	if !strings.Contains(got, want) {
		t.Fatalf("绝对地址异常：\n%s", got)
	}
	if !strings.HasPrefix(got, "#EXTM3U") || !strings.Contains(got, "#EXT-X-ENDLIST") {
		t.Fatalf("列表结构被破坏：\n%s", got)
	}
}

// 站方作者页分页在路径里，且中文作者名不能二次转义。
func TestHsexAuthorAddress(t *testing.T) {
	if got := hsexAuthorAddress("gxd89697031", 2); got != "https://hsex.icu/user-2.htm?author=gxd89697031" {
		t.Fatalf("作者页地址 = %s", got)
	}
	got := hsexAuthorAddress("91%E5%9D%8F%E5%8F%94%E5%8F%94", 3)
	if !strings.HasPrefix(got, "https://hsex.icu/user-3.htm?author=") || strings.Contains(got, "%25") {
		t.Fatalf("中文作者不应二次转义：%s", got)
	}
}

func TestHsexAuthorSlugValidation(t *testing.T) {
	for _, ok := range []string{"gxd89697031", "91坏叔叔", "a.b-c", "91%E5%9D%8F%E5%8F%94%E5%8F%94"} {
		if !validHsexAuthorSlug(ok) {
			t.Fatalf("%q 应合法", ok)
		}
	}
	for _, bad := range []string{"", "  ", "../x", "a/b", "a?b", "a&b", "a\\b", strings.Repeat("x", 300)} {
		if validHsexAuthorSlug(bad) {
			t.Fatalf("%q 应被拒", bad)
		}
	}
}

// 3.33：拨号层只把「被污染的页面域名」换成 Cloudflare 边缘 IP，
// 端口保留、域名不再出现；CDN 与其它站源一个都不能被牵连。
func TestHsexDialAddresses(t *testing.T) {
	pinned, ok := hsexDialAddresses("hsex.icu:443")
	if !ok || len(pinned) != len(hsexEdgeIPs) {
		t.Fatalf("hsex.icu 应展开成 %d 条边缘线路，实际 %v / %v", len(hsexEdgeIPs), pinned, ok)
	}
	for _, address := range pinned {
		if strings.Contains(address, "hsex.icu") {
			t.Fatalf("拨号地址里不该再出现被污染的域名：%s", address)
		}
		if !strings.HasSuffix(address, ":443") {
			t.Fatalf("端口应保留：%s", address)
		}
	}
	for _, host := range []string{"www.hsex.icu", "HSEX.ICU", " www.hsex.icu "} {
		if _, ok := hsexDialAddresses(host + ":443"); !ok {
			t.Fatalf("%q 也应被改写", host)
		}
	}
	for _, address := range []string{
		"cdn.hdcdn.store:443", // m3u8 / 分片 CDN，未被污染，不能改写
		"i.hdcdn.store:443",
		"91porn.com:443",
		"91porny.com:443",
		"not-hsex.icu:443",
	} {
		if _, ok := hsexDialAddresses(address); ok {
			t.Fatalf("%s 不该被改写", address)
		}
	}
	if isHsexPageHost("162.125.80.5") {
		t.Fatal("污染 IP 不是页面域名")
	}
}
