package core

import (
	"strings"
	"testing"
)

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
	// 「全部」不再走 /video/index：那是「最近更新」的 VIP 新片池，实测整页 1002。
	// 改走收藏最多，翻到第 5 页仍全部可播。
	if got := jiuseCatalogAddress(1, "", ""); got != "https://91porny.com/video/category/most-favorite" {
		t.Fatalf("默认目录地址不对：%s", got)
	}
	if got := jiuseCatalogAddress(2, "", ""); got != "https://91porny.com/video/category/most-favorite?page=2" {
		t.Fatalf("列表分页地址不对：%s", got)
	}
	if got := jiuseCatalogAddress(1, "top-list", ""); got != "https://91porny.com/video/category/top-list" {
		t.Fatalf("分类地址不对：%s", got)
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
	if !validJiuseCategory("most-favorite") || !validJiuseCategory("top-list") {
		t.Fatal("登记过的可播分类应合法")
	}
	// latest / hot-list / ori / nonpaid / long-list 这些分类实测整池 1002，
	// 已经从列表里摘掉，传进来必须拒掉，避免又挂出一个全死的入口。
	for _, dead := range []string{"latest", "hot-list", "ori", "nonpaid", "long-list", "chinese", "not-a-category"} {
		if validJiuseCategory(dead) {
			t.Fatalf("%s 是死分类，应被拒", dead)
		}
	}
}

func TestJiuseAuthorSlugValidation(t *testing.T) {
	// 站内作者 slug 就是显示名，中文名必须放行（/author/91大黄鸭 实测可访问）。
	for _, ok := range []string{"abc", "user_name-1", "a.b", "名字", "91大黄鸭", "粉柚映画", "Mr.Smith", "QAQ___"} {
		if !validJiuseAuthorSlug(ok) {
			t.Fatalf("%s 应合法", ok)
		}
	}
	for _, bad := range []string{"", "  ", "a/b", "../x", "a?b", "a#b", "a%b", "a&b", strings.Repeat("a", 97)} {
		if validJiuseAuthorSlug(bad) {
			t.Fatalf("%q 应被拒", bad)
		}
	}
}

// 详情页下方全是推荐卡片，每张都写「作者: <a href="/author/…">」。
// 早期在整页上取第一个「作者:」，结果每部片的作者都变成推荐位第一条的作者。
func TestJiuseDetailAuthorIgnoresRecommendations(t *testing.T) {
	head := strings.Repeat("x", 9000) + `
	<i class="fas fa-user"></i>
	<a href="/author/Judre"> Judre </a>`
	page := head + `
	<div class="colVideoList">
	  <a class="title" href="/video/view/e7f9b65d89f5238b6211">推荐片</a>
	  <div class="text-muted">作者: <a class="text-dark" href="/author/ukaodazx">ukaodazx</a></div>
	</div>`

	if got := jiuseDetailHead(page); strings.Contains(got, "ukaodazx") {
		t.Fatal("推荐位应被截断")
	}
	match := reJiuseAuthorLink.FindStringSubmatch(jiuseDetailHead(page))
	if match == nil || strings.TrimSpace(match[1]) != "Judre" {
		t.Fatalf("应解析出本片作者 Judre，实际 %v", match)
	}
}

// 十几秒的短片只有 2 段，但有 ENDLIST，属于完整片单，不能判废。
func TestJiusePlaylistUsableShortVod(t *testing.T) {
	short := "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:8\n" +
		"#EXTINF:8.341667,\nindex0.ts\n#EXTINF:3.636967,\nindex1.ts\n#EXT-X-ENDLIST\n"
	if !jiusePlaylistUsable(short) {
		t.Fatal("2 段且带 ENDLIST 的短片应可用")
	}
	open := "#EXTM3U\n#EXTINF:8.0,\nindex0.ts\n"
	if jiusePlaylistUsable(open) {
		t.Fatal("没有 ENDLIST 且不足 3 段应判废")
	}
	placeholder := "#EXTM3U\n#EXTINF:6.0,\nindex0.ts?__vnotice=1002\n#EXT-X-ENDLIST\n"
	if jiusePlaylistUsable(placeholder) {
		t.Fatal("1002 占位列表必须判废")
	}
}

