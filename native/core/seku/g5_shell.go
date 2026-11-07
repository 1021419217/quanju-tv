package seku

// ---- G5 shell sites: yuuhc / setang (whole-page AES-256-GCM shells) ----
//
// Both pages arrive as a WebCrypto shell; all key material lives inside
// the page's own script, so nothing is hardcoded. Material location
// differs per site and is ported verbatim from the py spiders:
//   yuuhc : assignment scan + role regexes over the last 6000 chars
//           (two share vars XOR to the key; iv:/additionalData: vars;
//           ciphertext var) — verified byte-exact on archived pages.
//   setang: blob state machine + length classification (longest blob =
//           ciphertext, 32B blobs XOR to the key, smalls tried as
//           IV/AAD) — verified byte-exact on archived pages.
// Covers: the py spiders append TVBox "@Referer=.../@User-Agent=..."
// tails to cover URLs. The Go core instead keeps bare URLs in dramas
// and serves the bytes through CoverFetch with the same headers.

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const g5ShellUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

func g5ShellHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      g5ShellUA,
		"Accept":          "text/html,application/xhtml+xml,*/*",
		"Accept-Language": "zh-CN,zh;q=0.9",
	}
}

// ================= yuuhc （欲含春） =================

var g5YuuhcHosts = []string{
	"https://co65a.yuuhc115.sbs",
	"https://yuuhc115.sbs",
	"https://www.yuuhc115.sbs",
}

// Navigation 1:1 (lines 1-4 merged, 32 cats; 色图色书 is an off-site
// image/text link with no in-site videos, excluded as in py).
var g5YuuhcCats = []struct{ id, name string }{
	{"4042620", "探花系列"}, {"4052620", "偷拍自拍"}, {"4062620", "熟女少妇"},
	{"4072620", "无码专区"}, {"4082620", "欧美性爱"}, {"4092620", "颜值正义"},
	{"4022620", "精品推荐"}, {"4032620", "国产传媒"}, {"4122630", "美乳巨乳"},
	{"4132630", "网曝事件"}, {"4142630", "国产主播"}, {"4152630", "中文字幕"},
	{"4162630", "制服丝袜"}, {"4172630", "口交自慰"}, {"4182630", "国产精品"},
	{"4192630", "大秀视频"}, {"4222640", "亚洲情色"}, {"4232640", "强奸乱伦"},
	{"4242640", "伦理三级"}, {"4252640", "女同性恋"}, {"4262640", "明星换脸"},
	{"4272640", "A V 解说"}, {"4282640", "少女萝莉"}, {"4292640", "角色剧情"},
	{"4322650", "精品网红"}, {"4332650", "多人群交"}, {"4342650", "S M 调教"},
	{"4352650", "动漫卡通"}, {"4362650", "变性伪娘"}, {"4372650", "V R 视角"},
	{"4382650", "反差母狗"}, {"4392650", "人妻系列"},
}

var (
	g5YuuhcAssignRe = regexp.MustCompile(`(?s)([A-Za-z0-9_]+)=(\[\[.*?\]\]|"[A-Za-z0-9+/=]+")`)
	g5YuuhcPairRe   = regexp.MustCompile(`\[\s*(\d+)\s*,\s*"([^"]+)"\s*\]`)
	g5YuuhcShareRe  = regexp.MustCompile(`const\s+\w+=\w+\(\w+\((\w+)\)\),\w+=\w+\(\w+\((\w+)\)\)`)
	g5YuuhcIVRe     = regexp.MustCompile(`iv:\w+\(\w+\((\w+)\)\),additionalData:\w+\(\w+\((\w+)\)\)`)
	g5YuuhcCTRe     = regexp.MustCompile(`\},\w+,\w+\(\w+\((\w+)\)\)\)`)
)

// g5YuuhcDecryptPage is py decrypt_page verbatim: unopenable pages are
// returned as-is.
func g5YuuhcDecryptPage(raw string) string {
	if !strings.Contains(raw, "subtle") || !strings.Contains(raw, "decrypt") {
		return raw
	}
	assign := map[string][]byte{}
	for _, m := range g5YuuhcAssignRe.FindAllStringSubmatch(raw, -1) {
		name, val := m[1], m[2]
		if strings.HasPrefix(val, "\"") {
			if dec, err := base64.StdEncoding.DecodeString(strings.Trim(val, "\"")); err == nil {
				assign[name] = dec
			}
			continue
		}
		type part struct {
			idx int
			s   string
		}
		var parts []part
		for _, pm := range g5YuuhcPairRe.FindAllStringSubmatch(val, -1) {
			idx, _ := strconv.Atoi(pm[1])
			parts = append(parts, part{idx, pm[2]})
		}
		sort.Slice(parts, func(i, j int) bool { return parts[i].idx < parts[j].idx })
		var sb strings.Builder
		for _, p := range parts {
			sb.WriteString(p.s)
		}
		if dec, err := base64.StdEncoding.DecodeString(sb.String()); err == nil {
			assign[name] = dec
		}
	}
	tail := raw
	if len(tail) > 6000 {
		tail = tail[len(tail)-6000:]
	}
	mx := g5YuuhcShareRe.FindStringSubmatch(tail)
	mi := g5YuuhcIVRe.FindStringSubmatch(tail)
	mc := g5YuuhcCTRe.FindStringSubmatch(tail)
	if mx == nil || mi == nil || mc == nil {
		return raw
	}
	a, ok1 := assign[mx[1]]
	b, ok2 := assign[mx[2]]
	iv, ok3 := assign[mi[1]]
	aad, ok4 := assign[mi[2]]
	ct, ok5 := assign[mc[1]]
	if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || len(a) != len(b) {
		return raw
	}
	key := make([]byte, len(a))
	for i := range a {
		key[i] = a[i] ^ b[i]
	}
	pt, err := g5GCMOpen(key, iv, aad, ct)
	if err != nil {
		return raw
	}
	return string(pt)
}

