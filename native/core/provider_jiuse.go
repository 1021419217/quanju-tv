package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 九色 / 91PORNY（91porny.com）：Laravel 服务端渲染，内容与 91porn 同源
// （viewkey 完全一致，例如 e7f9b65d89f5238b6211 在两边是同一部片子）。
//
// 路径：
//
//	列表   /videos                     + "?page=N"
//	分类   /videos/categories/{slug}    + "?page=N"
//	搜索   /search?keywords={kw}        + "&page=N"
//	作者   /author/{slug}              + "?page=N"
//	详情   /video/view/{viewkey}
//	备用   /video/view/{viewkey}?server=line1|line2|line3
//
// 播放：详情页 <video id="video-play" data-src="…/hls/{vid}/index.m3u8">。
// 首页带头像角标「高清」的条目（<small class="vip-layer">高清</small>），站方
// 前端会在点击后弹注册/付费框，但**服务端照样把 data-src 写进 HTML**，所以
// 直接取页面里的地址即可，不经过它的前端逻辑；?server=line1/2/3 是同一部片
// 在其它 CDN 上的线路，默认线不可用时依次顶上。
//
// 可用性判定：CDN 对「未经授权的外部引用」不回 403，而是回一份 200 的占位
// 播放列表——单分片 6 秒，内容就是「视频资源暂不可用 1002 / 视频地址已失效」。
// 播放器拿到它只会播 6 秒然后停住，用户看到的就是「点了没反应」。所以取回
// 列表后要校验一遍，占位就换下一条线路，别把废地址丢给播放器。
const (
	jiuseBaseURL  = "https://91porny.com"
	jiusePageSize = 24
	// jiusePlayMinSegments：一条正常 HLS 至少这么多分片（占位列表只有 1 段）。
	jiusePlayMinSegments = 3
)

// 九色有多个同源镜像：同一个 viewkey、同一个 CDN、同一份数据，只是域名不同。
// 实测 2026-11：91porny.com / 91porny.org / 91porn.mom / 91porn.is 全部命中，
// 91porny.net / .xyz / .vip / .cc / .top / .me 是空壳或跳转。
//
// 主域在国内时不时被墙，而 91 现在正是借九色取流（见 provider_p91_jiuse.go），
// 主域一挂就等于 91 一起废。所以这里做一层「首个能出页面的域名」探测，缓存
// 30 分钟；探测失败就退回主域，不改变原有行为。
var jiuseBaseMirrors = []string{
	"https://91porny.org",
	"https://91porn.mom",
	"https://91porn.is",
}

var jiuseBaseState struct {
	mu        sync.Mutex
	active    string
	checkedAt time.Time
}

// jiuseHost 判断一个 host 是否属于九色（含镜像域名）。
func jiuseHost(host string) bool {
	host = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(host), "www."))
	switch host {
	case "91porny.com", "91porny.org", "91porn.mom", "91porn.is":
		return true
	}
	return false
}

// jiuseBase 返回当前可用的域名，未探测过就是主域。
func jiuseBase() string {
	jiuseBaseState.mu.Lock()
	defer jiuseBaseState.mu.Unlock()
	if jiuseBaseState.active != "" && time.Since(jiuseBaseState.checkedAt) < 30*time.Minute {
		return jiuseBaseState.active
	}
	return jiuseBaseURL
}

// ensureJiuseBase 在首次使用（或缓存过期）时探一次域名。
// 判断依据不是状态码——被墙的域名也会超时——而是页面里有没有卡片容器。
func (d *Downloader) ensureJiuseBase(ctx context.Context) string {
	jiuseBaseState.mu.Lock()
	fresh := jiuseBaseState.active != "" && time.Since(jiuseBaseState.checkedAt) < 30*time.Minute
	jiuseBaseState.mu.Unlock()
	if fresh {
		return jiuseBase()
	}
	for _, base := range append([]string{jiuseBaseURL}, jiuseBaseMirrors...) {
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		pageText, err := d.fetchProviderText(probeCtx, base+"/video/category/"+jiuseDefaultCategory, base+"/")
		cancel()
		if err != nil {
			continue
		}
		if !strings.Contains(pageText, "colVideoList") {
			continue
		}
		jiuseBaseState.mu.Lock()
		jiuseBaseState.active = base
		jiuseBaseState.checkedAt = time.Now()
		jiuseBaseState.mu.Unlock()
		return base
	}
	return jiuseBase()
}

