package core

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	p91ViewkeyRE    = regexp.MustCompile(`view_video(?:_hd)?\.php\?viewkey=([0-9a-zA-Z]+)`)
	p91TitleRE      = regexp.MustCompile(`(?s)<span[^>]*class="[^"]*video-title[^"]*"[^>]*>(.*?)</span>`)
	p91TitleLinkRE  = regexp.MustCompile(`(?is)<a\b[^>]*class\s*=\s*"[^"]*video-title[^"]*"[^>]*>(.*?)</a>`)
	p91AnchorOpenRE = regexp.MustCompile(`(?is)<a\b[^>]*class\s*=\s*"[^"]*video-title[^"]*"[^>]*>`)
	p91HrefRE       = regexp.MustCompile(`(?is)\bhref\s*=\s*["']([^"']*)["']`)
	p91TitleAttrRE  = regexp.MustCompile(`(?is)\btitle\s*=\s*["']([^"']*)["']`)
	p91ThumbRE      = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	p91DurationRE   = regexp.MustCompile(`<span class="duration">\s*([0-9:]+)\s*</span>`)
	p91FromRE       = regexp.MustCompile(`<span class="info">From:</span>\s*([^<]*)`)
	p91TagRE        = regexp.MustCompile(`<[^>]*>`)

	// 用 div 配对切卡片：站方会在列表里插「混淆卡片」（外层容器带 col-lg-8，
	// 显示的是 A 的图和标题、链接却指向 B），只靠正则跨卡片配对就会
	// 「看见 A、点进去变 B」。这里按真实嵌套边界切块并跳过这些容器。
	p91DivTokenRE  = regexp.MustCompile(`(?is)<div\b[^>]*>|</div>`)
	p91ClassAttrRE = regexp.MustCompile(`(?is)\bclass\s*=\s*["']([^"']*)["']`)
	p91ImgTagRE    = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	p91ImgAttrRE   = regexp.MustCompile(`(?is)\b(data-src|data-original|src)\s*=\s*["']([^"']+)["']`)

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

// p91SessionState 保存本次运行期内的游客身份。
//
// 站方按 Cookie 认游客：每次请求都换一组身份，站点会当成新游客重新渲染，
// 表现为「列表顺序跳、点进去变另一部片」。这里固定一个 ga（首次生成的随机
// 数字），并把站方下发的 PHPSESSID 等会话 Cookie 记住，后续所有请求
// （包括媒体请求）都带同一套，页面与播放地址就会稳定对应同一部片。
var p91SessionState struct {
	mu      sync.Mutex
	guest   string
	cookies map[string]string
}

func p91NewGuestMark() string {
	var buffer [4]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano()%100_000_000, 10)
	}
	return strconv.FormatUint(uint64(binary.BigEndian.Uint32(buffer[:]))%100_000_000, 10)
}

func p91CookieHeader() string {
	p91SessionState.mu.Lock()
	defer p91SessionState.mu.Unlock()
	if p91SessionState.cookies == nil {
		p91SessionState.cookies = map[string]string{}
	}
	if p91SessionState.guest == "" {
		p91SessionState.guest = p91NewGuestMark()
	}
	values := map[string]string{
		"language":  "cn_CN",
		"over18":    "1",
		"CLIPSHARE": "1",
		"ga":        p91SessionState.guest,
	}
	for name, value := range p91SessionState.cookies {
		if name == "" || value == "" {
			continue
		}
		values[name] = value
	}
	order := []string{"language", "over18", "CLIPSHARE", "ga"}
	parts := make([]string, 0, len(values))
	added := make(map[string]bool, len(values))
	for _, name := range order {
		if value, found := values[name]; found {
			parts = append(parts, name+"="+value)
			added[name] = true
		}
	}
	for name, value := range values {
		if added[name] {
			continue
		}
		parts = append(parts, name+"="+value)
	}
	sort.Strings(parts[len(order):])
	return strings.Join(parts, "; ")
}

// p91RememberCookies 把站方下发的会话 Cookie 记下来，下一次请求继续带。
func p91RememberCookies(response *http.Response) {
	if response == nil {
		return
	}
	cookies := response.Cookies()
	if len(cookies) == 0 {
		return
	}
	p91SessionState.mu.Lock()
	defer p91SessionState.mu.Unlock()
	if p91SessionState.cookies == nil {
		p91SessionState.cookies = map[string]string{}
	}
	for _, cookie := range cookies {
		if cookie == nil || cookie.Name == "" {
			continue
		}
		if cookie.MaxAge < 0 || (!cookie.Expires.IsZero() && cookie.Expires.Before(time.Now())) {
			delete(p91SessionState.cookies, cookie.Name)
			continue
		}
		p91SessionState.cookies[cookie.Name] = cookie.Value
	}
}

