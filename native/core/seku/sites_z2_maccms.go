package seku

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
)

// ---- Batch2 MacCMS-template HTML sites (ZakaSec-restored recipes) ----

var z2RipCats = []htmlCat{
	{ID: "1", Name: "日韩"},
	{ID: "2", Name: "国产"},
	{ID: "5", Name: "网曝门"},
	{ID: "6", Name: "人妻熟女"},
	{ID: "7", Name: "日本无码"},
	{ID: "8", Name: "美乳巨乳"},
	{ID: "9", Name: "强制侵犯"},
	{ID: "10", Name: "制服诱惑"},
	{ID: "11", Name: "绝色佳人"},
	{ID: "12", Name: "风俗泡泡浴"},
	{ID: "20", Name: "家庭乱伦"},
	{ID: "21", Name: "少女萝莉"},
	{ID: "22", Name: "SM调教"},
	{ID: "23", Name: "绝顶潮吹"},
	{ID: "24", Name: "魔镜系列"},
	{ID: "25", Name: "时间停止"},
	{ID: "26", Name: "催眠洗脑"},
	{ID: "27", Name: "漫改系列"},
	{ID: "28", Name: "电车痴汉"},
	{ID: "29", Name: "淫欲痴女"},
	{ID: "33", Name: "OnlyFans"},
	{ID: "34", Name: "AV解说"},
}

var z2RipProfile = &htmlProfile{
	Base:      "https://17rip.com",
	UA:        z2UA,
	DetailURL: "/voddetail/{id}.html",
	Cats:      z2RipCats,
	ItemRe:    regexp.MustCompile(`href="/voddetail/(?P<id>\d+)\.html" title="(?P<title>[^"]*)"[^>]*>[\s\S]{0,300}?data-src="(?P<cover>[^"]+)"`),
	TitleRe:   regexp.MustCompile(`<em class="cor4">片名：</em><span>([^<]+)</span>`),
	CoverRe:   regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	DescRe:    regexp.MustCompile(`(?s)<div class="[^"]*(?:vod-content|content-desc|detail-desc)[^"]*"[^>]*>(.{1,1000}?)</div>`),
	SearchURL: "/vodsearch/{kw}-------------.html",
}

var (
	z2RipPlayLinkRe = regexp.MustCompile(`href="(/vodplay/[^"]+)"[^>]*>([^<]{0,40})</a>`)
	z2RipM3u8Re     = regexp.MustCompile(`"url"\s*:\s*"(https?:\\?/\\?/[^"]+?\.m3u8[^"]*)"`)
)

func z2RipListURL(cat string, page int) string {
	if page <= 1 {
		return "https://17rip.com/vodshow/" + cat + "-----------.html"
	}
	return fmt.Sprintf("https://17rip.com/vodshow/%s--------%d---.html", cat, page)
}

func z2RipResolve(slot, rawID string) (map[string]interface{}, error) {
	p := z2RipProfile
	detailURL := p.Base + "/voddetail/" + rawID + ".html"
	body, code, _, err := httpGet(detailURL, p.headers())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("z2_17rip detail http %d err %v", code, err)
	}
	m := z2RipPlayLinkRe.FindStringSubmatch(string(body))
	if m == nil {
		return nil, fmt.Errorf("z2_17rip no play link")
	}
	playURL := p.Base + m[1]
	body2, code2, _, err2 := httpGet(playURL, p.headers())
	if err2 != nil || code2 != 200 {
		return nil, fmt.Errorf("z2_17rip play http %d err %v", code2, err2)
	}
	mm := z2RipM3u8Re.FindStringSubmatch(string(body2))
	if mm == nil {
		return nil, fmt.Errorf("z2_17rip no m3u8 on play page")
	}
	u := strings.ReplaceAll(mm[1], `\/`, "/")
	return playResult(u, slot, rawID, p.UA, playURL, "m3u8", "hls"), nil
}

// ---- 321AV (Zaka321avAmns) https://321av.net ----
var z2Av321Cats = []htmlCat{
	{ID: "enter", Name: "推荐"},
	{ID: "20", Name: "有码影片"},
	{ID: "21", Name: "无码影片"},
	{ID: "22", Name: "中文字幕"},
	{ID: "actor", Name: "演员库"},
}