// 分类：只保留 2026-11 实测**能播**的入口。
//
// 九色把片子分成两个池子，用同一个 CDN：
//
//	免费老片池 → 播放列表正常（几十到几百个分片）
//	VIP 新片池 → 一律 x-video-notice: 1002，四条线路、换 Referer、换指纹都一样
//
// 实测（每个入口抽 3 部，逐部取播放列表）：
//
//	收藏最多 most-favorite      3/3   翻到 p5 仍 3/3
//	本月收藏 top-favorite        3/3
//	本月讨论 month-discuss       3/3
//	本月最热 top-list            3/3
//	上月最热 top-last            3/3
//	最近得分 recent-rating       1/3
//	最近更新 latest              0/3   ← 首页挂的就是它，所以首页"全部放不了"
//	当前最热 hot-list            0/3
//	91原创 ori                   0/3
//	高清 hd                      0/3
//	非付费 nonpaid               0/3
//	10分钟以上 long-list         0/3
//	20分钟以上 longer-list       0/3
//	最近加精 recent-favorite     0/3
//
// 「非付费」这个名字是幌子，里面照样是 VIP 片，别再被它骗。挂全死分类只会
// 让用户点开一片红，所以这里不挂。
//
// 注意别挂 /videos/categories/*：那是站内另一套日本 AV 库，和 91 内容无关，
// 点开会全空。91 内容的分类只在 /video/category/ 下。
var jiuseCategories = []nativeCategory{
	{ID: "most-favorite", Name: "收藏最多"},
	{ID: "top-favorite", Name: "本月收藏"},
	{ID: "month-discuss", Name: "本月讨论"},
	{ID: "top-list", Name: "本月最热"},
	{ID: "top-last", Name: "上月最热"},
	{ID: "recent-rating", Name: "最近得分"},
}

// jiuseDefaultCategory 是「全部」实际走的分类。
//
// 站内没有真正意义上的"全量且免费"列表：/video/index 是最近更新，整整一池
// VIP；而收藏最多是免费老片，翻到第 5 页仍然全部可播，是目前最好的默认入口。
const jiuseDefaultCategory = "most-favorite"

var (
	reJiuseCardStart = regexp.MustCompile(`<div class="colVideoList`)
	reJiuseCardHref  = regexp.MustCompile(`href="/video/view/([0-9a-fA-F]{8,32})"`)
	reJiuseCardTitle = regexp.MustCompile(`(?is)class="title[^"]*"[^>]*>([^<]{1,300})</a>`)
	// 首页用 <div class="img" style="background-image:…">，列表页改用
	// <img class="img" src="…">，两种都要认，否则列表页全是无图。
	reJiuseCardCover = regexp.MustCompile(`class="img"[^>]*background-image:\s*url\(\'([^\']+)\'\)`)
	reJiuseCardImg   = regexp.MustCompile(`(?is)<img class="img"[^>]*\bsrc="([^"]+)"`)
	reJiuseCardLayer = regexp.MustCompile(`<small class="layer">\s*([0-9:]{4,10})\s*</small>`)
	reJiuseCardAuth  = regexp.MustCompile(`作者:\s*<a[^>]*href="/author/([^"]+)"[^>]*>([^<]{0,80})</a>`)
	// 详情页的作者区不一定带「作者:」前缀，只认链接本身。
	reJiuseAuthorLink = regexp.MustCompile(`<a[^>]*href="/author/([^"]+)"[^>]*>([^<]{1,80})</a>`)

	reJiusePlayer    = regexp.MustCompile(`(?is)<video[^>]*id="video-play"[^>]*>`)
	reJiuseDataSrc   = regexp.MustCompile(`data-src="([^"]+)"`)
	reJiusePoster    = regexp.MustCompile(`data-poster="([^"]+)"`)
	reJiuseH4        = regexp.MustCompile(`(?is)<h4 class="container-title[^"]*"[^>]*>(.*?)</h4>`)
	reJiuseSchemaDur = regexp.MustCompile(`"duration":\s*"PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?"`)
	reJiuseVipLayer  = regexp.MustCompile(`class="vip-layer"`)
	// 老卡片只写「作者: 名字」不带链接，新卡片才包一层 <a href="/author/…">。
	reJiuseCardAuthPlain = regexp.MustCompile(`作者:\s*([^<\s][^<]{0,39})`)
)

