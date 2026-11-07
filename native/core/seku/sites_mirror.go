package seku

// ---- MissAV 镜像·免代理 (thisav.my -> missav06.net) — hongguo, v4 ----
//
// Ported from the user's ZakaTV jar class ZakaMirrorAmns (config key
// ZakaMirror "🔞MissAV镜像┃免代理", ext host https://www.thisav.my).
// User order 2026-10-23 17:48: hongguo tries this mirror first — same
// MissAV catalogue as the (CF-blocked) official site, but reachable
// without a proxy; 123AV (sites_123av.go) is the documented fallback
// and stays in the tree unregistered.
//
// Site mechanics (live-verified 2026-10-23):
//   - www.thisav.my 301-redirects to a rotating mirror host
//     (missav06.net today); list paths gain a per-section /dmNNN/
//     prefix through the redirect, detail paths stay /cn/<code>, and
//     ?page=N survives. The fetch layer therefore follows redirects
//     and re-learns the current host from the final URL.
//   - Pages are the MissAV frontend verbatim: thumbnail list cards,
//     name-slug actress/genre/maker index pages, packed-JS player page
//     whose token stream yields the surrit playlist (the shared
//     missavExtractM3u8 ports cleanly).
//   - No Cloudflare challenge was seen from this egress on any path.

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const mirrorSeedHost = "https://www.thisav.my"

// mirrorPlayReferer: the surrit media CDN whitelists the canonical
// MissAV origin as Referer and 403s the rotating mirror hosts
// (live-verified 2026-10-23: Referer https://www.missav06.net/ -> 403,
// Referer https://missav.ws/ -> 200, same UA). Playback headers must
// therefore use missav.ws, NOT the learned mirror host.
const mirrorPlayReferer = "https://missav.ws/"

const mirrorUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var mirrorState = &struct {
	mu    sync.Mutex
	host  string
	cache map[string]whosPageEntry
}{host: mirrorSeedHost, cache: map[string]whosPageEntry{}}

var mirrorClient = &http.Client{Timeout: 25 * time.Second, Transport: sharedTransport}

func mirrorHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      mirrorUA,
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.6",
		"Referer":         mirrorSeedHost + "/",
	}
}

func mirrorHost() string {
	mirrorState.mu.Lock()
	defer mirrorState.mu.Unlock()
	return mirrorState.host
}

// mirrorFetch GETs host+path following redirects, learns the live
// mirror host from the final URL, and caches pages for 120s.
func mirrorFetch(path string) (string, error) {
	mirrorState.mu.Lock()
	if e, ok := mirrorState.cache[path]; ok && time.Since(e.at) < 120*time.Second {
		t := e.text
		mirrorState.mu.Unlock()
		return t, nil
	}
	mirrorState.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		host := mirrorHost()
		req, err := http.NewRequest("GET", host+path, nil)
		if err != nil {
			return "", err
		}
		for k, v := range mirrorHeaders() {
			req.Header.Set(k, v)
		}
		resp, err := mirrorClient.Do(req)
		if err == nil {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
			resp.Body.Close()
			if rerr == nil && resp.StatusCode == 200 && !isChallengePage(string(body)) {
				text := string(body)
				if resp.Request != nil && resp.Request.URL != nil {
					final := resp.Request.URL.Scheme + "://" + resp.Request.URL.Host
					if final != host {
						mirrorState.mu.Lock()
						mirrorState.host = final
						mirrorState.mu.Unlock()
					}
				}
				mirrorState.mu.Lock()
				mirrorState.cache[path] = whosPageEntry{at: time.Now(), text: text}
				mirrorState.mu.Unlock()
				return text, nil
			}
			if rerr != nil {
				lastErr = rerr
			} else if resp.StatusCode != 200 {
				lastErr = fmt.Errorf("mirror fetch %s -> %d", path, resp.StatusCode)
			} else {
				lastErr = fmt.Errorf("mirror fetch %s: challenge page", path)
			}
		} else {
			lastErr = err
		}
		if tb, code, _, terr := tlsGet(host+path, mirrorHeaders()); terr == nil && code == 200 && !isChallengePage(string(tb)) {
			text := string(tb)
			mirrorState.mu.Lock()
			mirrorState.cache[path] = whosPageEntry{at: time.Now(), text: text}
			mirrorState.mu.Unlock()
			return text, nil
		} else if terr != nil {
			lastErr = terr
		}
	}
	return "", lastErr
}

// ---- categories: the jar CLASSES table (34) + index pages ----

