package core

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// 好色 TV（hsex.icu）：传统 PHP 服务端渲染站，页面就是 Bootstrap 模板，
// 没有会员/签到/加密播放器那一套。列表页 /list-{n}.htm，详情页
// /video-{id}.htm，播放地址直接写在 <video><source src="…index.m3u8"> 里，
// 分片是无签名的相对路径（index0.ts），连 Referer 都不用带，属于最好接的一类。
//
// 唯一需要留意的是 m3u8 路径本身带一个时间戳目录（/1794183362/<token>/…），
// 缓存久了可能失效；所以取流时优先回源详情页现取，失效时也能重新取一份。
const (
	hsexBaseURL  = "https://hsex.icu"
	hsexPageSize = 24
)

var hsexCategories = []nativeCategory{
	{ID: "new", Name: "最新"},
	{ID: "weekly", Name: "周榜"},
	{ID: "monthly", Name: "月榜"},
	{ID: "5min", Name: "5分钟+"},
	{ID: "long", Name: "10分钟+"},
}

// hsexCategoryPrefix 是分类路径前缀，页码直接拼在中间：list-2.htm。
var hsexCategoryPrefix = map[string]string{
	"new":     "list-",
	"weekly":  "top7_list-",
	"monthly": "top_list-",
	"5min":    "5min_list-",
	"long":    "long_list-",
}

var (
	reHsexCard       = regexp.MustCompile(`(?is)<div class="thumbnail">`)
	reHsexVideoHref  = regexp.MustCompile(`href="video-(\d+)\.htm"`)
	reHsexCover      = regexp.MustCompile(`background-image:\s*url\('([^']+)'\)`)
	reHsexTitle      = regexp.MustCompile(`(?is)<h5>\s*<a[^>]*>(.*?)</a>`)
	reHsexImageTitle = regexp.MustCompile(`(?is)class="image"[^>]*title=\s*"([^"]*)"`)
	reHsexDuration   = regexp.MustCompile(`<var class="duration">\s*(\d{1,2}):(\d{2})\s*</var>`)
	reHsexAuthor     = regexp.MustCompile(`href="user\.htm\?author=([^"&]+)"`)
	reHsexPanelTitle = regexp.MustCompile(`(?is)<h3[^>]*class="panel-title"[^>]*>(.*?)</h3>`)
	reHsexOgImage    = regexp.MustCompile(`(?is)property="og:image"[^>]*content="([^"]+)"`)
	reHsexSource     = regexp.MustCompile(`(?is)<source[^>]+src="(https?://[^"]+\.m3u8[^"]*)"`)
	reHsexM3U8       = regexp.MustCompile(`(https?://[^"'\s<>]+\.m3u8[^"'\s<>]*)`)
	reHsexTitleTag   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
)

func validHsexCategory(category string) bool {
	key := strings.TrimSpace(category)
	if key == "" {
		return true
	}
	_, ok := hsexCategoryPrefix[key]
	return ok
}

func hsexCategoryName(key string) string {
	for _, category := range hsexCategories {
		if category.ID == key {
			return category.Name
		}
	}
	return ""
}

