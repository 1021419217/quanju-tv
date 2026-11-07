package seku

// ---- G3 group: JSON/RSC-class py sources ported to Go providers ----
//
//   51xz    -> slot maoguo     (Next.js RSC embedded JSON)
//   pektino -> slot cloudfront (site JSON API /api/media + /api/tags)
//   vjav    -> slot huangguoai (vjav.com, KVS JSON API; NOT dayday's
//              vjav.tube — different site, different KVS dialect)
//
// Recipes: batch/recipes_py.json (keys 51xz/pektino/vjav). Where the
// recipe and the py source disagree, the py source wins; divergences
// are flagged inline with "PY-WINS".
//
// Core-contract downgrades (no per-site filters in the siteImpl
// contract): pektino's range/sort filters and vjav's sort filter
// cannot be expressed, so catalog uses each py's defaults
// (pektino range=all&sort=favorite; vjav latest-updates / the fixed
// classes' intrinsic sorts). vjav's Google-translate title layer is
// display-only in py and is omitted (titles stay site-original), as
// the recipe's portingNote allows.

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

func init() {
	registerSite("maoguo", &siteImpl{
		Key: "51xz", Name: "51X", DefaultCat: "x视频", HomeURL: xzHost + "/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, n := range xzCats {
				out = append(out, categoryObj(n, n))
			}
			return out
		},
		Catalog: xzCatalog,
		Detail:  xzDetail,
		Resolve: xzResolve,
		Search:  xzSearch,
		Headers: xzHeaders,
	}, "51xz")
	// v4: cloudfront was reassigned to QiShe (sites_qishe.go) by user
	// order 2026-10-23 17:48 — Pektino was reported unplayable on the
	// device in v3 and its play chain cannot be re-verified from this
	// egress (the egress proxy refuses pektino.com outright). The pk*
	// provider stays compiled but unregistered. Noted divergence found
	// during the v4 audit (unverified suspect): the py source's
	// playerContent sends ONLY a User-Agent header, while pkResolve
	// adds Referer=pektino.com/ via playResult.
	registerSite("v_vjav", &siteImpl{
		Key: "vjav", Name: "VJAV", DefaultCat: "latest", HomeURL: vjHost + "/",
		Categories: vjCategories,
		Catalog:    vjCatalog,
		Detail:     vjDetail,
		Resolve:    vjResolve,
		Search:     vjSearch,
		Headers:    vjHeaders,
	}, "vjav")
}

// =====================================================================
// 51xz.vip — Next.js RSC embedded JSON (py: ~/workspace/51xz_work/51xz.py)
// =====================================================================

const xzHost = "https://51xz.vip"
const xzUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

func xzHeaders() map[string]string {
	return map[string]string{
		"User-Agent": xzUA,
		"Referer":    xzHost + "/",
		"Accept":     "text/html,*/*",
	}
}

// 站内导航模块 1:1 (x小说 is text-only, excluded in py too).
var xzCats = []string{
	"x视频", "真实自拍", "网红女神", "国产精品", "爱好男同-1",
	"短剧", "成年短剧", "情感剧场", "古装历史", "年代农村", "穿越重生",
	"x吃瓜", "今日吃瓜", "海外吃瓜", "娱乐吃瓜", "网红黑料",
	"x-av", "日本无码", "国产av", "欧美av", "动漫av",
}

var xzPostModules = map[string]bool{
	"x吃瓜": true, "今日吃瓜": true, "海外吃瓜": true, "娱乐吃瓜": true, "网红黑料": true,
}

type xzEp struct {
	Sort    float64
	HasSort bool
	Hls     string
}

type xzObj struct {
	ID, Kind, Slug, Title, Cover, Hls string
	Duration                          interface{}
	EpisodeCount                      interface{}
	CurrentEpisode                    interface{}
	Episodes                          []xzEp
	CreatorName, CatName, Desc        string
	Tags                              []string
	Published                         string
}

func (o *xzObj) path() string { return "/" + o.Kind + "/" + o.ID + "/" + o.Slug }

var (
	xzCacheMu sync.Mutex
	xzObjByID = map[string]*xzObj{}
	xzPostAgg = map[string][]*xzObj{}
)

func xzFlexID(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func xzStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func xzFetch(rawURL string) (string, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		body, code, _, err := httpGet(rawURL, xzHeaders())
		if err == nil && code == 200 {
			return string(body), nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("http %d", code)
		}
	}
	return "", lastErr
}

var xzPushRe = regexp.MustCompile(`(?s)self\.__next_f\.push\(\[1,("(?:[^"\\]|\\.)*")\]\)`)

// xzObjects mirrors py _objects(): restore the RSC push chunks, then
// scan the joined stream with a JSON decoder for {"id":...} objects,
// classifying by cardKind/kind and deduping on (kind, id).
func xzObjects(page string) []*xzObj {
	chunks := []string{}
	for _, m := range xzPushRe.FindAllStringSubmatch(page, -1) {
		var s string
		if err := json.Unmarshal([]byte(m[1]), &s); err == nil {
			chunks = append(chunks, s)
		}
	}
	text := ""
	if len(chunks) > 0 {
		text = strings.Join(chunks, "\n")
	} else {
		text = strings.ReplaceAll(page, `\"`, `"`)
	}
	out := []*xzObj{}
	seen := map[string]bool{}
	pos := 0
	for {
		j := strings.Index(text[pos:], `{"id":"`)
		if j < 0 {
			break
		}
		start := pos + j
		pos = start + 1
		dec := json.NewDecoder(strings.NewReader(text[start:]))
		var raw map[string]interface{}
		if err := dec.Decode(&raw); err != nil {
			continue
		}
		kind := ""
		if raw["cardKind"] == "video" {
			kind = "video"
		} else if raw["kind"] == "drama" {
			kind = "drama"
		} else if raw["kind"] == "post" {
			kind = "post"
		}
		if kind == "" {
			continue
		}
		id := xzFlexID(raw["id"])
		if id == "" {
			continue
		}
		key := kind + "|" + id
		if seen[key] {
			continue
		}
		seen[key] = true
		o := &xzObj{ID: id, Kind: kind}
		o.Slug, _ = raw["slug"].(string)
		o.Title = strings.TrimSpace(xzStr(raw, "titleZh", "titleEn"))
		o.Cover, _ = raw["coverUrl"].(string)
		o.Hls, _ = raw["hlsUrl"].(string)
		o.Duration = raw["duration"]
		o.EpisodeCount = raw["episodeCount"]
		o.CurrentEpisode = raw["currentEpisode"]
		o.Published, _ = raw["publishedAt"].(string)
		o.Desc = xzStr(raw, "descriptionZh", "seoDescription")
		if c, ok := raw["creator"].(map[string]interface{}); ok {
			o.CreatorName, _ = c["name"].(string)
		}
		if c, ok := raw["category"].(map[string]interface{}); ok {
			o.CatName, _ = c["nameZh"].(string)
		}
		if tags, ok := raw["tags"].([]interface{}); ok {
			for _, tv := range tags {
				if tm, ok := tv.(map[string]interface{}); ok {
					if n, _ := tm["nameZh"].(string); n != "" {
						o.Tags = append(o.Tags, n)
					}
				}
			}
		}
		if eps, ok := raw["episodes"].([]interface{}); ok {
			for _, ev := range eps {
				em, ok := ev.(map[string]interface{})
				if !ok {
					continue
				}
				ep := xzEp{}
				if sv, ok := em["sort"].(float64); ok {
					ep.Sort, ep.HasSort = sv, true
				}
				ep.Hls, _ = em["hlsUrl"].(string)
				o.Episodes = append(o.Episodes, ep)
			}
		}
		xzCacheMu.Lock()
		xzObjByID[o.ID] = o
		xzCacheMu.Unlock()
		out = append(out, o)
	}
	return out
}

