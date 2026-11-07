package seku

// ---- 黄果短剧 huangguo (yh_huangguo) — H5Native HTML + 播放 JSON ----
//
// 配方来源：欲火 APK jadx 实证（模块 huangguoh5Native）
//   基址：HuangGuoGuideActivity 生成 https://{大学前缀}.{gkudvxhjh.cc|
//   ngfxaxnp.cc|xxpofweu.cc}（前缀取自 thu/pku/fdu/sjtu/zju/nju/xmu/
//   sysu/whu/jlu/scu/ruc/bnu/nku/tju/sdu/hust/xjtu/hit）；h6.b 默认
//   f8255k0 = https://xjtu.huxrzdjnv.cc。
//   导航（p2.a 抽屉，1:1）：首页 ""、AI成人短剧 /ai-duanju、AI成人
//   漫剧 /ai-manju、AI换脸 /ai-huanlian、AI魔改 /ai-mogai、排行榜
//   /ranks/hot；翻页 = 路径 + "/{页}"（HGFragment.j）；搜索 =
//   /search/video/{kw}（+ "/{页}"）。
//   列表解析：u2.a.b（jadx 方法体被跳过未反编译）——本件按其详情
//   解析器 u2.a.c 的选择器族（.hg-drama-card__*、.hg-web-detail__*、
//   .hg-play__*）+ 活体页面卡片结构（a[href=/video/{id}/] + 图片 +
//   标题 + 更新至N集/评分）以正则重建。
//   详情：GET {base}/video/{id}，按 u2.a.c 逐条复刻：标题取 h1 →
//   [data-play-page][data-track-video-title] → .hg-web-detail__title
//   族；简介取 p.hg-web-detail__desc 族 → meta description；选集取
//   .hg-web-detail__ep-grid a / .hg-play__ep-item / a[data-ep-id]，
//   标签按「第\s*(\d+)\s*集」/纯数字/链接 /ep-(\d+)/ 归一化为第N集，
//   无选集且含 .hg-web-detail__play 时只给第1集，再无则按「更新至N集」
//   生成 1..N 集。
//   播放：ChapterAdapter.watch 调 GET {base}/api/videos/{id}/play?ep={n}
//   → s2.b 取 JSON {data:{video_url}}（本件只取该字段；同一 JSON
//   的 video_api/video_iv/video_key 在活体验本中均为空串）。
//   付费：bean 含 money/pay_type 字段；本件 Resolve 在 video_url 为
//   空或 status!=1 时直接报错，不解析付费/加密变体。
//
// 验证门（逐腿，2026-10-24，Go 冒烟驱动实测通过，见回执）：
//   Categories：导航 6 项与 p2.a 抽屉 1:1 ✔（jadx + 冒烟 categories=6；
//     站方顶栏另有专题/吃瓜/回家的路三项，App 抽屉无此三项，故不入）。
//   Catalog：频道类目走站方页面脚本实测的 JSON API
//     /api/videos/category/{channel}?size=21&page=N → 冒烟 ai-duanju
//     p1 出 21 卡、total=1244、more=true，p2 出 21 张不同卡 ✔；
//     首页与排行页无公开 API，取服务端渲染子集（JSON-LD 主 feed
//     22 条 + 静态猜你喜欢卡，首页冒烟 25 条、排行 23 条），首页 /2
//     实测 404 故首页翻页返回空——子集口径如实标注，不冒充全量。
//   封面：pic.drleao.cn 直链 .jpg/.jpeg（App 以 Glide 明文加载，
//     无 CoverFetch 需求）；冒烟拉首卡封面回 200、170320 字节、
//     首字节 FF D8（JPEG 魔数）✔；首页 JSON-LD 条目本身无封面，
//     封面为空属实测形态，不编造。
//   Detail：/video/8675/ 活体：标题、更新至8集、播放次数 459.7w、
//     单集片长 2:49、题材 AI成人短剧·现代、评分 9.1、剧情简介齐全；
//     冒烟 Detail 8675 选集=8、封面为该剧真封面 ✔。
//   Resolve：/api/videos/8675/play?ep=1 活体返回 status=1，
//     data.video_url = https://yd-hls.hzjvlo.cn/videos5/...m3u8?
//     auth_key=...；冒烟现取 m3u8 拉回 200、9620 字节、以 #EXTM3U
//     起首（AES-128 段加密，key 在 tp2.drleao.cn，属站方正常形态），
//     ep8 同样出真地址 ✔ 播放清单字节。
//   Search：冒烟「重生」出 30 条正文结果（meta 称 82 部，首屏子集；
//     搜索建议面板已截除，不混入猜你喜欢）✔。

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

const yhHGUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"

// yhHGBases：首位为 h6.b 默认基址，次位为 2026-10-24 浏览器活体
// 观测到的跳转后镜像（xjtu.huxrzdjnv.cc → ccaed.h2na6qa39.cc），
// 其余按 HuangGuoGuideActivity 的前缀×域名组合展开。
var yhHGBases = []string{
	"https://xjtu.huxrzdjnv.cc",
	"https://ccaed.h2na6qa39.cc",
}

var yhHGPrefs = []string{"thu", "pku", "fdu", "sjtu", "zju", "nju", "xmu", "sysu", "whu", "jlu", "scu", "ruc", "bnu", "nku", "tju", "sdu", "hust", "hit"}
var yhHGDomains = []string{"gkudvxhjh.cc", "ngfxaxnp.cc", "xxpofweu.cc"}

var yhHGBaseMu sync.Mutex
var yhHGBase string

func yhHGHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      yhHGUA,
		"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"Accept-Language": "zh-CN,zh;q=0.9",
	}
}

func yhHGCandidates() []string {
	out := append([]string{}, yhHGBases...)
	for _, p := range yhHGPrefs {
		for _, d := range yhHGDomains {
			out = append(out, "https://"+p+"."+d)
		}
	}
	return out
}

// yhHGProbeBase：命中第一个返回 200 且含黄果/短剧标记的基址。
func yhHGProbeBase() string {
	yhHGBaseMu.Lock()
	defer yhHGBaseMu.Unlock()
	if yhHGBase != "" {
		return yhHGBase
	}
	for _, b := range yhHGCandidates() {
		body, code, _, err := httpGetTimeout(b+"/", yhHGHeaders(), 10000000000)
		if err != nil || code != 200 {
			continue
		}
		s := string(body)
		if strings.Contains(s, "黄果") || strings.Contains(s, "hg-drama-card") || strings.Contains(s, "hg-web-detail") {
			yhHGBase = b
			return b
		}
	}
	return yhHGBases[0]
}

func yhHGFetch(path string) (string, error) {
	base := yhHGProbeBase()
	body, code, _, err := httpGet(base+path, yhHGHeaders())
	if err != nil || code != 200 {
		// 基址失效时清缓存并重试一次（镜像轮换常见）。
		yhHGBaseMu.Lock()
		yhHGBase = ""
		yhHGBaseMu.Unlock()
		base = yhHGProbeBase()
		body, code, _, err = httpGet(base+path, yhHGHeaders())
		if err != nil {
			return "", err
		}
		if code != 200 {
			return "", fmt.Errorf("yh_huangguo http %d for %s", code, path)
		}
	}
	return string(body), nil
}

var yhHGTagRe = regexp.MustCompile(`(?is)<[^>]+>`)
var yhHGSpaceRe = regexp.MustCompile(`\s+`)

func yhHGText(s string) string {
	s = yhHGTagRe.ReplaceAllString(s, " ")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&quot;", `"`)
	s = strings.ReplaceAll(s, "&#39;", "'")
	return strings.TrimSpace(yhHGSpaceRe.ReplaceAllString(s, " "))
}

func yhHGAttr(tag, name string) string {
	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"`)
	if m := re.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	re2 := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(name) + `\s*=\s*'([^']*)'`)
	if m := re2.FindStringSubmatch(tag); m != nil {
		return m[1]
	}
	return ""
}

var yhHGCardSplitRe = regexp.MustCompile(`<div class="hg-drama-card">`)
var yhHGVideoIDRe = regexp.MustCompile(`/video/(\d+)/?`)
var yhHGImgRe = regexp.MustCompile(`(?is)<img[^>]+>`)
var yhHGScoreRe = regexp.MustCompile(`(\d+\.\d+分)`)
var yhHGEpisodeRe = regexp.MustCompile(`(更新至\d+集|全\d+集)`)

