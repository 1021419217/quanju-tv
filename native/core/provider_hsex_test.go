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