func xzDur(v interface{}) string {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		f, _ = strconv.ParseFloat(t, 64)
	default:
		return ""
	}
	s := int(f)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, (s%3600)/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func xzRemark(o *xzObj) string {
	switch o.Kind {
	case "video":
		return xzDur(o.Duration)
	case "drama":
		cnt := xzFlexID(o.EpisodeCount)
		if cnt == "" {
			cnt = xzFlexID(o.CurrentEpisode)
		}
		if cnt != "" {
			return "全 " + cnt + " 集"
		}
		return "短剧"
	default:
		if o.Hls != "" {
			return "有视频"
		}
		return "图文"
	}
}

func xzCard(slot string, o *xzObj, catName string) map[string]interface{} {
	title := o.Title
	if title == "" {
		title = o.path()
	}
	// Covers are returned bare (no py-style @Referer suffix): this
	// core fetches covers itself with the site headers (handleCover),
	// and the recipe explicitly leaves the bare URL as the Go-side
	// behaviour pending a bare-fetch check.
	return makeDrama(slot, o.path(), title, o.Cover, xzRemark(o), catName, "")
}

func xzModuleURL(name string, pg int) string {
	u := xzHost + "/m/" + name
	if pg > 1 {
		u += "?page=" + strconv.Itoa(pg)
	}
	return u
}

func xzCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if category == "" {
		category = "x视频"
	}
	if page < 1 {
		page = 1
	}
	if xzPostModules[category] {
		// 吃瓜 modules: aggregate up to 8 site pages of local posts
		// that carry an hlsUrl, then page locally at 24/page (py
		// _post_agg).
		xzCacheMu.Lock()
		agg, ok := xzPostAgg[category]
		xzCacheMu.Unlock()
		if !ok {
			agg = []*xzObj{}
			seen := map[string]bool{}
			for sp := 1; sp <= 8; sp++ {
				pageHTML, err := xzFetch(xzModuleURL(category, sp))
				if err != nil {
					break
				}
				posts := []*xzObj{}
				for _, o := range xzObjects(pageHTML) {
					if o.Kind == "post" {
						posts = append(posts, o)
					}
				}
				for _, o := range posts {
					if o.Hls != "" && !seen[o.ID] {
						seen[o.ID] = true
						agg = append(agg, o)
					}
				}
				if len(posts) < 24 {
					break
				}
			}
			xzCacheMu.Lock()
			xzPostAgg[category] = agg
			xzCacheMu.Unlock()
		}
		items := []interface{}{}
		lo := (page - 1) * 24
		if lo < len(agg) {
			hi := lo + 24
			if hi > len(agg) {
				hi = len(agg)
			}
			for _, o := range agg[lo:hi] {
				items = append(items, xzCard(slot, o, category))
			}
		}
		return items, len(agg), page*24 < len(agg), nil
	}
	pageHTML, err := xzFetch(xzModuleURL(category, page))
	if err != nil {
		return nil, 0, false, err
	}
	items := []interface{}{}
	for _, o := range xzObjects(pageHTML) {
		items = append(items, xzCard(slot, o, category))
	}
	hasMore := len(items) >= 24
	total := len(items) * page
	if hasMore {
		total = len(items) * (page + 1)
	}
	return items, total, hasMore, nil
}

// xzDetailObj resolves the RSC object for a card path, from the
// in-process cache when possible (drama objects without episodes do
// not count — py invalidates them), else by fetching the detail page.
func xzDetailObj(rawPath string) (*xzObj, error) {
	parts := strings.Split(rawPath, "/")
	target := ""
	if len(parts) > 2 {
		target = parts[2]
	}
	xzCacheMu.Lock()
	cached := xzObjByID[target]
	xzCacheMu.Unlock()
	if cached != nil && !(cached.Kind == "drama" && len(cached.Episodes) == 0) {
		return cached, nil
	}
	u := rawPath
	if strings.HasPrefix(u, "/") {
		u = xzHost + u
	} else {
		u = xzHost + "/" + u
	}
	pageHTML, err := xzFetch(u)
	if err != nil {
		return nil, err
	}
	objs := xzObjects(pageHTML)
	for _, o := range objs {
		if o.ID == target {
			return o, nil
		}
	}
	if len(objs) > 0 {
		return objs[0], nil
	}
	return nil, fmt.Errorf("51xz: object not found")
}

func xzSortedEps(o *xzObj) []xzEp {
	eps := []xzEp{}
	for _, e := range o.Episodes {
		if e.Hls != "" {
			eps = append(eps, e)
		}
	}
	sort.SliceStable(eps, func(i, j int) bool { return eps[i].Sort < eps[j].Sort })
	return eps
}

func xzDetail(slot, rawID string) (map[string]interface{}, error) {
	base, _ := splitEpisodeSuffix(rawID)
	o, err := xzDetailObj(base)
	if err != nil {
		return nil, err
	}
	title := o.Title
	if title == "" {
		title = base
	}
	desc := o.Desc
	if r := []rune(desc); len(r) > 600 {
		desc = string(r[:600])
	}
	pub := o.Published
	year := ""
	if len(pub) >= 4 {
		year = pub[:4]
	}
	d := makeDrama(slot, base, title, o.Cover, xzRemark(o), o.CatName, desc)
	// Extra descriptive fields the py detail carries (harmless extras
	// for the Dart models).
	d["actor"] = o.CreatorName
	d["vod_actor"] = o.CreatorName
	d["tags"] = func() []interface{} {
		out := []interface{}{}
		for _, t := range o.Tags {
			out = append(out, t)
		}
		return out
	}()
	d["year"] = year
	d["vod_year"] = year
	if o.Kind == "drama" {
		eps := xzSortedEps(o)
		if len(eps) > 0 {
			chapters := []interface{}{}
			for i, e := range eps {
				label := i + 1
				if e.HasSort && e.Sort != 0 {
					label = int(e.Sort)
				}
				name := fmt.Sprintf("第%d集", label)
				epID := fmt.Sprintf("%s#%d", base, i+1)
				chapters = append(chapters, map[string]interface{}{
					"episodeId": epID,
					"id":        dramaID(slot, epID),
					"dramaId":   dramaID(slot, base),
					"sourceId":  slot,
					"source":    slot,
					"title":     name,
					"name":      name,
					"index":     i + 1,
					"episode":   i + 1,
					"url":       "",
					"videoUrl":  "",
					"mediaSize": 0,
				})
			}
			d["chapters"] = chapters
			d["episodeList"] = chapters
			d["episodes"] = len(eps)
			d["episodeCount"] = len(eps)
			d["chapterCount"] = len(eps)
			d["totalEpisode"] = len(eps)
			d["totalEpisodes"] = len(eps)
			d["remark"] = fmt.Sprintf("全 %d 集", len(eps))
			d["remarks"] = d["remark"]
			d["vod_remarks"] = d["remark"]
		}
	}
	return d, nil
}

func xzResolve(slot, rawID string) (map[string]interface{}, error) {
	base, epIdx := splitEpisodeSuffix(rawID)
	o, err := xzDetailObj(base)
	if err != nil {
		return nil, err
	}
	play := ""
	if o.Kind == "drama" {
		eps := xzSortedEps(o)
		if len(eps) == 0 {
			return nil, fmt.Errorf("51xz: no episodes")
		}
		if epIdx < 1 || epIdx > len(eps) {
			epIdx = 1
		}
		play = eps[epIdx-1].Hls
	} else {
		play = o.Hls
	}
	if play == "" {
		return nil, fmt.Errorf("51xz: no play url")
	}
	return playResult(play, slot, rawID, xzUA, xzHost+"/", "m3u8", "hls"), nil
}

