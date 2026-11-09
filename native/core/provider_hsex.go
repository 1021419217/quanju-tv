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
	// 本片作者写在 panel-body 的「作者：」格子里，位置在推荐卡片之前；
	// 带名字一起抓，中文作者名（URL 编码）才能正常显示。
	reHsexAuthorLink = regexp.MustCompile(`(?is)作者：\s*<a[^>]*href="user\.htm\?author=([^"&]+)"[^>]*>(.*?)</a>`)
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
	if match := reHsexAuthorLink.FindStringSubmatch(pageText); match != nil {
		drama.AuthorID = strings.TrimSpace(match[1])
		name := p91CleanText(match[2])
		// 站内作者名常是 URL 编码（如 91%E5%9D%8F…），展示时解回来。
		if decoded, err := url.QueryUnescape(drama.AuthorID); err == nil {
			name = p91CleanText(decoded)
		}
		if name != "" {
			drama.Author = name
		} else {
			drama.Author = drama.AuthorID
		}
	} else if match := reHsexAuthor.FindStringSubmatch(pageText); match != nil {
		drama.AuthorID = strings.TrimSpace(match[1])
		if decoded, err := url.QueryUnescape(drama.AuthorID); err == nil {
			drama.Author = p91CleanText(decoded)
		}
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

// hsexAbsolutePlaylist 把 m3u8 里的相对分片改成绝对地址。
//
// 站方 m3u8 路径带一个时间戳目录（/1794185829/<token>/hls/<id>/），重新
// 取一次详情页时这个目录会变。中转刷新播放列表是按「host + path」认分片的，
// 若列表里还写相对地址，就只能拿旧目录去解析，永远换不到新目录。
func hsexAbsolutePlaylist(body, base string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return body
	}
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		ref, err := url.Parse(trimmed)
		if err != nil || ref.IsAbs() {
			continue
		}
		lines[i] = parsed.ResolveReference(ref).String()
	}
	return strings.Join(lines, "\n")
}

// hsexFetchPlaylist 回源详情页现取 m3u8，并把播放列表内容一起读回来。
//
// 3.31 只在「缓存里没有 .m3u8 地址」时才回源，否则直接拿着可能是几小时前
// 的地址去请求 CDN；用户真机上「大部分都提示站方 CDN 返回 HTTP 500」就是
// 这批陈地址。现在每次起播都现取，并把列表放进 providerMedia.Playlist，
// 播放器第一次请求直接由本地中转返回，连 CDN 的 m3u8 都不用碰。
func (d *Downloader) hsexFetchPlaylist(ctx context.Context, pageURL string) (string, string, error) {
	pageText, err := d.fetchProviderText(ctx, pageURL, hsexBaseURL+"/")
	if err != nil {
		return "", "", fmt.Errorf("好色TV 回源失败：%v", err)
	}
	address := hsexPlayAddress(pageText)
	if address == "" {
		return "", "", errors.New("好色TV 未解析到播放地址，请刷新详情后重试")
	}
	body, err := d.fetchProviderText(ctx, address, pageURL)
	if err != nil {
		return "", "", fmt.Errorf("好色TV 播放列表读取失败：%v", err)
	}
	body = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(body, "\ufeff")), "\ufeff")
	if !strings.HasPrefix(body, "#EXTM3U") {
		return "", "", errors.New("好色TV 播放列表无效，请刷新详情后重试")
	}
	return address, hsexAbsolutePlaylist(body, address), nil
}

func (d *Downloader) resolveHsexMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceHsex, sourceID, "")
	if !valid || source != sourceHsex || !validHsexID(sourceID) ||
		!strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("好色TV 播放信息无效，请刷新详情")
	}
	pageURL := hsexBaseURL + "/video-" + sourceID + ".htm"
	address, playlist, err := d.hsexFetchPlaylist(ctx, pageURL)
	if err != nil {
		return providerMedia{}, err
	}
	media := providerMedia{
		URL:      address,
		Playlist: playlist,
		Referer:  pageURL,
	}
	// 换线用：重新解析一条新的 m3u8 地址。
	media.Reload = func(reloadCtx context.Context) (string, error) {
		fresh, _, err := d.hsexFetchPlaylist(reloadCtx, pageURL)
		return fresh, err
	}
	// 分片 403/5xx 时：返回一份「绝对地址」的新播放列表文本，中转按分片
	// 身份把旧地址换掉。3.31 这里错把 m3u8 的 URL 当成列表文本返回，
	// nativeRefreshSegments 的 #EXTM3U 校验必然失败，刷新形同虚设。
	media.ReloadPlaylist = func(reloadCtx context.Context) (string, error) {
		_, fresh, err := d.hsexFetchPlaylist(reloadCtx, pageURL)
		return fresh, err
	}
	return media, nil
}

// validHsexAuthorSlug：站内作者名就是显示名，含中文与 URL 编码，不能套
// ASCII 白名单。只挡掉路径穿越、协议分隔符和明显过长的输入。
func validHsexAuthorSlug(slug string) bool {
	slug = strings.TrimSpace(slug)
	if slug == "" || len(slug) > 256 {
		return false
	}
	if strings.ContainsAny(slug, "/\\?#&<>\"'\x00") || strings.Contains(slug, "..") {
		return false
	}
	return true
}

// hsexAuthorAddress 是站方作者页地址。分页写在路径里（user-2.htm），
// 不是 user.htm?author=X&page=N——后者实测一直返回第一页。
func hsexAuthorAddress(slug string, page int) string {
	// 详情页里的 slug 可能是 URL 编码过的中文，先解回来再编码，避免二次转义。
	if decoded, err := url.QueryUnescape(slug); err == nil {
		slug = decoded
	}
	return fmt.Sprintf("%s/user-%d.htm?author=%s", hsexBaseURL, page, url.QueryEscape(slug))
}

// fetchHsexAuthorPage 取某个作者发布的其它作品，每页 20 张卡片，
// 结构与列表页完全一致，复用解析。
func (d *Downloader) fetchHsexAuthorPage(ctx context.Context, authorID string, page int) ([]Drama, bool, error) {
	slug := strings.TrimSpace(authorID)
	if !validHsexAuthorSlug(slug) {
		return nil, false, errors.New("好色TV 作者 ID 无效")
	}
	if decoded, err := url.QueryUnescape(slug); err == nil {
		slug = decoded
	}
	if !validHsexAuthorSlug(slug) {
		return nil, false, errors.New("好色TV 作者 ID 无效")
	}
	if page < 1 || page > 2000 {
		return nil, false, errors.New("页码无效")
	}
	address := hsexAuthorAddress(slug, page)
	pageText, err := d.fetchProviderText(ctx, address, hsexBaseURL+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseHsexCards(pageText, "")
	for i := range items {
		items[i].AuthorID = authorID
	}
	return items, len(items) >= 20, nil
}
