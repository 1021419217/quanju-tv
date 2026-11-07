package seku

// ---- G点短视频 gdian (yh_gdian) — 原生 JSON API（非纯 H5 壳） ----
//
// 配方来源：欲火 APK jadx 实证（模块 gdian；旧报告「纯 H5 壳」已由
//   本轮深读推翻：H5 入口页只用于捕获最终基址，内容全部走 Retrofit
//   z1.a 接口）
//   基址：h6.b.f8275u0 = https://g3d37na.com/（运行时可被远程配置
//   getURL_GDIAN 与 WebView 最终 URL 覆盖；本件以该值为初始基址，
//   收到 Loading 外壳页时跟进页内跳转目标刷新基址后重试一次）。
//   接口（z1.a，全部 GET，参数名按接口原文）：
//     GET /apiv2/video/search?is_av={0}&sort={sort}&num=20&page={n}
//         &keyword={kw}[&label_ids[]={ids}]
//     GET /apiv2/video/{id} -> Play bean（data.movie_m3u8_url[]）。
//   视频 Tab（VideoFragment，1:1）：水果派（sort=latest + label_ids
//   []=25）、最新上线（sort=latest）、热播（sort=hot）、好评
//   （sort=score）；搜索用 h(0,"latest",20,page,kw)（SearchResult
//   Fragment）。App 主 Tab 另有美图/小说/直播三区（album/fiction/
//   external-liverooms 接口），非视频内容，本件不纳入视频核，见
//   Categories 注释。
//   播放（Play.getPlayUrl 原路）：取第一条非空 movie_m3u8_url[].url，
//   拼基址后把 "/play?" 换成 "/play.m3u8?"；无 m3u8 时 App 回退到
//   https://www.baidu.com 占位——本件视为空播放并报错，不返回占位。
//   付费（guide 验证门）：列表过滤 need_vip>0 的卡；
//   Detail 照常返回信息，Resolve 在 need_vip>0 时拒解析；Play bean
//   另有 can_watch_time（试看秒数）与 forever_point 等账本字段，
//   本件不取试看段、不碰付费。
//   封面：movie_cover 直链（App 以 Glide 明文加载），无 CoverFetch。
//
// 验证门（逐腿，2026-10-24）：
//   Categories：与 VideoFragment 四 Tab 1:1 ✔（jadx 字符串实证）。
//   Catalog/Detail/Search/Resolve：接口与字段逐行按 jadx 复刻；
//     但活体拉取时 g3d37na.com 对 / 与 /apiv2/video/search 均回
//     同一 1778 字节 Loading 外壳页（非 JSON，冒烟报 parse 错误），
//     即入口已变为 JS 跳转壳；按 x1.a，App 以跳转后最终 URL 作
//     Retrofit 基址，而本轮未跟进该跳转后的最终基址（安全检查未
//     通过该外壳页所含跳转目标的跟进，故到此为止）——故标
//     「出口未验 + 最终基址未校准」，不冒充已验、不把入口问题
//     写成站已死。Google DoH 证实域名解析正常（198.16.55.243）。
//   封面魔数：同上，出口未验。

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var yhGDianBase = "https://g3d37na.com"
var yhGDianBaseMu sync.Mutex

func yhGDianBaseURL() string {
	yhGDianBaseMu.Lock()
	defer yhGDianBaseMu.Unlock()
	return yhGDianBase
}

func yhGDianSetBase(base string) bool {
	yhGDianBaseMu.Lock()
	defer yhGDianBaseMu.Unlock()
	if base == "" || base == yhGDianBase {
		return false
	}
	yhGDianBase = base
	return true
}

var yhGDianRedirectRe = []*regexp.Regexp{
	regexp.MustCompile(`(?i)location\.(?:href|replace)\s*\(\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)location\.href\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)window\.location\s*=\s*["']([^"']+)["']`),
	regexp.MustCompile(`(?i)<meta[^>]*http-equiv=["']?refresh["']?[^>]*content=["'][^"']*url=([^"'\s>]+)`),
}

func yhGDianCalibratedBase(u *url.URL) string {
	base := u.Scheme + "://" + u.Host
	p := strings.TrimSuffix(u.Path, "/")
	if p != "" && p != "/" {
		if i := strings.LastIndex(p, "/"); i >= 0 && strings.Contains(p[i+1:], ".") {
			p = p[:i]
		}
		if p != "" && p != "/" {
			base += p
		}
	}
	return base
}

