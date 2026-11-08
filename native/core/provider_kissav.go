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

// KISSAV：日本 AV 聚合站（jable 系模板），服务端渲染，列表每页 24 条。
//
// 播放链路比较特殊：页面里的 m3u8 直链带的是「列表级」auth_key，分片单独请求
// 一律 403；播放器实际是先请求站方自己的代理 /media/m3u8/?url=..&exp=..&token=..
// 由它返回一份「每个分片都带各自签名」的播放列表。所以这里解析出的播放地址
// 是那个代理地址，并在起播时一次性把列表取回来放进 Playlist —— 播放器随后
// 拉分片不需要再经过站点，体验与笔盒一致。
const (
	kissavBaseURL  = "https://kissavs.com"
	kissavPageSize = 24
	// kissavPlayerPrefix 是站方 m3u8 代理接口前缀，用来识别「详情页已带出地址」。
	kissavPlayerPrefix = kissavBaseURL + "/media/m3u8/"
)

// kissavCategories 是内置分类。ID 直接映射到站方路径（见 kissavCategoryPaths），
// 分页统一是「路径 + page/N/」。
var kissavCategories = []nativeCategory{
	{ID: "new", Name: "新作上市"},
	{ID: "updates", Name: "最近更新"},
	{ID: "hot", Name: "东京热"},
	{ID: "cosplay", Name: "Cosplay"},
	{ID: "roleplay", Name: "角色剧情"},
	{ID: "chinese", Name: "中文字幕"},
	{ID: "pantyhose", Name: "丝袜美腿"},
	{ID: "uncensored", Name: "无码流出"},
}

var kissavCategoryPaths = map[string]string{
	"new":        "/av/new/",
	"updates":    "/av/updates/",
	"hot":        "/hotmovies/alwayshot/",
	"cosplay":    "/av/tag/cosplay/update/",
	"roleplay":   "/av/theme/roleplay/hot/",
	"chinese":    "/av/theme/chinese-subtitle/hot/",
	"pantyhose":  "/av/theme/pantyhose/hot/",
	"uncensored": "/av/tag/uncensored/update/",
}

var (
	reKissavCard      = regexp.MustCompile(`(?is)<div class="video-img-box[^"]*">`)
	reKissavVideoHref = regexp.MustCompile(`href="/video/([^/"]+)/"`)
	reKissavCover     = regexp.MustCompile(`(?is)(?:z-image-loader-url|x-image-loader-url)="(https?://[^"]+)"`)
	reKissavCardTitle = regexp.MustCompile(`(?is)<p class="title">\s*<a[^>]*>(.*?)</a>`)
	reKissavImgAlt    = regexp.MustCompile(`(?is)<img[^>]*\balt="([^"]*)"`)
	reKissavNext      = regexp.MustCompile(`(?is)<link rel="next" href="([^"]+)"`)
	reKissavH1        = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	reKissavOgImage   = regexp.MustCompile(`(?is)property="og:image"[^>]*content="([^"]+)"`)
	reKissavM3U8      = regexp.MustCompile(`(https?://[^"'\s<>]+\.m3u8[^"'\s<>]*)`)
	reKissavToken     = regexp.MustCompile(`token\|([0-9a-f]{32,})\|`)
	reKissavExp       = regexp.MustCompile(`\|(\d{10})\|2Fvideo\|`)
	reKissavDuration  = regexp.MustCompile(`"duration":\s*"PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?"`)
	// 站方把播放器初始化整段做了 packer 混淆，解包后能直接拿到成品代理地址
	// （auth_key 已被重新签名）。比手工拼 token/exp 稳得多。
	reKissavPacker      = regexp.MustCompile(`(?s)eval\(function\(p,a,c,k,e,[dr]\)\{.*?\}\('(.*?)',(\d+),(\d+),'(.*?)'\.split\('\|'\)`)
	reKissavPackerToken = regexp.MustCompile(`[0-9a-zA-Z]+`)
	reKissavPlayerArg   = regexp.MustCompile(`['"]url['"]\s*:\s*['"](/media/m3u8/\?[^'"]+)['"]`)
	reKissavDirectArg   = regexp.MustCompile(`(/media/m3u8/\?url=[^'"\s<>\\]+)`)
)