var (
	g5YuuhcMu   sync.Mutex
	g5YuuhcHost = g5YuuhcHosts[0]
)

// g5YuuhcUsablePage reports whether a decrypted page can carry site
// content. v5 device hardening: a page that is still the raw WebCrypto
// shell (decrypt failed — the shell's own script contains both markers)
// is NEVER usable. The py adoption rule ("<title>" suffices) let such a
// shell through on egresses whose shell variant our material regexes
// cannot open, after which every list parsed to zero cards and the UI
// showed a fake empty state instead of an error.
func g5YuuhcUsablePage(page string) bool {
	if page == "" {
		return false
	}
	if strings.Contains(page, "subtle") && strings.Contains(page, "decrypt") {
		return false // still the encrypted shell: decrypt failed
	}
	return strings.Contains(page, "video.php") || strings.Contains(page, "list.php") ||
		strings.Contains(page, "<title>") || strings.Contains(page, "提示信息")
}

// g5YuuhcGet mirrors py _get (current host first, a host is adopted
// only when its decrypted page is usable), with v5 device bounds: 10s
// per try, 2 tries per host, 35s wall-clock deadline for the whole
// call. On a device network where the first candidate host blackholes
// (SYN silently dropped), the unbounded py shape (3 hosts x 3 retries
// x 20s) held a single catalog call for up to ~3 minutes and the
// browse chain starved; the adopted host stays sticky once found, so
// the steady-state cost is unchanged.
func g5YuuhcGet(path string) string {
	g5YuuhcMu.Lock()
	cur := g5YuuhcHost
	g5YuuhcMu.Unlock()
	hosts := append([]string{cur}, func() []string {
		rest := []string{}
		for _, h := range g5YuuhcHosts {
			if h != cur {
				rest = append(rest, h)
			}
		}
		return rest
	}()...)
	deadline := time.Now().Add(35 * time.Second)
	for _, host := range hosts {
		for try := 0; try < 2; try++ {
			if time.Now().After(deadline) {
				return ""
			}
			body, code, _, err := httpGetTimeout(host+path, g5ShellHeaders(), 10*time.Second)
			if err != nil || code >= 400 {
				continue
			}
			page := g5YuuhcDecryptPage(string(body))
			if g5YuuhcUsablePage(page) {
				g5YuuhcMu.Lock()
				g5YuuhcHost = host
				g5YuuhcMu.Unlock()
				return page
			}
			break // page fetched but unusable: next host (py behaviour)
		}
	}
	return ""
}

func g5YuuhcCurHost() string {
	g5YuuhcMu.Lock()
	defer g5YuuhcMu.Unlock()
	return g5YuuhcHost
}

var (
	g5YuuhcTitleRe    = regexp.MustCompile(`(?s)<a href="/video\.php\?id=(\d+)"[^>]*>\s*<h3>(.*?)</h3>`)
	g5YuuhcPicRe      = regexp.MustCompile(`(?s)<a href="/video\.php\?id=(\d+)"[^>]*>\s*<img[^>]+?src="([^"]+)"`)
	g5YuuhcH1Re       = regexp.MustCompile(`(?s)<h1 id="titleH">(.*?)</h1>`)
	g5YuuhcTitleTagRe = regexp.MustCompile(`<title>([^<]*)</title>`)
	g5YuuhcM3u8Re     = regexp.MustCompile(`https?://[^\s"']+?\.m3u8[^\s"']*`)
)

// g5YuuhcCards is py parse_cards: titles and covers paired by video id;
// the raw id carries the cover ("{id}|{pic}") as in py, because the
// detail page itself has no cover field.
func g5YuuhcCards(slot, page, catName string) []interface{} {
	titles := map[string]string{}
	for _, m := range g5YuuhcTitleRe.FindAllStringSubmatch(page, -1) {
		if _, ok := titles[m[1]]; !ok {
			titles[m[1]] = cleanText(g5TagRe.ReplaceAllString(m[2], ""))
		}
	}
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range g5YuuhcPicRe.FindAllStringSubmatch(page, -1) {
		vid, pic := m[1], strings.TrimSpace(m[2])
		if seen[vid] {
			continue
		}
		seen[vid] = true
		items = append(items, makeDrama(slot, vid+"|"+pic, titles[vid], pic, "", catName, ""))
	}
	return items
}