var mirrorCats = []struct{ id, name string }{
	{"chinese-subtitle", "中文字幕"},
	{"new", "最近更新"},
	{"release", "新作上市"},
	{"uncensored-leak", "无码流出"},
	{"actresses", "女优一览"},
	{"actresses_ranking", "女优排行"},
	{"genres", "全部类型"},
	{"makers", "全部发行商"},
	{"genres/VR", "VR专区"},
	{"today-hot", "今日热门"},
	{"weekly-hot", "本周热门"},
	{"monthly-hot", "本月热门"},
	{"fc2", "FC2"},
	{"heyzo", "HEYZO"},
	{"tokyohot", "东京热"},
	{"1pondo", "一本道"},
	{"caribbeancom", "加勒比"},
	{"caribbeancompr", "加勒比PR"},
	{"10musume", "天然素人"},
	{"pacopacomama", "熟女俱乐部"},
	{"gachinco", "Gachinco"},
	{"siro", "SIRO"},
	{"luxu", "LUXU"},
	{"gana", "GANA"},
	{"scute", "S-CUTE"},
	{"maan", "PRESTIGE"},
	{"ara", "ARA"},
	{"naughty4610", "顽皮4610"},
	{"naughty0930", "顽皮0930"},
	{"madou", "麻豆传媒"},
	{"twav", "TWAV"},
	{"furuke", "Furuke"},
	{"marriedslash", "人妻斩"},
	{"xxxav", "XXX-AV"},
}

var mirrorIndexCats = map[string]bool{
	"actresses": true, "genres": true, "makers": true,
}

func mirrorCatName(id string) string {
	for _, c := range mirrorCats {
		if c.id == id {
			return c.name
		}
	}
	return "MissAV镜像"
}

func mirrorCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range mirrorCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

// ---- list parsing (jar anchors; rawID = the video code) ----

func mirrorParseList(page string) []g2Work {
	cards := map[string]*g2Work{}
	order := []string{}
	for _, m := range missavListRe.FindAllStringSubmatch(page, -1) {
		code := strings.ToLower(m[2])
		body := m[3]
		if missavReserved[code] || !missavCodeRe.MatchString(code) {
			continue
		}
		c, ok := cards[code]
		if !ok {
			c = &g2Work{RawID: code}
			cards[code] = c
			order = append(order, code)
		}
		if pic := missavPickImage(body); pic != "" && c.Cover == "" {
			c.Cover = pic
		}
		if dm := missavDurRe.FindStringSubmatch(body); dm != nil && c.Remark == "" {
			c.Remark = dm[1]
		}
		if title := g2TxtOf(body); len([]rune(title)) > len([]rune(c.Title)) {
			c.Title = title
		}
	}
	out := []g2Work{}
	for _, code := range order {
		w := *cards[code]
		if w.Title == "" {
			w.Title = strings.ToUpper(code)
		}
		w.Title = g2Trunc(w.Title, 120)
		out = append(out, w)
	}
	return out
}

// ---- catalog ----

func mirrorCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "release"
	}
	if page < 1 {
		page = 1
	}
	if strings.HasPrefix(category, "idx:") {
		category = strings.TrimPrefix(category, "idx:")
	}
	// folder drill: category carries an index path ("/cn/actresses/<slug>")
	if strings.HasPrefix(category, "/cn/") {
		text, err := mirrorFetch(fmt.Sprintf("%s?page=%d", category, page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		works := mirrorParseList(text)
		return g2WorksToItems(slot, works, "索引"), len(works), len(works) > 0, nil
	}
	if mirrorIndexCats[category] {
		text, err := mirrorFetch(fmt.Sprintf("/cn/%s?page=%d", category, page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		entries := missavParseIndex(text, category)
		items := []interface{}{}
		for _, w := range entries {
			d := makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, mirrorCatName(category), "")
			d["category"] = w.RawID
			items = append(items, d)
		}
		return items, len(items), len(items) > 0, nil
	}
	if category == "actresses_ranking" {
		text, err := mirrorFetch(fmt.Sprintf("/cn/actresses/ranking?page=%d", page))
		if err != nil {
			return []interface{}{}, 0, false, err
		}
		entries := missavParseIndex(text, "actresses")
		items := []interface{}{}
		for _, w := range entries {
			d := makeDrama(slot, "idx:"+w.RawID, w.Title, w.Cover, w.Remark, "女优排行", "")
			d["category"] = w.RawID
			items = append(items, d)
		}
		return items, len(items), len(items) > 0, nil
	}
	text, err := mirrorFetch(fmt.Sprintf("/cn/%s?page=%d", category, page))
	if err != nil {
		return []interface{}{}, 0, false, err
	}
	works := mirrorParseList(text)
	return g2WorksToItems(slot, works, mirrorCatName(category)), len(works), len(works) > 0, nil
}

// ---- detail / resolve ----

var mirrorPlayCache = &struct {
	mu sync.Mutex
	m  map[string]string
}{m: map[string]string{}}

func mirrorDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		path := strings.TrimPrefix(rawID, "idx:")
		// v7 (hotfix, user device report on v6): actress folders go
		// through the SAME embed path as genre/maker folders below.
		// v6 returned a zero-episode folder detail whose "category"
		// carried the drill id, expecting the app to navigate there;
		// the Dart app has no such dispatch — a card tap goes
		// detail -> episodes, and zero episodes ends the tap with
		// "暂时没有可播放的集数". Embedding the works as titled
		// episodes is the only tappable drill this app has.
		// v5: the folder detail used to embed page 1 only (12 works),
		// which reads as "女优集数太少" (user report) even though the
		// catalog drill pages the actress's FULL list (verified:
		// 波多野結衣 mirror-side 285 pages / ~3,415 works, core paging
		// follows hasMore without truncation). Embed the first 5
		// pages (up to 60 works) so the detail view is representative.
		var works []g2Work
		seen := map[string]bool{}
		for pg := 1; pg <= 5; pg++ {
			u := path
			if pg > 1 {
				u = fmt.Sprintf("%s?page=%d", path, pg)
			}
			text, err := mirrorFetch(u)
			if err != nil {
				if pg == 1 {
					return nil, err
				}
				break
			}
			pageWorks := mirrorParseList(text)
			for _, w := range pageWorks {
				if !seen[w.RawID] {
					seen[w.RawID] = true
					works = append(works, w)
				}
			}
			if len(pageWorks) < 12 {
				break
			}
		}
		if len(works) == 0 {
			return nil, fmt.Errorf("mirror index folder empty: %s", path)
		}
		name := path
		if mm := missavIndexIDRe.FindStringSubmatch(path); mm != nil {
			if un, err := url.PathUnescape(mm[2]); err == nil {
				name = un
			}
		}
		return g2WorksDetail(slot, rawID, name, works[0].Cover,
			fmt.Sprintf("%d 部作品（前5页）", len(works)), "索引", works,
			fmt.Sprintf("已载前 5 页共 %d 部作品，选集即作品列表（集名=片名，点开即播）", len(works))), nil
	}
	page, err := mirrorFetch("/cn/" + rawID)
	if err != nil {
		return nil, err
	}
	title := ""
	if m := missavOgTitleRe.FindStringSubmatch(page); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	if title == "" {
		if m := missavH1Re.FindStringSubmatch(page); m != nil {
			title = g2TxtOf(m[1])
		}
	}
	if title == "" {
		title = strings.ToUpper(rawID)
	}
	pic := ""
	if m := missavOgImageRe.FindStringSubmatch(page); m != nil {
		pic = m[1]
	}
	tags := ""
	if m := missavKwRe.FindStringSubmatch(page); m != nil {
		tags = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	year := ""
	if m := missavRelDateRe.FindStringSubmatch(page); m != nil && len(m[1]) >= 4 {
		year = m[1][:4]
	}
	desc := tags
	if year != "" {
		desc = year + " · " + desc
	}
	remark := "网页播放"
	if found := missavExtractM3u8(page); found != "" {
		remark = "正片"
		mirrorPlayCache.mu.Lock()
		mirrorPlayCache.m[rawID] = found
		mirrorPlayCache.mu.Unlock()
	}
	d := makeDrama(slot, rawID, title, pic, remark, "MissAV镜像", desc)
	chs := []interface{}{g2Chapter(slot, rawID, rawID, "正片", 1)}
	g2SetChapters(d, chs)
	return d, nil
}

func mirrorResolve(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "idx:") {
		return nil, fmt.Errorf("mirror: folder has no play url")
	}
	mirrorPlayCache.mu.Lock()
	cached := mirrorPlayCache.m[rawID]
	mirrorPlayCache.mu.Unlock()
	if cached != "" {
		return playResult(cached, slot, rawID, mirrorUA, mirrorPlayReferer, "m3u8", "hls"), nil
	}
	page, err := mirrorFetch("/cn/" + rawID)
	if err != nil {
		return nil, err
	}
	found := missavExtractM3u8(page)
	if found == "" {
		return nil, fmt.Errorf("mirror: no m3u8 on detail page")
	}
	return playResult(found, slot, rawID, mirrorUA, mirrorPlayReferer, "m3u8", "hls"), nil
}

func mirrorSearch(slot, kw string) ([]interface{}, error) {
	text, err := mirrorFetch("/cn/search/" + url.PathEscape(kw))
	if err != nil {
		return []interface{}{}, err
	}
	works := mirrorParseList(text)
	return g2WorksToItems(slot, works, "搜索"), nil
}

func init() {
	registerSite("hongguo", &siteImpl{
		Key: "mirror", Name: "🔞MissAV镜像", DefaultCat: "release", HomeURL: mirrorSeedHost + "/",
		Categories: mirrorCategories,
		Catalog:    mirrorCatalog,
		Detail:     mirrorDetail,
		Resolve:    mirrorResolve,
		Search:     mirrorSearch,
		Headers:    mirrorHeaders,
	}, "mirror", "thisav")
}