var z2Av321Profile = &htmlProfile{
	Base:      "https://321av.net",
	UA:        z2UA,
	DetailURL: "/vodplay/{id}-1-1.html",
	Cats:      z2Av321Cats,
	ItemRe:    regexp.MustCompile(`href="/vodplay/(?P<id>\d+)-1-1\.html"[^>]*>[\s\S]{0,300}?data-src="(?P<cover>[^"]+)"[^>]*alt="(?P<title>[^"]*)"`),
	TitleRe:   regexp.MustCompile(`<meta[^>]*property="og:title"[^>]*content="([^"]+)"`),
	CoverRe:   regexp.MustCompile(`<meta[^>]*property="og:image"[^>]*content="([^"]+)"`),
	DescRe:    regexp.MustCompile(`<meta[^>]*property="og:description"[^>]*content="([^"]*)"`),
	SearchURL: "/index.php/ajax/suggest?mid=1&wd={kw}",
	Fetch:     z2Av321Fetch,
}

var z2Av321Hosts = []string{"https://321av.net", "https://321av.ws"}

var (
	z2Av321HostMu sync.Mutex
	z2Av321Host   = z2Av321Hosts[0]
)

func z2Av321LooksOk(body string) bool {
	if len(body) < 3000 {
		return false
	}
	lower := strings.ToLower(body)
	if strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf_chl") ||
		strings.Contains(lower, "challenge-form") ||
		strings.Contains(lower, "attention required") ||
		strings.Contains(lower, "<title>system error") {
		return false
	}
	return strings.Contains(lower, "vodplay/") ||
		strings.Contains(lower, "actordetail-") ||
		strings.Contains(lower, "thumbnail group")
}

func z2Av321FetchPath(path string, accept func(string) bool) (string, string, error) {
	if accept == nil {
		accept = z2Av321LooksOk
	}
	z2Av321HostMu.Lock()
	sticky := z2Av321Host
	z2Av321HostMu.Unlock()
	ordered := []string{sticky}
	for _, h := range z2Av321Hosts {
		if h != sticky {
			ordered = append(ordered, h)
		}
	}
	var lastErr error
	for _, host := range ordered {
		hdr := z2Av321Profile.headers()
		hdr["Referer"] = host + "/enter"
		body, code, _, err := httpGet(host+path, hdr)
		if err != nil {
			lastErr = err
			continue
		}
		if code != 200 {
			lastErr = fmt.Errorf("z2_321av fetch http %d", code)
			continue
		}
		if accept(string(body)) {
			z2Av321HostMu.Lock()
			z2Av321Host = host
			z2Av321HostMu.Unlock()
			return string(body), host, nil
		}
		lastErr = fmt.Errorf("z2_321av challenge page on %s", host)
	}
	if lastErr != nil {
		return "", "", lastErr
	}
	return "", "", fmt.Errorf("z2_321av fetch failed for %s", path)
}

var z2Av321ActorLinkRe = regexp.MustCompile(`<a href="/actordetail-(\d+)\.html"`)
var z2Av321ActorImgRe = regexp.MustCompile(`data-src="([^"]+)"[^>]*?alt="([^"]*)"`)

func z2Av321Actors(slot, html string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range z2Av321ActorLinkRe.FindAllStringSubmatchIndex(html, -1) {
		id := html[m[2]:m[3]]
		if seen[id] {
			continue
		}
		seen[id] = true
		end := m[1] + 700
		if end > len(html) {
			end = len(html)
		}
		win := html[m[1]:end]
		cover, name := "", ""
		if im := z2Av321ActorImgRe.FindStringSubmatch(win); im != nil {
			cover, name = im[1], cleanText(im[2])
		}
		if name == "" {
			continue
		}
		items = append(items, makeDrama(slot, "actor:"+id, name, z2Av321Profile.abs("", cover), "演员", "", ""))
	}
	return items
}

