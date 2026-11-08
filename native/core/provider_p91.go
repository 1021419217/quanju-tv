package core

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// ---- 91porn（sourceP91）----
//
// 站方是经典 PHP 站，页面直接输出 HTML：
//   列表：/v.php?category=<分类>&viewtype=basic&page=<页>
//   搜索：/search_result.php?search_id=<关键词>&search_type=search_videos&viewtype=basic
//   详情：/view_video.php?viewkey=<id>
//
// 两个必须照做的细节（来自站方现行行为，实测于第三方维护中的解析器）：
//  1. 请求必须带 Cookie `language=cn_CN; level=7`。桌面页 + level=7 才下发
//     strencode2 的真播放地址；否则只给一条广告占位片。
//  2. 真地址藏在 document.write(strencode2("<URL 编码的 <source> 标签>")) 里，
//     而 strencode2 的实现就是 unescape()，等价于一次百分号解码，不需要跑 JS。
//
// 页面里直出的 <source src="...ccm.91p52.com/xxx.mp4"> 是广告占位，必须排除。

const (
	p91SiteURL   = "https://91porn.com"
	p91UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	p91Cookie    = "language=cn_CN; level=7"
)

// p91CategoryEntries 是站方导航里的分类，原样保留。
var p91CategoryEntries = []struct {
	ID   string
	Name string
}{
	{"rf", "最近更新"}, {"hot", "本月最热"}, {"top", "本周最热"},
	{"mf", "收藏最多"}, {"md", "评论最多"},
	{"long", "10分钟以上"}, {"longer", "20分钟以上"},
	{"hd", "高清"}, {"ori", "原创"},
}

func validP91Category(category string) bool {
	category = strings.TrimSpace(category)
	if category == "" {
		return false
	}
	for _, entry := range p91CategoryEntries {
		if entry.ID == category {
			return true
		}
	}
	// 站方早期分类 id 仍有直达链接，放行避免旧选中态报「内容分类无效」。
	switch category {
	case "rp", "rf", "hot", "top", "mf", "md", "long", "longer", "hd", "ori":
		return true
	}
	return false
}

func p91CategoryName(category string) string {
	category = strings.TrimSpace(category)
	for _, entry := range p91CategoryEntries {
		if entry.ID == category {
			return entry.Name
		}
	}
	return ""
}

var (
	p91CardRE     = regexp.MustCompile(`(?s)<div class="well well-sm videos-text-align">(.*?)</div>\s*</div>`)
	p91ViewkeyRE  = regexp.MustCompile(`view_video\.php\?viewkey=([0-9a-zA-Z]+)`)
	p91TitleRE    = regexp.MustCompile(`(?s)<span class="video-title[^"]*">(.*?)</span>`)
	p91ThumbRE    = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	p91DurationRE = regexp.MustCompile(`<span class="duration">\s*([0-9:]+)\s*</span>`)
	p91FromRE     = regexp.MustCompile(`<span class="info">From:</span>\s*([^<]*)`)
	p91TagRE      = regexp.MustCompile(`<[^>]*>`)

	p91StrencodeRE = regexp.MustCompile(`strencode2\("([^"]+)"\)`)
	p91SrcRE       = regexp.MustCompile(`src=["']([^"']+)["']`)
	p91MP4RE       = regexp.MustCompile(`(?i)(https?://[^\s"'<>]+\.mp4[^\s"'<>]*)`)

	p91PageTitleRE = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	p91PosterRE    = regexp.MustCompile(`poster="([^"]+)"`)
	p91OGImageRE   = regexp.MustCompile(`(?i)og:image["'][^>]+content=["']([^"']+)`)
	p91DateRE      = regexp.MustCompile(`<span class="title-yakov">(\d{4}-\d{2}-\d{2})</span>`)
	p91DailyRE     = regexp.MustCompile(`作为游客，你每天只可观看\s*(\d+)\s*个视频`)
	p91DeletedText = "视频不存在,可能已经被删除或者被举报为不良内容"
)