func xzSearch(slot, kw string) ([]interface{}, error) {
	pageHTML, err := xzFetch(xzHost + "/search?q=" + url.QueryEscape(kw))
	if err != nil {
		return []interface{}{}, err
	}
	items := []interface{}{}
	for _, o := range xzObjects(pageHTML) {
		items = append(items, xzCard(slot, o, "搜索"))
	}
	return items, nil
}

// =====================================================================
// pektino.com — site JSON API (py: ~/workspace/pektino_work/pektino.py)
// =====================================================================

const pkHost = "https://pektino.com"
const pkUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
const pkTweetUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

func pkHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      pkUA,
		"Accept-Language": "zh-CN,zh;q=0.9,ja;q=0.8",
		"Referer":         pkHost + "/",
	}
}

type pkTag struct{ Code, JA, ZH, Aliases string }

// (code, 站方日文名, zh-CN 显示名, 别名|分隔) — verbatim from py TAGS.
var pkTags = []pkTag{
	{"shirouto", "素人", "业余", "业余|amateur|素人"},
	{"kyonyu", "巨乳", "丰胸", "丰胸|large bust|巨乳"},
	{"masturbation", "オナニー", "自我表达", "自慰|masturbation|オナニー"},
	{"jk", "女子高生", "女高中生", "女高中生|jk|女子高生"},
	{"anime", "アニメ・二次元", "动漫/二次元", "动漫|二次元|anime|アニメ"},
	{"female-teacher", "女教師", "女教师", "女教师|teacher|女教師"},
	{"nurse", "看護婦・ナース", "护士", "护士|nurse|ナース"},
	{"female-pervert", "痴女", "大胆女性", "痴女|slut|female pervert"},
	{"married-woman", "人妻", "已婚女性", "已婚|人妻|married"},
	{"beautiful-girl", "美少女", "美少女", "美少女|beautiful girl"},
	{"big-sister", "お姉さん", "姐姐", "姐姐|お姉さん|sister"},
	{"gal", "ギャル", "时尚女孩", "时尚女孩|gal|ギャル"},
	{"shaved", "パイパン", "光滑风格", "白虎|无毛|shaved|パイパン"},
	{"small-breasts", "貧乳・微乳", "小胸", "贫乳|小胸|small breasts|貧乳"},
	{"lolita", "ロリ系", "少女系", "萝莉|lolita|ロリ"},
	{"swimsuit", "水着", "泳装", "泳装|swimsuit|水着"},
	{"sm", "SM", "SM题材", "sm|调教"},
	{"special-feature", "企画", "企划", "企划|feature|企画"},
	{"incest", "近親相姦", "家庭主题", "近亲|incest|近親"},
	{"rape", "レイプ", "冲突主题", "强暴|rape|レイプ"},
	{"molestation", "痴漢", "骚扰主题", "骚扰|痴汉|molestation|痴漢"},
	{"voyeur", "盗撮・のぞき", "隐藏拍摄", "偷拍|voyeur|盗撮"},
	{"pickup", "ナンパ", "邂逅", "搭讪|pickup|ナンパ"},
	{"massage", "マッサージ", "按摩", "按摩|massage|マッサージ"},
	{"outdoor", "野外・露出", "户外场景", "野外|露出|outdoor|野外"},
	{"orgy", "乱交", "群体场景", "群交|orgy|乱交"},
	{"anal", "アナル", "背面主题", "肛|anal|アナル"},
	{"deep-throat", "イラマチオ", "深层表达", "深喉|throat|イラマチオ"},
	{"facial", "顔射", "面部艺术", "颜射|facial|顔射"},
	{"cum-swallowing", "ごっくん", "吞咽主题", "吞精|swallow|ごっくん"},
	{"handjob", "手コキ", "手部表演", "手活|handjob|手コキ"},
	{"creampie", "中出し", "内部主题", "中出|内射|creampie|中出し"},
	{"titjob", "パイズリ", "胸部表演", "乳交|titjob|パイズリ"},
	{"fellatio", "フェラ", "口部艺术", "口交|fellatio|フェラ"},
	{"bukkake", "ぶっかけ", "泼洒艺术", "颜射|bukkake|ぶっかけ"},
	{"hamedori", "ハメ撮り", "自摄", "自拍|hamedori|ハメ撮り"},
	{"personal-filming", "個人撮影", "私人拍摄", "个人拍摄|自摄|personal|個人撮影"},
	{"uncensored", "無修正", "未修饰", "无码|无修|uncensored|無修正"},
	{"gay", "ゲイ・男の娘", "男同性恋・男娘", "男同|伪娘|gay|ゲイ"},
	{"cosplay", "コスプレ", "角色扮演", "角色扮演|cosplay|コスプレ"},
}

// verbatim from py LANGS (+ZH).
var pkLangs = []pkTag{
	{"ja", "日本", "日本", "日本|日语|ja|japanese"},
	{"en", "English", "英语", "英语|英文|en|english"},
	{"ko", "한국어", "韩语", "韩国|韩语|ko|korean"},
	{"th", "ไทย", "泰语", "泰国|泰语|th|thai"},
	{"id", "Indonesia", "印尼语", "印尼|id|indonesia"},
	{"pt", "Português", "葡萄牙语", "葡萄牙|葡语|pt"},
	{"fr", "Français", "法语", "法国|法语|fr|french"},
	{"de", "Deutsch", "德语", "德国|德语|de|german"},
}

func pkNormURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	if strings.HasPrefix(u, "http://") {
		u = "https://" + u[len("http://"):]
	}
	return u
}

func pkFlexStr(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func pkNum(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	}
	return 0
}