func validKissavCategory(category string) bool {
	key := strings.TrimSpace(category)
	if key == "" {
		return true
	}
	_, ok := kissavCategoryPaths[key]
	return ok
}

func kissavCategoryName(key string) string {
	for _, category := range kissavCategories {
		if category.ID == key {
			return category.Name
		}
	}
	return ""
}

// validKissavID 校验视频 ID：32 位 hex 与 mide-762 这类番号 slug 都合法。
func validKissavID(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || len(id) > 128 {
		return false
	}
	for _, char := range id {
		if !((char >= '0' && char <= '9') || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || char == '-' || char == '_') {
			return false
		}
	}
	return true
}

func kissavDrama(id, title, cover, category string) Drama {
	id = strings.TrimSpace(id)
	title = strings.TrimSpace(title)
	if title == "" {
		title = id
	}
	drama := Drama{
		ID:           providerDramaID(sourceKissav, id),
		Source:       sourceKissav,
		SourceID:     id,
		Title:        title,
		Name:         title,
		ChannelName:  "KISSAV",
		Category:     category,
		CategoryName: kissavCategoryName(category),
	}
	if cover != "" {
		drama.Cover = cover
	}
	return drama
}

// parseKissavCards 按卡片起点切块，避免不同区块的链接互相串。
func parseKissavCards(pageText, category string) []Drama {
	locs := reKissavCard.FindAllStringIndex(pageText, -1)
	items := make([]Drama, 0, len(locs))
	seen := make(map[string]bool, len(locs))
	for index, loc := range locs {
		end := len(pageText)
		if index+1 < len(locs) {
			end = locs[index+1][0]
		}
		block := pageText[loc[0]:end]
		href := reKissavVideoHref.FindStringSubmatch(block)
		if href == nil || !validKissavID(href[1]) || seen[href[1]] {
			continue
		}
		id := href[1]
		title := ""
		if match := reKissavCardTitle.FindStringSubmatch(block); match != nil {
			title = p91CleanText(match[1])
		}
		if title == "" {
			if match := reKissavImgAlt.FindStringSubmatch(block); match != nil {
				title = p91CleanText(match[1])
			}
		}
		cover := ""
		if match := reKissavCover.FindStringSubmatch(block); match != nil {
			cover = strings.ReplaceAll(match[1], "&amp;", "&")
		}
		seen[id] = true
		items = append(items, kissavDrama(id, title, cover, category))
	}
	return items
}

func kissavCatalogAddress(page int, category, query string) string {
	keyword := strings.TrimSpace(query)
	path := ""
	if keyword != "" {
		path = "/search/" + url.PathEscape(keyword) + "/"
	} else {
		path = kissavCategoryPaths[strings.TrimSpace(category)]
		if path == "" {
			path = "/av/updates/"
		}
	}
	if page > 1 {
		path += "page/" + strconv.Itoa(page) + "/"
	}
	return kissavBaseURL + path
}

func (d *Downloader) fetchKissavCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || !validKissavCategory(category) || len(query) > 128 {
		return nil, false, errors.New("KISSAV 目录查询参数无效")
	}
	address := kissavCatalogAddress(page, category, query)
	pageText, err := d.fetchProviderText(ctx, address, kissavBaseURL+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseKissavCards(pageText, category)
	hasMore := len(items) >= kissavPageSize
	if !hasMore && reKissavNext.MatchString(pageText) && len(items) > 0 {
		// 站方最后一页也可能正好满 24 条，以 rel=next 为准更稳。
		hasMore = true
	}
	return items, hasMore, nil
}