// ---- 请求 ----

func (d *Downloader) p91Get(ctx context.Context, rawURL string) (string, error) {
	retries := d.cfg.Retries
	if retries <= 0 {
		retries = 3
	}
	var lastErr error
	for attempt := 1; attempt <= retries; attempt++ {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if attempt > 1 {
			select {
			case <-time.After(time.Duration(attempt) * 600 * time.Millisecond):
			case <-ctx.Done():
				return "", ctx.Err()
			}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return "", err
		}
		request.Header.Set("User-Agent", p91UserAgent)
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		request.Header.Set("Referer", p91SiteURL+"/")
		request.Header.Set("Cookie", p91Cookie)
		response, err := d.doPreparedCatalogRequest(request, 20*time.Second, nil)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, providerMaxBodyBytes+1))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode >= 400 {
			lastErr = fmt.Errorf("91porn 返回 HTTP %d", response.StatusCode)
			continue
		}
		return p91DecodeBody(body), nil
	}
	if lastErr == nil {
		lastErr = errors.New("91porn 请求失败")
	}
	return "", lastErr
}

// p91DecodeBody 老页面是 GBK/GB18030，新页面是 UTF-8，按声明转码。
func p91DecodeBody(body []byte) string {
	if xbpqCharsetPattern.Match(body) {
		if decoded, _, err := transform.String(simplifiedchinese.GB18030.NewDecoder(), string(body)); err == nil {
			return decoded
		}
	}
	return string(body)
}

// ---- 文本工具 ----

func p91StripTags(raw string) string {
	return p91TagRE.ReplaceAllString(raw, "")
}

func p91CleanText(raw string) string {
	text := strings.TrimSpace(html.UnescapeString(p91StripTags(raw)))
	text = strings.Join(strings.Fields(text), " ")
	return text
}

func p91CleanTitle(raw string) string {
	title := p91CleanText(raw)
	title = strings.TrimSpace(strings.TrimSuffix(title, "Chinese homemade video"))
	title = strings.TrimSpace(strings.TrimSuffix(title, "- 91porn"))
	title = strings.TrimSpace(strings.TrimSuffix(title, "- 91Porn"))
	return title
}

// p91DurationText 把「12:34」规整成卡片副标题能用的短文本。
func p91DurationText(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	return raw
}

// ---- 列表 ----

func (d *Downloader) fetchP91CatalogPage(ctx context.Context, page int, category string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	if !validP91Category(category) {
		category = "rf"
	}
	address := p91SiteURL + "/v.php?category=" + url.QueryEscape(category) +
		"&viewtype=basic&page=" + fmt.Sprint(page)
	pageText, err := d.p91Get(ctx, address)
	if err != nil {
		return nil, false, err
	}
	items := parseP91Cards(pageText, category)
	return items, len(items) >= 12, nil
}

func (d *Downloader) searchP91(ctx context.Context, query string, page int) ([]Drama, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, false, errors.New("请输入搜索内容")
	}
	address := p91SiteURL + "/search_result.php?search_id=" + url.QueryEscape(query) +
		"&search_type=search_videos&viewtype=basic"
	if page > 1 {
		address += "&page=" + fmt.Sprint(page)
	}
	pageText, err := d.p91Get(ctx, address)
	if err != nil {
		return nil, false, err
	}
	items := parseP91Cards(pageText, "")
	return items, len(items) >= 12, nil
}