func yhGDianCalibrate() bool {
	changed := false
	for round := 0; round < 2; round++ {
		body, code, _, err := httpGet(yhGDianBaseURL()+"/", yhGDianHeaders())
		if err != nil || code != 200 || len(body) == 0 {
			return changed
		}
		page := string(body)
		cur := yhGDianBaseURL()
		advanced := false
		for _, re := range yhGDianRedirectRe {
			m := re.FindStringSubmatch(page)
			if m == nil {
				continue
			}
			target := strings.TrimSpace(m[1])
			if target == "" {
				continue
			}
			if !strings.HasPrefix(target, "http") {
				if strings.HasPrefix(target, "/") {
					target = cur + target
				} else {
					target = cur + "/" + target
				}
			}
			if u, err := url.Parse(target); err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https") {
				if yhGDianSetBase(yhGDianCalibratedBase(u)) {
					changed = true
					advanced = true
				}
				break
			}
		}
		if !advanced {
			return changed
		}
	}
	return changed
}
const yhGDianUA = "Mozilla/5.0 (Linux; Android 8.0.0; SM-G955U Build/R16NW) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/87.0.4280.141 Mobile Safari/537.36 Edg/106.0.0.0"

func yhGDianHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      yhGDianUA,
		"Accept":          "application/json, text/plain, */*",
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Referer":         yhGDianBaseURL() + "/",
	}
}

func yhGDianGetFrom(base, path string) (map[string]interface{}, []byte, error) {
	b, code, _, err := httpGet(base+path, yhGDianHeaders())
	if err != nil {
		return nil, nil, err
	}
	if code != 200 {
		return nil, b, fmt.Errorf("yh_gdian http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, b, fmt.Errorf("yh_gdian parse: %v", err)
	}
	return obj, b, nil
}

func yhGDianGet(path string) (map[string]interface{}, error) {
	var lastErr error
	shell := false
	for attempt := 0; attempt < 3; attempt++ {
		obj, raw, err := yhGDianGetFrom(yhGDianBaseURL(), path)
		if err == nil {
			return obj, nil
		}
		lastErr = err
		if raw == nil || !strings.Contains(string(raw), "<") {
			return nil, err
		}
		shell = true
		before := yhGDianBaseURL()
		if yhGDianCalibrate() && yhGDianBaseURL() != before {
			continue
		}
		break
	}
	if shell {
		return nil, fmt.Errorf("yh_gdian: base not calibrated (shell page, target not resolved): %v", lastErr)
	}
	return nil, lastErr
}

func yhGDianCover(rec map[string]interface{}) string {
	c := g5StrVal(rec["movie_cover"])
	if c == "" {
		c = g5StrVal(rec["movie_preview_img"])
	}
	if c == "" {
		return ""
	}
	if strings.HasPrefix(c, "http") {
		return c
	}
	if strings.HasPrefix(c, "/") {
		return yhGDianBaseURL() + c
	}
	return c
}

func yhGDianCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		if v, ok := g5MapInt(rec, "need_vip"); ok && v > 0 {
			continue
		}
		id := ""
		if s := g5StrVal(rec["movie_id"]); s != "" {
			id = s
		} else if f, ok := rec["movie_id"].(float64); ok {
			id = strconv.Itoa(int(f))
		}
		title := g5StrVal(rec["movie_name"])
		if id == "" || title == "" {
			continue
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		remark := g5StrVal(rec["movie_long"])
		if wc, ok := g5MapInt(rec, "watch_count"); ok && wc > 0 {
			if remark != "" {
				remark += " "
			}
			remark += fmt.Sprintf("%d播放", wc)
		}
		labels := []string{}
		for _, l := range yhArr(rec["labels"]) {
			if s := g5StrVal(l); s != "" {
				labels = append(labels, s)
			}
		}
		out = append(out, makeDrama(slot, id, title, yhGDianCover(rec), remark, strings.Join(labels, " "), g5StrVal(rec["jav_number"])))
	}
	return out
}