func validJiuseID(id string) bool {
	if len(id) < 8 || len(id) > 32 {
		return false
	}
	for _, char := range id {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return false
		}
	}
	return true
}

// validJiuseCategory 只认上面登记过的分类 slug（空字符串是「全部」）。
func validJiuseCategory(category string) bool {
	category = strings.TrimSpace(category)
	if category == "" {
		return true
	}
	for _, entry := range jiuseCategories {
		if entry.ID == category {
			return true
		}
	}
	return false
}

func jiuseCategoryName(category string) string {
	for _, entry := range jiuseCategories {
		if entry.ID == category {
			return entry.Name
		}
	}
	return ""
}

func jiuseAbsoluteAddress(raw string) string {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "&amp;", "&"))
	if strings.HasPrefix(raw, "//") {
		return "https:" + raw
	}
	return raw
}

func jiuseDrama(id, title, cover, author, authorID, category string, duration any, vip bool) Drama {
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	if title == "" {
		title = id
	}
	drama := Drama{
		ID:           providerDramaID(sourceJiuse, id),
		Source:       sourceJiuse,
		SourceID:     id,
		Title:        title,
		Name:         title,
		ChannelName:  "九色",
		Author:       strings.TrimSpace(author),
		AuthorID:     strings.TrimSpace(authorID),
		Category:     category,
		CategoryName: jiuseCategoryName(category),
	}
	if cover != "" {
		drama.Cover = cover
	}
	if duration != nil {
		drama.Duration = duration
	}
	if vip {
		badge := true
		drama.VIP = &badge
		if drama.Badge == "" {
			drama.Badge = "高清"
		}
	}
	return drama
}

// parseJiuseCards 按卡片容器切块：同一块里的 display 链接与标题链接指向同一
// 个 viewkey，外链广告卡片没有 /video/view/ 地址，会被自然跳过。
func parseJiuseCards(pageText, category string) []Drama {
	return parseJiuseCardsFiltered(pageText, category, false)
}

// parseJiuseCardsFiltered 是 parseJiuseCards 的可过滤版本。
//
// skipVip 只给「全部」用：站内每个类目页都混着带 <small class="vip-layer">高清</small>
// 角标的卡片，这类卡片点进去 CDN 一律回 1002（见 resolveJiuseMedia）。用户在
// 真机上验证「分类里的带标片能播、首页的带标片不能播」，所以只清洗默认入口，
// 具体分类保持原样，避免误杀。
func parseJiuseCardsFiltered(pageText, category string, skipVip bool) []Drama {
	locs := reJiuseCardStart.FindAllStringIndex(pageText, -1)
	items := make([]Drama, 0, len(locs))
	seen := make(map[string]bool, len(locs))
	for index, loc := range locs {
		end := len(pageText)
		if index+1 < len(locs) {
			end = locs[index+1][0]
		}
		block := pageText[loc[0]:end]
		href := reJiuseCardHref.FindStringSubmatch(block)
		if href == nil || !validJiuseID(href[1]) {
			continue
		}
		id := href[1]
		if seen[id] {
			continue
		}
		title := ""
		if match := reJiuseCardTitle.FindStringSubmatch(block); match != nil {
			title = p91CleanText(match[1])
		}
		cover := ""
		if match := reJiuseCardCover.FindStringSubmatch(block); match != nil {
			cover = jiuseAbsoluteAddress(match[1])
		} else if match := reJiuseCardImg.FindStringSubmatch(block); match != nil {
			cover = jiuseAbsoluteAddress(match[1])
		}
		author, authorID := "", ""
		if match := reJiuseCardAuth.FindStringSubmatch(block); match != nil {
			authorID = strings.TrimSpace(match[1])
			author = p91CleanText(match[2])
		} else if match := reJiuseCardAuthPlain.FindStringSubmatch(block); match != nil {
			author = p91CleanText(match[1])
		}
		vip := reJiuseVipLayer.MatchString(block)
		seen[id] = true
		if skipVip && vip {
			continue
		}
		items = append(items, jiuseDrama(id, title, cover, author, authorID, category, nil, vip))
	}
	return items
}