func z2Av321Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" && len(z2Av321Cats) > 0 {
		category = z2Av321Cats[0].ID
	}
	if page < 1 {
		page = 1
	}
	if category == "actor" {
		path := "/actor.html"
		if page > 1 {
			path = fmt.Sprintf("/actor-%d.html", page)
		}
		html, _, err := z2Av321FetchPath(path, nil)
		if err != nil {
			return nil, 0, false, err
		}
		items := z2Av321Actors(slot, html)
		return items, page * 24, len(items) > 0, nil
	}
	var path string
	switch category {
	case "enter":
		if page <= 1 {
			path = "/enter"
		} else {
			path = fmt.Sprintf("/enter-%d.html", page)
		}
	default:
		if page <= 1 {
			path = "/vodtype/" + category + ".html"
		} else {
			path = fmt.Sprintf("/vodtype/%s-%d.html", category, page)
		}
	}
	html, _, err := z2Av321FetchPath(path, nil)
	if err != nil {
		return nil, 0, false, err
	}
	items := z2Av321Profile.parseItems(slot, html, z2Av321Profile.catName(category), nil)
	return items, page * 24, len(items) > 0, nil
}

func z2Av321Search(slot, kw string) ([]interface{}, error) {
	z2Av321HostMu.Lock()
	sticky := z2Av321Host
	z2Av321HostMu.Unlock()
	ordered := []string{sticky}
	for _, h := range z2Av321Hosts {
		if h != sticky {
			ordered = append(ordered, h)
		}
	}
	var lastErr error
	for _, host := range ordered {
		hdr := z2Av321Profile.headers()
		hdr["Referer"] = host + "/enter"
		body, code, _, err := httpGet(host+"/index.php/ajax/suggest?mid=1&wd="+url.QueryEscape(kw), hdr)
		if err != nil || code != 200 {
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("z2_321av search http %d", code)
			}
			continue
		}
		var root struct {
			List []map[string]interface{} `json:"list"`
		}
		if err := json.Unmarshal(body, &root); err != nil {
			lastErr = err
			continue
		}
		z2Av321HostMu.Lock()
		z2Av321Host = host
		z2Av321HostMu.Unlock()
		items := []interface{}{}
		seen := map[string]bool{}
		for _, e := range root.List {
			id := g5StrVal(e["id"])
			name := cleanText(g5StrVal(e["name"]))
			if len(id) < 3 || name == "" || seen[id] {
				continue
			}
			seen[id] = true
			items = append(items, makeDrama(slot, id, name, z2Av321Profile.abs("", g5StrVal(e["pic"])), "搜索", "", ""))
		}
		return items, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("z2_321av search failed")
}

func z2Av321Fetch(rawurl string, headers map[string]string, _ string) ([]byte, int, error) {
	base := z2Av321Hosts[0]
	for _, host := range z2Av321Hosts {
		u := rawurl
		if strings.HasPrefix(u, base) && host != base {
			u = host + strings.TrimPrefix(u, base)
		}
		body := zkShieldRawGet(u, headers, host+"/enter", 20000)
		if z2Av321LooksOk(body) {
			return []byte(body), 200, nil
		}
	}
	return nil, 0, fmt.Errorf("z2_321av all hosts failed looksOk")
}

var z2Av321PlayerRe = regexp.MustCompile(`(?s)var\s+player_aaaa\s*=\s*(\{.*?\})\s*</script>`)

func z2Av321B64Decode(input string) []byte {
	cleaned := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, input)
	if idx := strings.IndexByte(cleaned, '='); idx >= 0 {
		cleaned = cleaned[:idx]
	}
	for _, enc := range []*base64.Encoding{base64.URLEncoding, base64.RawURLEncoding, base64.StdEncoding, base64.RawStdEncoding} {
		if out, err := enc.DecodeString(cleaned); err == nil {
			return out
		}
	}
	return nil
}

func z2Av321TailSegment(path string) string {
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(parts[i])
		if seg == "" || seg == "cn" {
			continue
		}
		// 播放页里那段 base64 有时只解出半截 JSON，例如
		// [["标清","https://321av.net/cn/fntr-004"]]}
		// 直接把最后一段当 slug 会把 "]]} 之类的残渣拼进请求路径，
		// 站点回 404，表现就是「有目录、点进去播不了」。
		// slug 只可能是字母数字加 - _ .，截到第一个非法字符为止。
		if cut := strings.IndexFunc(seg, func(r rune) bool {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
				return false
			case r == '-', r == '_', r == '.':
				return false
			}
			return true
		}); cut >= 0 {
			seg = seg[:cut]
		}
		seg = strings.TrimSuffix(strings.TrimSuffix(seg, ".html"), ".")
		if seg != "" {
			return seg
		}
	}
	return ""
}