var yhGDianCats = []struct{ id, sort, label, name string }{
	{"fruit", "latest", "25", "水果派"},
	{"latest", "latest", "", "最新上线"},
	{"hot", "hot", "", "热播"},
	{"score", "score", "", "好评"},
}

func yhGDianCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range yhGDianCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func yhGDianCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	cat := yhGDianCats[1]
	for _, c := range yhGDianCats {
		if c.id == category {
			cat = c
			break
		}
	}
	q := fmt.Sprintf("/apiv2/video/search?is_av=0&sort=%s&num=20&page=%d&keyword=", cat.sort, page)
	if cat.label != "" {
		q += "&label_ids[]=" + cat.label
	}
	obj, err := yhGDianGet(q)
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, 0, false, fmt.Errorf("yh_gdian: empty data")
	}
	items := yhGDianCards(slot, yhArr(data["data"]))
	total, _ := g5MapInt(data, "total")
	return items, total, len(items) == 20, nil
}

func yhGDianDetailData(id string) (map[string]interface{}, error) {
	obj, err := yhGDianGet("/apiv2/video/" + url.PathEscape(id))
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_gdian: empty detail for %s", id)
	}
	return data, nil
}

func yhGDianDetail(slot, rawID string) (map[string]interface{}, error) {
	id, _ := splitEpisodeSuffix(rawID)
	data, err := yhGDianDetailData(id)
	if err != nil {
		return nil, err
	}
	title := g5StrVal(data["movie_name"])
	if title == "" {
		title = "视频" + id
	}
	remark := g5StrVal(data["movie_long"])
	if v, ok := g5MapInt(data, "need_vip"); ok && v > 0 {
		if remark != "" {
			remark += " "
		}
		remark += "VIP"
	}
	d := makeDrama(slot, id, title, yhGDianCover(data), remark, "", g5StrVal(data["introduction"]))
	g5SetChapters(d, slot, id, []string{id}, []string{"正片"})
	related := []interface{}{}
	for _, s := range yhArr(data["suggestion_list"]) {
		sm := yhMap(s)
		if sm == nil {
			continue
		}
		related = append(related, yhGDianCards(slot, []interface{}{sm})...)
	}
	if len(related) > 0 {
		d["related"] = related
	}
	return d, nil
}

func yhGDianResolve(slot, rawID string) (map[string]interface{}, error) {
	id, _ := splitEpisodeSuffix(rawID)
	data, err := yhGDianDetailData(id)
	if err != nil {
		return nil, err
	}
	if v, ok := g5MapInt(data, "need_vip"); ok && v > 0 {
		return nil, fmt.Errorf("yh_gdian: paid video %s not resolved", id)
	}
	for _, e := range yhArr(data["movie_m3u8_url"]) {
		em := yhMap(e)
		if em == nil {
			continue
		}
		u := g5StrVal(em["url"])
		if u == "" {
			continue
		}
		if !strings.HasPrefix(u, "http") {
			u = yhGDianBaseURL() + u
		}
		u = strings.Replace(u, "/play?", "/play.m3u8?", 1)
		if strings.Contains(u, "baidu.com") {
			continue
		}
		format, protocol := "mp4", ""
		if strings.Contains(u, ".m3u8") {
			format, protocol = "m3u8", "hls"
		}
		return playResult(u, slot, rawID, yhGDianUA, yhGDianBaseURL()+"/", format, protocol), nil
	}
	return nil, fmt.Errorf("yh_gdian: no play url for %s", id)
}

func yhGDianSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhGDianGet("/apiv2/video/search?is_av=0&sort=latest&num=20&page=1&keyword=" + url.QueryEscape(kw))
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_gdian: empty search data")
	}
	return yhGDianCards(slot, yhArr(data["data"])), nil
}

func init() {
	registerSite("yh_gdian", &siteImpl{
		Key:        "yh_gdian",
		Name:       "G点短视频",
		DefaultCat: "latest",
		HomeURL:    yhGDianBase + "/",
		Categories: yhGDianCategories,
		Catalog:    yhGDianCatalog,
		Detail:     yhGDianDetail,
		Resolve:    yhGDianResolve,
		Search:     yhGDianSearch,
		Headers:    yhGDianHeaders,
	}, "yh_gdian", "gdian", "G点短视频")
}
