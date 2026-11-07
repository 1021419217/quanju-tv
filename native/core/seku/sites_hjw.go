package seku

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 海角网 web 版 — 配方出 ZakaTV jar ZakaHjwAmns 反编译，与影视区已有的
// 海角 App 系（fanguo）是两个站，别混淆。本站是 WordPress 式结构：
// /archives/{id}/ 帖子、/page/N/ 分页、/search/{词}/ 搜索。
//   - 入口主机轮换：hjw01.com 只回 CF 301 页；jar 的子域池
//     believe/broad/by × bskayiiu.cc/ttxrochzh.cc 是活入口，
//     2026-10-24 实测跳转到 *.clovurwbp.cc 并真回首页（88 帖锚）。
//     实现按快活同款「从种子跳转学活主机」处理。
//   - 详情页 "video":{...,"url":"..."} JSON 直给带 auth_key 的
//     m3u8（2026-10-24 活页实证），无需 jar 的分片 AES 代理层。
//   - 分类为 jar 的 8 个固定索引类；各索引页同样以 /archives/
//     卡片解析（页面带多少帖卡出多少），不做标签/作者文件夹下钻。

var hjwSeeds = []string{
	"https://believe.bskayiiu.cc",
	"https://broad.bskayiiu.cc",
	"https://by.bskayiiu.cc",
	"https://believe.ttxrochzh.cc",
	"https://broad.ttxrochzh.cc",
	"https://by.ttxrochzh.cc",
	"https://hjw2026.com",
	"https://hjw01.com",
	"https://gktgyicy.cc",
}

const hjwUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var hjwState = struct {
	sync.Mutex
	host string
}{}

var hjwClient = &http.Client{Timeout: 25 * time.Second, Transport: sharedTransport}

type hjwCat struct{ id, name string }

var hjwCats = []hjwCat{
	{"home", "最新"},
	{"tags", "标签"},
	{"authors", "作者"},
	{"authors_hot", "作者热榜"},
	{"authors_up", "作者更新"},
	{"communitys", "社区"},
	{"date", "日期"},
	{"ai", "合集"},
}

var (
	hjwReCard    = regexp.MustCompile(`(?is)<a\s+href="(/archives/(\d+)/)"([^>]*)>(.*?)</a>`)
	hjwReLoader  = regexp.MustCompile(`(?i)(?:z-image-loader-url|x-image-loader-url)="([^"]+)"`)
	hjwReImgTag  = regexp.MustCompile(`(?i)<img[^>]+(?:src|data-src|data-original)="([^"]+)"`)
	hjwReAlt     = regexp.MustCompile(`(?i)alt="([^"]*)"`)
	hjwReH1      = regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`)
	hjwReTitle   = regexp.MustCompile(`(?is)<title>([^<]*)</title>`)
	hjwReOGImage = regexp.MustCompile(`(?i)<meta[^>]+property="og:image"[^>]+content="([^"]+)"`)
	hjwReVideo   = regexp.MustCompile(`(?is)"video"\s*:\s*\{[^}]*?"url"\s*:\s*"([^"]+)"`)
	hjwReM3U8    = regexp.MustCompile(`https?://[^\s"'<>\\\)]+?\.m3u8[^\s"'<>\\\)]*`)
	hjwReMP4     = regexp.MustCompile(`https?://[^\s"'<>\\\)]+?\.(?:mp4|m4v)[^\s"'<>\\\)]*`)
	hjwReDateDiv = regexp.MustCompile(`(?is)<div class="date">([^<]*)</div>`)
	hjwRePlayDiv = regexp.MustCompile(`(?is)<div class="play">([^<]*)</div>`)
	hjwReTags    = regexp.MustCompile(`(?is)<[^>]+>`)
)

func hjwClean(s string) string {
	s = hjwReTags.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

func hjwGet(rawURL string) (string, string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", hjwUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	resp, err := hjwClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return "", "", err
	}
	final := resp.Request.URL.Scheme + "://" + resp.Request.URL.Host
	return string(b), final, nil
}

func hjwLearnedHost() string {
	hjwState.Lock()
	defer hjwState.Unlock()
	return hjwState.host
}

func hjwSetHost(h string) {
	hjwState.Lock()
	hjwState.host = h
	hjwState.Unlock()
}

// hjwFetch GETs a site path: learned host first, else walk the seed
// pool and learn the live host from wherever real content answers.
func hjwFetch(path string) (string, error) {
	if host := hjwLearnedHost(); host != "" {
		if body, _, err := hjwGet(host + path); err == nil && len(body) > 2000 && !isChallengePage(body) {
			return body, nil
		}
	}
	for _, seed := range hjwSeeds {
		body, final, err := hjwGet(seed + path)
		if err != nil || len(body) <= 2000 || isChallengePage(body) {
			continue
		}
		if !strings.Contains(body, "/archives/") && !strings.Contains(body, "<h1") {
			continue
		}
		if final != "" {
			hjwSetHost(final)
		}
		return body, nil
	}
	return "", fmt.Errorf("hjw: no live host for %s", path)
}