func TestParseJiuseCardsSkipsVip(t *testing.T) {
	page := `<div class="colVideoList">
	<a href="/video/view/e7f9b65d89f5238b6211"><img class="img" src="/a.jpg"></a>
	<a class="title" href="/video/view/e7f9b65d89f5238b6211">VIP 片</a>
	<small class="layer">03:20</small><small class="vip-layer">高清</small>
</div>
<div class="colVideoList">
	<a href="/video/view/7dff460579f327b234a2"><img class="img" src="/b.jpg"></a>
	<a class="title" href="/video/view/7dff460579f327b234a2">免费片</a>
	<small class="layer">05:00</small>
</div>`

	all := parseJiuseCards(page, "")
	if len(all) != 2 {
		t.Fatalf("不过滤时应有两张卡，实际 %d", len(all))
	}
	if all[0].VIP == nil || !*all[0].VIP {
		t.Fatal("带 vip-layer 的卡片应标 VIP")
	}

	free := parseJiuseCardsFiltered(page, "", true)
	if len(free) != 1 || free[0].SourceID != "7dff460579f327b234a2" {
		t.Fatalf("过滤后应只剩免费片，实际 %+v", free)
	}
	if got := jiuseFreeOnly(all); len(got) != 1 || got[0].SourceID != "7dff460579f327b234a2" {
		t.Fatalf("jiuseFreeOnly 结果不对：%+v", got)
	}
}

// 3.33：91 详情借九色之后，剧集与章节身份必须换回 91 源，
// 且不能把九色的 m3u8 带进章节（否则 resolveP91Media 会当直链起播）。
func TestP91DetailFromJiuse(t *testing.T) {
	const viewkey = "dd52a6d867d4f6b7c86d"
	in := Drama{
		ID:    providerDramaID(sourceJiuse, viewkey),
		Source: sourceJiuse,
		Title:  "测试片",
	}
	chapters := []Chapter{{
		ID:       providerChapterID(sourceJiuse, viewkey, "main"),
		Source:   sourceJiuse,
		VideoURL: "https://cdn.example.com/index.m3u8",
		PageURL:  "https://91porny.com/video/view/" + viewkey,
	}}
	got, gotChapters := p91DetailFromJiuse(in, chapters, viewkey)
	if got.ID != providerDramaID(sourceP91, viewkey) || got.Source != sourceP91 {
		t.Fatalf("剧集身份没换回 91：%+v", got)
	}
	if got.ChannelName != "91porn" || got.SourceID != viewkey {
		t.Fatalf("频道/源 ID 不对：%+v", got)
	}
	if len(gotChapters) != 1 {
		t.Fatalf("章节数 = %d", len(gotChapters))
	}
	chapter := gotChapters[0]
	if chapter.Source != sourceP91 || chapter.ID != providerChapterID(sourceP91, viewkey, "main") {
		t.Fatalf("章节身份没换回 91：%+v", chapter)
	}
	if chapter.VideoURL != "" {
		t.Fatalf("不能把九色地址塞进 91 章节：%s", chapter.VideoURL)
	}
	if chapter.PageURL != p91SiteURL+"/view_video.php?viewkey="+viewkey {
		t.Fatalf("PageURL 应指回 91 主站：%s", chapter.PageURL)
	}
	_, empty := p91DetailFromJiuse(Drama{}, nil, viewkey)
	if len(empty) != 1 || empty[0].Source != sourceP91 || empty[0].VideoURL != "" {
		t.Fatalf("空章节兜底失败：%+v", empty)
	}
}