// yhHGParseList：按活体卡片结构解析列表页（u2.a.b 的正则重建，
// 2026-10-24 实测 HTML 校准）：每块 <div class="hg-drama-card">
// 内含 cover-link（img data-src 为真封面，src 仅为占位图）、
// __score（评分）、__episode（更新至N集）、__title、__desc、
// __tags。搜索建议区的同类链接不在 hg-drama-card 块内，故不混入。
func yhHGParseList(slot, html string) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	parts := yhHGCardSplitRe.Split(html, -1)
	for _, block := range parts[1:] {
		idm := yhHGVideoIDRe.FindStringSubmatch(block)
		if idm == nil {
			continue
		}
		id := idm[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		cover, alt := "", ""
		if im := yhHGImgRe.FindString(block); im != "" {
			cover = yhHGAttr(im, "data-src")
			if cover == "" {
				if s := yhHGAttr(im, "src"); !strings.Contains(s, "cover-placeholder") {
					cover = s
				}
			}
			alt = yhHGAttr(im, "alt")
		}
		title := ""
		if tm := regexp.MustCompile(`(?is)<div class="hg-drama-card__title"><a[^>]*>(.*?)</a>`).FindStringSubmatch(block); tm != nil {
			title = yhHGText(tm[1])
			title = strings.TrimSuffix(title, "全集在线观看")
			title = strings.TrimSpace(title)
		}
		if title == "" {
			title = alt
		}
		if title == "" {
			continue
		}
		remarkParts := []string{}
		if sm := yhHGScoreRe.FindString(block); sm != "" {
			remarkParts = append(remarkParts, sm)
		}
		if em := yhHGEpisodeRe.FindString(block); em != "" {
			remarkParts = append(remarkParts, em)
		}
		desc := ""
		if dm := regexp.MustCompile(`(?is)<p class="hg-drama-card__desc">(.*?)</p>`).FindStringSubmatch(block); dm != nil {
			desc = yhHGText(dm[1])
		}
		tags := []string{}
		if tg := regexp.MustCompile(`(?is)<div class="hg-drama-card__tags">(.*?)</div>`).FindStringSubmatch(block); tg != nil {
			for _, tm := range regexp.MustCompile(`(?is)<span class="hg-tag">(.*?)<span`).FindAllStringSubmatch(tg[1], -1) {
				if s := yhHGText(tm[1]); s != "" {
					tags = append(tags, s)
				}
			}
		}
		out = append(out, makeDrama(slot, id, title, cover, strings.Join(remarkParts, " · "), strings.Join(tags, " "), desc))
	}
	return out
}

var yhHGCats = []struct{ id, name string }{
	{"", "首页"},
	{"/ai-duanju", "AI成人短剧"},
	{"/ai-manju", "AI成人漫剧"},
	{"/ai-huanlian", "AI换脸"},
	{"/ai-mogai", "AI魔改"},
	{"/ranks/hot", "排行榜"},
}

func yhHGCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range yhHGCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