func parseP91Cards(pageText, category string) []Drama {
	cards := make([]string, 0, 24)
	for _, block := range p91CardRE.FindAllStringSubmatch(pageText, -1) {
		if len(block) > 1 {
			cards = append(cards, block[1])
		}
	}
	if len(cards) == 0 {
		// 兜底：站方偶尔调 class，主正则整页不命中时按 viewkey 位置开窗，
		// 保证列表不会直接空白。只在主路径失败时启用，不干扰正常解析。
		cards = p91FallbackWindows(pageText)
	}
	items := make([]Drama, 0, len(cards))
	seen := make(map[string]bool, len(cards))
	categoryName := p91CategoryName(category)
	for _, card := range cards {
		match := p91ViewkeyRE.FindStringSubmatch(card)
		if match == nil {
			continue
		}
		viewkey := strings.TrimSpace(match[1])
		if viewkey == "" || seen[viewkey] {
			continue
		}
		seen[viewkey] = true

		title := ""
		if m := p91TitleRE.FindStringSubmatch(card); m != nil {
			title = p91CleanText(m[1])
		}
		if title == "" {
			title = "91porn " + viewkey
		}
		thumb := ""
		if m := p91ThumbRE.FindStringSubmatch(card); m != nil {
			thumb = strings.TrimSpace(html.UnescapeString(m[1]))
			if strings.HasPrefix(thumb, "//") {
				thumb = "https:" + thumb
			}
		}
		duration := ""
		if m := p91DurationRE.FindStringSubmatch(card); m != nil {
			duration = p91DurationText(m[1])
		}
		uploader := ""
		if m := p91FromRE.FindStringSubmatch(card); m != nil {
			uploader = p91CleanText(m[1])
		}

		item := Drama{
			ID:          providerDramaID(sourceP91, viewkey),
			Source:      sourceP91,
			SourceID:    viewkey,
			Title:       title,
			Name:        title,
			Desc:        descriptionOr(uploader),
			Cover:       thumb,
			Remark:      duration,
			TypeName:    categoryName,
			ChannelName: "91porn",
		}
		items = append(items, item)
	}
	return items
}

func descriptionOr(uploader string) string {
	if strings.TrimSpace(uploader) == "" {
		return ""
	}
	return "作者: " + uploader
}

// p91FallbackWindows 以每个 viewkey 链接为中心各取一段文本当卡片解析。
func p91FallbackWindows(pageText string) []string {
	matches := p91ViewkeyRE.FindAllStringIndex(pageText, -1)
	windows := make([]string, 0, len(matches))
	const radius = 1400
	for _, m := range matches {
		start := m[0] - radius
		if start < 0 {
			start = 0
		}
		end := m[1] + radius
		if end > len(pageText) {
			end = len(pageText)
		}
		windows = append(windows, pageText[start:end])
	}
	return windows
}

// ---- 详情 ----

func (d *Downloader) fetchP91Detail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || strings.ContainsAny(sourceID, "/?#& ") {
		return Drama{}, nil, errors.New("91porn 视频 ID 无效")
	}
	pageURL := p91SiteURL + "/view_video.php?viewkey=" + url.QueryEscape(sourceID)
	pageText, err := d.p91Get(ctx, pageURL)
	if err != nil {
		return Drama{}, nil, err
	}
	if strings.Contains(pageText, p91DeletedText) {
		return Drama{}, nil, errors.New("该视频已删除或被举报")
	}
	if m := p91DailyRE.FindStringSubmatch(pageText); m != nil {
		return Drama{}, nil, fmt.Errorf("91porn 提示：游客每天只能观看 %s 个视频，明天再试", m[1])
	}

	title := ""
	if m := p91PageTitleRE.FindStringSubmatch(pageText); m != nil {
		title = p91CleanTitle(m[1])
	}
	if title == "" {
		title = "91porn " + sourceID
	}
	cover := ""
	if m := p91PosterRE.FindStringSubmatch(pageText); m != nil {
		cover = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if cover == "" {
		if m := p91OGImageRE.FindStringSubmatch(pageText); m != nil {
			cover = strings.TrimSpace(html.UnescapeString(m[1]))
		}
	}
	if strings.HasPrefix(cover, "//") {
		cover = "https:" + cover
	}
	onlineDate := ""
	if m := p91DateRE.FindStringSubmatch(pageText); m != nil {
		onlineDate = m[1]
	}

	drama := Drama{
		ID:         providerDramaID(sourceP91, sourceID),
		Source:     sourceP91,
		SourceID:   sourceID,
		Title:      title,
		Name:       title,
		ChannelName: "91porn",
		Cover:      cover,
		OnlineDate: onlineDate,
	}

	address, sourceErr := p91SourceFromPage(pageText)
	chapter := Chapter{
		ID:       providerChapterID(sourceP91, sourceID, "main"),
		Source:   sourceP91,
		Title:    "正片",
		VideoURL: address,
		PageURL:  pageURL,
		Referer:  p91SiteURL + "/",
	}
	chapters := []Chapter{chapter}
	if sourceErr != nil {
		// 解析不到播放地址也返回章节，起播时 resolveP91Media 会再回源试一次，
		// 避免详情页一次抖动就把整条记录判死。
		return drama, chapters, nil
	}
	return drama, chapters, nil
}