func g5YuuhcCatName(id string) string {
	for _, c := range g5YuuhcCats {
		if c.id == id {
			return c.name
		}
	}
	return ""
}

func g5YuuhcCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	tid := category
	if tid == "" {
		tid = g5YuuhcCats[0].id
	}
	body := g5YuuhcGet(fmt.Sprintf("/list.php?id=%s&page=%d", tid, page))
	if body == "" {
		return nil, 0, false, fmt.Errorf("yuuhc catalog empty")
	}
	items := g5YuuhcCards(slot, body, g5YuuhcCatName(tid))
	if len(items) == 0 {
		// v5 honesty: a fetched page that parses to zero cards is a
		// failure (shell variant / parse drift), not a genuinely empty
		// category — every yuuhc category has content on page 1, and
		// later pages are only reached via hasMore. Report an error so
		// the UI shows its error state instead of a fake empty list.
		if page <= 1 {
			return nil, 0, false, fmt.Errorf("yuuhc catalog: no cards parsed (cat %s)", tid)
		}
		return items, 0, false, nil
	}
	pc := 1
	re := regexp.MustCompile(`list\.php\?id=` + regexp.QuoteMeta(tid) + `&page=(\d+)`)
	for _, m := range re.FindAllStringSubmatch(body, -1) {
		if n, _ := strconv.Atoi(m[1]); n > pc {
			pc = n
		}
	}
	return items, pc * 20, page < pc, nil
}

func g5YuuhcDetail(slot, rawID string) (map[string]interface{}, error) {
	vid, pic, _ := strings.Cut(rawID, "|")
	body := g5YuuhcGet("/video.php?id=" + vid)
	if body == "" {
		return nil, fmt.Errorf("yuuhc detail empty")
	}
	title := ""
	if m := g5YuuhcH1Re.FindStringSubmatch(body); m != nil {
		title = cleanText(g5TagRe.ReplaceAllString(m[1], ""))
	}
	if title == "" {
		if m := g5YuuhcTitleTagRe.FindStringSubmatch(body); m != nil {
			title = strings.TrimSpace(m[1])
		} else {
			title = vid
		}
	}
	return makeDrama(slot, rawID, title, pic, "", "欲含春", ""), nil
}

func g5YuuhcResolve(slot, rawID string) (map[string]interface{}, error) {
	vid, _, _ := strings.Cut(rawID, "|")
	body := g5YuuhcGet("/video.php?id=" + vid)
	if body == "" {
		return nil, fmt.Errorf("yuuhc resolve empty")
	}
	m := g5YuuhcM3u8Re.FindString(body)
	if m == "" {
		return nil, fmt.Errorf("yuuhc no m3u8")
	}
	u := html.UnescapeString(m)
	return playResult(u, slot, rawID, g5ShellUA, g5YuuhcCurHost()+"/", "m3u8", "hls"), nil
}

func g5YuuhcSearch(slot, kw string) ([]interface{}, error) {
	q := url.QueryEscape(kw)
	page := g5YuuhcGet("/search.php?content=" + q)
	if page != "" && !strings.Contains(page, "搜索功能暂不可用") {
		if cards := g5YuuhcCards(slot, page, "搜索"); len(cards) > 0 {
			return cards, nil
		}
	}
	// py fallback while site search is disabled: scan category pages
	// for real in-site items (named categories first, then title match).
	k := strings.ToLower(strings.TrimSpace(kw))
	if k == "" {
		return []interface{}{}, nil
	}
	type catRef struct{ id, name string }
	named := []catRef{}
	rest := []catRef{}
	for _, c := range g5YuuhcCats {
		if strings.Contains(strings.ToLower(c.name), k) {
			named = append(named, catRef{c.id, c.name})
		} else {
			rest = append(rest, catRef{c.id, c.name})
		}
	}
	out := []interface{}{}
	seen := map[string]bool{}
	for _, c := range named {
		p := g5YuuhcGet(fmt.Sprintf("/list.php?id=%s&page=1", c.id))
		for _, card := range g5YuuhcCards(slot, p, c.name) {
			if m, _ := card.(map[string]interface{}); m != nil {
				id, _ := m["vod_id"].(string)
				if !seen[id] {
					seen[id] = true
					out = append(out, card)
				}
			}
		}
		if len(out) > 0 {
			if len(out) > 20 {
				out = out[:20]
			}
			return out, nil
		}
	}
	budget := 8
	for _, c := range rest {
		if budget <= 0 || len(out) >= 20 {
			break
		}
		budget--
		p := g5YuuhcGet(fmt.Sprintf("/list.php?id=%s&page=1", c.id))
		if p == "" {
			continue
		}
		for _, card := range g5YuuhcCards(slot, p, c.name) {
			m, _ := card.(map[string]interface{})
			if m == nil {
				continue
			}
			title, _ := m["title"].(string)
			id, _ := m["vod_id"].(string)
			if strings.Contains(strings.ToLower(title), k) && !seen[id] {
				seen[id] = true
				out = append(out, card)
			}
		}
	}
	if len(out) > 20 {
		out = out[:20]
	}
	return out, nil
}