func hjwParseCards(pageHTML string) []map[string]interface{} {
	out := []map[string]interface{}{}
	seen := map[string]bool{}
	for _, m := range hjwReCard.FindAllStringSubmatchIndex(pageHTML, -1) {
		id := pageHTML[m[4]:m[5]]
		if seen[id] {
			continue
		}
		seen[id] = true
		inner := ""
		if m[8] >= 0 {
			inner = pageHTML[m[8]:m[9]]
		}
		title := hjwClean(inner)
		// 封面：卡内 loader 属性 / img；再向后 600 字内找
		pic := ""
		if lm := hjwReLoader.FindStringSubmatch(inner); lm != nil {
			pic = lm[1]
		} else if im := hjwReImgTag.FindStringSubmatch(inner); im != nil {
			pic = im[1]
		}
		if pic == "" {
			hi := m[1] + 600
			if hi > len(pageHTML) {
				hi = len(pageHTML)
			}
			seg := pageHTML[m[1]:hi]
			if lm := hjwReLoader.FindStringSubmatch(seg); lm != nil {
				pic = lm[1]
			} else if im := hjwReImgTag.FindStringSubmatch(seg); im != nil {
				pic = im[1]
			}
		}
		if pic != "" && strings.HasPrefix(pic, "/") {
			if host := hjwLearnedHost(); host != "" {
				pic = host + pic
			}
		}
		remark := ""
		if dm := hjwReDateDiv.FindStringSubmatch(inner); dm != nil {
			remark = hjwClean(dm[1])
		} else if pm := hjwRePlayDiv.FindStringSubmatch(inner); pm != nil {
			remark = hjwClean(pm[1])
		}
		if title == "" {
			title = "帖子 " + id
		}
		out = append(out, makeDrama("hjw", id, title, pic, remark, "", ""))
		if len(out) >= 60 {
			break
		}
	}
	return out
}

func hjwCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range hjwCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func hjwCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	var path string
	if category == "" || category == "home" {
		path = "/"
		if page > 1 {
			path = fmt.Sprintf("/page/%d/", page)
		}
	} else {
		path = "/" + category + "/"
		if page > 1 {
			path += fmt.Sprintf("page/%d/", page)
		}
	}
	body, err := hjwFetch(path)
	if err != nil {
		return nil, 0, false, err
	}
	items := hjwParseCards(body)
	out := make([]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out, 0, len(items) >= 10, nil
}

func hjwDetailPage(rawID string) (string, error) {
	return hjwFetch("/archives/" + rawID + "/")
}

func hjwDetail(slot, rawID string) (map[string]interface{}, error) {
	body, err := hjwDetailPage(rawID)
	if err != nil {
		return nil, err
	}
	title := ""
	if m := hjwReH1.FindStringSubmatch(body); m != nil {
		title = hjwClean(m[1])
	}
	if title == "" {
		if m := hjwReTitle.FindStringSubmatch(body); m != nil {
			title = hjwClean(m[1])
		}
	}
	if title == "" {
		title = "帖子 " + rawID
	}
	d := map[string]interface{}{"name": title}
	if m := hjwReOGImage.FindStringSubmatch(body); m != nil && !strings.Contains(m[1], "social-default") {
		d["pic"] = html.UnescapeString(m[1])
	}
	d["desc"] = title
	g5SetChapters(d, "hjw", rawID, []string{rawID}, []string{"正片"})
	rel := hjwParseCards(body)
	related := make([]interface{}, 0, len(rel))
	for _, it := range rel {
		if g5StrVal(it["id"]) == dramaID("hjw", rawID) {
			continue
		}
		related = append(related, it)
		if len(related) >= 12 {
			break
		}
	}
	if len(related) > 0 {
		d["related"] = related
	}
	return d, nil
}

func hjwResolve(slot, rawID string) (map[string]interface{}, error) {
	body, err := hjwDetailPage(rawID)
	if err != nil {
		return nil, err
	}
	u := ""
	if m := hjwReVideo.FindStringSubmatch(body); m != nil {
		u = strings.ReplaceAll(m[1], `\/`, "/")
		u = html.UnescapeString(u)
	}
	if u == "" {
		flat := strings.ReplaceAll(body, `\/`, "/")
		if m := hjwReM3U8.FindString(flat); m != "" {
			u = m
		} else if m := hjwReMP4.FindString(flat); m != "" {
			u = m
		}
	}
	if u == "" {
		return nil, fmt.Errorf("hjw: no stream on archives %s", rawID)
	}
	format := "mp4"
	if strings.Contains(u, ".m3u8") {
		format = "hls"
	}
	host := hjwLearnedHost()
	return playResult(u, "hjw", rawID, hjwUA, host+"/", "", format), nil
}

func hjwSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return []interface{}{}, nil
	}
	body, err := hjwFetch("/search/" + url.PathEscape(kw) + "/")
	if err != nil {
		return nil, err
	}
	items := hjwParseCards(body)
	out := make([]interface{}, 0, len(items))
	for _, it := range items {
		out = append(out, it)
	}
	return out, nil
}

func init() {
	registerSite("hjw", &siteImpl{
		Key: "hjw", Name: "🔞海角网", DefaultCat: "home", HomeURL: hjwSeeds[0],
		Categories: hjwCategories,
		Catalog:    hjwCatalog,
		Detail:     hjwDetail,
		Resolve:    hjwResolve,
		Search:     hjwSearch,
		Headers:    func() map[string]string { return map[string]string{"User-Agent": hjwUA} },
	}, "hjw")
}
