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

// VIRAL：ViralXXXPorn（KVS 内核，服务端渲染）。
//
// 这条链路是这个站最省事的一点：详情页直接把成品 mp4 写进 flashvars
// （video_url / video_alt_url），地址会 302 跳到自家 CDN，CDN 公开、支持
// Range。所以不需要任何代理、鉴权或探测 —— 详情页拿到地址就能播，
// 起播是零额外请求。
//
// 唯一要注意的是详情页地址必须带 slug（/zh/video/551431/ 直接 404），
// 因此 SourceID 存 "数字/slug" 两段。
const (
	viralBaseURL  = "https://viralxxxporn.com"
	viralSitePath = "/zh"
	viralPageSize = 27
)

// viralCategories 是内置分类，按站方导航 + 分类索引里出现频率最高的几项。
var viralCategories = []nativeCategory{
	{ID: "latest", Name: "最近更新"},
	{ID: "onlyfans", Name: "OnlyFans 流出"},
	{ID: "celebrity", Name: "名人泄露"},
	{ID: "amateur", Name: "业余自拍"},
	{ID: "bigtits", Name: "巨乳"},
	{ID: "teen", Name: "少女"},
	{ID: "anal", Name: "肛交"},
	{ID: "asian", Name: "亚洲"},
	{ID: "masturbation", Name: "自慰"},
	{ID: "blowjob", Name: "口交"},
}

var viralCategoryPaths = map[string]string{
	"latest":       "/latest-updates/",
	"onlyfans":     "/categories/onlyfans-leaks/",
	"celebrity":    "/categories/celebrity/",
	"amateur":      "/categories/amateur/",
	"bigtits":      "/categories/big-tits/",
	"teen":         "/categories/teen/",
	"anal":         "/categories/anal/",
	"asian":        "/categories/asiannsfw/",
	"masturbation": "/categories/masturbation/",
	"blowjob":      "/categories/blowjob/",
}

var (
	reViralCard     = regexp.MustCompile(`(?is)<div class="[^"]*vx-th item[^"]*"\s+data-cw-id="(\d+)"`)
	reViralCardHref = regexp.MustCompile(`href="(?:https?://viralxxxporn\.com)?/zh/video/(\d+)/([^"/?#]+)/"`)
	reViralMediaTag = regexp.MustCompile(`(?is)<a class="vx-media"[^>]*title="([^"]*)"`)
	// 站方省略了属性间的空格（loading="lazy"src=），不能要求 \s+。
	reViralImg       = regexp.MustCompile(`(?is)<img class="vx-img[^"]*"[^>]*?\bsrc="([^"]+)"`)
	reViralDuration  = regexp.MustCompile(`(?is)<div class="vx-duration">\s*([0-9:]+)\s*</div>`)
	reViralOwner     = regexp.MustCompile(`(?is)class="video-owner"[^>]*>\s*([^<]+?)\s*</a>`)
	reViralOwnerID   = regexp.MustCompile(`href="(?:https?://viralxxxporn\.com)?/zh/members/(\d+)/"`)
	reViralH1        = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	reViralOgImage   = regexp.MustCompile(`(?is)property="og:image"[^>]*content="([^"]+)"`)
	reViralVideoURL  = regexp.MustCompile(`video_url:\s*'([^']+)'`)
	reViralAltURL    = regexp.MustCompile(`video_alt_url:\s*'([^']+)'`)
	reViralVideoID   = regexp.MustCompile(`(?m)^\s*video_id:\s*'(\d+)'`)
	reViralJSONLD    = regexp.MustCompile(`"duration":\s*"PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?"`)
	reViralSlugClean = regexp.MustCompile(`[^a-z0-9\-]+`)
)

func validViralCategory(category string) bool {
	key := strings.TrimSpace(category)
	if key == "" {
		return true
	}
	_, ok := viralCategoryPaths[key]
	return ok
}

func viralCategoryName(key string) string {
	for _, category := range viralCategories {
		if category.ID == key {
			return category.Name
		}
	}
	return ""
}

