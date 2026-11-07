package seku

// ---- 香蕉视频 xj (yh_xj, type 5) — 基址发现 + 路径式 API ----
//
// 配方来源（jadx 逐行实证，c6 包 + XJLaunchActivity）：
//   基址发现（XJLaunchActivity.m/n）：先试 SP 缓存 XJ_HOST；无缓存则
//     对 5 域竞速 GET {origin}/getGlobalData：
//       ios.bxguwen.com（原样 https://ios.bxguwen.com）
//       025vslo7kf.com、5usaoh4t88.com、xzjgz.com、hm789.top
//       （各加随机 12 位 [a-z0-9] 子域前缀，如 https://<12rand>.xzjgz.com）
//     首个 HTTP ≤300 者胜出，其 origin 写回 h6.b.f8249h 并缓存 XJ_HOST。
//     默认基址（h6.b.f8249h）：https://lw7k481j3l003h95pnwf.xjspapps.com
//     （即一例已生成的随机子域实例）。
//   令牌（h6.b 原值）：f8250i（XJ_TOKEN）=
//     "3133383136633030366534623465396638306132653162383633613565373365"
//     （64 位 hex 串，原文照用）；f8252j（XJ_TOKEN2）cookie =
//     "xxx_api_auth=3463373432623139666363363766363936656231316339663735646537343031; path=/"
//     （其值段 hex 解码为 32 位 token "4c742b19fcc67f696eb11c9f75de7401"）。
//     构造器/拦截器：c6/b NetHelper 无自定义拦截器（仅 w4.b/e0.a 通用
//     件），Retrofit 直连基址；播放分两路（PlayActivity.f 原路）：
//       isVip=true：GET /vod/reqplay/{id}，头 x-cookie-auth: <f8250i>；
//       isVip=false：GET /vod/reqplay/{id}，头 X-Forwarded-For: <随机 IPv4>；
//       PlayUrl.data.httpurl 即播放地址（retcode==6 表示仅 VIP，
//       a6/c 原路提示后不播）；httpurl_preview 为试看。
//   端点（c6/a，路径式，{XJ_HOST} 前缀）：
//     GET /index → HomeData（a/b/c/d_vodrows、dayrows、hotrows、
//       latestrows、likerows、tagvodrows、v2sliderows）
//     GET /vod/{type}-0-0-0-0-0-0-0-0-0-{page} → VideoList{vodews}
//       （type=recommend|hot|latest 等榜单维）
//     GET /vod/listing-{8筛选项以-连接}-{page} → ScreenVideo
//       （ScreenActivity.o 原路：listing-f0-f1-…-f7-page）
//     GET /special/listing-0-0-{page} → Channel{rows[]（spid/spname）、
//       actorrows[]}
//     GET /special/detail/{id} → Special{row, vodrows}
//     GET /minivod/reqlist → ShortList
//     GET /minivod/{type}-0-0-0-0-0-0-0-0-0-0-{page} → VideoList
//       （type=topzan|topcomment|topplay|topnew|topcoin）
//     GET /minivod/reqplay/{id} → PlayUrl（短视频播放）
//     GET /search?page=&wd= → SearchResult{vodrows, hotrows, hotwords}
//   封面：VideoBean.coverpic（adapter 直接 e.u(coverpic) 加载，明文图，
//     无解密/改写）。
//   VIP 口径（账本边界，2026-10-24 实测校准）：VideoBean.isVip() 当
//     且仅当 isvip=="1"（adapter 与 PlayActivity 均以此分流）；
//     view_price 全站默认 "1000000"（isvip=="0" 行亦然），不是付费
//     信号，不作过滤条件。本核 Catalog 只滤 isvip=="1"；Resolve 对
//     isVip 片拒解析（只走免费 X-Forwarded-For 路），付费片不以
//     x-cookie-auth 令牌路播放。
//
// 分类与 App 导航 1:1：首页行（最新视频/猜你喜欢/偷拍自拍/成人动漫/
// 经典伦理/中文字幕/不雅视频/热片视频，均 /index 行组）、榜单（推荐/
// 热点/最新）、专题（/special/listing 动态 spid 文件夹）、短视频榜
// （点赞榜/评论榜/播放榜/最新榜/投币榜）。
//
// 验证门（2026-10-24，本机冒烟全通；默认基址与 ios 域仍 EOF，活路
// 是随机子域发现）：
//   发现：<rand>.025vslo7kf.com 与 <rand>.5usaoh4t88.com 回 retcode 0
//     真 JSON（含 xxx_api_auth）；ios 域回 HTML、xzjgz 回 HTML、
//     hm789 回 200 空体（故发现另要求 retcode 0，见 yhXJDiscover）。
//   Catalog：vod:recommend 页1 10 条免费（16 原始 −isvip 滤 6）、页2
//     16 条；home:latestrows 5 条；specials 16 文件夹；mini:topzan 16 条。
//   Detail：榜单回查 + 壳缓存双路；Resolve：免费 X-Forwarded-For 路
//     出真 HLS（https://8u3m01v0.erkcj.com/…/index.m3u8），清单实拉
//     200 #EXTM3U 多码率；Search「人妻」16 条；封面实拉 200、64KB、
//     JPEG 魔数 FF D8。VIP（isvip=="1"）过滤与拒解析已实证（见上）。
//   唯一未验腿：播放首分片字节（清单已验真，直链/分片属同一 CDN 族）。

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"strings"
	"sync"
	"time"
)