// g5YuuhcCoverFetch serves cover bytes with the Referer the image host
// demands (py appends it as a TVBox tail; here it is a real header).
func g5YuuhcCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpGet(u, map[string]string{
		"User-Agent": g5ShellUA,
		"Referer":    "https://co65a.yuuhc115.sbs/",
	})
	if err != nil || code != 200 {
		return nil, fmt.Errorf("yuuhc cover http %d err %v", code, err)
	}
	return body, nil
}

func g5YuuhcImpl() *siteImpl {
	return &siteImpl{
		Key: "yuuhc", Name: "🔞欲含春", DefaultCat: g5YuuhcCats[0].id, HomeURL: g5YuuhcHosts[0] + "/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range g5YuuhcCats {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog:    g5YuuhcCatalog,
		Detail:     g5YuuhcDetail,
		Resolve:    g5YuuhcResolve,
		Search:     g5YuuhcSearch,
		Headers:    g5ShellHeaders,
		CoverFetch: g5YuuhcCoverFetch,
	}
}

// ================= setang （色堂， Discuz mobile, AES-GCM shell) =================

var g5SetangHosts = []string{
	"https://dt9nm.ssstt121.sbs",
	"https://galaxy.ssstt121.sbs",
}

var g5SetangCats = []struct{ id, name string }{
	{"18", "国产自拍"}, {"19", "亚洲无码"}, {"20", "亚洲有码"},
	{"21", "中文字幕"}, {"22", "欧美专区"}, {"23", "卡通动漫"},
	{"24", "福利写真"}, {"25", "三级经典"}, {"45", "有声小说"},
}

var g5SetangForumNames = []string{
	"国产自拍", "亚洲无码", "亚洲有码", "中文字幕", "欧美专区", "卡通动漫",
	"福利写真", "三级经典", "有声小说",
	"网友自拍", "偷拍街拍", "日韩激情", "欧美色图", "唯美写真",
	"成人动漫", "家庭乱伦", "诱惑人妻", "校园春色", "职场经历",
	"都市激情", "武侠玄幻", "狼友互动",
}

var g5SetangSections = []string{"在线电影", "有声小说", "激情图片", "成人小说", "互动交流"}

var g5SetangPairRe = regexp.MustCompile(`\[\s*(\d+)\s*,\s*"([^"]*)"\s*\]`)
var g5SetangBlobStartRe = regexp.MustCompile(`\[\[\d+\s*,\s*"`)

// g5SetangExtractBlobs is py _extract_blobs: a state machine over
// [[idx,"part"],...] arrays, parts sorted by index and base64-decoded.
func g5SetangExtractBlobs(page string) [][]byte {
	blobs := [][]byte{}
	for _, loc := range g5SetangBlobStartRe.FindAllStringIndex(page, -1) {
		pos := loc[0] + 1
		type part struct {
			idx int
			s   string
		}
		var pairs []part
		ok := true
		for {
			m := g5SetangPairRe.FindStringSubmatchIndex(page[pos:])
			if m == nil || m[0] != 0 {
				ok = false
				break
			}
			idx, _ := strconv.Atoi(page[pos+m[2] : pos+m[3]])
			pairs = append(pairs, part{idx, page[pos+m[4] : pos+m[5]]})
			pos += m[1]
			for pos < len(page) && (page[pos] == ' ' || page[pos] == '\t' || page[pos] == '\r' || page[pos] == '\n') {
				pos++
			}
			if pos < len(page) && page[pos] == ',' {
				pos++
				for pos < len(page) && (page[pos] == ' ' || page[pos] == '\t' || page[pos] == '\r' || page[pos] == '\n') {
					pos++
				}
				continue
			}
			if pos < len(page) && page[pos] == ']' {
				break
			}
			ok = false
			break
		}
		if ok && len(pairs) > 0 {
			sort.Slice(pairs, func(i, j int) bool { return pairs[i].idx < pairs[j].idx })
			var sb strings.Builder
			for _, p := range pairs {
				sb.WriteString(p.s)
			}
			joined := strings.ReplaceAll(sb.String(), `\/`, "/")
			if dec, err := base64.StdEncoding.DecodeString(joined); err == nil {
				blobs = append(blobs, dec)
			}
		}
	}
	return blobs
}