func jiuseCatalogAddress(page int, category, query string) string {
	keyword := strings.TrimSpace(query)
	if keyword != "" {
		address := jiuseBase() + "/search?keywords=" + url.QueryEscape(keyword)
		if page > 1 {
			address += "&page=" + strconv.Itoa(page)
		}
		return address
	}
	// /video/index 是站内 91 内容的全量列表；分类页在 /video/category/{slug}。
	// （/videos 与 /videos/categories/* 是另一套日本 AV 库，路径 /videos/view/{数字}/{slug}/，
	// 与本站源无关，挂上去只会点开全空。）
	category = strings.TrimSpace(category)
	if category == "" {
		category = jiuseDefaultCategory
	}
	path := "/video/category/" + url.PathEscape(category)
	if page > 1 {
		path += "?page=" + strconv.Itoa(page)
	}
	return jiuseBase() + path
}

// jiuseMinCatalogItems 是一屏至少要有的条数。清洗掉 VIP 卡片后不足这个数，
// 就继续往后补页，避免「全部」第一屏稀稀拉拉只剩三四个。
const jiuseMinCatalogItems = 8

func (d *Downloader) fetchJiuseCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || !validJiuseCategory(category) || len(query) > 128 {
		return nil, false, errors.New("九色目录查询参数无效")
	}
	d.ensureJiuseBase(ctx)
	items, hasMore, err := d.fetchJiuseCatalogRaw(ctx, page, category, query)
	if err != nil {
		return nil, false, err
	}
	// 只清洗默认入口（「全部」），具体分类保持原样。
	if strings.TrimSpace(category) != "" {
		return items, hasMore, nil
	}
	kept := jiuseFreeOnly(items)
	if len(kept) == 0 {
		// 整页都是 VIP 片：原样返回比空屏好，至少能看到有什么。
		return items, hasMore, nil
	}
	// 剔完不够一屏就往后再抓两页补齐（同一批 ID 去重）。
	for offset := 1; offset <= 2 && hasMore && len(kept) < jiuseMinCatalogItems; offset++ {
		next, nextMore, err := d.fetchJiuseCatalogRaw(ctx, page+offset, category, query)
		if err != nil {
			break
		}
		hasMore = nextMore
		seen := make(map[string]bool, len(kept))
		for _, item := range kept {
			seen[item.ID] = true
		}
		for _, item := range jiuseFreeOnly(next) {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			kept = append(kept, item)
		}
	}
	return kept, hasMore, nil
}