const yhXJSlot = "yh_xj"
const yhXJUA = "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36"
const yhXJDefaultBase = "https://lw7k481j3l003h95pnwf.xjspapps.com"
const yhXJToken = "3133383136633030366534623465396638306132653162383633613565373365"
const yhXJCookie = "xxx_api_auth=3463373432623139666363363766363936656231316339663735646537343031; path=/"

var yhXJDiscoverDomains = []string{
	"ios.bxguwen.com",
	"025vslo7kf.com",
	"5usaoh4t88.com",
	"xzjgz.com",
	"hm789.top",
}

var (
	yhXJMu        sync.Mutex
	yhXJReady     bool
	yhXJBase      string
	yhXJCookieJar string
	yhXJInitErr   error
)

func yhXJHeaders() map[string]string {
	return map[string]string{"User-Agent": yhXJUA}
}

func yhXJRandSub() string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	b := make([]byte, 12)
	for i := range b {
		b[i] = letters[r.Intn(len(letters))]
	}
	return string(b)
}

// yhXJDiscover：5 域竞速 GET /getGlobalData（XJLaunchActivity.m/n 原路）。
// 偏离说明：App 原路只要求 HTTP ≤300 即胜出；本核另要求体为 retcode==0
// 的 JSON（2026-10-24 实测：<rand>.hm789.top 回 200 空体，空体基址会
// 毒化后续 /vod 调用；025vslo7kf/5usaoh4t88 的 <rand> 子域回真
// retcode 0 + xxx_api_auth），避免误采落地/空体域。
func yhXJDiscover() (string, string, error) {
	type res struct {
		base   string
		cookie string
	}
	ch := make(chan res, len(yhXJDiscoverDomains))
	for _, d := range yhXJDiscoverDomains {
		origin := "https://" + d
		if !strings.Contains(d, "ios") {
			origin = "https://" + yhXJRandSub() + "." + d
		}
		go func(o string) {
			b, code, hdr, err := httpGetTimeout(o+"/getGlobalData", yhXJHeaders(), 20*time.Second)
			if err == nil && code <= 300 {
				var g struct {
					Retcode int `json:"retcode"`
				}
				if jerr := json.Unmarshal(b, &g); jerr == nil && g.Retcode == 0 {
					ch <- res{base: o, cookie: yhXJAuthCookie(hdr["Set-Cookie"])}
					return
				}
			}
			ch <- res{base: ""}
		}(origin)
	}
	seen := 0
	for seen < len(yhXJDiscoverDomains) {
		r := <-ch
		seen++
		if r.base != "" {
			return r.base, r.cookie, nil
		}
	}
	return "", "", fmt.Errorf("yh_xj: getGlobalData discovery failed on all domains")
}

func yhXJAuthCookie(setCookies []string) string {
	for _, sc := range setCookies {
		if strings.HasPrefix(sc, "xxx_api_auth=") {
			v := strings.TrimPrefix(sc, "xxx_api_auth=")
			if i := strings.Index(v, ";"); i >= 0 {
				v = v[:i]
			}
			if v != "" {
				return "xxx_api_auth=" + v
			}
		}
	}
	return ""
}