// g5SetangDecryptPage is py decrypt_page: longest blob is the
// ciphertext, all 32B blobs XOR to the key, smalls are tried as IV/AAD.
func g5SetangDecryptPage(page string) string {
	dec, _ := g5SetangDecryptPageOK(page)
	return dec
}

func g5SetangDecryptPageOK(page string) (string, bool) {
	if !strings.Contains(page, "crypto.subtle") {
		return page, true
	}
	blobs := g5SetangExtractBlobs(page)
	if len(blobs) == 0 {
		return page, false
	}
	ctIdx := 0
	for i, b := range blobs {
		if len(b) > len(blobs[ctIdx]) {
			ctIdx = i
		}
	}
	ct := blobs[ctIdx]
	var shares, smalls [][]byte
	for i, b := range blobs {
		if len(b) == 32 {
			shares = append(shares, b)
		} else if i != ctIdx {
			smalls = append(smalls, b)
		}
	}
	if len(shares) < 2 {
		return page, false
	}
	key := make([]byte, 32)
	copy(key, shares[0])
	for _, sh := range shares[1:] {
		for i := range key {
			key[i] ^= sh[i]
		}
	}
	var ivs [][]byte
	for _, b := range smalls {
		if len(b) == 12 {
			ivs = append(ivs, b)
		}
	}
	if len(ivs) == 0 {
		ivs = smalls
	}
	aads := append(append([][]byte{}, smalls...), []byte{})
	for _, iv := range ivs {
		for _, aad := range aads {
			if bytes.Equal(aad, iv) {
				continue
			}
			pt, err := g5GCMOpen(key, iv, aad, ct)
			if err != nil {
				continue
			}
			if txt := string(pt); strings.Contains(txt, "<") {
				return txt, true
			}
		}
	}
	return page, false
}

type g5SetangState struct {
	mu        sync.Mutex
	host      string
	decryptOK bool
	dcache    map[string]*g5SetangThread
	jar       http.CookieJar
	client    *http.Client
	searchID  map[string]string
}

type g5SetangThread struct {
	tid      string
	name     string
	pic      string
	video    string
	episodes []g5SetangEpisode
	artist   string
}

type g5SetangEpisode struct {
	name, url, cover string
}

var g5Setang = &g5SetangState{
	host:     g5SetangHosts[0],
	dcache:   map[string]*g5SetangThread{},
	searchID: map[string]string{},
}

func (s *g5SetangState) sessionClient() *http.Client {
	if s.client == nil {
		jar, _ := cookiejar.New(nil)
		s.jar = jar
		s.client = &http.Client{Timeout: 25 * time.Second, Jar: jar, Transport: sharedTransport}
	}
	return s.client
}

func g5SetangHeaders(host string) map[string]string {
	return map[string]string{
		"User-Agent": g5ShellUA,
		"Referer":    host + "/",
		"Accept":     "text/html,*/*",
	}
}