// validViralID 校验 "551431/slug" 形式的站内 ID。
func validViralID(id string) bool {
	parts := strings.SplitN(strings.TrimSpace(id), "/", 2)
	if len(parts) != 2 {
		return false
	}
	if len(parts[0]) == 0 || len(parts[0]) > 12 {
		return false
	}
	for _, char := range parts[0] {
		if char < '0' || char > '9' {
			return false
		}
	}
	slug := parts[1]
	if len(slug) == 0 || len(slug) > 160 {
		return false
	}
	for _, char := range slug {
		if !((char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-') {
			return false
		}
	}
	return true
}

func viralDrama(sourceID, title, cover, category string) Drama {
	sourceID = strings.TrimSpace(sourceID)
	title = strings.TrimSpace(title)
	if title == "" {
		title = sourceID
	}
	drama := Drama{
		ID:           providerDramaID(sourceViral, sourceID),
		Source:       sourceViral,
		SourceID:     sourceID,
		Title:        title,
		Name:         title,
		ChannelName:  "ViralXXX",
		Category:     category,
		CategoryName: viralCategoryName(category),
	}
	if cover != "" {
		drama.Cover = cover
	}
	return drama
}

// viralIDFromHref 把 "/zh/video/551431/some-title/" 归一成 "551431/some-title"。
func viralIDFromHref(pageText string) (string, string) {
	match := reViralCardHref.FindStringSubmatch(pageText)
	if match == nil {
		return "", ""
	}
	slug := reViralSlugClean.ReplaceAllString(strings.ToLower(match[2]), "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		return "", ""
	}
	return match[1] + "/" + slug, match[1]
}

func viralDurationSeconds(text string) int {
	if match := reViralDuration.FindStringSubmatch(text); match != nil {
		parts := strings.Split(strings.TrimSpace(match[1]), ":")
		if len(parts) == 2 {
			minutes, _ := strconv.Atoi(parts[0])
			seconds, _ := strconv.Atoi(parts[1])
			total := minutes*60 + seconds
			if total > 0 && total <= 12*3600 {
				return total
			}
		}
		if len(parts) == 3 {
			hours, _ := strconv.Atoi(parts[0])
			minutes, _ := strconv.Atoi(parts[1])
			seconds, _ := strconv.Atoi(parts[2])
			total := hours*3600 + minutes*60 + seconds
			if total > 0 && total <= 12*3600 {
				return total
			}
		}
	}
	if match := reViralJSONLD.FindStringSubmatch(text); match != nil {
		hours, _ := strconv.Atoi(match[1])
		minutes, _ := strconv.Atoi(match[2])
		seconds, _ := strconv.Atoi(match[3])
		total := hours*3600 + minutes*60 + seconds
		if total > 0 && total <= 12*3600 {
			return total
		}
	}
	return 0
}

// parseViralCards 按 data-cw-id 切卡片：这个站每个视频卡都带这个属性，
// 广告位（spot-native）没有，天然被排除。
func parseViralCards(pageText, category string) []Drama {
	locs := reViralCard.FindAllStringIndex(pageText, -1)
	items := make([]Drama, 0, len(locs))
	seen := make(map[string]bool, len(locs))
	for index, loc := range locs {
		end := len(pageText)
		if index+1 < len(locs) {
			end = locs[index+1][0]
		}
		block := pageText[loc[0]:end]
		sourceID, numericID := viralIDFromHref(block)
		if sourceID == "" || seen[sourceID] {
			continue
		}
		title := ""
		if match := reViralMediaTag.FindStringSubmatch(block); match != nil {
			title = p91CleanText(match[1])
		}
		if title == "" {
			if id := reViralVideoID.FindStringSubmatch(block); id != nil {
				title = numericID
			}
		}
		cover := ""
		if match := reViralImg.FindStringSubmatch(block); match != nil {
			cover = strings.ReplaceAll(match[1], "&amp;", "&")
		}
		drama := viralDrama(sourceID, title, cover, category)
		if seconds := viralDurationSeconds(block); seconds > 0 {
			drama.Duration = seconds
		}
		if match := reViralOwner.FindStringSubmatch(block); match != nil {
			drama.Author = p91CleanText(match[1])
		}
		if match := reViralOwnerID.FindStringSubmatch(block); match != nil {
			drama.AuthorID = match[1]
		}
		seen[sourceID] = true
		items = append(items, drama)
	}
	return items
}

func viralCatalogAddress(page int, category, query string) string {
	keyword := strings.TrimSpace(query)
	path := ""
	if keyword != "" {
		path = "/search/" + url.PathEscape(keyword) + "/"
	} else {
		path = viralCategoryPaths[strings.TrimSpace(category)]
		if path == "" {
			path = viralCategoryPaths["latest"]
		}
	}
	if page > 1 {
		path += strconv.Itoa(page) + "/"
	}
	return viralBaseURL + viralSitePath + path
}

func (d *Downloader) fetchViralCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 || page > 100000 || !validViralCategory(category) || len(query) > 128 {
		return nil, false, errors.New("ViralXXX 目录查询参数无效")
	}
	address := viralCatalogAddress(page, category, query)
	pageText, err := d.fetchProviderText(ctx, address, viralBaseURL+viralSitePath+"/")
	if err != nil {
		return nil, false, err
	}
	items := parseViralCards(pageText, category)
	return items, len(items) >= viralPageSize-7, nil
}