func pkDur(sec float64) string {
	s := int(sec)
	if s <= 0 {
		return ""
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, (s%3600)/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func pkIsReal(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	return t != "" && t != "null" && t != "undefined" && t != "none" && t != "nan"
}

func pkRemark(item map[string]interface{}) string {
	parts := []string{}
	if t := pkNum(item["time"]); t != 0 {
		parts = append(parts, "⏱"+pkDur(t))
	}
	if pv := strings.TrimSpace(pkFlexStr(item["pv"])); pv != "" {
		parts = append(parts, "👁"+pv)
	}
	if fav := strings.TrimSpace(pkFlexStr(item["favorite"])); fav != "" && fav != "0" {
		parts = append(parts, "❤"+fav)
	}
	return strings.Join(parts, " | ")
}

func pkRealTitle(item map[string]interface{}) string {
	if at, _ := item["anime_title"].(string); pkIsReal(at) {
		return strings.TrimSpace(at)
	}
	acct, _ := item["tweet_account"].(string)
	head := "推特视频"
	if strings.TrimSpace(acct) != "" {
		head = "@" + strings.TrimSpace(acct)
	}
	if d, _ := item["posted_at"].(string); len(d) >= 10 {
		head += " · " + d[5:7] + "-" + d[8:10]
	}
	if t := pkNum(item["time"]); t != 0 {
		head += " · " + pkDur(t)
	}
	if head != "" {
		return head
	}
	if cd, _ := item["url_cd"].(string); cd != "" {
		return cd
	}
	return "PeKtino"
}

// ---- fxtwitter title completion (py _fill_titles/_tweet_text) ----

var pkTweetKeyRe = regexp.MustCompile(`/([^/]+)/status/(\d+)`)

var (
	pkTweetMu    sync.Mutex
	pkTweetCache = map[string]string{}
)

func pkTweetText(tweetURL string) string {
	m := pkTweetKeyRe.FindStringSubmatch(tweetURL)
	if m == nil {
		return ""
	}
	ck := m[1] + "/" + m[2]
	pkTweetMu.Lock()
	if hit, ok := pkTweetCache[ck]; ok {
		pkTweetMu.Unlock()
		return hit
	}
	pkTweetMu.Unlock()
	result := ""
	body, code, _, err := httpGet("https://api.fxtwitter.com/"+ck, map[string]string{
		"User-Agent": pkTweetUA,
		"Accept":     "application/json,text/plain,*/*",
	})
	if err == nil && code == 200 && len(body) > 20 {
		var root map[string]interface{}
		if json.Unmarshal(body, &root) == nil {
			if tw, ok := root["tweet"].(map[string]interface{}); ok {
				result, _ = tw["text"].(string)
			}
		}
	}
	pkTweetMu.Lock()
	pkTweetCache[ck] = result
	pkTweetMu.Unlock()
	return result
}

var pkTagStripRe = regexp.MustCompile(`<[^>]*>`)

func pkTitleOf(text string) string {
	text = pkTagStripRe.ReplaceAllString(text, " ")
	words := []string{}
	for _, tok := range strings.Fields(text) {
		if strings.HasPrefix(tok, "http://") || strings.HasPrefix(tok, "https://") ||
			strings.HasPrefix(tok, "t.co/") || strings.HasPrefix(tok, "www.") {
			continue
		}
		words = append(words, tok)
	}
	buf := ""
	for _, w := range words {
		if buf == "" {
			buf = w
		} else {
			buf += " " + w
		}
		if len([]rune(buf)) >= 24 {
			break
		}
	}
	if buf != "" && len(words) > len(strings.Fields(buf)) {
		buf += "…"
	}
	return buf
}

type pkCard struct {
	CD, Title, Pic, Remark, TweetURL string
}

// pkFillTitles replaces fallback titles with tweet-text titles,
// concurrently (py uses a 10-worker pool).
func pkFillTitles(cards []*pkCard) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for _, c := range cards {
		if c.TweetURL == "" {
			continue
		}
		wg.Add(1)
		go func(c *pkCard) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if t := pkTitleOf(pkTweetText(c.TweetURL)); t != "" {
				c.Title = t
			}
		}(c)
	}
	wg.Wait()
}

func pkAPI(pathAndQuery string) (string, error) {
	var lastErr error
	for i := 0; i < 2; i++ {
		body, code, _, err := httpGet(pkHost+pathAndQuery, pkHeaders())
		if err == nil && code == 200 && len(body) > 40 {
			return string(body), nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("pektino http %d len %d", code, len(body))
		}
	}
	return "", lastErr
}

func pkListQuery(rangeV string, page int, category, sortV string) string {
	if rangeV == "" {
		rangeV = "all"
	}
	if sortV == "" {
		sortV = "favorite"
	}
	if page < 1 {
		page = 1
	}
	return fmt.Sprintf("range=%s&page=%d&per_page=50&category=%s&ids=&isFilteredOnly=0&sort=%s",
		rangeV, page, url.QueryEscape(category), sortV)
}

func pkParseCards(text string) []*pkCard {
	cards := []*pkCard{}
	var root map[string]interface{}
	if json.Unmarshal([]byte(text), &root) != nil {
		return cards
	}
	items, _ := root["items"].([]interface{})
	for _, iv := range items {
		it, ok := iv.(map[string]interface{})
		if !ok {
			continue
		}
		cd := strings.TrimSpace(pkFlexStr(it["url_cd"]))
		if cd == "" {
			continue
		}
		tw, _ := it["tweet_url"].(string)
		cards = append(cards, &pkCard{
			CD:       cd,
			Title:    pkRealTitle(it),
			Pic:      pkNormURL(pkFlexStr(it["thumbnail"])),
			Remark:   pkRemark(it),
			TweetURL: strings.TrimSpace(tw),
		})
	}
	return cards
}

func pkListResult(slot, text string, page int) ([]interface{}, int, bool, error) {
	cards := pkParseCards(text)
	pkFillTitles(cards)
	items := []interface{}{}
	for _, c := range cards {
		items = append(items, makeDrama(slot, c.CD, c.Title, c.Pic, c.Remark, "PeKtino", ""))
	}
	total, last := 0, 0
	var root map[string]interface{}
	if json.Unmarshal([]byte(text), &root) == nil {
		total = int(pkNum(root["total"]))
		last = int(pkNum(root["lastPage"]))
	}
	hasMore := false
	if last > 0 {
		hasMore = page < last
	} else {
		hasMore = len(cards) >= 50
		if total <= 0 {
			total = len(cards)
		}
	}
	return items, total, hasMore, nil
}

func pkCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	cat := category
	if cat == "hot" || cat == "" {
		cat = ""
	}
	text, err := pkAPI("/api/media?" + pkListQuery("all", page, cat, "favorite"))
	if err != nil {
		return nil, 0, false, err
	}
	return pkListResult(slot, text, page)
}

var (
	pkTagsMapOnce sync.Once
	pkTagsByID    = map[int]string{}
)

func pkTagsMap() map[int]string {
	pkTagsMapOnce.Do(func() {
		text, err := pkAPI("/api/tags")
		if err != nil {
			return
		}
		var root interface{}
		if json.Unmarshal([]byte(text), &root) != nil {
			return
		}
		var rows []interface{}
		switch t := root.(type) {
		case []interface{}:
			rows = t
		case map[string]interface{}:
			rows, _ = t["tags"].([]interface{})
		}
		for _, rv := range rows {
			row, ok := rv.(map[string]interface{})
			if !ok || row["id"] == nil {
				continue
			}
			code := pkFlexStr(row["code"])
			name := pkFlexStr(row["name_zh_cn"])
			if name == "" {
				name = pkFlexStr(row["name"])
			}
			if name == "" {
				name = code
			}
			pkTagsByID[int(pkNum(row["id"]))] = name
		}
	})
	return pkTagsByID
}

func pkDetailItem(cd string) (map[string]interface{}, error) {
	text, err := pkAPI("/api/media/" + url.PathEscape(cd))
	if err != nil {
		return nil, err
	}
	var item map[string]interface{}
	if json.Unmarshal([]byte(text), &item) != nil || item == nil {
		return nil, fmt.Errorf("pektino: detail parse")
	}
	return item, nil
}