// fetchLocked mirrors py _fetch: hosts in order, 2 tries each, first
// fetched page wins (decrypt may still fall back to raw, as in py).
func (s *g5SetangState) fetchLocked(path string) (string, error) {
	order := append([]string{s.host}, func() []string {
		rest := []string{}
		for _, h := range g5SetangHosts {
			if h != s.host {
				rest = append(rest, h)
			}
		}
		return rest
	}()...)
	var lastErr error
	for _, host := range order {
		for try := 0; try < 2; try++ {
			body, code, _, err := httpGet(host+path, g5SetangHeaders(host))
			if err != nil || code != 200 {
				if err != nil {
					lastErr = err
				} else {
					lastErr = fmt.Errorf("setang http %d", code)
				}
				continue
			}
			s.host = host
			dec, ok := g5SetangDecryptPageOK(string(body))
			s.decryptOK = ok
			return dec, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", fmt.Errorf("setang fetch failed")
}

func (s *g5SetangState) threadcoverLocked(tid string) string {
	h := g5MD5Hex(tid)
	return fmt.Sprintf("%s/data/attachment/forum/threadcover/%s/%s/%s.jpg", s.host, h[:2], h[2:4], tid)
}

// g5SetangClean mirrors py _clean.
func g5SetangClean(s string) string {
	s = strings.ReplaceAll(s, `\/`, "/")
	s = strings.ReplaceAll(s, "\u00a0", " ")
	s = g5TagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(html.UnescapeString(s))
}

// g5SetangAbsolutize mirrors py _pic's URL completion (the "@Referer"
// tail is served via CoverFetch instead).
func g5SetangAbsolutize(host, u string) string {
	if u == "" {
		return ""
	}
	switch {
	case strings.HasPrefix(u, "//"):
		return "https:" + u
	case strings.HasPrefix(u, "/"):
		return host + u
	case strings.HasPrefix(u, "http"):
		return u
	default:
		return host + "/" + u
	}
}

var (
	// 活站模板已在 extra 页码后追加 &amp;mobile=2 等参数（交付期存档页无此
	// 后缀、原版 py 正则在现行活页同样 0 命中），页码后须容忍任意查询后缀。
	g5SetangListRe       = regexp.MustCompile(`viewthread&amp;tid=(\d+)&amp;extra=page%3D\d+(?:&amp;[^"]*)?"\s+title="([^"]*)"\s+class="z">\s*<img src="([^"]+)"`)
	g5SetangListFBRe     = regexp.MustCompile(`viewthread&amp;tid=(\d+)&amp;extra=page%3D\d+(?:&amp;[^"]*)?"\s+title="([^"]*)"`)
	g5SetangListLooseRe  = regexp.MustCompile(`viewthread&amp;tid=(\d+)&amp;extra=page%3D\d+(?:&amp;[^"]*)?"[^>]*>`)
	g5SetangTitleAttrRe  = regexp.MustCompile(`title="([^"]*)"`)
	g5SetangLooseImgRe   = regexp.MustCompile(`<img[^>]*?(?:src|data-src)="([^"]+)"`)
	g5SetangPageCountRe  = regexp.MustCompile(`/\s*(\d+)\s*页`)
	g5SetangTitleTagRe   = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	g5SetangDPlayerRe    = regexp.MustCompile(`(?s)DPlayer\(\{.*?url:\s*'([^']+)'\s*,\s*type:\s*'hls'(?:\s*,\s*pic:\s*'([^']*)')?`)
	g5SetangAudioBlockRe = regexp.MustCompile(`(?s)audio:\s*\[(.*?)\]\s*\}`)
	g5SetangAudioEpRe    = regexp.MustCompile(`\{([^{}]*)\}`)
	g5SetangFormhashRe   = regexp.MustCompile(`formhash" value="([a-z0-9]+)"`)
	g5SetangSearchIDRe   = regexp.MustCompile(`searchid=(\d+)`)
	g5SetangSearchRowRe  = regexp.MustCompile(`(?s)viewthread&amp;tid=(\d+)&amp;highlight=[^"]*">\s*<span[^>]*>(.*?)</span>`)
)

func g5SetangAudioField(e, key string) string {
	m := regexp.MustCompile(key + `:\s*'([^']*)'`).FindStringSubmatch(e)
	if m == nil {
		return ""
	}
	return m[1]
}

func (s *g5SetangState) parseListLocked(slot, page, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range g5SetangListRe.FindAllStringSubmatch(page, -1) {
		tid := m[1]
		if seen[tid] {
			continue
		}
		seen[tid] = true
		pic := g5SetangAbsolutize(s.host, strings.TrimSpace(m[3]))
		items = append(items, makeDrama(slot, tid, g5SetangClean(m[2]), pic, "", catName, ""))
	}
	if len(items) == 0 {
		for _, m := range g5SetangListFBRe.FindAllStringSubmatch(page, -1) {
			tid := m[1]
			if seen[tid] {
				continue
			}
			seen[tid] = true
			items = append(items, makeDrama(slot, tid, g5SetangClean(m[2]), s.threadcoverLocked(tid), "", catName, ""))
		}
	}
	if len(items) == 0 {
		for _, loc := range g5SetangListLooseRe.FindAllStringSubmatchIndex(page, -1) {
			tid := page[loc[2]:loc[3]]
			if seen[tid] {
				continue
			}
			seen[tid] = true
			tag := page[loc[0]:loc[1]]
			title := tid
			if m := g5SetangTitleAttrRe.FindStringSubmatch(tag); m != nil && m[1] != "" {
				title = g5SetangClean(m[1])
			}
			pic := s.threadcoverLocked(tid)
			rest := page[loc[1]:]
			if len(rest) > 1000 {
				rest = rest[:1000]
			}
			if m := g5SetangLooseImgRe.FindStringSubmatch(rest); m != nil && m[1] != "" {
				pic = g5SetangAbsolutize(s.host, strings.TrimSpace(m[1]))
			}
			items = append(items, makeDrama(slot, tid, title, pic, "", catName, ""))
		}
	}
	return items
}

func g5SetangCatName(fid string) string {
	for _, c := range g5SetangCats {
		if c.id == fid {
			return c.name
		}
	}
	return ""
}

func g5SetangCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	s := g5Setang
	s.mu.Lock()
	defer s.mu.Unlock()
	fid := category
	if fid == "" {
		fid = "18"
	}
	path := fmt.Sprintf("/forum.php?mod=forumdisplay&fid=%s&mobile=2", fid)
	if page > 1 {
		path = fmt.Sprintf("/forum.php?mod=forumdisplay&fid=%s&page=%d&mobile=2", fid, page)
	}
	body, err := s.fetchLocked(path)
	if err != nil {
		return nil, 0, false, err
	}
	items := s.parseListLocked(slot, body, g5SetangCatName(fid))
	if page == 1 && len(items) == 0 {
		if !s.decryptOK {
			return nil, 0, false, fmt.Errorf("setang: shell page not decrypted for fid %s", fid)
		}
		return nil, 0, false, fmt.Errorf("setang: list anchors not matched for fid %s", fid)
	}
	pc := 1
	if m := g5SetangPageCountRe.FindStringSubmatch(body); m != nil {
		pc, _ = strconv.Atoi(m[1])
	} else if len(items) > 0 {
		pc = 9999
	}
	return items, len(items), page < pc, nil
}

// parseThreadLocked mirrors py _parse_thread (with its cache).
func (s *g5SetangState) parseThreadLocked(tid string) (*g5SetangThread, error) {
	if t, ok := s.dcache[tid]; ok {
		return t, nil
	}
	body, err := s.fetchLocked("/forum.php?mod=viewthread&tid=" + tid + "&mobile=2")
	if err != nil {
		return nil, err
	}
	info := &g5SetangThread{tid: tid}
	name := ""
	if m := g5SetangTitleTagRe.FindStringSubmatch(body); m != nil {
		name = m[1]
	}
	name = strings.ReplaceAll(name, "-6996色堂|六九色堂", "")
	for _, sec := range g5SetangSections {
		if strings.HasSuffix(name, "-"+sec) {
			name = name[:len(name)-len(sec)-1]
			break
		}
	}
	for _, fn := range g5SetangForumNames {
		if strings.HasSuffix(name, "-"+fn) {
			name = name[:len(name)-len(fn)-1]
			break
		}
	}
	info.name = strings.Trim(name, " -")
	if dp := g5SetangDPlayerRe.FindStringSubmatch(body); dp != nil {
		info.video = dp[1]
		// py derives the cover from the m3u8 directory (/1.jpg); the
		// DPlayer pic field is captured but unused there too.
		base := dp[1]
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[:i]
		}
		info.pic = base + "/1.jpg"
	} else if block := g5SetangAudioBlockRe.FindStringSubmatch(body); block != nil {
		for _, em := range g5SetangAudioEpRe.FindAllStringSubmatch(block[1], -1) {
			e := em[1]
			u := g5SetangAudioField(e, "url")
			if u == "" {
				continue
			}
			info.episodes = append(info.episodes, g5SetangEpisode{
				name:  g5SetangAudioField(e, "name"),
				url:   u,
				cover: g5SetangAudioField(e, "cover"),
			})
			if ar := g5SetangAudioField(e, "artist"); ar != "" && info.artist == "" {
				info.artist = ar
			}
		}
		if len(info.episodes) > 0 {
			info.pic = info.episodes[0].cover
		}
	}
	if info.pic == "" {
		info.pic = s.threadcoverLocked(tid)
	}
	s.dcache[tid] = info
	return info, nil
}