func viralCategoriesForSource() []nativeCategory {
	return viralCategories
}

// viralPlayURL 从详情页 flashvars 里挑地址：优先 720p（video_alt_url），
// 没有再退回 480p（video_url）。两者都是成品 mp4，302 到自家 CDN。
func viralPlayURL(pageText string) string {
	for _, pattern := range []*regexp.Regexp{reViralAltURL, reViralVideoURL} {
		if match := pattern.FindStringSubmatch(pageText); match != nil {
			address := strings.TrimSpace(strings.ReplaceAll(match[1], "&amp;", "&"))
			if isProviderHTTPMediaURL(address) {
				return address
			}
		}
	}
	return ""
}

func (d *Downloader) fetchViralDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if !validViralID(sourceID) {
		return Drama{}, nil, errors.New("ViralXXX 视频 ID 无效")
	}
	pageURL := viralBaseURL + viralSitePath + "/video/" + sourceID + "/"
	pageText, err := d.fetchProviderText(ctx, pageURL, viralBaseURL+viralSitePath+"/")
	if err != nil {
		return Drama{}, nil, err
	}
	// 防「点 A 变 B」：页面上必须真的出现这个视频的数字 ID。
	numericID := strings.SplitN(sourceID, "/", 2)[0]
	if !strings.Contains(pageText, "video_id: '"+numericID+"'") && !strings.Contains(pageText, "/video/"+numericID+"/") {
		return Drama{}, nil, errors.New("ViralXXX 详情与请求视频不符，请刷新重试")
	}
	title := ""
	if match := reViralH1.FindStringSubmatch(pageText); match != nil {
		title = p91CleanText(match[1])
	}
	cover := ""
	if match := reViralOgImage.FindStringSubmatch(pageText); match != nil {
		cover = strings.ReplaceAll(match[1], "&amp;", "&")
	}
	drama := viralDrama(sourceID, title, cover, "")
	if seconds := viralDurationSeconds(pageText); seconds > 0 {
		drama.Duration = seconds
	}
	if match := reViralOwner.FindStringSubmatch(pageText); match != nil {
		drama.Author = p91CleanText(match[1])
	}
	if match := reViralOwnerID.FindStringSubmatch(pageText); match != nil {
		drama.AuthorID = match[1]
	}
	chapter := Chapter{
		ID:       providerChapterID(sourceViral, sourceID, "main"),
		Source:   sourceViral,
		Title:    "正片",
		VideoURL: viralPlayURL(pageText),
		PageURL:  pageURL,
		Referer:  pageURL,
	}
	drama.EpisodeCount, drama.TotalEpisode = 1, 1
	return drama, []Chapter{chapter}, nil
}

func (d *Downloader) resolveViralMedia(ctx context.Context, task Task) (providerMedia, error) {
	source, sourceID, valid := splitProviderDramaID(task.DramaID)
	prefix := providerChapterID(sourceViral, sourceID, "")
	if !valid || source != sourceViral || !validViralID(sourceID) || !strings.HasPrefix(task.Chapter.ID, prefix) {
		return providerMedia{}, errors.New("ViralXXX 播放信息无效，请刷新详情")
	}
	pageURL := viralBaseURL + viralSitePath + "/video/" + sourceID + "/"
	address := strings.TrimSpace(task.Chapter.VideoURL)
	if !isProviderHTTPMediaURL(address) {
		// 详情页没带出地址：回源重解析一次。
		pageText, err := d.fetchProviderText(ctx, pageURL, viralBaseURL+viralSitePath+"/")
		if err != nil {
			return providerMedia{}, fmt.Errorf("ViralXXX 回源失败：%v", err)
		}
		address = viralPlayURL(pageText)
		if address == "" {
			return providerMedia{}, errors.New("ViralXXX 未解析到播放地址，请刷新详情")
		}
	}
	// 直链是成品 mp4（302 到公开 CDN），中转直接转发即可，不需要额外参数。
	return providerMedia{URL: address, Referer: pageURL}, nil
}
