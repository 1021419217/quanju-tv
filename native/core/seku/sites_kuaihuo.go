package seku

// ---- 快活影院 (kuaihuo) — static HTML + Netlify search API ----
//
// Ported from the user's ZakaTV jar class ZakaKuaihuoAmns (config key
// kuaihuo "🔞快活影院┃静态站直取"), re-grounded on live pages fetched
// 2026-10-24 from this egress:
//   - Seed host tbryy.opsizte.com 301s to a rotating live host
//     (tbryy.ksdlcme.cn today) and collapses deep paths to the root, so
//     the live host is learned once from the seed root redirect and all
//     paths are then built on it (same pattern as the MissAV mirror).
//   - Categories are the live homepage nav's 18 video sections 1:1
//     (图片/小说 sections exist on the site but are not video; the jar
//     class likewise only carries the 18 video dirs).
//   - Catalog: /{dir}/index.html then /{dir}/list_{n}.html, 40 cards a
//     page; cards carry data-original covers on res1.pkckgcg.com.
//   - Detail: <h1> title, var posterImg, var playUrl; the playUrl host
//     is obfuscated as +@token@+ and resolves to stream1.pkckgcg.com
//     (verified: substituted URL returns a real HLS playlist).
//   - Search: POST an Elasticsearch-style DSL to
//     ser.m3u8111222333.com/.netlify/functions/search; hits carry
//     class_dir + year/month/day + id, enough to rebuild the detail
//     path exactly.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const khSeedHost = "https://tbryy.opsizte.com"
const khSearchAPI = "https://ser.m3u8111222333.com/.netlify/functions/search"
const khResBase = "https://res1.pkckgcg.com"
const khStreamHost = "stream1.pkckgcg.com"
const khUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var khCats = []struct{ id, name string }{
	{"shipin", "视频·最新"}, {"dianying", "电影·最新"},
	{"yazhouav", "亚洲AV"}, {"yazhouxingai", "亚洲性爱"},
	{"oumeiav", "欧美AV"}, {"oumeixingai", "欧美性爱"},
	{"zipaitoupai", "自拍偷拍"}, {"toupaizipai", "偷拍自拍"},
	{"3ddonghua", "3D动画"}, {"chengrenkatong", "成人卡通"},
	{"shunvrenqi", "熟女人妻"}, {"siwazhifu", "丝袜制服"},
	{"zhifusiwa", "制服丝袜"}, {"zhubozhibo", "主播直播"},
	{"smlinglei", "SM另类"}, {"lingleibiantai", "另类变态"},
	{"sanjijingdian", "经典三级"}, {"luanlunshunv", "乱伦熟女"},
}

var khState = &struct {
	sync.Mutex
	host string
}{host: khSeedHost}

var khClient = &http.Client{Timeout: 25 * time.Second, Transport: sharedTransport}

func khHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      khUA,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
	}
}

func khLearnedHost() string {
	khState.Lock()
	defer khState.Unlock()
	return khState.host
}

// khFetch GETs a path on the learned host. The seed host 301s to the
// live host's ROOT (deep paths collapse), so whenever a response lands
// on a different host than requested, the path is re-fetched there —
// otherwise the first request per process would silently return the
// homepage for every category.
func khFetch(path string) (string, error) {
	host := khLearnedHost()
	body, finalHost, err := khFetchFrom(host, path)
	if err == nil && finalHost != "" && finalHost != host {
		khState.Lock()
		khState.host = finalHost
		khState.Unlock()
		body, _, err = khFetchFrom(finalHost, path)
	}
	if err != nil && host != khSeedHost {
		// Learned host died; fall back to the seed and re-learn.
		body2, finalHost2, err2 := khFetchFrom(khSeedHost, path)
		if err2 == nil {
			if finalHost2 != "" && finalHost2 != khSeedHost {
				khState.Lock()
				khState.host = finalHost2
				khState.Unlock()
				body2, _, err2 = khFetchFrom(finalHost2, path)
			}
			if err2 == nil {
				return body2, nil
			}
		}
	}
	return body, err
}

func khFetchFrom(host, path string) (string, string, error) {
	req, err := http.NewRequest("GET", host+path, nil)
	if err != nil {
		return "", "", err
	}
	for k, v := range khHeaders() {
		req.Header.Set(k, v)
	}
	resp, err := khClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("kuaihuo http %d", resp.StatusCode)
	}
	finalHost := ""
	if resp.Request != nil && resp.Request.URL != nil {
		finalHost = resp.Request.URL.Scheme + "://" + resp.Request.URL.Host
	}
	return string(b), finalHost, nil
}

var (
	khCardRe     = regexp.MustCompile(`(?s)<li>\s*<a\s+href="(/htm/[^"]+)"\s+title="([^"]*)".*?data-original="([^"]+)"`)
	khCardFBRe   = regexp.MustCompile(`<a\s+href="(/htm/[^"]+)"\s+title="([^"]*)"`)
	khH1Re       = regexp.MustCompile(`(?s)<h1[^>]*>(.*?)</h1>`)
	khPlayURLRe  = regexp.MustCompile(`var\s+playUrl="([^"]*)"`)
	khPosterRe   = regexp.MustCompile(`var\s+posterImg="([^"]*)"`)
	khTokenRe    = regexp.MustCompile(`\+@[a-zA-Z0-9_.\-]+@\+`)
	khNextPageRe = regexp.MustCompile(`list_(\d+)\.html`)
)