func z2Av321SlugFromDecoded(decoded string) string {
	trimmed := strings.TrimSpace(decoded)
	if strings.HasPrefix(trimmed, "{") {
		var root struct {
			SS [][]string `json:"ss"`
		}
		if err := json.Unmarshal([]byte(trimmed), &root); err == nil {
			for _, row := range root.SS {
				for _, item := range row {
					item = strings.TrimSpace(item)
					if idx := strings.IndexByte(item, '?'); idx >= 0 {
						item = item[:idx]
					}
					if idx := strings.IndexByte(item, '#'); idx >= 0 {
						item = item[:idx]
					}
					if strings.Contains(item, "/cn/") {
						if slug := z2Av321TailSegment(item); slug != "" {
							return slug
						}
					}
				}
			}
		}
	}
	if strings.Contains(trimmed, "/cn/") {
		return z2Av321TailSegment(trimmed)
	}
	return ""
}

func z2Av321PlayerJSON(html string) string {
	if m := z2Av321PlayerRe.FindStringSubmatch(html); m != nil {
		return m[1]
	}
	idx := strings.Index(html, "player_aaaa")
	if idx < 0 {
		return ""
	}
	rest := html[idx:]
	open := strings.IndexByte(rest, '{')
	if open < 0 {
		return ""
	}
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(rest); i++ {
		c := rest[i]
		if inString {
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return rest[open : i+1]
			}
		}
	}
	return ""
}

func z2Av321SlugFromDetail(html string) (string, error) {
	raw := z2Av321PlayerJSON(html)
	if raw == "" {
		return "", fmt.Errorf("z2_321av no player_aaaa on detail page")
	}
	var obj struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil || obj.URL == "" {
		return "", fmt.Errorf("z2_321av player_aaaa has no url")
	}
	encoded := strings.TrimSpace(obj.URL)
	if strings.Contains(encoded, "%") {
		if decoded, err := url.QueryUnescape(encoded); err == nil {
			encoded = decoded
		}
	}
	if len(encoded) < 8 {
		return "", fmt.Errorf("z2_321av player url too short")
	}
	decoded := z2Av321B64Decode(encoded)
	if decoded == nil {
		return "", fmt.Errorf("z2_321av player url b64 decode failed")
	}
	slug := z2Av321SlugFromDecoded(string(decoded))
	if slug == "" {
		return "", fmt.Errorf("z2_321av no slug in player url")
	}
	return slug, nil
}