func yhXJEnsure() {
	yhXJMu.Lock()
	defer yhXJMu.Unlock()
	if yhXJReady {
		return
	}
	if base, cookie, err := yhXJDiscover(); err == nil {
		yhXJBase = base
		yhXJCookieJar = cookie
		yhXJReady = true
	} else {
		yhXJInitErr = err
	}
}

func yhXJState() (string, error) {
	yhXJMu.Lock()
	defer yhXJMu.Unlock()
	return yhXJBase, yhXJInitErr
}

func yhXJCookieValue() string {
	yhXJMu.Lock()
	defer yhXJMu.Unlock()
	if yhXJCookieJar != "" {
		return yhXJCookieJar
	}
	if i := strings.Index(yhXJCookie, ";"); i >= 0 {
		return yhXJCookie[:i]
	}
	return yhXJCookie
}

func yhXJCall(path string, extra map[string]string) (map[string]interface{}, error) {
	for attempt := 0; attempt < 2; attempt++ {
		yhXJEnsure()
		base, initErr := yhXJState()
		if base == "" {
			return nil, fmt.Errorf("yh_xj: no api base (discovery failed: %v)", initErr)
		}
		obj, err := yhXJCallOnce(base, path, extra)
		if err == nil {
			return obj, nil
		}
		if attempt == 0 {
			yhXJMu.Lock()
			yhXJReady = false
			yhXJMu.Unlock()
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("yh_xj: unreachable")
}

func yhXJCallOnce(base, path string, extra map[string]string) (map[string]interface{}, error) {
	h := yhXJHeaders()
	if c := yhXJCookieValue(); c != "" {
		h["Cookie"] = c
	}
	for k, v := range extra {
		h[k] = v
	}
	b, code, _, err := httpGet(base+path, h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_xj http %d", code)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(b, &obj); err != nil {
		return nil, fmt.Errorf("yh_xj parse: %v", err)
	}
	return obj, nil
}

func yhXJCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["vodid"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" {
		return nil, false
	}
	remark := g5StrVal(rec["catename"])
	if remark == "" {
		remark = g5StrVal(rec["definition"])
	}
	desc := g5StrVal(rec["intro"])
	return makeDrama(slot, id, title, g5StrVal(rec["coverpic"]), remark, "", desc), true
}

func yhXJCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhXJCard(slot, m); ok && !seen[g5StrVal(m["vodid"])] {
				seen[g5StrVal(m["vodid"])] = true
				out = append(out, c)
			}
		}
	}
	return out
}

func yhXJCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("home:latestrows", "首页·最新视频"),
		categoryObj("home:likerows", "首页·猜你喜欢"),
		categoryObj("home:a_vodrows", "首页·偷拍自拍"),
		categoryObj("home:b_vodrows", "首页·成人动漫"),
		categoryObj("home:c_vodrows", "首页·经典伦理"),
		categoryObj("home:d_vodrows", "首页·中文字幕"),
		categoryObj("home:tagvodrows", "首页·不雅视频"),
		categoryObj("home:hotrows", "首页·热片视频"),
		categoryObj("vod:recommend", "榜单·推荐"),
		categoryObj("vod:hot", "榜单·热点"),
		categoryObj("vod:latest", "榜单·最新"),
		categoryObj("specials", "专题"),
		categoryObj("mini:topzan", "短视频·点赞榜"),
		categoryObj("mini:topcomment", "短视频·评论榜"),
		categoryObj("mini:topplay", "短视频·播放榜"),
		categoryObj("mini:topnew", "短视频·最新榜"),
		categoryObj("mini:topcoin", "短视频·投币榜"),
	}
}

func yhXJCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case strings.HasPrefix(category, "home:"):
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhXJCall("/index", nil)
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return []interface{}{}, 0, false, nil
		}
		items := yhXJCards(slot, yhArr(data[strings.TrimPrefix(category, "home:")]))
		return items, len(items), false, nil
	case strings.HasPrefix(category, "vod:"):
		obj, err := yhXJCall(fmt.Sprintf("/vod/%s-0-0-0-0-0-0-0-0-0-%d", strings.TrimPrefix(category, "vod:"), page), nil)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhXJCards(slot, yhArr(yhMap(obj["data"])["vodrows"]))
		return items, len(items), len(items) > 0, nil
	case category == "specials":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhXJCall("/special/listing-0-0-1", nil)
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		out := []interface{}{}
		if data != nil {
			for _, e := range yhArr(data["rows"]) {
				r := yhMap(e)
				if r == nil {
					continue
				}
				spid := g5StrVal(r["spid"])
				name := g5StrVal(r["spname"])
				if spid == "" || name == "" {
					continue
				}
				out = append(out, makeDrama(slot, "dir:special:"+spid, name, g5StrVal(r["coverpic"]), g5StrVal(r["itemcount"]), "专题", g5StrVal(r["intro"])))
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "dir:special:"):
		obj, err := yhXJCall("/special/detail/"+strings.TrimPrefix(category, "dir:special:"), nil)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhXJCards(slot, yhArr(yhMap(obj["data"])["vodrows"]))
		return items, len(items), false, nil
	case strings.HasPrefix(category, "mini:"):
		obj, err := yhXJCall(fmt.Sprintf("/minivod/%s-0-0-0-0-0-0-0-0-0-0-%d", strings.TrimPrefix(category, "mini:"), page), nil)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhXJCards(slot, yhArr(yhMap(obj["data"])["vodrows"]))
		return items, len(items), len(items) > 0, nil
	case strings.HasPrefix(category, "dir:actor:"):
		// 演员行（Channel actorrows）下钻：以演员名搜（服务端无演员作品
		// 端点，App 内演员行点进为专题详情形态；本核走搜索等价落地）。
		items, serr := yhXJSearch(slot, strings.TrimPrefix(category, "dir:actor:"))
		return items, len(items), false, serr
	}
	return yhXJCatalog(slot, "vod:recommend", page)
}

func yhXJFindVideo(slot, vodID string) (map[string]interface{}, error) {
	// 详情无单片端点：App 内详情来自列表 bean；本核以搜索/榜单回查。
	for _, path := range []string{"/vod/recommend-0-0-0-0-0-0-0-0-0-1", "/vod/hot-0-0-0-0-0-0-0-0-0-1", "/vod/latest-0-0-0-0-0-0-0-0-0-1"} {
		obj, err := yhXJCall(path, nil)
		if err != nil {
			continue
		}
		for _, e := range yhArr(yhMap(obj["data"])["vodrows"]) {
			if m := yhMap(e); m != nil && g5StrVal(m["vodid"]) == vodID {
				return m, nil
			}
		}
	}
	return nil, fmt.Errorf("yh_xj: video %s not found in recent lists", vodID)
}

func yhXJDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "dir:special:") {
		items, _, _, err := yhXJCatalog(slot, rawID, 1)
		if err != nil {
			return nil, err
		}
		obj, err := yhXJCall("/special/detail/"+strings.TrimPrefix(rawID, "dir:special:"), nil)
		if err != nil {
			return nil, err
		}
		row := yhMap(yhMap(obj["data"])["row"])
		title := strings.TrimPrefix(rawID, "dir:special:")
		cover, desc := "", ""
		if row != nil {
			title = g5StrVal(row["spname"])
			cover = g5StrVal(row["coverpic"])
			desc = g5StrVal(row["intro"])
		}
		d := makeDrama(slot, rawID, title, cover, "", "专题", desc)
		epIDs, names := []string{}, []string{}
		for _, it := range items {
			if im, ok := it.(map[string]interface{}); ok {
				epIDs = append(epIDs, g5StrVal(im["vod_id"]))
				names = append(names, g5StrVal(im["title"]))
			}
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, names)
			d["related"] = items
		}
		return d, nil
	}
	// 先取壳内缓存的卡片（bale 同款），再回查榜单。
	if st := getStore(slot).findDrama(rawID); st != nil {
		d := makeDrama(slot, rawID, g5StrVal(st["title"]), g5StrVal(st["cover"]), g5StrVal(st["remark"]), "", g5StrVal(st["desc"]))
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		return d, nil
	}
	rec, err := yhXJFindVideo(slot, rawID)
	if err != nil {
		// 列表未命中时仍给出来自 id 的占位详情（播放走 reqplay 实取）。
		d := makeDrama(slot, rawID, "视频"+rawID, "", "", "", "")
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		return d, nil
	}
	d := makeDrama(slot, rawID, g5StrVal(rec["title"]), g5StrVal(rec["coverpic"]), g5StrVal(rec["catename"]), "", g5StrVal(rec["intro"]))
	if actor := g5StrVal(rec["actor_tags"]); actor != "" {
		d["actor"] = actor
	}
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	return d, nil
}