// yhHGAPICards：频道 JSON API（站方页面脚本实测：
// /api/videos/category/{channel}?size=21[&page=N] →
// {data:{items:[{id,title,cover,description,score,episode_count,
// total_episodes,is_finished,tags}],pagination:{page,pages,total}}}）。
func yhHGAPICards(slot, channel string, page int) ([]interface{}, int, bool, error) {
	body, err := yhHGFetch("/api/videos/category/" + channel + fmt.Sprintf("?size=21&page=%d", page))
	if err != nil {
		return nil, 0, false, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return nil, 0, false, fmt.Errorf("yh_huangguo api parse: %v", err)
	}
	if st, _ := obj["status"].(float64); st != 1 {
		return nil, 0, false, fmt.Errorf("yh_huangguo api status %v: %v", obj["status"], obj["msg"])
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, 0, false, fmt.Errorf("yh_huangguo api empty data")
	}
	out := []interface{}{}
	for _, e := range yhArr(data["items"]) {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		id := ""
		if s := g5StrVal(rec["id"]); s != "" {
			id = s
		} else if f, ok := rec["id"].(float64); ok {
			id = strconv.Itoa(int(f))
		}
		title := g5StrVal(rec["title"])
		if id == "" || title == "" {
			continue
		}
		remarkParts := []string{}
		if sc, ok := rec["score"].(float64); ok && sc > 0 {
			remarkParts = append(remarkParts, fmt.Sprintf("%.1f分", sc))
		}
		finished := false
		if b, ok := rec["is_finished"].(bool); ok {
			finished = b
		}
		ep, _ := g5MapInt(rec, "episode_count")
		tot, _ := g5MapInt(rec, "total_episodes")
		if finished && tot > 0 {
			remarkParts = append(remarkParts, fmt.Sprintf("全%d集", tot))
		} else if ep > 0 {
			remarkParts = append(remarkParts, fmt.Sprintf("更新至%d集", ep))
		}
		tags := []string{}
		for _, t := range yhArr(rec["tags"]) {
			if s := g5StrVal(t); s != "" {
				tags = append(tags, s)
			}
		}
		out = append(out, makeDrama(slot, id, title, g5StrVal(rec["cover"]), strings.Join(remarkParts, " · "), strings.Join(tags, " "), g5StrVal(rec["description"])))
	}
	total, _ := g5MapInt(yhMap(data["pagination"]), "total")
	hasMore := false
	if pg := yhMap(data["pagination"]); pg != nil {
		if pages, ok := g5MapInt(pg, "pages"); ok {
			hasMore = page < pages
		}
	}
	if total == 0 {
		hasMore = len(out) == 21
	}
	return out, total, hasMore, nil
}

var yhHGJSONLDRe = regexp.MustCompile(`"name":"([^"]+)","position":\d+,"url":"[^"]*/video/(\d+)/"`)

// yhHGHTMLItems：首页/排行/搜索的服务端渲染子集——JSON-LD
// ItemList（标题+id，无封面）+ 静态 hg-drama-card 块（带封面）。
// 站方主网格由 JS 调分类 API 渲染，首页/排行/搜索页无对应公开
// API，故此处只给服务端可见的真子集，不编造。
func yhHGHTMLItems(slot, html string) []interface{} {
	out := []interface{}{}
	byID := map[string]int{}
	push := func(id, title, cover, remark, cat, desc string) {
		if id == "" || title == "" {
			return
		}
		if i, ok := byID[id]; ok {
			if cover != "" {
				if m, ok := out[i].(map[string]interface{}); ok {
					m["cover"], m["coverUrl"], m["vod_pic"], m["pic"], m["thumb"] = cover, cover, cover, cover, cover
				}
			}
			return
		}
		byID[id] = len(out)
		out = append(out, makeDrama(slot, id, title, cover, remark, cat, desc))
	}
	for _, m := range yhHGJSONLDRe.FindAllStringSubmatch(html, -1) {
		push(m[2], m[1], "", "", "", "")
	}
	for _, c := range yhHGParseList(slot, html) {
		if m, ok := c.(map[string]interface{}); ok {
			push(g5StrVal(m["vod_id"]), g5StrVal(m["title"]), g5StrVal(m["cover"]), g5StrVal(m["remark"]), g5StrVal(m["category"]), g5StrVal(m["desc"]))
		}
	}
	return out
}

func yhHGCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch category {
	case "", "/ranks/hot":
		// 首页 feed 实测无 /2（404）：首页为单页；排行页同理按
		// 服务端渲染子集返回，翻页返回空。
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		path := "/"
		if category != "" {
			path = category
		}
		html, err := yhHGFetch(path)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhHGHTMLItems(slot, html)
		return items, len(items), false, nil
	default:
		channel := strings.Trim(category, "/")
		return yhHGAPICards(slot, channel, page)
	}
}