func z2Av321GetVideo(slug string) ([]string, error) {
	p := z2Av321Profile
	z2Av321HostMu.Lock()
	sticky := z2Av321Host
	z2Av321HostMu.Unlock()
	ordered := []string{sticky}
	for _, h := range z2Av321Hosts {
		if h != sticky {
			ordered = append(ordered, h)
		}
	}
	var lastErr error
	for _, host := range ordered {
		hdr := p.headers()
		hdr["Referer"] = host + "/enter"
		body, code, _, err := httpGet(host+"/private-getvideo/"+url.PathEscape(slug), hdr)
		if err != nil || code != 200 {
			if err != nil {
				lastErr = err
			} else {
				lastErr = fmt.Errorf("z2_321av getvideo http %d", code)
			}
			continue
		}
		var root struct {
			Playlist []map[string]interface{} `json:"playlist"`
		}
		if err := json.Unmarshal(body, &root); err != nil {
			lastErr = fmt.Errorf("z2_321av getvideo json: %v", err)
			continue
		}
		z2Av321HostMu.Lock()
		z2Av321Host = host
		z2Av321HostMu.Unlock()
		entries := z2Av321ParsePlaylist(root.Playlist)
		return z2Av321OrderCandidates(entries), nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("z2_321av getvideo failed")
}

type z2Av321Candidate struct {
	url     string
	urlType string
	urlSign string
}

func z2Av321ParsePlaylist(nodes []map[string]interface{}) []z2Av321Candidate {
	var entries []z2Av321Candidate
	for _, node := range nodes {
		u, _ := node["url"].(string)
		if !strings.HasPrefix(u, "http") {
			continue
		}
		urlType, _ := node["urlType"].(string)
		if urlType == "" {
			urlType, _ = node["url_type"].(string)
		}
		urlSign, _ := node["urlSign"].(string)
		if urlSign == "" {
			urlSign, _ = node["url_sign"].(string)
		}
		entries = append(entries, z2Av321Candidate{u, urlType, urlSign})
	}
	return entries
}

func z2Av321OrderCandidates(entries []z2Av321Candidate) []string {
	var quality, unsigned string
	for _, e := range entries {
		if quality == "" && strings.EqualFold(e.urlType, "QualityUrls") {
			quality = e.url
		}
		if unsigned == "" && e.urlSign == "" {
			unsigned = e.url
		}
	}
	ordered := []string{}
	push := func(u string) {
		if u == "" {
			return
		}
		for _, v := range ordered {
			if v == u {
				return
			}
		}
		ordered = append(ordered, u)
	}
	push(quality)
	push(unsigned)
	for _, e := range entries {
		push(e.url)
	}
	return ordered
}

func z2Av321PickLive(candidates []string, referer string) (string, error) {
	tried := 0
	for _, candidate := range candidates {
		if tried >= 4 {
			break
		}
		tried++
		if probePlaylist(candidate, referer) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("z2_321av no live playlist")
}

func z2Av321Resolve(slot, rawID string) (map[string]interface{}, error) {
	p := z2Av321Profile
	body, host, err := z2Av321FetchPath("/vodplay/"+rawID+"-1-1.html", func(b string) bool {
		return z2Av321LooksOk(b) || strings.Contains(b, "player_aaaa")
	})
	if err != nil {
		return nil, err
	}
	slug, err := z2Av321SlugFromDetail(body)
	if err != nil {
		return nil, err
	}
	candidates, err := z2Av321GetVideo(slug)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("z2_321av empty playlist")
	}
	chosen, err := z2Av321PickLive(candidates, host+"/")
	if err != nil {
		return nil, err
	}
	StoreStreamHook(chosen, &av321StreamHook{slug: slug, referer: host + "/", tried: map[string]bool{chosen: true}})
	return playResult(chosen, slot, rawID, p.UA, host+"/", "m3u8", "hls"), nil
}

func init() {
	rip := htmlProfileImpl("z2_17rip", "17rip", z2RipProfile)
	rip.Catalog = z2PagedCatalog(z2RipProfile, "z2_17rip", func(cat string, page int) string {
		return z2RipListURL(cat, page)
	})
	rip.Resolve = z2RipResolve
	registerSite("z2_17rip", rip, "17rip")

	a321 := htmlProfileImpl("z2_321av", "321AV", z2Av321Profile)
	a321.Catalog = z2Av321Catalog
	a321.Search = z2Av321Search
	genericDetail := a321.Detail
	a321.Detail = func(slot, rawID string) (map[string]interface{}, error) {
		if !strings.HasPrefix(rawID, "actor:") {
			return genericDetail(slot, rawID)
		}
		id := strings.TrimPrefix(rawID, "actor:")
		html, _, err := z2Av321FetchPath("/actordetail-"+id+".html", nil)
		if err != nil {
			return nil, err
		}
		works := z2Av321Profile.parseItems(slot, html, "演员作品", nil)
		if len(works) == 0 {
			return nil, fmt.Errorf("z2_321av actor %s no works", id)
		}
		d := makeDrama(slot, rawID, "演员作品", "", "", "", "")
		epIDs, names := []string{}, []string{}
		for _, w := range works {
			if m, ok := w.(map[string]interface{}); ok {
				if vodID := g5StrVal(m["vod_id"]); vodID != "" {
					epIDs = append(epIDs, vodID)
					names = append(names, g5StrVal(m["title"]))
				}
			}
		}
		if len(epIDs) == 0 {
			return nil, fmt.Errorf("z2_321av actor %s no works", id)
		}
		g5SetChapters(d, slot, rawID, epIDs, names)
		return d, nil
	}
	a321.Resolve = z2Av321Resolve
	registerSite("z2_321av", a321, "321av")
}