func pkDetail(slot, rawID string) (map[string]interface{}, error) {
	cd := strings.TrimSpace(rawID)
	if cd == "" || strings.HasPrefix(cd, "http") {
		return nil, fmt.Errorf("pektino: bad id")
	}
	item, err := pkDetailItem(cd)
	if err != nil {
		return nil, err
	}
	realCD := pkFlexStr(item["url_cd"])
	if realCD == "" {
		realCD = cd
	}
	// Tweet text (title + intro): tweet_url may need the ids= lookup.
	tweetURL, _ := item["tweet_url"].(string)
	if strings.TrimSpace(tweetURL) == "" && item["id"] != nil {
		if text, err := pkAPI("/api/media?ids=" + pkFlexStr(item["id"]) + "&per_page=1"); err == nil {
			var root map[string]interface{}
			if json.Unmarshal([]byte(text), &root) == nil {
				if arr, _ := root["items"].([]interface{}); len(arr) > 0 {
					if first, ok := arr[0].(map[string]interface{}); ok {
						tweetURL, _ = first["tweet_url"].(string)
					}
				}
			}
		}
	}
	tweet := pkTweetText(strings.TrimSpace(tweetURL))
	name := pkTitleOf(tweet)
	if name == "" {
		name = pkRealTitle(item)
	}
	// Tag line via /api/tags id -> zh-CN name.
	tagNames := []string{}
	if tags, ok := item["tags"].([]interface{}); ok && len(tags) > 0 {
		mp := pkTagsMap()
		for _, tv := range tags {
			tm, ok := tv.(map[string]interface{})
			if !ok {
				continue
			}
			if tm["tag_id"] != nil {
				if n, ok := mp[int(pkNum(tm["tag_id"]))]; ok {
					tagNames = append(tagNames, n)
					continue
				}
			}
			if n := pkFlexStr(tm["name"]); n != "" {
				tagNames = append(tagNames, n)
			}
		}
	}
	tagLine := strings.Join(tagNames, "、")
	acct := strings.TrimSpace(pkFlexStr(item["tweet_account"]))
	posted := pkFlexStr(item["posted_at"])
	if len(posted) > 10 {
		posted = posted[:10]
	}
	lines := []string{}
	if tweet != "" {
		lines = append(lines, "推文："+strings.TrimSpace(strings.ReplaceAll(tweet, "\n", " ")))
	}
	if tagLine != "" {
		lines = append(lines, "標籤："+tagLine)
	}
	if acct != "" {
		lines = append(lines, "來源：@"+acct)
	}
	if t := pkNum(item["time"]); t != 0 {
		lines = append(lines, "片長："+pkDur(t))
	}
	if posted != "" {
		lines = append(lines, "發布："+posted)
	}
	if pv := strings.TrimSpace(pkFlexStr(item["pv"])); pv != "" {
		lines = append(lines, "播放："+pv)
	}
	if fav := strings.TrimSpace(pkFlexStr(item["favorite"])); fav != "" {
		lines = append(lines, "收藏："+fav)
	}
	lines = append(lines, "", realCD)
	catName := tagLine
	if catName == "" {
		catName = "PeKtino"
	}
	d := makeDrama(slot, realCD, name, pkNormURL(pkFlexStr(item["thumbnail"])), pkRemark(item), catName, strings.Join(lines, "\n"))
	actor := ""
	if acct != "" {
		actor = "@" + acct
	}
	d["actor"] = actor
	d["vod_actor"] = actor
	if len(posted) >= 4 {
		d["year"] = posted[:4]
		d["vod_year"] = posted[:4]
	}
	return d, nil
}

func pkResolve(slot, rawID string) (map[string]interface{}, error) {
	// Play URL is taken fresh from the detail record (py detail stage
	// already holds item.url; the core re-reads it at resolve time).
	item, err := pkDetailItem(strings.TrimSpace(rawID))
	if err != nil {
		return nil, err
	}
	play := pkNormURL(pkFlexStr(item["url"]))
	if play == "" {
		return nil, fmt.Errorf("pektino: no play url")
	}
	return playResult(play, slot, rawID, pkUA, pkHost+"/", "mp4", "file"), nil
}

func pkMatchTag(kw string) string {
	k := strings.ToLower(strings.TrimSpace(kw))
	if k == "" {
		return ""
	}
	table := append(append([]pkTag{}, pkTags...), pkLangs...)
	for _, t := range table {
		if k == strings.ToLower(t.Code) || k == strings.ToLower(t.JA) {
			return t.Code
		}
		for _, a := range strings.Split(t.Aliases, "|") {
			if k == strings.ToLower(strings.TrimSpace(a)) {
				return t.Code
			}
		}
	}
	for _, t := range table {
		if len([]rune(t.JA)) > 1 && strings.Contains(strings.ToLower(t.JA), k) {
			return t.Code
		}
		if len(t.Code) > 2 && strings.Contains(strings.ToLower(t.Code), k) {
			return t.Code
		}
		for _, a := range strings.Split(t.Aliases, "|") {
			a = strings.TrimSpace(a)
			if len([]rune(a)) > 1 && strings.Contains(strings.ToLower(a), k) {
				return t.Code
			}
		}
	}
	return ""
}

var pkTagCodeRe = regexp.MustCompile(`"tag":"([^"]+)"`)