func kissavCategoriesForSource() []nativeCategory {
	return kissavCategories
}

// kissavStripAuthKey 去掉列表级 auth_key：代理接口只需要基础路径，
// 保留其余参数与原始顺序（站方按原串做校验，重排过一次就可能被判无效）。
func kissavStripAuthKey(raw string) string {
	index := strings.Index(raw, "?")
	if index < 0 {
		return raw
	}
	base, query := raw[:index], raw[index+1:]
	parts := strings.Split(query, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.HasPrefix(part, "auth_key=") {
			continue
		}
		kept = append(kept, part)
	}
	if len(kept) == 0 {
		return base
	}
	return base + "?" + strings.Join(kept, "&")
}

// kissavUnbase 还原 packer 的 base62 索引（0-9 a-z A-Z）。
func kissavUnbase(token string, base int) int {
	value := 0
	for _, char := range token {
		var digit int
		switch {
		case char >= '0' && char <= '9':
			digit = int(char - '0')
		case char >= 'a' && char <= 'z':
			digit = int(char-'a') + 10
		case char >= 'A' && char <= 'Z':
			digit = int(char-'A') + 36
		default:
			return -1
		}
		value = value*base + digit
		if value > 1<<20 {
			return -1
		}
	}
	return value
}

// kissavUnpack 还原 Dean Edwards packer：把 p 里的短标记替换回字典词。
func kissavUnpack(payload string, base int, dictionary []string) string {
	text := strings.ReplaceAll(payload, `\\'`, `'`)
	return reKissavPackerToken.ReplaceAllStringFunc(text, func(token string) string {
		index := kissavUnbase(token, base)
		if index < 0 || index >= len(dictionary) {
			return token
		}
		if dictionary[index] == "" {
			return token
		}
		return dictionary[index]
	})
}

// kissavPlayerURL 从详情页里取出站方的播放列表代理地址。
//
// 页面的播放器初始化是 packer 混淆过的，里面那条 url 已经带好重签后的
// auth_key；直接用它，播放器拉分片就不再经过本站。
func kissavPlayerURL(pageText string) (string, error) {
	if match := reKissavPacker.FindStringSubmatch(pageText); match != nil {
		base, _ := strconv.Atoi(match[2])
		if base >= 2 && base <= 62 {
			unpacked := kissavUnpack(match[1], base, strings.Split(match[4], "|"))
			if arg := reKissavPlayerArg.FindStringSubmatch(unpacked); arg != nil {
				return kissavBaseURL + strings.ReplaceAll(arg[1], "&amp;", "&"), nil
			}
			if arg := reKissavDirectArg.FindStringSubmatch(unpacked); arg != nil {
				return kissavBaseURL + strings.ReplaceAll(arg[1], "&amp;", "&"), nil
			}
		}
	}
	// 少数页面没走 packer：直接找页面里的代理地址。
	if arg := reKissavDirectArg.FindStringSubmatch(pageText); arg != nil {
		return kissavBaseURL + strings.ReplaceAll(arg[1], "&amp;", "&"), nil
	}
	// 最后兜底：用 m3u8 直链 + token/exp 自己拼一次（老版本页面能成）。
	match := reKissavM3U8.FindStringSubmatch(pageText)
	if match == nil {
		return "", errors.New("KISSAV 未解析到播放地址")
	}
	raw := kissavStripAuthKey(strings.ReplaceAll(match[1], "&amp;", "&"))
	token := ""
	if found := reKissavToken.FindStringSubmatch(pageText); found != nil {
		token = found[1]
	}
	exp := ""
	if found := reKissavExp.FindStringSubmatch(pageText); found != nil {
		exp = found[1]
	}
	if token == "" || exp == "" {
		return "", errors.New("KISSAV 播放凭证缺失，请刷新详情后重试")
	}
	return kissavPlayerPrefix + "?url=" + url.QueryEscape(raw) + "&exp=" + exp + "&token=" + token, nil
}