// yhHGDetailParse：u2.a.c 的正则复刻。
func yhHGDetailParse(html string) (title, desc, cover string, eps int) {
	if m := regexp.MustCompile(`(?is)<h1[^>]*>(.*?)</h1>`).FindStringSubmatch(html); m != nil {
		title = yhHGText(m[1])
	}
	if title == "" {
		if m := regexp.MustCompile(`(?is)<[^>]+data-track-video-title="([^"]+)"`).FindStringSubmatch(html); m != nil {
			title = m[1]
		}
	}
	if title == "" {
		if m := regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`).FindStringSubmatch(html); m != nil {
			title = yhHGText(m[1])
			if i := strings.Index(title, " - "); i >= 0 {
				title = strings.TrimSpace(title[:i])
			}
		}
	}
	for _, cls := range []string{"hg-web-detail__desc", "hg-play__panel-desc", "hg-drama-card__desc"} {
		re := regexp.MustCompile(`(?is)<[^>]+class="[^"]*` + cls + `[^"]*"[^>]*>(.*?)</[^>]+>`)
		if m := re.FindStringSubmatch(html); m != nil {
			desc = yhHGText(m[1])
			if desc != "" {
				break
			}
		}
	}
	if desc == "" {
		if m := regexp.MustCompile(`(?is)<meta[^>]+name="description"[^>]+content="([^"]*)"`).FindStringSubmatch(html); m != nil {
			desc = m[1]
		}
	}
	if m := regexp.MustCompile(`(?is)<img[^>]+(?:data-src|src)="(https://pic\.drleao\.cn[^"]+)"`).FindStringSubmatch(html); m != nil {
		cover = m[1]
	}
	if cover == "" {
		if m := regexp.MustCompile(`(?is)<meta[^>]+property="og:image"[^>]+content="([^"]*)"`).FindStringSubmatch(html); m != nil {
			cover = m[1]
		}
	}
	// 选集：策略 1（ep 锚点）→ 策略 2（单集播放页）→ 策略 3（更新至N集）。
	epSet := map[int]bool{}
	for _, m := range regexp.MustCompile(`(?is)<a[^>]+>(.*?)</a>`).FindAllStringSubmatch(html, -1) {
		tag := m[0]
		if !strings.Contains(tag, "data-ep-id") && !strings.Contains(tag, "ep-item") && !strings.Contains(tag, "ep-grid") && !strings.Contains(tag, "/ep-") {
			continue
		}
		if v := yhHGAttr(tag, "data-ep-id"); v != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
				epSet[n] = true
				continue
			}
		}
		if mm := regexp.MustCompile(`/ep-(\d+)/?`).FindStringSubmatch(tag); mm != nil {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				epSet[n] = true
				continue
			}
		}
		txt := yhHGText(m[1])
		if mm := regexp.MustCompile(`第\s*(\d+)\s*集`).FindStringSubmatch(txt); mm != nil {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				epSet[n] = true
			}
		} else if mm := regexp.MustCompile(`^\s*(\d+)\s*$`).FindStringSubmatch(txt); mm != nil {
			if n, err := strconv.Atoi(mm[1]); err == nil {
				epSet[n] = true
			}
		}
	}
	if len(epSet) > 0 {
		nums := []int{}
		for n := range epSet {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		eps = nums[len(nums)-1]
		return
	}
	if strings.Contains(html, "hg-web-detail__play") || strings.Contains(html, "data-play-id") {
		eps = 1
		return
	}
	if m := regexp.MustCompile(`更新至(\d+)集`).FindStringSubmatch(html); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			eps = n
			return
		}
	}
	if m := regexp.MustCompile(`全(\d+)集`).FindStringSubmatch(html); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			eps = n
			return
		}
	}
	eps = 1
	return
}

func yhHGDetail(slot, rawID string) (map[string]interface{}, error) {
	id, _ := splitEpisodeSuffix(rawID)
	html, err := yhHGFetch("/video/" + id)
	if err != nil {
		return nil, err
	}
	title, desc, cover, eps := yhHGDetailParse(html)
	if title == "" {
		title = "视频" + id
	}
	if st := getStore(slot).findDrama(id); st != nil {
		if s := g5StrVal(st["title"]); s != "" {
			title = s
		}
		if cover == "" {
			cover = g5StrVal(st["cover"])
		}
		if desc == "" {
			desc = g5StrVal(st["desc"])
		}
	}
	d := makeDrama(slot, id, title, cover, "", "", desc)
	if eps < 1 {
		eps = 1
	}
	epIDs := []string{}
	epNames := []string{}
	for i := 1; i <= eps; i++ {
		epIDs = append(epIDs, fmt.Sprintf("%s#%d", id, i))
		epNames = append(epNames, fmt.Sprintf("第%d集", i))
	}
	g5SetChapters(d, slot, id, epIDs, epNames)
	return d, nil
}

