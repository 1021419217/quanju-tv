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
	"strconv"
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
// 三个必须照做的细节（来自站方 2026 年现行行为，第三方解析器实测）：
//  1. 请求要带 Cookie `language=cn_CN`，站点才给中文界面；再带游客标记 `ga`
//     （值不限，`1` 亦可）。不带 ga 时站点按会话随机挑一部片渲染媒体与封面，
//     表现为「标题对、播放/封面对不上」，所以固定给一个值把渲染钉住。
//  2. 真地址藏在 document.write(strencode2("<URL 编码的 <source> 标签>")) 里，
//     而 strencode2 的实现就是 unescape()，等价于一次百分号解码，不需要跑 JS。
//  3. 真播放地址就在 `*.91p52.com` 上（如 https://ccm.91p52.com/358999.mp4?st=…），
//     这个域名是站方自己的 CDN，不能当广告过滤掉 —— 早期版本误滤，导致
//     「列表正常、点进去永远提示未返回可播放地址」。

const (
	p91SiteURL   = "https://91porn.com"
	p91UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	p91Cookie    = "language=cn_CN; ga=1"
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
	p91ViewkeyRE  = regexp.MustCompile(`view_video(?:_hd)?\.php\?viewkey=([0-9a-zA-Z]+)`)
	p91TitleRE    = regexp.MustCompile(`(?s)<span class="video-title[^"]*">(.*?)</span>`)
	p91ThumbRE    = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	p91DurationRE = regexp.MustCompile(`<span class="duration">\s*([0-9:]+)\s*</span>`)
	p91FromRE     = regexp.MustCompile(`<span class="info">From:</span>\s*([^<]*)`)
	p91TagRE      = regexp.MustCompile(`<[^>]*>`)

	p91SourceTagRE = regexp.MustCompile(`(?i)<source[^>]+src\s*=\s*["']?([^"'\s>]+)`)
	p91VideoTagRE  = regexp.MustCompile(`(?i)<video[^>]+src\s*=\s*["']?([^"'\s>]+)`)
	p91MediaURLRE  = regexp.MustCompile(`(?i)https?://[^\s"'<>\\]+?\.(?:mp4|m3u8)[^\s"'<>\\]*`)

	p91PageTitleRE = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	p91PosterRE    = regexp.MustCompile(`poster="([^"]+)"`)
	p91OGImageRE   = regexp.MustCompile(`(?i)og:image["'][^>]+content=["']([^"']+)`)
	p91DateRE      = regexp.MustCompile(`<span class="title-yakov">(\d{4}-\d{2}-\d{2})</span>`)
	p91DailyRE     = regexp.MustCompile(`每天只可观看\s*(\d+)\s*个视频`)
	p91DeletedText = "视频不存在,可能已经被删除或者被举报为不良内容"
)

// p91AdMediaHosts 是站方贴片广告/统计用的域名，只排除这些；站方自家 CDN
// （91p52.com、rsc.cdn77.org）承载真播放流，绝不能进这个表。
var p91AdMediaHosts = []string{"kwai.net", "xhlive.net", "9splt.com", "jads.co", "storagexhd.com", "91p52.net"}

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
		ID:          providerDramaID(sourceP91, sourceID),
		Source:      sourceP91,
		SourceID:    sourceID,
		Title:       title,
		Name:        title,
		ChannelName: "91porn",
		Cover:       cover,
		OnlineDate:  onlineDate,
	}

	address, sourceErr := p91SourceFromPage(pageText, pageURL)
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
//
// 依次尝试：脚本里的加密串（多写法）→ 页面直出的 <source>/<video> → 整页 mp4/m3u8 兜底。
func p91SourceFromPage(pageText, pageURL string) (string, error) {
	candidates := make([]string, 0, 16)
	for _, literal := range p91ScriptLiterals(pageText) {
		candidates = append(candidates, p91CandidatesFromMarkup(p91PercentDecode(literal))...)
	}
	candidates = append(candidates, p91CandidatesFromMarkup(pageText)...)
	for _, match := range p91MediaURLRE.FindAllString(pageText, -1) {
		candidates = append(candidates, match)
	}

	blocked := false
	for _, raw := range candidates {
		address := p91NormalizeMediaURL(raw, pageURL)
		if address == "" {
			continue
		}
		if p91BlockedMediaHost(address) {
			blocked = true
			continue
		}
		if p91PlayableMediaURL(address) {
			return address, nil
		}
	}
	if blocked {
		return "", errors.New("91porn 只返回了广告占位片，请重试")
	}
	return "", errors.New("91porn 未返回可播放地址")
}

// p91ScriptLiterals 把页面里 strencode2/strencode1/strencode/unescape 调用的
// 字符串参数全部取出来，单双引号、空白、换行都兼容 —— 站方不同模板写法不一。
func p91ScriptLiterals(pageText string) []string {
	lower := strings.ToLower(pageText)
	markers := []string{"strencode2(", "strencode1(", "strencode(", "unescape("}
	literals := make([]string, 0, 4)
	for _, marker := range markers {
		offset := 0
		for {
			index := strings.Index(lower[offset:], marker)
			if index < 0 {
				break
			}
			start := offset + index + len(marker)
			offset = start
			if literal, ok := p91ReadJSString(pageText, start); ok && literal != "" {
				literals = append(literals, literal)
			}
		}
	}
	return literals
}