// validHsexID 只认纯数字的站内视频号（/video-1257475.htm）。
func validHsexID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 12 {
		return false
	}
	for _, char := range id {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func hsexDrama(id, title, cover, category string, seconds int) Drama {
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	if title == "" {
		title = "视频 " + id
	}
	drama := Drama{
		ID:           providerDramaID(sourceHsex, id),
		Source:       sourceHsex,
		SourceID:     id,
		Title:        title,
		Name:         title,
		ChannelName:  "好色TV",
		Category:     category,
		CategoryName: hsexCategoryName(category),
	}
	if cover != "" {
		drama.Cover = cover
	}
	if seconds > 0 {
		drama.Duration = seconds
	}
	return drama
}

func parseHsexCards(pageText, category string) []Drama {
	locs := reHsexCard.FindAllStringIndex(pageText, -1)
	items := make([]Drama, 0, len(locs))
	seen := make(map[string]bool, len(locs))
	for index, loc := range locs {
		end := len(pageText)
		if index+1 < len(locs) {
			end = locs[index+1][0]
		}
		block := pageText[loc[0]:end]
		href := reHsexVideoHref.FindStringSubmatch(block)
		if href == nil || !validHsexID(href[1]) || seen[href[1]] {
			continue
		}
		id := href[1]
		title := ""
		if match := reHsexTitle.FindStringSubmatch(block); match != nil {
			title = p91CleanText(match[1])
		}
		if title == "" {
			if match := reHsexImageTitle.FindStringSubmatch(block); match != nil {
				title = p91CleanText(match[1])
			}
		}
		cover := ""
		if match := reHsexCover.FindStringSubmatch(block); match != nil {
			cover = strings.ReplaceAll(match[1], "&amp;", "&")
		}
		seconds := 0
		if match := reHsexDuration.FindStringSubmatch(block); match != nil {
			minutes, _ := strconv.Atoi(match[1])
			secs, _ := strconv.Atoi(match[2])
			seconds = minutes*60 + secs
		}
		seen[id] = true
		items = append(items, hsexDrama(id, title, cover, category, seconds))
	}
	return items
}

func hsexCatalogAddress(page int, category, query string) string {
	keyword := strings.TrimSpace(query)
	if keyword != "" {
		// 站方搜索是 GET 表单：search.htm?search=关键词&sort=new。
		address := hsexBaseURL + "/search.htm?search=" + url.QueryEscape(keyword) + "&sort=new"
		if page > 1 {
			address += "&page=" + strconv.Itoa(page)
		}
		return address
	}
	prefix := hsexCategoryPrefix[strings.TrimSpace(category)]
	if prefix == "" {
		prefix = hsexCategoryPrefix["new"]
	}
	if page < 1 {
		page = 1
	}
	return fmt.Sprintf("%s/%s%d.htm", hsexBaseURL, prefix, page)
}

func (d *Downloader) fetchHsexCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 2000 || !validHsexCategory(category) || len(query) > 128 {
		return nil, false, errors.New("好色TV 目录查询参数无效")
	}
	address := hsexCatalogAddress(page, category, query)
	pageText, err := d.fetchProviderText(ctx, address, hsexBaseURL+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseHsexCards(pageText, category)
	if len(items) == 0 && strings.Contains(pageText, "搜索的太快了") {
		return nil, false, errors.New("好色TV 搜索太频繁，请稍等十几秒再试")
	}
	// 列表页每页 24 条，搜索结果每页 20 条。
	size := hsexPageSize
	if strings.TrimSpace(query) != "" {
		size = 20
	}
	hasMore := len(items) >= size
	if !hasMore && len(items) > 0 && strings.Contains(pageText, fmt.Sprintf("list-%d.htm", page+1)) {
		hasMore = true
	}
	return items, hasMore, nil
}

func hsexCategoriesForSource() []nativeCategory {
	return hsexCategories
}

// hsexPlayAddress 从详情页取播放地址：站方把成品 m3u8 直接写在 <source> 里。
func hsexPlayAddress(pageText string) string {
	if match := reHsexSource.FindStringSubmatch(pageText); match != nil {
		return strings.ReplaceAll(match[1], "&amp;", "&")
	}
	if match := reHsexM3U8.FindStringSubmatch(pageText); match != nil {
		return strings.ReplaceAll(match[1], "&amp;", "&")
	}
	return ""
}

// hsexDetailTitle 详情页标题优先取面板标题，其次 <title>（要去掉站点后缀）。
func hsexDetailTitle(pageText string) string {
	if match := reHsexPanelTitle.FindStringSubmatch(pageText); match != nil {
		return p91CleanText(match[1])
	}
	if match := reHsexTitleTag.FindStringSubmatch(pageText); match != nil {
		title := p91CleanText(match[1])
		for _, suffix := range []string{" - 好色™ Tv", "- 好色™ Tv", " - 好色TV", "_好色TV"} {
			title = strings.TrimSuffix(title, suffix)
		}
		return strings.TrimSpace(title)
	}
	return ""
}

func (d *Downloader) fetchHsexDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !validHsexID(sourceID) {
		return Drama{}, nil, errors.New("好色TV 视频 ID 无效")
	}
	pageURL := hsexBaseURL + "/video-" + sourceID + ".htm"
	pageText, err := d.fetchProviderText(ctx, pageURL, hsexBaseURL+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	if !strings.Contains(pageText, "video-"+sourceID+".htm") {
		return Drama{}, nil, errors.New("好色TV 详情与请求视频不符，请刷新重试")
	}
	title := hsexDetailTitle(pageText)
	cover := ""
	if match := reHsexOgImage.FindStringSubmatch(pageText); match != nil {
		cover = strings.ReplaceAll(match[1], "&amp;", "&")
	}
	seconds := 0
	if match := reHsexDuration.FindStringSubmatch(pageText); match != nil {
		minutes, _ := strconv.Atoi(match[1])
		secs, _ := strconv.Atoi(match[2])
		seconds = minutes*60 + secs
	}
	drama := hsexDrama(sourceID, title, cover, "", seconds)
	if match := reHsexAuthor.FindStringSubmatch(pageText); match != nil {
		drama.Author = p91CleanText(match[1])
	}
	chapter := Chapter{
		ID:       providerChapterID(sourceHsex, sourceID, "main"),
		Source:   sourceHsex,
		Title:    "正片",
		VideoURL: hsexPlayAddress(pageText),
		PageURL:  pageURL,
		Referer:  pageURL,
	}
	drama.EpisodeCount, drama.TotalEpisode = 1, 1
	return drama, []Chapter{chapter}, nil
}

func (d *Downloader) resolveHsexMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceHsex, sourceID, "")
	if !valid || source != sourceHsex || !validHsexID(sourceID) ||
		!strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("好色TV 播放信息无效，请刷新详情")
	}
	pageURL := hsexBaseURL + "/video-" + sourceID + ".htm"
	address := strings.TrimSpace(task.Chapter.VideoURL)
	if !strings.Contains(address, ".m3u8") {
		// 详情缓存里没有地址（老缓存 / 当时解析失败）：回源现取。
		pageText, err := d.fetchProviderText(ctx, pageURL, hsexBaseURL+"/")
		if err != nil {
			return providerMedia{}, fmt.Errorf("好色TV 回源失败：%v", err)
		}
		fresh := hsexPlayAddress(pageText)
		if fresh == "" {
			return providerMedia{}, errors.New("好色TV 未解析到播放地址，请刷新详情后重试")
		}
		address = fresh
	}
	return providerMedia{
		URL:     address,
		Referer: pageURL,
		// m3u8 路径里带站点下发的时间戳目录，播放久了可能整批失效。
		// 传输层遇到分片 403 时调这里重新解析一次详情页，拿一份新地址。
		ReloadPlaylist: func(reloadCtx context.Context) (string, error) {
			pageText, err := d.fetchProviderText(reloadCtx, pageURL, hsexBaseURL+"/")
			if err != nil {
				return "", err
			}
			fresh := hsexPlayAddress(pageText)
			if fresh == "" {
				return "", errors.New("好色TV 重新解析播放地址失败")
			}
			return fresh, nil
		},
	}, nil
}