func (d *Downloader) fetchJiuseCatalogRaw(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	address := jiuseCatalogAddress(page, category, query)
	pageText, err := d.fetchProviderText(ctx, address, jiuseBase()+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseJiuseCards(pageText, category)
	// 站方每页固定 24 条；取满就认为还有下一页，不足即到底。
	return items, len(items) >= jiusePageSize, nil
}

// jiuseFreeOnly 去掉带 VIP 角标的卡片。
func jiuseFreeOnly(items []Drama) []Drama {
	kept := make([]Drama, 0, len(items))
	for _, item := range items {
		if item.VIP != nil && *item.VIP {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}

func jiuseCategoriesForSource() []nativeCategory {
	return jiuseCategories
}

// jiuseDurationSeconds 从详情页的 schema.org VideoObject 里取时长（PT3M37S）。
func jiuseDurationSeconds(pageText string) int {
	match := reJiuseSchemaDur.FindStringSubmatch(pageText)
	if match == nil {
		return 0
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	seconds, _ := strconv.Atoi(match[3])
	total := hours*3600 + minutes*60 + seconds
	if total <= 0 {
		return 0
	}
	return total
}

// jiusePlayerAddress 从详情页取当前线路的 m3u8 地址。
func jiusePlayerAddress(pageText string) (address, poster string) {
	tag := reJiusePlayer.FindString(pageText)
	if tag == "" {
		return "", ""
	}
	if match := reJiuseDataSrc.FindStringSubmatch(tag); match != nil {
		address = jiuseAbsoluteAddress(match[1])
	}
	if match := reJiusePoster.FindStringSubmatch(tag); match != nil {
		poster = jiuseAbsoluteAddress(match[1])
	}
	return address, poster
}

func (d *Downloader) fetchJiuseDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !validJiuseID(sourceID) {
		return Drama{}, nil, errors.New("九色视频 ID 无效")
	}
	d.ensureJiuseBase(ctx)
	pageURL := jiuseBase() + "/video/view/" + sourceID
	pageText, err := d.fetchProviderText(ctx, pageURL, jiuseBase()+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	return parseJiuseDetail(pageText, sourceID)
}

// parseJiuseDetail 是纯解析：不联网，方便用真实页面做离线回归。
func parseJiuseDetail(pageText, sourceID string) (Drama, []Chapter, error) {
	if !validJiuseID(sourceID) {
		return Drama{}, nil, errors.New("九色视频 ID 无效")
	}
	// 防「点 A 变 B」。
	if !strings.Contains(pageText, "/video/view/"+sourceID) {
		return Drama{}, nil, errors.New("九色详情与请求视频不符，请刷新重试")
	}
	pageURL := jiuseBase() + "/video/view/" + sourceID
	title := ""
	if match := reJiuseH4.FindStringSubmatch(pageText); match != nil {
		title = p91CleanText(match[1])
	}
	if title == "" {
		if match := reJiuseOgTitle.FindStringSubmatch(pageText); match != nil {
			title = p91CleanText(match[1])
		}
	}
	address, poster := jiusePlayerAddress(pageText)
	cover := poster
	if cover == "" {
		if match := reJiuseOgImage.FindStringSubmatch(pageText); match != nil {
			cover = jiuseAbsoluteAddress(match[1])
		}
	}
	author, authorID := "", ""
	if match := reJiuseCardAuth.FindStringSubmatch(pageText); match != nil {
		authorID = strings.TrimSpace(match[1])
		author = p91CleanText(match[2])
	} else if match := reJiuseCardAuthPlain.FindStringSubmatch(pageText); match != nil {
		author = p91CleanText(match[1])
	}
	// 详情页把作者放在 <a href="/author/{slug}"> 里，不带「作者:」前缀，
	// 上面两条都对不上，于是作者名有了但点不动。这里补一条只认链接的兜底。
	if authorID == "" {
		if match := reJiuseAuthorLink.FindStringSubmatch(pageText); match != nil {
			authorID = strings.TrimSpace(match[1])
			if author == "" {
				author = p91CleanText(match[2])
			}
		}
	}
	// 详情页整页里会带上样式表与推荐位的 vip-layer，按整页判断必然误判成 VIP
	// （每部片都会被标「高清」）。VIP 只由列表卡片决定，这里不再判定。
	drama := jiuseDrama(sourceID, title, cover, author, authorID, "", nil, false)
	if seconds := jiuseDurationSeconds(pageText); seconds > 0 {
		drama.Duration = seconds
	}
	chapter := Chapter{
		ID:       providerChapterID(sourceJiuse, sourceID, "main"),
		Source:   sourceJiuse,
		Title:    "正片",
		VideoURL: address,
		PageURL:  pageURL,
		Referer:  pageURL,
	}
	drama.EpisodeCount, drama.TotalEpisode = 1, 1
	return drama, []Chapter{chapter}, nil
}

// jiusePlaylistUsable 判断播放列表是真片单还是 CDN 的占位。
// 占位列表的特征：单分片 + 分片地址带 __vnotice=1002（「视频资源暂不可用」）。
//
// 注意两种情况都不能算废：
//   - 多码率主列表（#EXT-X-STREAM-INF）：里面只有子列表地址、没有 .ts 分片，
//     早期按「分片数 < 3」判断会把整条线路误杀，真机表现就是「所有线路都不可用」；
//   - 分片用无扩展名路径的站点（这里九色不用，但别写死）。
func jiusePlaylistUsable(playlist string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff"))
	if !strings.HasPrefix(trimmed, "#EXTM3U") {
		return false
	}
	if strings.Contains(trimmed, "__vnotice") {
		return false
	}
	// 主列表：能列出子列表就说明 CDN 认了这次请求，交给播放器继续拉。
	if strings.Contains(trimmed, "#EXT-X-STREAM-INF") {
		return true
	}
	segments := strings.Count(trimmed, ".ts") + strings.Count(trimmed, ".m4s")
	if segments >= jiusePlayMinSegments {
		return true
	}
	// 分片地址不带扩展名时按 EXTINF 条数兜底。
	return strings.Count(trimmed, "#EXTINF:") >= jiusePlayMinSegments
}

// jiusePlaylistReason 给出「为什么判废」，直接写进报错里。
// 真机上用户只能看到一行提示，靠它才能判断是占位、空列表还是别的。
func jiusePlaylistReason(playlist string) string {
	trimmed := strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff"))
	switch {
	case !strings.HasPrefix(trimmed, "#EXTM3U"):
		head := strings.TrimSpace(strings.SplitN(trimmed, "\n", 2)[0])
		if len(head) > 40 {
			head = head[:40]
		}
		return "返回的不是播放列表(" + head + ")"
	case strings.Contains(trimmed, "__vnotice"):
		return "CDN 返回占位列表(1002)"
	case strings.Contains(trimmed, "#EXT-X-STREAM-INF"):
		return ""
	default:
		return fmt.Sprintf("分片不足(%d 段)", strings.Count(trimmed, "#EXTINF:"))
	}
}

// jiusePlayLine 是站点给的线路。空字符串是详情页默认线路。
var jiusePlayLines = []string{"", "line1", "line2", "line3"}

// jiusePlaylist 取一条线路的播放列表并校验。
//
// 必须走 fetchMediaPlaylist：它带浏览器请求头（Accept */*、Sec-Fetch-Mode/Dest）、
// 按「播放页 → 站点根 → CDN 自身域」逐级降级 Referer，并且会换 Chrome 指纹重试。
// 早期这里用的是详情页抓取（catalog）通道，指纹和请求头都是错的，CDN 一律回
// 200 的占位列表——真机上就是「每条线路都提示暂不可用」。
func (d *Downloader) jiusePlaylist(ctx context.Context, address, pageURL string) (string, string, error) {
	playlistCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	playlist, finalURL, err := d.fetchMediaPlaylist(playlistCtx, address, pageURL)
	if err != nil {
		return "", "", err
	}
	if !jiusePlaylistUsable(playlist) {
		return "", "", errors.New(jiusePlaylistReason(playlist))
	}
	return strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), finalURL, nil
}

func jiuseLineLabel(line string) string {
	if line == "" {
		return "默认线路"
	}
	return line
}

func (d *Downloader) resolveJiuseMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceJiuse, sourceID, "")
	if !valid || source != sourceJiuse || !validJiuseID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("九色播放信息无效，请刷新详情")
	}
	d.ensureJiuseBase(ctx)
	pageURL := jiuseBase() + "/video/view/" + sourceID

	// 换线只在「这一条线路本身出问题」时才有意义（超时 / 详情页没给地址）。
	// 1002 是片源级的：同一个视频四条线路回的都是同一份占位，换线纯属让用户
	// 多等十几秒。所以 1002 直接判死并给出准确原因。
	budget, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// 详情页已经带出地址：先用它，失败再按线路换。
	candidates := make([]string, 0, len(jiusePlayLines))
	if address := strings.TrimSpace(task.Chapter.VideoURL); address != "" {
		candidates = append(candidates, address)
	}

	var reasons []string
	for index, line := range jiusePlayLines {
		if budget.Err() != nil {
			break
		}
		address := ""
		if len(candidates) > index && index == 0 {
			address = candidates[0]
		} else {
			lineURL := pageURL
			if line != "" {
				lineURL += "?server=" + line
			}
			pageText, err := d.fetchProviderText(budget, lineURL, pageURL)
			if err != nil {
				reasons = append(reasons, jiuseLineLabel(line)+": "+publicError(err).Error())
				continue
			}
			address, _ = jiusePlayerAddress(pageText)
		}
		if address == "" {
			reasons = append(reasons, jiuseLineLabel(line)+": 详情页未给出地址")
			continue
		}
		playlist, finalURL, err := d.jiusePlaylist(budget, address, pageURL)
		if err != nil {
			reason := publicError(err).Error()
			reasons = append(reasons, jiuseLineLabel(line)+": "+reason)
			if strings.Contains(reason, "1002") {
				return providerMedia{}, errors.New(jiuseVipHint)
			}
			continue
		}
		if finalURL == "" {
			finalURL = address
		}
		return providerMedia{
			URL:      finalURL,
			Playlist: playlist,
			Referer:  pageURL,
		}, nil
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "没有可用的播放线路")
	}
	return providerMedia{}, fmt.Errorf("九色取流失败：%s", strings.Join(reasons, "；"))
}