// p91ResetGuest 丢弃当前游客身份，下一次请求会拿到一套全新的会话。
func p91ResetGuest() {
	p91SessionState.mu.Lock()
	p91SessionState.guest = ""
	p91SessionState.cookies = map[string]string{}
	p91SessionState.mu.Unlock()
}

var (
	p91CanonicalRE = regexp.MustCompile(`(?is)<link[^>]+rel=["']canonical["'][^>]*>`)
	p91OGURLRE     = regexp.MustCompile(`(?is)<meta[^>]+property=["']og:url["'][^>]*>`)
	p91ContentRE   = regexp.MustCompile(`(?is)content=["']([^"']+)["']`)
)

// p91PageMatchesViewkey 判断页面是不是讲的这部片。
//
// 详情页里「相关推荐 / 热门」会带上一堆别的 viewkey，用整页 contains 判断
// 太容易把「页面其实是 B、推荐位里恰好有 A」误判成 A。优先看 canonical /
// og:url —— 这两个只会写当前页自己的地址，是页面的唯一身份。
func p91PageMatchesViewkey(pageText string, viewkey string) bool {
	viewkey = strings.TrimSpace(viewkey)
	if viewkey == "" {
		return true
	}
	if own := p91CanonicalViewkey(pageText); own != "" {
		return strings.EqualFold(own, viewkey)
	}
	if strings.Contains(pageText, "viewkey="+viewkey) {
		return true
	}
	if strings.Contains(strings.ToLower(pageText), "viewkey%3d"+strings.ToLower(viewkey)) {
		return true
	}
	return !strings.Contains(pageText, "viewkey=")
}

// p91CanonicalViewkey 从 canonical / og:url 里取当前页自己的 viewkey。
func p91CanonicalViewkey(pageText string) string {
	for _, pattern := range []struct {
		tag  *regexp.Regexp
		attr *regexp.Regexp
	}{
		{p91CanonicalRE, p91HrefRE},
		{p91OGURLRE, p91ContentRE},
	} {
		tag := pattern.tag.FindString(pageText)
		if tag == "" {
			continue
		}
		match := pattern.attr.FindStringSubmatch(tag)
		if match == nil {
			continue
		}
		if id := p91ViewkeyFromHref(html.UnescapeString(match[1])); id != "" {
			return id
		}
	}
	return ""
}

func (d *Downloader) p91Get(ctx context.Context, rawURL string) (string, error) {
	return d.p91GetWith(ctx, rawURL, "")
}