func pkResolveCode(kw string) string {
	hdr := pkHeaders()
	hdr["RSC"] = "1"
	body, code, _, err := httpGet(pkHost+"/search?tag="+url.QueryEscape(kw), hdr)
	if err != nil || code != 200 || len(body) < 100 {
		return ""
	}
	m := pkTagCodeRe.FindSubmatch(body)
	if m == nil {
		return ""
	}
	c := string(m[1])
	if len(c) <= 64 && !strings.ContainsAny(c, `/\`) {
		return c
	}
	return ""
}

func pkSearch(slot, kw string) ([]interface{}, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return []interface{}{}, nil
	}
	code := pkMatchTag(kw)
	if code == "" {
		code = pkResolveCode(kw)
	}
	if code == "" {
		// Site has no free-text search; an unmatched keyword honestly
		// yields an empty result (py behaviour).
		return []interface{}{}, nil
	}
	text, err := pkAPI("/api/media?" + pkListQuery("all", 1, code, "favorite"))
	if err != nil {
		return []interface{}{}, err
	}
	items, _, _, err := pkListResult(slot, text, 1)
	return items, err
}

// =====================================================================
// vjav.com — KVS JSON API (py: ~/workspace/vjav_work/vjav.py)
// PY-WINS vs recipe/dayday: this site uses the 86400 videos2 dialect
// and its own cyrillic-alphabet deobf (see vjDecode); dayday.go's
// comma/tilde standard-base64 variant must NOT be reused here.
// =====================================================================

const vjHost = "https://vjav.com"
const vjUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
const vjPageSize = 24
const vjModelsPageSize = 40

func vjHeaders() map[string]string {
	return map[string]string{
		"User-Agent": vjUA,
		"Accept":     "application/json, */*",
		"Referer":    vjHost + "/",
	}
}

type vjCat struct{ Dir, Name string }

// CATS_SNAPSHOT verbatim from py (156 entries, 2026-10-22 live count).
var vjSnapshot = []vjCat{
	{"asian", "亚洲人"},
	{"japanese", "日本"},
	{"hd", "HD"},
	{"jav-censored", "日本AV截尾"},
	{"female-orgasm", "女性高潮"},
	{"brunette", "黑发美女"},
	{"compilation", "合辑"},
	{"blowjob", "口交"},
	{"big-tits", "大奶"},
	{"lingerie", "性感内衣"},
	{"milf", "欠干辣妈"},
	{"hairy", "浓毛"},
	{"creampie", "体内射精"},
	{"amateur", "素人自拍"},
	{"jav-uncensored", "无码影片"},
	{"jav-idols", "日本AV偶像"},
	{"bbw", "丰满大美女"},
	{"college2", "青少年(18+)"},
	{"pov", "第一人称视角"},
	{"dildos-toys", "假阴茎和性爱玩具"},
	{"fetish", "恋物"},
	{"deep-throat", "深喉"},
	{"big-butt", "大屁股"},
	{"cumshots", "射精"},
	{"mature", "熟女"},
	{"handjob", "手淫"},
	{"step-fantasy", "继亲幻想"},
	{"Oldie", "老人"},
	{"big-dick", "大鸡巴"},
	{"pantyhose", "丝袜"},
	{"threesomes", "三人行"},
	{"group-sex", "群交"},
	{"cunnilingus", "舔阴"},
	{"hardcore", "硬调色情"},
	{"wife", "妻子"},
	{"small-tits", "贫乳"},
	{"public", "公共场所"},
	{"lesbian", "女同"},
	{"bdsm", "虐恋"},
	{"anal", "肛交"},
	{"fingering", "指交"},
	{"facial", "射精在脸上"},
	{"gangbang", "群交"},
	{"massage", "按摩"},
	{"babes", "宝贝"},
	{"bandages-houtai", "绷带"},
	{"titty-fucking-paizuri", "山雀之间鸡巴"},
	{"cuckold", "妻子外遇"},
	{"casting", "试镜"},
	{"squirting", "潮吹"},
	{"masturbate", "自慰"},
	{"bathroom", "浴室"},
	{"solo-girl", "自慰"},
	{"doggy-style", "老汉推车"},
	{"outdoor", "户外"},
	{"college", "大学"},
	{"redhead", "红头发"},
	{"voyeur", "窥淫"},
	{"interracial", "跨种族"},
	{"office-lady-oh-el", "办公室"},
	{"couple", "夫妇"},
	{"foot-fetish", "恋足癖"},
	{"teacher-sensei", "性爱教师"},
	{"breast-play-oppaifechi", "乳房玩"},
	{"cosplay", "角色扮演"},
	{"bukkake", "颜射"},
	{"reality", "现实"},
	{"pissing", "小便"},
	{"big-natural-tits", "天然巨乳"},
	{"showers", "淋浴"},
	{"rimming", "舔肛"},
	{"blonde", "金发女郎"},
	{"dp", "双龙入洞"},
	{"femdom", "女王扮演"},
	{"shaved", "刮毛"},
	{"korean", "韩国"},
	{"natural-tits", "天然女性乳房"},
	{"panties", "女性内裤"},
	{"strapon", "捆绑"},
	{"skinny", "娇小"},
	{"slave", "奴隶"},
	{"tattoos", "纹身"},
	{"vintage", "经典老片"},
	{"close-up", "特写"},
	{"nipples", "乳头"},
	{"nurse-naasu", "看護師"},
	{"medical", "医疗人员"},
	{"hidden-cams", "隐藏摄像机"},
	{"upskirts", "裙底风光"},
	{"domination", "支配"},
	{"swallow", "吞精"},
	{"fisting", "拳交"},
	{"69", "69"},
	{"webcam", "网络摄像头"},
	{"pregnant", "孕妇"},
	{"muscular-man", "肌肉男"},
	{"bus", "公车痴汉"},
	{"face-sitting", "坐脸"},
	{"pick-up-nanpa", "即兴性爱"},
	{"cougar", "欲望熟女"},
	{"secretary", "秘书"},
	{"softcore", "软色情"},
	{"car", "汽车"},
	{"party", "派对"},
	{"gagging", "作呕"},
	{"striptease", "脱衣舞"},
	{"girlfriend", "女朋友"},
	{"shibari-bondage", "日式绑缚"},
	{"business-woman", "办公室女郎"},
	{"chinese", "中国人"},
	{"bikini", "ビキニ"},
	{"celebrities", "名人"},
	{"maid-meido", "女佣"},
	{"kimono", "和服"},
	{"swimsuit-mizugi", "泳装"},
	{"handcuffs", "手铐"},
	{"grannies", "老奶奶"},
	{"sports", "运动"},
	{"beach", "ビーチ"},
	{"latex", "乳胶紧身衣"},
	{"cfnm", "衣女裸男"},
	{"foot-job-ashifechi", "足交"},
	{"high-heels", "高跟鞋"},
	{"soap-play-soopufechi", "肥皂"},
	{"changing-room", "更衣室"},
	{"spanking", "打屁股"},
	{"hanging", "悬吊"},
	{"funny", "有趣的"},
	{"mistress", "女主人"},
	{"device-bondage", "器械束缚"},
	{"european", "欧洲人"},
	{"behind-the-scenes", "幕后花絮"},
	{"swingers", "交换伴侣"},
	{"cheerleaders", "啦啦队"},
	{"kitchen", "厨房"},
	{"interview", "访问"},
	{"fucking-machines", "性爱机器"},
	{"fishnet", "渔网"},
	{"gym-reotaado", "健身房"},
	{"yoga", "瑜伽"},
	{"cat-fights", "女女打架"},
	{"fake-tits", "假奶"},
	{"whipping", "鞭打"},
	{"pegging", "假阳具"},
	{"bisexual", "双性恋"},
	{"glory-hole", "偷窥孔"},
	{"medium-tits", "大乳头"},
	{"smoking", "吸烟"},
	{"hogtied", "四马绑缚"},
	{"live-shows", "现场表演"},
	{"race-queen-reesu-kuiin", "赛车女郎"},
	{"submissive", "顺从"},
	{"joint-fucking-wakifechi", "共同性爱"},
	{"shaving-manko-teimou", "刮"},
	{"bar", "酒吧"},
	{"russian", "俄罗斯人"},
}

var vjSnapshotName = func() map[string]string {
	m := map[string]string{}
	for _, c := range vjSnapshot {
		m[c.Dir] = c.Name
	}
	return m
}()

var vjFixed = []vjCat{
	{"latest", "最新视频"},
	{"toprated", "最佳视频"},
	{"popular", "热门视频"},
	{"uncensored", "无码专区"},
	{"models", "女优索引"},
	{"playlists", "播放列表"},
}

var vjSortOfFixed = map[string]string{
	"latest": "latest-updates", "toprated": "top-rated", "popular": "most-popular",
}

var (
	vjCatsMu    sync.Mutex
	vjCatsCache []vjCat
)

func vjGetJSON(path string) (map[string]interface{}, error) {
	var lastErr error
	for i := 0; i < 3; i++ {
		body, code, _, err := httpGet(vjHost+path, vjHeaders())
		if err == nil && code == 200 {
			var root map[string]interface{}
			if jerr := json.Unmarshal(body, &root); jerr == nil && root != nil {
				return root, nil
			} else if jerr != nil {
				lastErr = jerr
			}
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = fmt.Errorf("vjav http %d", code)
		}
	}
	return nil, lastErr
}

func vjFlexStr(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func vjNum(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	}
	return 0
}

// vjLiveCats mirrors py _categories_live(): live category table with
// zh-CN names, snapshot fallback when the live table is unusable.
func vjLiveCats() []vjCat {
	vjCatsMu.Lock()
	defer vjCatsMu.Unlock()
	if vjCatsCache != nil {
		return vjCatsCache
	}
	cats := []vjCat{}
	if data, err := vjGetJSON("/api/json/categories/14400/str.all.en.json"); err == nil {
		trans, _ := data["categories_translations"].(map[string]interface{})
		transData, _ := trans["data"].(map[string]interface{})
		if arr, ok := data["categories"].([]interface{}); ok {
			for _, cv := range arr {
				cm, ok := cv.(map[string]interface{})
				if !ok {
					continue
				}
				dir := vjFlexStr(cm["dir"])
				title := vjFlexStr(cm["title"])
				if dir == "" || title == "" {
					continue
				}
				name := ""
				if tv, ok := transData[dir].(map[string]interface{}); ok {
					name = strings.TrimSpace(vjFlexStr(tv["title_cn"]))
				}
				if name == "" {
					name = vjSnapshotName[dir]
				}
				if name == "" {
					name = title
				}
				cats = append(cats, vjCat{dir, name})
			}
		}
	}
	if len(cats) < 20 {
		cats = vjSnapshot
	}
	vjCatsCache = cats
	return cats
}

func vjCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range vjFixed {
		out = append(out, categoryObj(c.Dir, c.Name))
	}
	for _, c := range vjLiveCats() {
		if c.Dir == "jav-uncensored" {
			continue // already exposed as the fixed "uncensored" class
		}
		out = append(out, categoryObj("cat@@"+c.Dir, c.Name))
	}
	return out
}

func vjVideoCard(slot string, v map[string]interface{}, catName string) map[string]interface{} {
	vid := vjFlexStr(v["video_id"])
	title := vjFlexStr(v["title"])
	if title == "" {
		title = vid
	}
	pic, _ := v["scr"].(string)
	remark, _ := v["duration"].(string)
	return makeDrama(slot, vid, title, pic, remark, catName, "")
}

func vjListResult(slot string, data map[string]interface{}, page int, catName string) ([]interface{}, int, bool, error) {
	items := []interface{}{}
	if arr, ok := data["videos"].([]interface{}); ok {
		for _, vv := range arr {
			vm, ok := vv.(map[string]interface{})
			if !ok || vjFlexStr(vm["video_id"]) == "" {
				continue
			}
			items = append(items, vjVideoCard(slot, vm, catName))
		}
	}
	total := vjNum(data["total_count"])
	pagecount := vjNum(data["pages"])
	if pagecount == 0 && total > 0 {
		pagecount = (total + vjPageSize - 1) / vjPageSize
	}
	if total == 0 {
		total = len(items)
	}
	hasMore := false
	if pagecount > 0 {
		hasMore = page < pagecount
	} else {
		hasMore = len(items) >= vjPageSize
	}
	return items, total, hasMore, nil
}

// vjVideos: /api/json/videos2/86400/str/{sort}/{count}/{section}.{oid}.{page}.all...json
func vjVideos(slot, section, oid string, page int, sortV, catName string) ([]interface{}, int, bool, error) {
	path := fmt.Sprintf("/api/json/videos2/86400/str/%s/%d/%s.%s.%d.all...json", sortV, vjPageSize, section, oid, page)
	data, err := vjGetJSON(path)
	if err != nil {
		return nil, 0, false, err
	}
	return vjListResult(slot, data, page, catName)
}

func vjModelsFolder(slot string, page int) ([]interface{}, int, bool, error) {
	path := fmt.Sprintf("/api/json/models/86400/str/filt........../most-popular/%d/%d.json", vjModelsPageSize, page)
	data, err := vjGetJSON(path)
	if err != nil {
		return nil, 0, false, err
	}
	items := []interface{}{}
	if arr, ok := data["models"].([]interface{}); ok {
		for _, mv := range arr {
			mm, ok := mv.(map[string]interface{})
			if !ok || vjFlexStr(mm["dir"]) == "" {
				continue
			}
			dir := vjFlexStr(mm["dir"])
			title := vjFlexStr(mm["title"])
			if title == "" {
				title = dir
			}
			img, _ := mm["img"].(string)
			remark := ""
			if st, ok := mm["statistics"].(map[string]interface{}); ok {
				remark = fmt.Sprintf("%s 部作品", vjFlexStr(st["videos"]))
			}
			items = append(items, makeDrama(slot, "model@@"+dir, title, img, remark, "女优索引", ""))
		}
	}
	total := vjNum(data["total_count"])
	pagecount := 0
	if total > 0 {
		pagecount = (total + vjModelsPageSize - 1) / vjModelsPageSize
	}
	if total == 0 {
		total = len(items)
	}
	return items, total, pagecount > 0 && page < pagecount, nil
}

func vjPlaylistsFolder(slot string, page int) ([]interface{}, int, bool, error) {
	path := fmt.Sprintf("/api/json/playlists/86400/most-popular/%d/all.%d.str..json", vjPageSize, page)
	data, err := vjGetJSON(path)
	if err != nil {
		return nil, 0, false, err
	}
	items := []interface{}{}
	if arr, ok := data["playlists"].([]interface{}); ok {
		for _, pv := range arr {
			pm, ok := pv.(map[string]interface{})
			if !ok || vjFlexStr(pm["playlist_id"]) == "" {
				continue
			}
			pid := vjFlexStr(pm["playlist_id"])
			title := vjFlexStr(pm["title"])
			if title == "" {
				title = pid
			}
			cover := ""
			if thumbs, ok := pm["thumbs"].([]interface{}); ok && len(thumbs) > 0 {
				cover = vjFlexStr(thumbs[0])
			}
			remark := ""
			if st, ok := pm["statistics"].(map[string]interface{}); ok {
				remark = fmt.Sprintf("%s 部视频", vjFlexStr(st["videos"]))
			}
			items = append(items, makeDrama(slot, "playlist@@"+pid, title, cover, remark, "播放列表", ""))
		}
	}
	total := vjNum(data["total_count"])
	pagecount := 0
	if total > 0 {
		pagecount = (total + vjPageSize - 1) / vjPageSize
	}
	if total == 0 {
		total = len(items)
	}
	return items, total, pagecount > 0 && page < pagecount, nil
}

func vjCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "models":
		return vjModelsFolder(slot, page)
	case category == "playlists":
		return vjPlaylistsFolder(slot, page)
	case strings.HasPrefix(category, "model@@"):
		return vjVideos(slot, "model", strings.TrimPrefix(category, "model@@"), page, "latest-updates", "女优")
	case strings.HasPrefix(category, "playlist@@"):
		return vjVideos(slot, "playlist", strings.TrimPrefix(category, "playlist@@"), page, "latest-updates", "播放列表")
	case strings.HasPrefix(category, "cat@@"):
		dir := strings.TrimPrefix(category, "cat@@")
		return vjVideos(slot, "categories", dir, page, "latest-updates", vjSnapshotName[dir])
	case category == "uncensored":
		return vjVideos(slot, "categories", "jav-uncensored", page, "latest-updates", "无码专区")
	case vjSortOfFixed[category] != "":
		return vjVideos(slot, "", "", page, vjSortOfFixed[category], category)
	default:
		return vjVideos(slot, "", "", page, "latest-updates", "最新视频")
	}
}

var vjDigitsRe = regexp.MustCompile(`\d+`)

func vjDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "model@@") || strings.HasPrefix(rawID, "playlist@@") {
		// v7 (hotfix, user device report on v6): embed the folder's
		// videos as titled episodes (pages 1..3, same source as the
		// catalog categories). The v6 stub (zero episodes, category
		// = rawID) was untappable in the app: card taps dispatch
		// detail -> episodes only, zero episodes aborts the tap.
		section, oid, catName := "model", strings.TrimPrefix(rawID, "model@@"), "女优"
		if strings.HasPrefix(rawID, "playlist@@") {
			section, oid, catName = "playlist", strings.TrimPrefix(rawID, "playlist@@"), "播放列表"
		}
		var items []interface{}
		for pg := 1; pg <= 3; pg++ {
			pi, _, _, perr := vjVideos(slot, section, oid, pg, "latest-updates", catName)
			if perr != nil {
				if pg == 1 {
					return nil, perr
				}
				break
			}
			items = append(items, pi...)
			if len(pi) == 0 {
				break
			}
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("vjav: folder empty: %s", rawID)
		}
		title := oid
		cover := ""
		if st := getStore(slot).findDrama(rawID); st != nil {
			if t, _ := st["title"].(string); t != "" {
				title = t
			}
			if c, _ := st["cover"].(string); c != "" {
				cover = c
			}
		}
		if cover == "" {
			if m0, ok := items[0].(map[string]interface{}); ok {
				cover, _ = m0["cover"].(string)
			}
		}
		d := makeDrama(slot, rawID, title, cover, fmt.Sprintf("%d 部作品", len(items)), catName,
			"选集即作品列表（集名=片名，点开即播）")
		chs := []interface{}{}
		for i, it := range items {
			im, _ := it.(map[string]interface{})
			wraw, _ := im["vod_id"].(string)
			wtitle, _ := im["title"].(string)
			if wraw == "" {
				continue
			}
			chs = append(chs, g2Chapter(slot, rawID, wraw, wtitle, i+1))
		}
		g2SetChapters(d, chs)
		return d, nil
	}
	if rawID == "models" || rawID == "playlists" {
		// Index roots are category ids, not cards; defensive stub.
		d := makeDrama(slot, rawID, rawID, "", "索引", "VJAV", "")
		g2ZeroEpisodes(d)
		return d, nil
	}
	m := vjDigitsRe.FindString(rawID)
	if m == "" {
		return nil, fmt.Errorf("vjav: bad id")
	}
	vid := m
	n, _ := strconv.ParseInt(vid, 10, 64)
	bucket := fmt.Sprintf("%d/%d", (n/1000000)*1000000, (n/1000)*1000)
	data, err := vjGetJSON(fmt.Sprintf("/api/json/video/86400/%s/%s.json", bucket, vid))
	if err != nil {
		// py still returns a card pointing at the watch page.
		return makeDrama(slot, vid, vid, "", "", "VJAV", ""), nil
	}
	v, _ := data["video"].(map[string]interface{})
	if v == nil {
		return makeDrama(slot, vid, vid, "", "", "VJAV", ""), nil
	}
	title := vjFlexStr(v["title"])
	if title == "" {
		title = vid
	}
	pic := vjFlexStr(v["thumbsrc"])
	if pic == "" {
		pic = vjFlexStr(v["thumb"])
	}
	remark := vjFlexStr(v["duration"])
	catNames := []string{}
	if cats, ok := v["categories"].(map[string]interface{}); ok {
		keys := []string{}
		for k := range cats {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			cm, ok := cats[k].(map[string]interface{})
			if !ok {
				continue
			}
			dir := vjFlexStr(cm["dir"])
			name := vjSnapshotName[dir]
			if name == "" {
				name = vjFlexStr(cm["title"])
			}
			if name != "" {
				catNames = append(catNames, name)
			}
		}
	}
	modelNames := []string{}
	if models, ok := v["models"].(map[string]interface{}); ok {
		keys := []string{}
		for k := range models {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			mm, ok := models[k].(map[string]interface{})
			if ok {
				if t := vjFlexStr(mm["title"]); t != "" {
					modelNames = append(modelNames, t)
				}
			}
		}
	}
	if len(modelNames) == 0 {
		if arr, ok := v["models_suggested"].([]interface{}); ok {
			for _, nv := range arr {
				if s := vjFlexStr(nv); s != "" {
					modelNames = append(modelNames, s)
				}
			}
		}
	}
	desc := vjFlexStr(v["description"])
	d := makeDrama(slot, vid, title, pic, remark, strings.Join(catNames, ", "), desc)
	actor := strings.Join(modelNames, ", ")
	d["actor"] = actor
	d["vod_actor"] = actor
	if pd := vjFlexStr(v["post_date"]); len(pd) >= 4 {
		d["year"] = pd[:4]
		d["vod_year"] = pd[:4]
	}
	return d, nil
}

// vjDecode reverses videofile.php's video_url obfuscation, byte-for-
// byte the py _decode_videofile_url: a base64 variant whose alphabet
// is the latin order with ".,~" in place of "+/=" (indices 62,63,64 —
// 64/'~' doubles as the padding sentinel), with indices 0,1,2,4,12
// swapped for cyrillic homoglyphs А В С Е М.
func vjDecode(enc string) string {
	alpha := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789.,~")
	alpha[0] = 'А'  // U+0410
	alpha[1] = 'В'  // U+0412
	alpha[2] = 'С'  // U+0421
	alpha[4] = 'Е'  // U+0415
	alpha[12] = 'М' // U+041C
	idx := map[rune]int{}
	for i, r := range alpha {
		idx[r] = i
	}
	clean := []rune{}
	for _, ch := range enc {
		if _, ok := idx[ch]; ok {
			clean = append(clean, ch)
		}
	}
	out := []byte{}
	for i := 0; i < len(clean); i += 4 {
		at := func(pos int) int {
			if pos >= len(clean) {
				return 64
			}
			return idx[clean[pos]]
		}
		o, a, b, c := at(i), at(i+1), at(i+2), at(i+3)
		out = append(out, byte((o<<2)|(a>>4)))
		if b != 64 {
			out = append(out, byte(((15&a)<<4)|(b>>2)))
		}
		if c != 64 {
			out = append(out, byte(((3&b)<<6)|c))
		}
	}
	text := string(out)
	if strings.Contains(text, "%") {
		if dec, err := url.PathUnescape(text); err == nil {
			text = dec
		}
	}
	return text
}

var vjWatchRe = regexp.MustCompile(`/videos/(\d+)`)

func vjResolve(slot, rawID string) (map[string]interface{}, error) {
	u := strings.TrimSpace(rawID)
	real := ""
	if strings.Contains(u, "get_file") || strings.HasSuffix(strings.SplitN(u, "?", 2)[0], ".mp4") {
		real = u
	} else {
		vid := ""
		if m := vjWatchRe.FindStringSubmatch(u); m != nil {
			vid = m[1]
		} else if vjDigitsRe.MatchString(u) && !strings.Contains(u, "/") {
			vid = vjDigitsRe.FindString(u)
		}
		if vid != "" {
			// Fresh fetch at play time: the signed URL lives ~24h.
			data, err := func() (interface{}, error) {
				body, code, _, herr := httpGet(fmt.Sprintf("%s/api/videofile.php?video_id=%s&lifetime=8640000", vjHost, vid), vjHeaders())
				if herr != nil || code != 200 {
					return nil, fmt.Errorf("videofile http %d err %v", code, herr)
				}
				var arr []interface{}
				if jerr := json.Unmarshal(body, &arr); jerr != nil {
					return nil, jerr
				}
				return arr, nil
			}()
			if err == nil {
				arr, _ := data.([]interface{})
				var chosen map[string]interface{}
				for _, ev := range arr {
					em, ok := ev.(map[string]interface{})
					if ok && vjFlexStr(em["is_default"]) == "1" {
						chosen = em
						break
					}
				}
				if chosen == nil && len(arr) > 0 {
					chosen, _ = arr[0].(map[string]interface{})
				}
				if chosen != nil {
					if vu := vjFlexStr(chosen["video_url"]); vu != "" {
						if dec := vjDecode(vu); dec != "" {
							real = dec
							if !strings.HasPrefix(real, "http") {
								real = vjHost + real
							}
						}
					}
				}
			}
		}
	}
	if real == "" {
		return nil, fmt.Errorf("vjav: no play url")
	}
	format, proto := "mp4", "file"
	if strings.Contains(real, ".m3u8") {
		format, proto = "m3u8", "hls"
	}
	return playResult(real, slot, rawID, vjUA, vjHost+"/", format, proto), nil
}

func vjSearch(slot, kw string) ([]interface{}, error) {
	// KVS search endpoint; keywords <2 chars honestly return the
	// site's videos_not_found (empty list), as in py.
	path := "/api/videos2.php?params=86400/str/relevance/24/search..1.all...&s=" + url.QueryEscape(kw)
	data, err := vjGetJSON(path)
	if err != nil {
		return []interface{}{}, err
	}
	if e := vjFlexStr(data["error"]); e != "" {
		return []interface{}{}, nil
	}
	items, _, _, err := vjListResult(slot, data, 1, "搜索")
	return items, err
}