func kissavDurationSeconds(pageText string) int {
	match := reKissavDuration.FindStringSubmatch(pageText)
	if match == nil {
		return 0
	}
	hours, _ := strconv.Atoi(match[1])
	minutes, _ := strconv.Atoi(match[2])
	seconds, _ := strconv.Atoi(match[3])
	total := hours*3600 + minutes*60 + seconds
	if total <= 0 || total > 12*3600 {
		return 0
	}
	return total
}

func (d *Downloader) fetchKissavDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	if !validKissavID(sourceID) {
		return Drama{}, nil, errors.New("KISSAV 视频 ID 无效")
	}
	pageURL := kissavBaseURL + "/video/" + url.PathEscape(sourceID) + "/"
	pageText, err := d.fetchProviderText(ctx, pageURL, kissavBaseURL+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	// 防「点 A 变 B」：返回的详情必须确实是这部片。
	if !strings.Contains(pageText, "/video/"+sourceID+"/") {
		return Drama{}, nil, errors.New("KISSAV 详情与请求视频不符，请刷新重试")
	}
	title := ""
	if match := reKissavH1.FindStringSubmatch(pageText); match != nil {
		title = p91CleanText(match[1])
	}
	cover := ""
	if match := reKissavOgImage.FindStringSubmatch(pageText); match != nil {
		cover = strings.ReplaceAll(match[1], "&amp;", "&")
	}
	drama := kissavDrama(sourceID, title, cover, "")
	if seconds := kissavDurationSeconds(pageText); seconds > 0 {
		drama.Duration = seconds
	}
	address, playErr := kissavPlayerURL(pageText)
	chapter := Chapter{
		ID:       providerChapterID(sourceKissav, sourceID, "main"),
		Source:   sourceKissav,
		Title:    "正片",
		VideoURL: address,
		PageURL:  pageURL,
		Referer:  pageURL,
	}
	chapters := []Chapter{chapter}
	if playErr != nil {
		// 解析不到播放地址也返回章节：起播时 resolveKissavMedia 会再回源试一次，
		// 一次页面抖动不该把整条记录判死。
		chapters[0].VideoURL = ""
	}
	drama.EpisodeCount, drama.TotalEpisode = 1, 1
	return drama, chapters, nil
}

func (d *Downloader) resolveKissavMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceKissav, sourceID, "")
	if !valid || source != sourceKissav || !validKissavID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("KISSAV 播放信息无效，请刷新详情")
	}
	pageURL := kissavBaseURL + "/video/" + url.PathEscape(sourceID) + "/"
	address := strings.TrimSpace(task.Chapter.VideoURL)
	if !strings.HasPrefix(address, kissavPlayerPrefix) {
		// 详情页没有带出地址（旧缓存 / 当时解析失败）：回源重新组一次。
		pageText, err := d.fetchProviderText(ctx, pageURL, kissavBaseURL+"/")
		if err != nil {
			return providerMedia{}, fmt.Errorf("KISSAV 回源失败：%v", err)
		}
		fresh, err := kissavPlayerURL(pageText)
		if err != nil {
			return providerMedia{}, err
		}
		address = fresh
	}
	// 一次性把播放列表取回来：分片地址里已经写好各自的签名，播放器拿到后
	// 直接拉 CDN，不再经过本站。这一步失败才让播放器去撞墙，比让它自己
	// 反复试代理接口要快得多。
	playlist, err := d.fetchProviderText(ctx, address, pageURL)
	if err != nil {
		return providerMedia{}, fmt.Errorf("KISSAV 取播放列表失败：%v", err)
	}
	playlist = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(playlist, "\ufeff")), "\ufeff")
	if !strings.HasPrefix(playlist, "#EXTM3U") {
		return providerMedia{}, errors.New("KISSAV 播放列表无效，请刷新详情后重试")
	}
	return providerMedia{URL: address, Playlist: playlist, Referer: pageURL}, nil
}