func yhHGResolve(slot, rawID string) (map[string]interface{}, error) {
	id, epIdx := splitEpisodeSuffix(rawID)
	if epIdx < 1 {
		epIdx = 1
	}
	body, err := yhHGFetch(fmt.Sprintf("/api/videos/%s/play?ep=%d", id, epIdx))
	if err != nil {
		return nil, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		return nil, fmt.Errorf("yh_huangguo play parse: %v", err)
	}
	if st, _ := obj["status"].(float64); st != 1 {
		return nil, fmt.Errorf("yh_huangguo play status %v: %v", obj["status"], obj["msg"])
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_huangguo play empty data")
	}
	u := g5StrVal(data["video_url"])
	if u == "" {
		return nil, fmt.Errorf("yh_huangguo play empty video_url (paid/encrypted variant not resolved)")
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhHGUA, yhHGProbeBase()+"/", format, protocol), nil
}

func yhHGSearch(slot, kw string) ([]interface{}, error) {
	html, err := yhHGFetch("/search/video/" + url.PathEscape(kw) + "/")
	if err != nil {
		return nil, err
	}
	// 正文在 </header> 之后：搜索建议面板（热搜+猜你喜欢）在 header
	// 内，截掉它，只取正文结果区，避免猜你喜欢混入结果首位。
	body := html
	if i := strings.Index(html, "</header>"); i >= 0 {
		body = html[i:]
	}
	items := []interface{}{}
	seen := map[string]bool{}
	push := func(id, title string) {
		if id == "" || title == "" || seen[id] {
			return
		}
		seen[id] = true
		items = append(items, makeDrama(slot, id, title, "", "", "", ""))
	}
	// 结果链接两种形态：标题锚「标题 全集在线观看」、封面 sr-only
	// 「《标题》题材」；正文区内两者同 id 去重。
	for _, m := range regexp.MustCompile(`(?is)<a[^>]+href="/video/(\d+)/"[^>]*>(.*?)</a>`).FindAllStringSubmatch(body, -1) {
		id, raw := m[1], yhHGText(m[2])
		title := ""
		if strings.HasSuffix(raw, "全集在线观看") {
			title = strings.TrimSpace(strings.TrimSuffix(raw, "全集在线观看"))
		} else if strings.HasPrefix(raw, "《") {
			if j := strings.Index(raw, "》"); j > 0 {
				title = raw[len("《"):j]
			}
		}
		if title == "" || strings.Contains(title, "黄果") {
			continue
		}
		push(id, title)
	}
	for _, c := range yhHGParseList(slot, body) {
		if m, ok := c.(map[string]interface{}); ok {
			id, cover := g5StrVal(m["vod_id"]), g5StrVal(m["cover"])
			push(id, g5StrVal(m["title"]))
			if cover != "" {
				for _, it := range items {
					if mm, ok := it.(map[string]interface{}); ok && g5StrVal(mm["vod_id"]) == id {
						mm["cover"], mm["coverUrl"], mm["vod_pic"], mm["pic"], mm["thumb"] = cover, cover, cover, cover, cover
					}
				}
			}
		}
	}
	return items, nil
}

func init() {
	registerSite("yh_huangguo", &siteImpl{
		Key:        "yh_huangguo",
		Name:       "黄果",
		DefaultCat: "",
		HomeURL:    "https://xjtu.huxrzdjnv.cc/",
		Categories: yhHGCategories,
		Catalog:    yhHGCatalog,
		Detail:     yhHGDetail,
		Resolve:    yhHGResolve,
		Search:     yhHGSearch,
		Headers:    yhHGHeaders,
	}, "yh_huangguo", "huangguo", "黄果")
}
