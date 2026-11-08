package core

import "testing"

// 以下片段取自 91porny.com 真实页面（2026-11 抓取），保留原始属性顺序，
// 用于回归「站方改版把正则打断」这一类问题。
const jiuseCardsSample = `
<div class="colVideoList">
    <div class="video-elem">
        <a target="_blank" class="display d-block" href="https://wswmx.bilkyv.com/mj/?channelCode=js1019">
            <div class="img" style="background-image: url('https://example.com/ad.jpg')"></div>
            <small class="layer"> 18:18</small>
        </a>
        <a target="_blank" class="title text-sub-title mt-2 mb-1" href="https://wswmx.bilkyv.com/mj/?channelCode=js1019">顶级万人求迷奸偷拍越反抗越兴奋</a>
        <small class="d-inline-block text-truncate mt-1 mb-1" style="max-width: 100%;">
            <div class="text-muted">
                作者: 发哥                    </div>
        </small>
    </div>
</div>
<div class="colVideoList">
    <div class="video-elem">
        <a target="_self" class="display d-block" href="/video/view/e7f9b65d89f5238b6211">
            <div class="scale"></div>
                        <div class="img" style="background-image: url('//efa3f4dc6d.ucloud165.xyz/thumb/1258066.jpg')"></div>
                        <small class="layer"> 00:04:31</small>
        </a>
        <a target="_self" class="title text-sub-title mt-2 mb-1" href="/video/view/e7f9b65d89f5238b6211">[原创] (补发)约已婚御姐初中老师KTV唱歌最后内射</a>
        <small class="text-sub-title d-inline-block text-truncate mt-1 mb-1" style="max-width: 100%;">
            <div class="text-muted">
                作者: <a class="text-dark" href="/author/ttv88880">ttv88880</a>                    </div>
            <div class="text-muted">
                18小时前&nbsp;|&nbsp;18.8万次播放                    </div>
        </small>
    </div>
</div>
<div class="colVideoList">
    <div class="video-elem">
        <a target="_self" class="display d-block" href="/video/view/9a1b2c3d4e5f60718293">
            <div class="img" style="background-image: url('//efa3f4dc6d.ucloud165.xyz/thumb/1258449.jpg')"></div>
            <small class="layer"> 00:03:20</small>
                    <small class="vip-layer"> 高清 </small>
                            </a>
        <a target="_self" class="title text-sub-title mt-2 mb-1" href="/video/view/9a1b2c3d4e5f60718293">[原创] 高清无码姐妹花</a>
        <small class="text-sub-title d-inline-block text-truncate mt-1 mb-1" style="max-width: 100%;">
            <div class="text-muted">
                作者: <a class="text-dark" href="/author/%E5%B0%91%E5%A6%87%E6%89%B9%E5%8F%91%E5%95%86">少妇批发商</a>                    </div>
        </small>
    </div>
</div>
`

const jiuseDetailSample = `<!doctype html>
<html lang="zh-cn"><head>
<title>[原创] 巨乳御姐摇摇车 - 91视频|91自拍|国产自拍</title>
<meta property="og:image" content="https://efa3f4dc6d.ucloud165.xyz/thumb/1258072.jpg" />
<script type="application/ld+json">{ "@context": "http://schema.org", "@type": "VideoObject", "name": "[原创] 巨乳御姐摇摇车", "thumbnailUrl": "https://efa3f4dc6d.ucloud165.xyz/thumb/1258072.jpg", "uploadDate": "2026-11-06T23:11:52+08:00", "duration": "PT3M37S", "url": "https://91porny.com/video/view/dd52a6d867d4f6b7c86d" }</script>
</head><body>
<div id="videoShowPage">
    <video id="video-play" playsinline class="video-js vjs-default-skin vjs-big-play-centered" data-poster="https://efa3f4dc6d.ucloud165.xyz/thumb/1258072.jpg" data-src="https://cdn2.jiuse3.cloud/s/1794204000/LvvmznlCIaUy0-AfR48pBA/hls/1258072/index.m3u8">
        <track kind="captions" src="/static/css/91captions.vtt" srclang="zh" label="Chinese" default>
    </video>
    <h4 class="container-title py-3 mb-0">[原创] 巨乳御姐摇摇车</h4>
    <a href="/author/ttv88880">ttv88880</a>
</div>
</body></html>`

const jiusePlaceholderPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:6
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXTINF:6.000000,
index0.ts?__vnotice=1002
#EXT-X-ENDLIST
`

const jiuseRealPlaylist = `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:8
#EXT-X-MEDIA-SEQUENCE:0
#EXT-X-PLAYLIST-TYPE:VOD
#EXTINF:8.341333,
index0.ts
#EXTINF:8.341333,
index1.ts
#EXTINF:8.341333,
index2.ts
#EXTINF:8.320000,
index3.ts
#EXT-X-ENDLIST
`

func TestJiuseCardsParseSkipsAds(t *testing.T) {
	items := parseJiuseCards(jiuseCardsSample, "")
	// 广告卡片没有 /video/view/ 链接，必须被丢掉。
	if len(items) != 2 {
		t.Fatalf("应解析出 2 条，实际 %d", len(items))
	}
	first := items[0]
	if first.SourceID != "e7f9b65d89f5238b6211" || first.Source != sourceJiuse {
		t.Fatalf("首条 ID/来源不对：%+v", first)
	}
	if first.Title != "[原创] (补发)约已婚御姐初中老师KTV唱歌最后内射" {
		t.Fatalf("标题不对：%q", first.Title)
	}
	if first.Cover != "https://efa3f4dc6d.ucloud165.xyz/thumb/1258066.jpg" {
		t.Fatalf("封面没补全协议：%v", first.Cover)
	}
	if first.Author != "ttv88880" || first.AuthorID != "ttv88880" {
		t.Fatalf("作者不对：%q / %q", first.Author, first.AuthorID)
	}
	if first.VIP != nil {
		t.Fatalf("普通卡片不该标 VIP")
	}
	vip := items[1]
	if vip.VIP == nil || !*vip.VIP {
		t.Fatalf("vip-layer 卡片应标 VIP")
	}
	if vip.Author != "少妇批发商" {
		t.Fatalf("中文作者没解码：%q", vip.Author)
	}
}

func TestJiuseDetailParse(t *testing.T) {
	drama, chapters, err := parseJiuseDetail(jiuseDetailSample, "dd52a6d867d4f6b7c86d")
	if err != nil {
		t.Fatalf("详情解析失败：%v", err)
	}
	if drama.Title != "[原创] 巨乳御姐摇摇车" {
		t.Fatalf("标题不对：%q", drama.Title)
	}
	if drama.Duration != 217 {
		t.Fatalf("时长应为 217 秒（PT3M37S），实际 %v", drama.Duration)
	}
	if len(chapters) != 1 {
		t.Fatalf("应只有 1 个正片章节，实际 %d", len(chapters))
	}
	want := "https://cdn2.jiuse3.cloud/s/1794204000/LvvmznlCIaUy0-AfR48pBA/hls/1258072/index.m3u8"
	if chapters[0].VideoURL != want {
		t.Fatalf("播放地址不对：%q", chapters[0].VideoURL)
	}
}

func TestJiusePlaylistUsable(t *testing.T) {
	if jiusePlaylistUsable(jiusePlaceholderPlaylist) {
		t.Fatal("单分片 + __vnotice 的占位列表必须判为不可用")
	}
	if !jiusePlaylistUsable(jiuseRealPlaylist) {
		t.Fatal("正常列表应判为可用")
	}
	if jiusePlaylistUsable("<!DOCTYPE html><html>402</html>") {
		t.Fatal("非 m3u8 内容必须判为不可用")
	}
}

func TestJiuseCatalogAddress(t *testing.T) {
	if got := jiuseCatalogAddress(1, "", ""); got != "https://91porny.com/video/index" {
		t.Fatalf("首页地址不对：%s", got)
	}
	if got := jiuseCatalogAddress(2, "", ""); got != "https://91porny.com/video/index?page=2" {
		t.Fatalf("列表分页地址不对：%s", got)
	}
	if got := jiuseCatalogAddress(1, "", "巨乳"); got != "https://91porny.com/search?keywords=%E5%B7%A8%E4%B9%B3" {
		t.Fatalf("搜索地址不对：%s", got)
	}
	if got := jiuseCatalogAddress(3, "", "巨乳"); got != "https://91porny.com/search?keywords=%E5%B7%A8%E4%B9%B3&page=3" {
		t.Fatalf("搜索分页地址不对：%s", got)
	}
}

func TestJiuseIDValidation(t *testing.T) {
	for _, id := range []string{"e7f9b65d89f5238b6211", "dd52a6d867d4f6b7c86d"} {
		if !validJiuseID(id) {
			t.Fatalf("%s 应合法", id)
		}
	}
	for _, id := range []string{"", "short", "../etc/passwd", "e7f9b65d89f5238b6211&x=1", "zzzzzzzzzzzzzzzzzzzz"} {
		if validJiuseID(id) {
			t.Fatalf("%q 应判为非法", id)
		}
	}
}

func TestJiuseCategoryValidation(t *testing.T) {
	if !validJiuseCategory("") || !validJiuseCategory("  ") {
		t.Fatal("「全部」应合法")
	}
	// 站内没有自拍题材分类，任何非空分类都要拒掉，避免点开一个空列表。
	if validJiuseCategory("chinese") || validJiuseCategory("not-a-category") {
		t.Fatal("非空分类应被拒")
	}
}