func g5SetangDetail(slot, rawID string) (map[string]interface{}, error) {
	s := g5Setang
	s.mu.Lock()
	defer s.mu.Unlock()
	tid, _ := splitEpisodeSuffix(rawID)
	// setang episode suffix is "@n" not "#n"; a raw "tid@n" id resolves
	// to its thread here.
	if i := strings.LastIndex(tid, "@"); i > 0 {
		tid = tid[:i]
	}
	info, err := s.parseThreadLocked(tid)
	if err != nil {
		return nil, err
	}
	name := info.name
	if name == "" {
		name = tid
	}
	pic := g5SetangAbsolutize(s.host, info.pic)
	d := makeDrama(slot, tid, cleanText(name), pic, "", "", name)
	if info.artist != "" {
		d["actor"] = info.artist
		d["vod_actor"] = info.artist
	}
	switch {
	case info.video != "":
		g5SetChapters(d, slot, tid, []string{tid}, []string{"正片"})
	case len(info.episodes) > 0:
		epIDs := []string{}
		names := []string{}
		for i, e := range info.episodes {
			epIDs = append(epIDs, fmt.Sprintf("%s@%d", tid, i))
			nm := e.name
			if nm == "" {
				nm = fmt.Sprintf("第%d集", i+1)
			}
			names = append(names, nm)
		}
		g5SetChapters(d, slot, tid, epIDs, names)
	}
	return d, nil
}

func g5SetangResolve(slot, rawID string) (map[string]interface{}, error) {
	s := g5Setang
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := rawID
	idx := 0
	if i := strings.LastIndex(raw, "@"); i > 0 {
		if n, err := strconv.Atoi(raw[i+1:]); err == nil {
			idx = n
			raw = raw[:i]
		}
	}
	tid := raw
	if m := regexp.MustCompile(`tid=(\d+)`).FindStringSubmatch(tid); m != nil {
		tid = m[1]
	} else if m := regexp.MustCompile(`(\d+)`).FindStringSubmatch(tid); m != nil {
		tid = m[1]
	}
	info, err := s.parseThreadLocked(tid)
	if err != nil {
		return nil, err
	}
	u := ""
	format, proto := "m3u8", "hls"
	if info.video != "" {
		u = info.video
	} else if idx >= 0 && idx < len(info.episodes) {
		u = info.episodes[idx].url
		format, proto = "mp3", "file"
	}
	if u == "" {
		return nil, fmt.Errorf("setang no play url")
	}
	return playResult(u, slot, rawID, g5ShellUA, s.host+"/", format, proto), nil
}