// jiuseVipHint 是 1002 的准确说法。写成「线路不可用」会让人以为是网络问题，
// 实际上这部片对所有人都不可播（除非买会员），换成免费片才有用。
const jiuseVipHint = "该片是九色 VIP 新片（1002），站方只给会员播放；请换「收藏最多 / 本月」分类里的免费片"

var (
	reJiuseOgTitle = regexp.MustCompile(`(?is)<meta property="og:title" content="([^"]*)"`)
	reJiuseOgImage = regexp.MustCompile(`(?is)<meta property="og:image" content="([^"]*)"`)
)

// ---- 作者主页 ----

// validJiuseAuthorSlug 校验作者路径参数。站内作者地址是 /author/{slug}，
// slug 只由字母、数字、- _ . 组成，带别的一律拒掉，避免把任意路径拼进请求。
func validJiuseAuthorSlug(slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(slug) > 96 {
		return false
	}
	for _, char := range slug {
		switch {
		case char >= 'a' && char <= 'z', char >= 'A' && char <= 'Z',
			char >= '0' && char <= '9', char == '-', char == '_', char == '.':
		default:
			return false
		}
	}
	return true
}

// fetchJiuseAuthorPage 取某个作者发布的其它作品。
//
// 作者页与列表页共用同一套卡片（div.colVideoList + img.img + /video/view/{id}），
// 所以直接复用 parseJiuseCards；老卡片只写「作者: 名字」不带链接，那种拿不到
// slug，也就进不了这个页面。
func (d *Downloader) fetchJiuseAuthorPage(ctx context.Context, authorID string, page int) ([]Drama, bool, error) {
	authorID = strings.TrimSpace(authorID)
	if !validJiuseAuthorSlug(authorID) {
		return nil, false, errors.New("九色作者 ID 无效")
	}
	if page < 1 || page > 100000 {
		page = 1
	}
	d.ensureJiuseBase(ctx)
	address := jiuseBase() + "/author/" + url.PathEscape(authorID)
	if page > 1 {
		address += "?page=" + strconv.Itoa(page)
	}
	pageText, err := d.fetchProviderText(ctx, address, jiuseBase()+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseJiuseCards(pageText, "")
	// 站方每页 24 条，取满即认为还有下一页。
	return items, len(items) >= jiusePageSize, nil
}