// p91SourceFromPage 从详情页提取真实播放地址。
func p91SourceFromPage(pageText string) (string, error) {
	if m := p91StrencodeRE.FindStringSubmatch(pageText); m != nil {
		// strencode2 等价于 unescape()：一次百分号解码即可。
		decoded, err := url.PathUnescape(m[1])
		if err == nil {
			if src := p91SrcRE.FindStringSubmatch(decoded); src != nil {
				address := strings.TrimSpace(html.UnescapeString(src[1]))
				if p91PlayableMediaURL(address) {
					return address, nil
				}
			}
			if mp4 := p91MP4RE.FindStringSubmatch(decoded); mp4 != nil {
				address := strings.TrimSpace(html.UnescapeString(mp4[1]))
				if p91PlayableMediaURL(address) {
					return address, nil
				}
			}
		}
	}
	// 兜底：整页扫 mp4，排除站方广告占位域名。
	for _, candidate := range p91MP4RE.FindAllString(pageText, -1) {
		address := strings.TrimSpace(html.UnescapeString(candidate))
		if p91PlayableMediaURL(address) {
			return address, nil
		}
	}
	return "", errors.New("91porn 未返回可播放地址")
}

// p91PlayableMediaURL 过滤广告占位片：广告域名、以及不带签名参数的裸 mp4。
func p91PlayableMediaURL(address string) bool {
	if !isProviderHTTPMediaURL(address) {
		return false
	}
	lower := strings.ToLower(address)
	for _, host := range []string{"91p52.com", "kwai.net", "cdn77.org"} {
		if strings.Contains(lower, host) {
			return false
		}
	}
	return true
}

// ---- 取流 ----

func (d *Downloader) resolveP91Media(ctx context.Context, task Task) (providerMedia, error) {
	address := strings.TrimSpace(task.Chapter.VideoURL)
	if !p91PlayableMediaURL(address) {
		sourceID := ""
		if _, id, ok := splitProviderDramaID(task.DramaID); ok {
			sourceID = id
		}
		if sourceID == "" {
			sourceID = strings.TrimSpace(task.Chapter.PageURL)
			if index := strings.Index(sourceID, "viewkey="); index >= 0 {
				sourceID = sourceID[index+len("viewkey="):]
				sourceID = strings.TrimSpace(strings.Split(sourceID, "&")[0])
			}
		}
		if sourceID == "" || strings.ContainsAny(sourceID, "/?#& ") {
			return providerMedia{}, errors.New("91porn 播放请求缺少有效的视频 ID")
		}
		pageURL := p91SiteURL + "/view_video.php?viewkey=" + url.QueryEscape(sourceID)
		pageText, err := d.p91Get(ctx, pageURL)
		if err != nil {
			return providerMedia{}, err
		}
		fresh, err := p91SourceFromPage(pageText)
		if err != nil {
			return providerMedia{}, err
		}
		address = fresh
	}
	return providerMedia{
		URL:     address,
		Referer: p91SiteURL + "/",
		credentials: &providerMediaCredentials{
			referer:   p91SiteURL + "/",
			userAgent: p91UserAgent,
		},
	}, nil
}