func khClean(s string) string {
	s = html.UnescapeString(s)
	s = g5TagRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func khParseCards(slot, page, catName string) []interface{} {
	items := []interface{}{}
	seen := map[string]bool{}
	for _, m := range khCardRe.FindAllStringSubmatch(page, -1) {
		path, title, pic := m[1], khClean(m[2]), m[3]
		if seen[path] {
			continue
		}
		seen[path] = true
		items = append(items, makeDrama(slot, path, title, pic, "", catName, ""))
	}
	if len(items) == 0 {
		for _, m := range khCardFBRe.FindAllStringSubmatch(page, -1) {
			path, title := m[1], khClean(m[2])
			if seen[path] || title == "" {
				continue
			}
			seen[path] = true
			items = append(items, makeDrama(slot, path, title, "", "", catName, ""))
		}
	}
	return items
}

func khCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range khCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func khCatName(id string) string {
	for _, c := range khCats {
		if c.id == id {
			return c.name
		}
	}
	return ""
}

func khCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	dir := category
	if dir == "" {
		dir = "shipin"
	}
	if page < 1 {
		page = 1
	}
	path := "/" + dir + "/index.html"
	if page > 1 {
		path = fmt.Sprintf("/%s/list_%d.html", dir, page)
	}
	body, err := khFetch(path)
	if err != nil {
		return nil, 0, false, err
	}
	items := khParseCards(slot, body, khCatName(dir))
	hasMore := false
	for _, m := range khNextPageRe.FindAllStringSubmatch(body, -1) {
		var n int
		if _, err := fmt.Sscanf(m[1], "%d", &n); err == nil && n > page {
			hasMore = true
			break
		}
	}
	return items, len(items), hasMore, nil
}

func khDetailPage(path string) (string, error) {
	if !strings.HasPrefix(path, "/htm/") {
		return "", fmt.Errorf("kuaihuo bad detail id %q", path)
	}
	return khFetch(path)
}

func khDetail(slot, rawID string) (map[string]interface{}, error) {
	body, err := khDetailPage(rawID)
	if err != nil {
		return nil, err
	}
	name := rawID
	if m := khH1Re.FindStringSubmatch(body); m != nil {
		name = khClean(m[1])
	}
	pic := ""
	if m := khPosterRe.FindStringSubmatch(body); m != nil {
		pic = m[1]
	}
	d := makeDrama(slot, rawID, name, pic, "", "", name)
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	rel := []interface{}{}
	for _, it := range khParseCards(slot, body, "") {
		if m, ok := it.(map[string]interface{}); ok {
			if id, _ := m["id"].(string); id != "" && !strings.HasSuffix(id, rawID) {
				rel = append(rel, it)
			}
		}
	}
	if len(rel) > 0 {
		d["related"] = rel
	}
	return d, nil
}

func khPlayURL(body string) string {
	m := khPlayURLRe.FindStringSubmatch(body)
	if m == nil {
		return ""
	}
	return khTokenRe.ReplaceAllString(m[1], khStreamHost)
}

func khResolve(slot, rawID string) (map[string]interface{}, error) {
	body, err := khDetailPage(rawID)
	if err != nil {
		return nil, err
	}
	u := khPlayURL(body)
	if u == "" {
		return nil, fmt.Errorf("kuaihuo no play url")
	}
	return playResult(u, slot, rawID, khUA, khLearnedHost()+"/", "m3u8", "hls"), nil
}

type khSearchHit struct {
	ID     string `json:"_id"`
	Source struct {
		Name    string `json:"data_name"`
		Actor   string `json:"data_actor"`
		PicBig  string `json:"data_picbig"`
		Intro   string `json:"data_intro"`
		ClassDir string `json:"class_dir"`
		Year    int    `json:"year"`
		Month   int    `json:"month"`
		Day     int    `json:"day"`
		IDNum   int    `json:"id"`
	} `json:"_source"`
}

func khSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return []interface{}{}, nil
	}
	dsl := map[string]interface{}{
		"query": map[string]interface{}{
			"multi_match": map[string]interface{}{
				"query":  kw,
				"fields": []string{"data_name", "data_intro", "data_actor"},
			},
		},
		"from": 0,
		"size": 30,
		"_source": []string{
			"data_name", "data_actor", "data_picbig", "data_intro",
			"class_dir", "year", "month", "day", "id",
		},
	}
	raw, _ := json.Marshal(dsl)
	req, err := http.NewRequest("POST", khSearchAPI, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", khUA)
	req.Header.Set("Origin", khLearnedHost())
	req.Header.Set("Referer", khLearnedHost()+"/")
	resp, err := khClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("kuaihuo search http %d", resp.StatusCode)
	}
	var parsed struct {
		Hits struct {
			Hits []khSearchHit `json:"hits"`
		} `json:"hits"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		return nil, err
	}
	items := []interface{}{}
	for _, h := range parsed.Hits.Hits {
		s := h.Source
		if s.ClassDir == "" || s.IDNum == 0 {
			continue
		}
		path := fmt.Sprintf("/htm/%04d/%d/%d/%s/%d.html", s.Year, s.Month, s.Day, s.ClassDir, s.IDNum)
		pic := s.PicBig
		if strings.HasPrefix(pic, "/") {
			pic = khResBase + pic
		}
		remark := s.Actor
		items = append(items, makeDrama(slot, path, khClean(s.Name), pic, remark, khCatName(s.ClassDir), khClean(s.Intro)))
	}
	return items, nil
}

func init() {
	registerSite("kuaihuo", &siteImpl{
		Key: "kuaihuo", Name: "🔞快活影院", DefaultCat: "shipin", HomeURL: khSeedHost + "/",
		Categories: khCategories,
		Catalog:    khCatalog,
		Detail:     khDetail,
		Resolve:    khResolve,
		Search:     khSearch,
		Headers:    khHeaders,
	}, "kuaihuo")
}