// p91GetWith 允许带一份额外 Cookie（用于换游客标记重新渲染，正常情况下传空）。
func (d *Downloader) p91GetWith(ctx context.Context, rawURL, extraCookie string) (string, error) {
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
		cookie := p91CookieHeader()
		if extra := strings.TrimSpace(extraCookie); extra != "" {
			cookie += "; " + extra
		}
		request.Header.Set("User-Agent", p91UserAgent)
		request.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		request.Header.Set("Referer", p91SiteURL+"/")
		request.Header.Set("Cookie", cookie)
		response, err := d.doPreparedCatalogRequest(request, 20*time.Second, nil)
		if err != nil {
			lastErr = err
			continue
		}
		p91RememberCookies(response)
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
	blocks := p91CardBlocks(pageText)
	for _, block := range blocks {
		if p91DecoyCard(block.ancestor) {
			continue
		}
		cards = append(cards, block.html)
	}
	if len(cards) == 0 && len(blocks) == 0 {
		// 兜底：站方偶尔调 class，主路径整页不命中时按 viewkey 位置开窗，
		// 保证列表不会直接空白。只在主路径完全没切出卡片时启用，
		// 避免把刚刚过滤掉的混淆卡片又捡回来。
		cards = p91FallbackWindows(pageText)
	}
	items := make([]Drama, 0, len(cards))
	seen := make(map[string]bool, len(cards))
	categoryName := p91CategoryName(category)
	for _, card := range cards {
		viewkey := p91CardViewkey(card)
		if viewkey == "" || seen[viewkey] {
			continue
		}
		seen[viewkey] = true

		title := p91CardTitle(card)
		if title == "" {
			title = "91porn " + viewkey
		}
		thumb := ""
		if tag := p91ImgTagRE.FindString(card); tag != "" {
			thumb = p91ImgSource(tag)
		}
		if strings.HasPrefix(thumb, "//") {
			thumb = "https:" + thumb
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

// p91CardBlock 是一张列表卡片的 HTML 片段和它所有祖先的 class。
type p91CardBlock struct {
	html     string
	ancestor string
}

type p91DivFrame struct {
	card    bool
	classes string
	start   int
}

// p91CardBlocks 用 div 配对把页面切成一张张真实卡片。
// 正则跨卡片配对会把「A 的标题」配到「B 的链接」上，是按图点开却播放别的内容的根因之一。
func p91CardBlocks(pageText string) []p91CardBlock {
	blocks := make([]p91CardBlock, 0, 24)
	stack := make([]p91DivFrame, 0, 32)
	for _, loc := range p91DivTokenRE.FindAllStringIndex(pageText, -1) {
		token := pageText[loc[0]:loc[1]]
		if strings.HasPrefix(token, "</") {
			if len(stack) == 0 {
				continue
			}
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !frame.card {
				continue
			}
			ancestor := make([]string, 0, len(stack))
			for _, parent := range stack {
				ancestor = append(ancestor, parent.classes)
			}
			blocks = append(blocks, p91CardBlock{html: pageText[frame.start:loc[1]], ancestor: strings.Join(ancestor, " ")})
			continue
		}
		classes := ""
		if m := p91ClassAttrRE.FindStringSubmatch(token); m != nil {
			classes = strings.ToLower(strings.TrimSpace(m[1]))
		}
		stack = append(stack, p91DivFrame{
			card:    strings.Contains(classes, "videos-text-align"),
			classes: classes,
			start:   loc[0],
		})
	}
	return blocks
}

// p91DecoyCard 判断卡片是否属于站方插入的混淆/广告容器。
// 正常卡片外层是 col-lg-3，混淆卡片是 col-lg-8（标题图片抄隔壁，链接指向别的片子）。
func p91DecoyCard(ancestor string) bool {
	ancestor = strings.ToLower(ancestor)
	if strings.Contains(ancestor, "col-lg-8") || strings.Contains(ancestor, "sponsor") {
		return true
	}
	for _, name := range strings.Fields(ancestor) {
		if name == "ad" || strings.HasPrefix(name, "ad-") || strings.HasPrefix(name, "ads-") {
			return true
		}
	}
	return false
}

// p91CardViewkey 取卡片里的视频 ID：优先标题链接，其次整卡第一个 viewkey。
// 标题链接和封面链接正常情况指向同一部片；混淆卡片会把其中一个指到别处。
func p91CardViewkey(card string) string {
	if anchor := p91AnchorOpenRE.FindString(card); anchor != "" {
		if m := p91HrefRE.FindStringSubmatch(anchor); m != nil {
			if viewkey := p91ViewkeyFromHref(m[1]); viewkey != "" {
				return viewkey
			}
		}
	}
	if m := p91ViewkeyRE.FindStringSubmatch(card); m != nil {
		return m[1]
	}
	return ""
}

func p91ViewkeyFromHref(href string) string {
	if m := p91ViewkeyRE.FindStringSubmatch(href); m != nil {
		return m[1]
	}
	return ""
}

// p91CardTitle 取卡片标题：优先 <a class="video-title" title="..."> 的属性，
// 再取标签内文本，最后退回 <span class="video-title">。
func p91CardTitle(card string) string {
	if m := p91TitleLinkRE.FindStringSubmatch(card); m != nil {
		title := p91CleanText(m[1])
		if title == "" {
			if anchor := p91AnchorOpenRE.FindString(card); anchor != "" {
				if attr := p91TitleAttrRE.FindStringSubmatch(anchor); attr != nil {
					title = p91CleanText(attr[1])
				}
			}
		}
		if title != "" {
			return title
		}
	}
	if m := p91TitleRE.FindStringSubmatch(card); m != nil {
		return p91CleanText(m[1])
	}
	if anchor := p91AnchorOpenRE.FindString(card); anchor != "" {
		if attr := p91TitleAttrRE.FindStringSubmatch(anchor); attr != nil {
			return p91CleanText(attr[1])
		}
	}
	return ""
}

func p91ImgSource(tag string) string {
	values := make(map[string]string, 3)
	for _, m := range p91ImgAttrRE.FindAllStringSubmatch(tag, -1) {
		name := strings.ToLower(m[1])
		if _, found := values[name]; !found {
			values[name] = strings.TrimSpace(m[2])
		}
	}
	for _, name := range []string{"data-src", "data-original", "src"} {
		value := values[name]
		if value == "" {
			continue
		}
		lower := strings.ToLower(value)
		if strings.Contains(lower, "loading") || strings.Contains(lower, "blank") || strings.Contains(lower, "default") {
			continue
		}
		return html.UnescapeString(value)
	}
	return ""
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

// mergeP91Detail 把详情页补到的字段并进列表里已有的条目。
//
// 列表卡片是用户实际点的那一条，标题和封面必须保持不动：站方详情页按游客身份
// 渲染，偶尔会给出另一部片的标题/封面。如果直接用 mergeNativeDrama 覆盖，
// 就会出现「点 A 进去看到 B、返回列表这一格也变成 B」。
func mergeP91Detail(listed, fresh nativeDrama) nativeDrama {
	if listed.ID != fresh.ID {
		return mergeNativeDrama(listed, fresh)
	}
	merged := mergeNativeDrama(listed, fresh)
	if title := strings.TrimSpace(listed.Title); !p91PlaceholderTitle(title) {
		merged.Title = listed.Title
	}
	if strings.TrimSpace(listed.Cover) != "" {
		merged.Cover = listed.Cover
	}
	return merged
}

// p91PlaceholderTitle 判断列表标题是不是「没解析出标题」时的占位
// （`91porn <viewkey>`）。这种时候应该采用详情页给的标题。
func p91PlaceholderTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || title == "短剧" {
		return true
	}
	rest, ok := strings.CutPrefix(title, "91porn ")
	return ok && !strings.Contains(rest, " ")
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
	// 站方按游客身份渲染：身份被复用/被限流时会拿别的片子顶上来，
	// 表现为「看的是 A，播放的是 B」。换一套游客身份重取一次，
	// 仍对不上就明说，不把别的片子当成本片放给用户。
	if !p91PageMatchesViewkey(pageText, sourceID) {
		p91ResetGuest()
		if retryText, retryErr := d.p91Get(ctx, pageURL); retryErr == nil {
			pageText = retryText
		}
		if !p91PageMatchesViewkey(pageText, sourceID) {
			return Drama{}, nil, errors.New("91porn 返回了别的视频（游客身份异常），请重试")
		}
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

// p91SourceFromPage 从详情页提取第一个候选播放地址（详情页展示用，不做连通性探测）。
func p91SourceFromPage(pageText, pageURL string) (string, error) {
	candidates, blocked := p91MediaCandidates(pageText, pageURL)
	if len(candidates) > 0 {
		return candidates[0], nil
	}
	if blocked > 0 {
		return "", errors.New("91porn 只返回了广告占位片，请重试")
	}
	return "", errors.New("91porn 未返回可播放地址")
}

// p91MediaCandidates 返回去重后的全部候选直链（按可靠性排序），第二个返回值是被
// 广告规则拦掉的条数，用来区分「页面没给地址」和「给了但都是广告」。
//
// 顺序：脚本加密串（播放器真正的 source）→ 页面直出 <source>/<video> → 整页 mp4/m3u8 兜底。
func p91MediaCandidates(pageText, pageURL string) ([]string, int) {
	raw := make([]string, 0, 16)
	for _, literal := range p91ScriptLiterals(pageText) {
		raw = append(raw, p91CandidatesFromMarkup(p91PercentDecode(literal))...)
	}
	raw = append(raw, p91CandidatesFromMarkup(pageText)...)
	for _, match := range p91MediaURLRE.FindAllString(pageText, -1) {
		raw = append(raw, match)
	}

	seen := make(map[string]bool, len(raw))
	candidates := make([]string, 0, len(raw))
	blocked := 0
	for _, item := range raw {
		address := p91NormalizeMediaURL(item, pageURL)
		if address == "" {
			continue
		}
		if p91BlockedMediaHost(address) {
			blocked++
			continue
		}
		if !p91PlayableMediaURL(address) || seen[address] {
			continue
		}
		seen[address] = true
		candidates = append(candidates, address)
	}
	return candidates, blocked
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

// p91ProbeLimit 是单次取流最多探测几条候选：详情页通常只给 1~2 条真地址，
// 多条时按可靠性顺序试前面几条即可，避免把打开播放拖慢。
const p91ProbeLimit = 4

func (d *Downloader) resolveP91Media(ctx context.Context, task Task) (providerMedia, error) {
	candidates := make([]string, 0, 4)
	if address := strings.TrimSpace(task.Chapter.VideoURL); p91PlayableMediaURL(address) {
		candidates = append(candidates, address)
	}

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

	// 详情页带出来的地址也可能失效（签名过期 / 站方换节点），先探测；不通就回源重取。
	var lastProbeErr error
	if media, err := d.p91PickMedia(ctx, candidates, pageURL); err == nil {
		return media, nil
	} else {
		lastProbeErr = err
	}

	pageText, pageErr := d.p91Get(ctx, pageURL)
	blocked := 0
	if pageErr == nil {
		fresh, blockedCount := p91MediaCandidates(pageText, pageURL)
		blocked += blockedCount
		candidates = append(candidates, fresh...)
		if media, err := d.p91PickMedia(ctx, candidates, pageURL); err == nil {
			return media, nil
		} else {
			lastProbeErr = err
		}
	}

	// ev.php?VID=<viewkey> 是站方「按视频取播放器」的入口：返回内容只对应这一部片，
	// 不受详情页随机渲染和相关推荐干扰，作为第二来源。
	evAddress := p91SiteURL + "/ev.php?VID=" + url.QueryEscape(sourceID)
	if evText, evErr := d.p91Get(ctx, evAddress); evErr == nil {
		fresh, evBlocked := p91MediaCandidates(evText, evAddress)
		blocked += evBlocked
		candidates = append(candidates, fresh...)
		if media, err := d.p91PickMedia(ctx, candidates, pageURL); err == nil {
			return media, nil
		} else {
			lastProbeErr = err
		}
	}

	// 还是不通就换一套游客身份（同一游客站方会缓存/限流），把详情页整页重取一次。
	p91ResetGuest()
	if retryText, retryErr := d.p91Get(ctx, pageURL); retryErr == nil {
		fresh, _ := p91MediaCandidates(retryText, pageURL)
		candidates = append(candidates, fresh...)
		if media, err := d.p91PickMedia(ctx, candidates, pageURL); err == nil {
			return media, nil
		} else {
			lastProbeErr = err
		}
	}

	if len(candidates) == 0 {
		if blocked > 0 {
			return providerMedia{}, errors.New("91porn 只返回了广告占位片，请重试")
		}
		return providerMedia{}, errors.New("91porn 未返回可播放地址")
	}
	if lastProbeErr != nil {
		return providerMedia{}, lastProbeErr
	}
	return providerMedia{}, errors.New("91porn 播放地址连不上，请重试")
}

// p91PickMedia 依次探测候选地址，返回第一条真的能出流的，其余作为备用线路
// （播放器恢复时会自动换线，用户也可以在清晰度/线路里手动换）。
func (d *Downloader) p91PickMedia(ctx context.Context, candidates []string, pageURL string) (providerMedia, error) {
	if len(candidates) == 0 {
		return providerMedia{}, errors.New("91porn 未返回可播放地址")
	}
	// 整轮探测给 15 秒上限：站方 CDN 卡住时不能让「点播放」干等半分钟。
	probeCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	var lastErr error
	probed := 0
	seen := make(map[string]bool, len(candidates))
	for index, address := range candidates {
		if probed >= p91ProbeLimit {
			break
		}
		if seen[address] {
			continue
		}
		seen[address] = true
		probed++
		insecure, err := d.p91Probe(probeCtx, address, pageURL)
		if err != nil {
			lastErr = err
			continue
		}
		media := d.p91MediaFor(address, insecure, pageURL)
		for _, extra := range candidates[index+1:] {
			if len(media.Variants) >= 2 {
				break
			}
			media.Variants = append(media.Variants, d.p91MediaFor(extra, false, pageURL))
		}
		return media, nil
	}
	if lastErr == nil {
		lastErr = errors.New("91porn 未返回可播放地址")
	}
	return providerMedia{}, fmt.Errorf("91porn 播放地址连不上（%s），请重试", p91ProbeReason(lastErr))
}

// p91MediaFor 组装播放凭据。Referer 用具体的详情页而不是首页：
// 站方 CDN 按 Referer 判定来源，给首页地址时部分节点只放行前一小段就掐断，
// 表现为「播几秒就中断」，而同一部片用详情页地址能完整拉完。
func (d *Downloader) p91MediaFor(address string, insecure bool, referer string) providerMedia {
	if strings.TrimSpace(referer) == "" {
		referer = p91SiteURL + "/"
	}
	return providerMedia{
		URL:     address,
		Referer: referer,
		credentials: &providerMediaCredentials{
			cookie:      p91CookieHeader(),
			referer:     referer,
			userAgent:   p91UserAgent,
			insecureTLS: insecure,
			// 站方 CDN 会掐长连接（看几秒到几十秒就断），播放端看到的就是「传输中断」。
			// 标记成可续传，本地中转在断点处自动重新拉一段，播放器不用重连。
			resilient:  true,
			sendCookie: true,
		},
	}
}

// p91Probe 确认直链真的能出流。
// 只用 GET 拉前 4KB：HEAD 常常「通」但真拉流被拒/被断，用它判断会误判成可用。
// 站方 CDN（*.91p52.com）证书链不完整（缺中间证书），标准校验会失败但流本身可播，
// 所以遇到证书错误时用「跳过校验」再试一次，成功就记住这个源要走宽松传输。
func (d *Downloader) p91Probe(ctx context.Context, address, referer string) (bool, error) {
	if strings.TrimSpace(referer) == "" {
		referer = p91SiteURL + "/"
	}
	var lastErr error
	for _, insecure := range []bool{false, true} {
		err := d.p91ProbeOnce(ctx, address, referer, insecure)
		if err == nil {
			return insecure, nil
		}
		lastErr = err
		if !p91CertificateError(err) {
			return false, err
		}
	}
	return false, lastErr
}

func (d *Downloader) p91ProbeOnce(ctx context.Context, address, referer string, insecure bool) error {
	probeCtx, cancel := context.WithTimeout(ctx, 7*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", p91UserAgent)
	request.Header.Set("Referer", referer)
	request.Header.Set("Accept", "*/*")
	request.Header.Set("Cookie", p91CookieHeader())
	request.Header.Set("Range", "bytes=0-65535")

	client := *d.client
	client.Timeout = 8 * time.Second
	if insecure {
		client.Transport = insecureMediaTransport(d.client.Transport)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return fmt.Errorf("91porn 直链返回 HTTP %d", response.StatusCode)
	}
	if contentType := strings.ToLower(response.Header.Get("Content-Type")); strings.Contains(contentType, "text/html") {
		return errors.New("91porn 直链返回的是网页而不是视频")
	}
	buffer := make([]byte, 4096)
	read, readErr := io.ReadFull(io.LimitReader(response.Body, int64(len(buffer))), buffer)
	if readErr != nil && readErr != io.ErrUnexpectedEOF && readErr != io.EOF {
		return readErr
	}
	if read < 512 {
		return errors.New("91porn 直链返回内容过短")
	}
	return nil
}

func p91CertificateError(err error) bool {
	if err == nil {
		return false
	}
	var certificate *tls.CertificateVerificationError
	return errors.As(err, &certificate) || strings.Contains(strings.ToLower(err.Error()), "certificate")
}

// p91InsecureTransport 复制一份跳过证书校验的传输层，只给证书链不完整的站方 CDN 用。
func p91InsecureTransport(base http.RoundTripper) http.RoundTripper {
	transport, ok := base.(*http.Transport)
	if !ok || transport == nil {
		return base
	}
	cloned := transport.Clone()
	if cloned.TLSClientConfig == nil {
		cloned.TLSClientConfig = &tls.Config{}
	} else {
		cloned.TLSClientConfig = cloned.TLSClientConfig.Clone()
	}
	cloned.TLSClientConfig.InsecureSkipVerify = true
	return cloned
}

// p91ProbeReason 把底层错误压成一句用户能看懂的短说明，便于用户反馈定位。
func p91ProbeReason(err error) string {
	if err == nil {
		return "未知原因"
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		return "证书校验失败"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "请求超时"
	}
	text := err.Error()
	for _, pair := range [][2]string{
		{"HTTP 403", "HTTP 403"},
		{"HTTP 404", "HTTP 404"},
		{"HTTP 5", "站方服务异常"},
		{"no such host", "域名解析失败"},
		{"connection reset", "连接被重置"},
		{"connection refused", "连接被拒绝"},
	} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(pair[0])) {
			return pair[1]
		}
	}
	return "网络不可达"
}