// searchPageLocked mirrors py _search_page: cookie-session formhash
// flow; page 1 uses the POST response, later pages the searchid URL.
// The core Search contract is single-page, so callers use page 1.
func (s *g5SetangState) searchPageLocked(key string, pg int) (string, error) {
	client := s.sessionClient()
	host := s.host
	headers := g5SetangHeaders(host)
	do := func(req *http.Request) (string, string, error) {
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		if err != nil {
			return "", "", err
		}
		return string(b), resp.Request.URL.String(), nil
	}
	if pg == 1 || s.searchID[key] == "" {
		req, err := http.NewRequest("GET", host+"/search.php?mod=forum&mobile=2", nil)
		if err != nil {
			return "", err
		}
		raw, _, err := do(req)
		if err != nil {
			return "", err
		}
		page, ok := g5SetangDecryptPageOK(raw)
		s.decryptOK = ok
		formhash := ""
		if m := g5SetangFormhashRe.FindStringSubmatch(page); m != nil {
			formhash = m[1]
		}
		form := url.Values{
			"formhash": {formhash}, "srchtxt": {key}, "searchsubmit": {"true"},
		}
		req2, err := http.NewRequest("POST", host+"/search.php?mod=forum&mobile=2", strings.NewReader(form.Encode()))
		if err != nil {
			return "", err
		}
		req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		raw2, final, err := do(req2)
		if err != nil {
			return "", err
		}
		if m := g5SetangSearchIDRe.FindStringSubmatch(final); m != nil {
			s.searchID[key] = m[1]
		}
		if pg == 1 {
			dec, ok := g5SetangDecryptPageOK(raw2)
			s.decryptOK = ok
			return dec, nil
		}
	}
	sid := s.searchID[key]
	if sid == "" {
		return "", nil
	}
	u := fmt.Sprintf("%s/search.php?mod=forum&searchid=%s&orderby=lastpost&ascdesc=desc&searchsubmit=yes&kw=%s&page=%d&mobile=2",
		host, sid, url.QueryEscape(key), pg)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	raw, _, err := do(req)
	if err != nil {
		return "", err
	}
	dec, ok := g5SetangDecryptPageOK(raw)
	s.decryptOK = ok
	return dec, nil
}

func g5SetangSearch(slot, kw string) ([]interface{}, error) {
	s := g5Setang
	s.mu.Lock()
	defer s.mu.Unlock()
	page, err := s.searchPageLocked(kw, 1)
	if err != nil {
		return nil, err
	}
	if page == "" {
		return nil, fmt.Errorf("setang search empty page")
	}
	items := []interface{}{}
	seen := map[string]bool{}
	rows := 0
	for _, m := range g5SetangSearchRowRe.FindAllStringSubmatch(page, -1) {
		rows++
		tid := m[1]
		if seen[tid] {
			continue
		}
		seen[tid] = true
		info, err := s.parseThreadLocked(tid)
		if err != nil {
			continue
		}
		if info.video == "" && len(info.episodes) == 0 {
			continue // image/text threads are not playable (py behaviour)
		}
		name := info.name
		if name == "" {
			name = g5SetangClean(m[2])
		}
		pic := g5SetangAbsolutize(s.host, info.pic)
		items = append(items, makeDrama(slot, tid, cleanText(name), pic, "", "搜索", ""))
	}
	if rows == 0 {
		if !s.decryptOK {
			return nil, fmt.Errorf("setang: search page not decrypted")
		}
		return nil, fmt.Errorf("setang: search anchors not matched")
	}
	return items, nil
}

// g5SetangCoverFetch serves cover bytes with the site Referer (py
// appends it as a TVBox tail; here it is a real header).
func g5SetangCoverFetch(u string) ([]byte, error) {
	g5Setang.mu.Lock()
	host := g5Setang.host
	g5Setang.mu.Unlock()
	body, code, _, err := httpGet(u, g5SetangHeaders(host))
	if err != nil || code != 200 {
		return nil, fmt.Errorf("setang cover http %d err %v", code, err)
	}
	return body, nil
}

func g5SetangImpl() *siteImpl {
	return &siteImpl{
		Key: "setang", Name: "🔞6996色堂", DefaultCat: "18", HomeURL: g5SetangHosts[0] + "/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range g5SetangCats {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog:    g5SetangCatalog,
		Detail:     g5SetangDetail,
		Resolve:    g5SetangResolve,
		Search:     g5SetangSearch,
		Headers:    func() map[string]string { return g5SetangHeaders(g5SetangHosts[0]) },
		CoverFetch: g5SetangCoverFetch,
	}
}
