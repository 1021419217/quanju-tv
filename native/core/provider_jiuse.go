package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
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

// 站内没有「自拍题材」分类：/videos 与 /videos/categories/* 是另一套日本 AV
// 库（/videos/view/{数字}/{slug}/），和本站源的 91 内容不是一回事，挂上去只会
// 点开全空。所以分类只保留「全部」，目录走 /video/index。
var jiuseCategories = []nativeCategory{}

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

// validJiuseCategory 只认「全部」：站点没有可用的自拍题材分类（见上面注释）。
func validJiuseCategory(category string) bool {
	return strings.TrimSpace(category) == ""
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
		seen[id] = true
		items = append(items, jiuseDrama(id, title, cover, author, authorID, category, nil, reJiuseVipLayer.MatchString(block)))
	}
	return items
}

func jiuseCatalogAddress(page int, category, query string) string {
	keyword := strings.TrimSpace(query)
	if keyword != "" {
		address := jiuseBaseURL + "/search?keywords=" + url.QueryEscape(keyword)
		if page > 1 {
			address += "&page=" + strconv.Itoa(page)
		}
		return address
	}
	// /video/index 是站内唯一的「自拍」全量列表（/videos 与 /videos/categories/*
	// 是另一套日本 AV 库，路径是 /videos/view/{数字}/{slug}/，与本站源无关）。
	path := "/video/index"
	if page > 1 {
		path += "?page=" + strconv.Itoa(page)
	}
	return jiuseBaseURL + path
}

func (d *Downloader) fetchJiuseCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || !validJiuseCategory(category) || len(query) > 128 {
		return nil, false, errors.New("九色目录查询参数无效")
	}
	address := jiuseCatalogAddress(page, category, query)
	pageText, err := d.fetchProviderText(ctx, address, jiuseBaseURL+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseJiuseCards(pageText, category)
	// 站方每页固定 24 条；取满就认为还有下一页，不足即到底。
	hasMore := len(items) >= jiusePageSize
	return items, hasMore, nil
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
	pageURL := jiuseBaseURL + "/video/view/" + sourceID
	pageText, err := d.fetchProviderText(ctx, pageURL, jiuseBaseURL+"/")
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
	pageURL := jiuseBaseURL + "/video/view/" + sourceID
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
	drama := jiuseDrama(sourceID, title, cover, author, authorID, "", nil, strings.Count(pageText, "vip-layer") > 0)
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
// 占位列表的特征：只有一个分片且带 __vnotice（1002 = 资源不可用）。
func jiusePlaylistUsable(playlist string) bool {
	trimmed := strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff"))
	if !strings.HasPrefix(trimmed, "#EXTM3U") {
		return false
	}
	if strings.Contains(trimmed, "__vnotice") {
		return false
	}
	segments := strings.Count(trimmed, ".ts") + strings.Count(trimmed, ".m4s")
	return segments >= jiusePlayMinSegments
}

// jiusePlayLine 是站点给的线路。空字符串是详情页默认线路。
var jiusePlayLines = []string{"", "line1", "line2", "line3"}

func (d *Downloader) resolveJiuseMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceJiuse, sourceID, "")
	if !valid || source != sourceJiuse || !validJiuseID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("九色播放信息无效，请刷新详情")
	}
	pageURL := jiuseBaseURL + "/video/view/" + sourceID

	// 换线最多走四条，单次请求又有重试；给整段一个总预算，避免全挂时
	// 让用户对着转圈等一两分钟。
	budget, cancel := context.WithTimeout(ctx, 24*time.Second)
	defer cancel()

	// 详情页已经带出地址：先用它，失败再按线路换。
	candidates := make([]string, 0, len(jiusePlayLines))
	if address := strings.TrimSpace(task.Chapter.VideoURL); address != "" {
		candidates = append(candidates, address)
	}

	var lastErr error
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
				lastErr = err
				continue
			}
			address, _ = jiusePlayerAddress(pageText)
		}
		if address == "" {
			continue
		}
		// 播放列表本身很小，单独给短超时：拿不到就当这条线不通。
		playlistCtx, playlistCancel := context.WithTimeout(budget, 8*time.Second)
		playlist, err := d.fetchProviderText(playlistCtx, address, pageURL)
		playlistCancel()
		if err != nil {
			lastErr = err
			continue
		}
		if !jiusePlaylistUsable(playlist) {
			// CDN 回的是「资源不可用 / 地址已失效」占位，换下一条线路。
			lastErr = errors.New("九色当前线路暂不可用")
			continue
		}
		return providerMedia{
			URL:      address,
			Playlist: strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")),
			Referer:  pageURL,
		}, nil
	}
	if lastErr == nil {
		lastErr = errors.New("九色没有可用的播放线路")
	}
	return providerMedia{}, fmt.Errorf("九色取流失败：%v", lastErr)
}

var (
	reJiuseOgTitle = regexp.MustCompile(`(?is)<meta property="og:title" content="([^"]*)"`)
	reJiuseOgImage = regexp.MustCompile(`(?is)<meta property="og:image" content="([^"]*)"`)
)