// p91ReadJSString 从 start 处读一个字面量字符串，跳过空白并支持转义引号。
func p91ReadJSString(pageText string, start int) (string, bool) {
	i := start
	for i < len(pageText) {
		switch pageText[i] {
		case ' ', '\t', '\r', '\n':
			i++
			continue
		}
		break
	}
	if i >= len(pageText) || (pageText[i] != '"' && pageText[i] != '\'') {
		return "", false
	}
	quote := pageText[i]
	i++
	begin := i
	for i < len(pageText) {
		if pageText[i] == '\\' {
			i += 2
			continue
		}
		if pageText[i] == quote {
			return p91UnescapeJSString(pageText[begin:i]), true
		}
		i++
	}
	return "", false
}

// p91UnescapeJSString 处理 JS 字符串里的常见转义（站方有的模板会把引号转义后再塞进加密串）。
func p91UnescapeJSString(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var builder strings.Builder
	builder.Grow(len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' || i+1 >= len(raw) {
			builder.WriteByte(raw[i])
			continue
		}
		i++
		builder.WriteByte(raw[i])
	}
	return builder.String()
}

// p91PercentDecode 模拟 JS 的 unescape()：支持 %XX 与 %uXXXX，
// 遇到孤立的 % 原样保留（PathUnescape 会直接报错，这是早期取不到地址的隐患之一）。
func p91PercentDecode(raw string) string {
	buffer := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		if raw[i] != '%' {
			buffer = append(buffer, raw[i])
			continue
		}
		if i+5 < len(raw) && (raw[i+1] == 'u' || raw[i+1] == 'U') {
			if value, err := strconv.ParseUint(raw[i+2:i+6], 16, 32); err == nil {
				buffer = append(buffer, []byte(string(rune(value)))...)
				i += 5
				continue
			}
		}
		if i+2 < len(raw) {
			high, okHigh := p91HexValue(raw[i+1])
			low, okLow := p91HexValue(raw[i+2])
			if okHigh && okLow {
				buffer = append(buffer, high<<4|low)
				i += 2
				continue
			}
		}
		buffer = append(buffer, raw[i])
	}
	return string(buffer)
}

func p91HexValue(char byte) (byte, bool) {
	switch {
	case char >= '0' && char <= '9':
		return char - '0', true
	case char >= 'a' && char <= 'f':
		return char - 'a' + 10, true
	case char >= 'A' && char <= 'F':
		return char - 'A' + 10, true
	}
	return 0, false
}

// p91CandidatesFromMarkup 从一段 HTML 里按可靠性顺序取播放地址候选。
func p91CandidatesFromMarkup(text string) []string {
	candidates := make([]string, 0, 6)
	for _, pattern := range []*regexp.Regexp{p91SourceTagRE, p91VideoTagRE, p91MediaURLRE} {
		for _, match := range pattern.FindAllStringSubmatch(text, -1) {
			if len(match) > 1 && strings.TrimSpace(match[1]) != "" {
				candidates = append(candidates, match[1])
			}
		}
	}
	return candidates
}

// p91NormalizeMediaURL 解码实体、去掉包裹引号、把 `\/` 还原成 `/`，并按详情页地址补全相对路径。
func p91NormalizeMediaURL(raw, pageURL string) string {
	address := strings.TrimSpace(html.UnescapeString(raw))
	address = strings.Trim(address, "\"'\\")
	address = strings.ReplaceAll(address, "\\/", "/")
	if address == "" || strings.ContainsAny(address, " \t\r\n") {
		return ""
	}
	base, err := url.Parse(pageURL)
	if err != nil || base.Scheme == "" {
		base, _ = url.Parse(p91SiteURL + "/")
	}
	reference, err := url.Parse(address)
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(reference)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return ""
	}
	return resolved.String()
}

// p91PlayableMediaURL 只要是可播放的 MP4/HLS 地址就放行；
// 站方 CDN（91p52.com 等）必须保留，不能按域名误杀。
func p91PlayableMediaURL(address string) bool {
	if !isProviderHTTPMediaURL(address) {
		return false
	}
	lower := strings.ToLower(address)
	if strings.ContainsAny(lower, " \t\r\n") {
		return false
	}
	for _, ext := range []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg", ".css", ".js"} {
		if strings.Contains(lower, ext) {
			return false
		}
	}
	return strings.Contains(lower, ".mp4") || strings.Contains(lower, ".m3u8")
}

// p91BlockedMediaHost 只拦明确的贴片广告域名与广告路径。
func p91BlockedMediaHost(address string) bool {
	lower := strings.ToLower(address)
	for _, host := range p91AdMediaHosts {
		if strings.Contains(lower, host) {
			return true
		}
	}
	return strings.Contains(lower, "/ads/") || strings.Contains(lower, "/advert")
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
		fresh, err := p91SourceFromPage(pageText, pageURL)
		if err != nil {
			return providerMedia{}, err
		}
		address = fresh
	}
	return providerMedia{
		URL:     address,
		Referer: p91SiteURL + "/",
		credentials: &providerMediaCredentials{
			cookie:    p91Cookie,
			referer:   p91SiteURL + "/",
			userAgent: p91UserAgent,
		},
	}, nil
}