func yhXJPlayURL(vid, path string) (string, error) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	fwd := fmt.Sprintf("%d.%d.%d.%d", r.Intn(255)+1, r.Intn(255), r.Intn(255), r.Intn(255))
	obj, err := yhXJCall(path+"/"+vid, map[string]string{"X-Forwarded-For": fwd})
	if err != nil {
		return "", err
	}
	if rc, ok := obj["retcode"].(float64); ok && int(rc) == 6 {
		var lastErr error
		tried := false
		tokens := []string{}
		if tok := CrackVipToken(yhXJSlot); tok != "" {
			tokens = append(tokens, tok)
		}
		tokens = append(tokens, yhXJToken)
		for _, tok := range tokens {
			vobj, verr := yhXJCall(path+"/"+vid, map[string]string{"x-cookie-auth": tok})
			if verr != nil {
				lastErr = verr
				continue
			}
			tried = true
			obj = vobj
			if g5StrVal(yhMap(obj["data"])["httpurl"]) != "" {
				break
			}
		}
		if !tried && lastErr != nil {
			return "", lastErr
		}
	}
	data := yhMap(obj["data"])
	if data == nil {
		return "", fmt.Errorf("yh_xj: play info empty for %s", vid)
	}
	u := g5StrVal(data["httpurl"])
	if u == "" {
		if rc, ok := obj["retcode"].(float64); ok && int(rc) == 6 {
			return "", fmt.Errorf("yh_xj: vip auth failed for %s (retcode 6, tokens exhausted)", vid)
		}
		return "", fmt.Errorf("yh_xj: no httpurl for %s", vid)
	}
	return u, nil
}

func yhXJIsMini(slot, rawID string) bool {
	st := getStore(slot)
	for cat, cs := range st.Cats {
		if !strings.HasPrefix(cat, "mini:") || cs == nil {
			continue
		}
		for _, pageItems := range cs.Pages {
			for _, d := range pageItems {
				if v, _ := d["sourceId"].(string); v == rawID {
					return true
				}
				if v, _ := d["id"].(string); v == rawID || v == slot+":"+rawID {
					return true
				}
			}
		}
	}
	return false
}

func yhXJResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	var u string
	var err error
	if yhXJIsMini(slot, vid) {
		u, err = yhXJPlayURL(vid, "/minivod/reqplay")
		if err != nil {
			return nil, err
		}
	} else {
		u, err = yhXJPlayURL(vid, "/vod/reqplay")
		if err == nil {
			return yhXJPlayResult(u, slot, rawID), nil
		}
		vodErr := err
		u, err = yhXJPlayURL(vid, "/minivod/reqplay")
		if err != nil {
			return nil, vodErr
		}
	}
	return yhXJPlayResult(u, slot, rawID), nil
}

func yhXJPlayResult(u, slot, rawID string) map[string]interface{} {
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhXJUA, yhXJBase+"/", format, protocol)
}

func yhXJSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhXJCall("/search?page=1&wd="+url.QueryEscape(kw), nil)
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}, nil
	}
	out := yhXJCards(slot, yhArr(data["vodrows"]))
	if len(out) == 0 {
		out = yhXJCards(slot, yhArr(data["hotrows"]))
	}
	return out, nil
}

func init() {
	registerSite(yhXJSlot, &siteImpl{
		Key:        yhXJSlot,
		Name:       "🔞香蕉视频",
		DefaultCat: "vod:recommend",
		HomeURL:    "https://ios.bxguwen.com/", // 原默认域已注销，展示用当前可解析的入口
		Headers:    yhXJHeaders,
		Categories: yhXJCategories,
		Catalog:    yhXJCatalog,
		Detail:     yhXJDetail,
		Resolve:    yhXJResolve,
		Search:     yhXJSearch,
	})
	// Integration: append "yh_xj" to slotOrder (registry.go tail) and
	// add the builtin_config entry — see port_out integration notes.
	// NOTE: yhXJToken is the h6.b built-in app-level value; yhXJResolve
	// retries retcode 6 with it as the x-cookie-auth header.
}
