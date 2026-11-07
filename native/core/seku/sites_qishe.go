package seku

// ---- QiShe (妻社, user-called 人妻社) — cloudfront slot, v4 ----
//
// Ported from the user's ZakaTV jar class ZakaQiSheAmns (config key
// ZakaQiShe, brand " 妻社"). The config has no entry literally named
// 人妻社; QiShe is the wife-community forum the user's list refers to
// (same Discuz qsts_app plugin API, wife-swapping boards). Cloudfront's
// previous occupant Pektino was removed by user order after the v3
// device report "cannot play" (its play chain could not be re-verified
// from this egress at all — the egress proxy refuses pektino.com; the
// pk* code stays in sites_g3.go, unregistered).
//
// Recipe (jar, then live-verified 2026-10-23):
//   - API host https://qs.qsh52693.org.cn, plugin path
//     /source/plugin/qsts_app/index.php/<endpoint>?<query>&sign=<md5>
//     &version=V1.1 with
//     sign = md5("url=" + endpoint + "&version=V1.1&time=DATA&domain="
//     + "qs.qsh52693.org.cn")   ("time=DATA" is a literal).
//   - Endpoints: post.getIndexList (home feed), postlist.getThreadList
//     (fid boards). Response {code:200, data:{list:[threads]}}; each
//     thread carries attachData.data[] attachments — the first .m3u8
//     (else .mp4/.mov/.m4v) attachment is the video, the first image
//     attachment the cover. The jar's searchContent returns an empty
//     list; Search here does the same (no search endpoint exists).
//   - Live from this egress: API answers 200 with real thread JSON;
//     video attachments are direct .m3u8 URLs on a qsimg CDN. That
//     CDN answers this datacenter egress with a Cloudflare managed
//     challenge, so the media layer is NOT verified from here — it is
//     marked "pending user-network verification" in the v4 notes (the
//     jar ships a shield helper for this CDN, i.e. it plays inside
//     the user's ecosystem). Structurally the streams are single-file
//     UGC uploads: no stitched ad segments are possible in the API
//     payload itself.
//   - Play headers follow the jar's playJson: desktop UA + Referer of
//     the public shell host (ZakaSec s(222)).

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

const qsHost = "https://qs.qsh52693.org.cn"
const qsAPIBase = qsHost + "/source/plugin/qsts_app/index.php/"
const qsSignDom = "qs.qsh52693.org.cn"
const qsShell = "https://xn---253k3-vs4kz59r.qishegg56.com"
const qsDefPic = "https://img.qsimg26515.org.cn/img/fqjh.png"
const qsAPIUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
const qsPlayUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var qsCats = []struct{ id, name string }{
	{"home", "🏠 首页推荐"},
	{"9", "淫妻分享"},
	{"11", "绿帽献妻"},
	{"5", "夫妻交换"},
	{"4", "夫妻认证交友"},
	{"6", "调教露出"},
	{"44", "妻社私密树洞"},
	{"22", "经验分享"},
	{"7", "素质单男"},
	{"25", "母狗公示榜"},
	{"18", "妻子原味"},
	{"19", "出租妻子认证"},
	{"53", "裸聊一对一认证"},
}

func qsHeaders() map[string]string {
	return map[string]string{"User-Agent": qsAPIUA, "Referer": qsHost + "/"}
}

func qsSign(endpoint string) string {
	sum := md5.Sum([]byte("url=" + endpoint + "&version=V1.1&time=DATA&domain=" + qsSignDom))
	return hex.EncodeToString(sum[:])
}

func qsAPI(endpoint, query string) (map[string]interface{}, error) {
	u := qsAPIBase + endpoint + "?" + query + "&sign=" + qsSign(endpoint) + "&version=V1.1"
	b, code, _, err := httpGet(u, qsHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("qishe: HTTP %d", code)
	}
	var root map[string]interface{}
	if json.Unmarshal(b, &root) != nil {
		return nil, fmt.Errorf("qishe: bad json")
	}
	if c, _ := root["code"].(float64); c != 200 {
		return nil, fmt.Errorf("qishe: api code %v", root["code"])
	}
	return root, nil
}

type qsThread struct {
	tid, title, cover, video, forum, message string
}

var (
	qsMemoMu sync.Mutex
	qsMemo   = map[string]qsThread{}
	qsImgRe  = regexp.MustCompile(`<img[^>]+src=["']([^"']+)["']`)
)

func qsAbsURL(u string) string {
	u = strings.TrimSpace(u)
	if strings.HasPrefix(u, "//") {
		return "https:" + u
	}
	return u
}

func qsParseThread(it map[string]interface{}) (qsThread, bool) {
	t := qsThread{tid: pkFlexStr(it["tid"]), title: cleanText(pkFlexStr(it["subject"]))}
	if t.tid == "" || t.title == "" {
		return t, false
	}
	t.message = cleanText(pkFlexStr(it["message"]))
	if fd, ok := it["forumData"].(map[string]interface{}); ok {
		t.forum = cleanText(pkFlexStr(fd["name"]))
	}
	atts := []interface{}{}
	if ad, ok := it["attachData"].(map[string]interface{}); ok {
		atts, _ = ad["data"].([]interface{})
	}
	imgFallback := ""
	for _, av := range atts {
		am, ok := av.(map[string]interface{})
		if !ok {
			continue
		}
		u := pkFlexStr(am["url"])
		if u == "" {
			u = pkFlexStr(am["attachment"])
		}
		u = qsAbsURL(u)
		if u == "" {
			continue
		}
		low := strings.ToLower(u)
		isImg := pkFlexStr(am["isimage"]) == "1" ||
			strings.Contains(low, ".jpg") || strings.Contains(low, ".jpeg") ||
			strings.Contains(low, ".png") || strings.Contains(low, ".webp") ||
			strings.Contains(low, ".gif")
		if t.video == "" && (strings.Contains(low, ".m3u8") ||
			strings.Contains(low, ".mp4") || strings.Contains(low, ".mov") ||
			strings.Contains(low, ".m4v")) {
			t.video = u
			continue
		}
		if isImg && imgFallback == "" {
			imgFallback = u
		}
	}
	t.cover = imgFallback
	if t.cover == "" {
		if m := qsImgRe.FindStringSubmatch(pkFlexStr(it["message"])); m != nil {
			t.cover = qsAbsURL(m[1])
		}
	}
	if t.cover == "" {
		if fd, ok := it["forumData"].(map[string]interface{}); ok {
			t.cover = qsAbsURL(pkFlexStr(fd["icon"]))
		}
	}
	if t.cover == "" {
		t.cover = qsDefPic
	}
	qsMemoMu.Lock()
	qsMemo[t.tid] = t
	qsMemoMu.Unlock()
	return t, true
}

func qsPage(endpoint, query string) ([]qsThread, error) {
	root, err := qsAPI(endpoint, query)
	if err != nil {
		return nil, err
	}
	data, _ := root["data"].(map[string]interface{})
	list, _ := data["list"].([]interface{})
	out := []qsThread{}
	for _, iv := range list {
		if im, ok := iv.(map[string]interface{}); ok {
			if t, ok := qsParseThread(im); ok {
				out = append(out, t)
			}
		}
	}
	return out, nil
}

func qsCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range qsCats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func qsCatName(id string) string {
	for _, c := range qsCats {
		if c.id == id {
			return c.name
		}
	}
	return "🏠 首页推荐"
}

func qsCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	var threads []qsThread
	var err error
	if category == "" || category == "home" {
		threads, err = qsPage("post.getIndexList", fmt.Sprintf("page=%d&perpage=20", page))
	} else {
		threads, err = qsPage("postlist.getThreadList", fmt.Sprintf("fid=%s&page=%d&perpage=20", category, page))
	}
	if err != nil {
		return nil, 0, false, err
	}
	items := []interface{}{}
	for _, t := range threads {
		remark := "图文"
		if t.video != "" {
			remark = "🎬 可播"
		}
		items = append(items, makeDrama(slot, t.tid, t.title, t.cover, remark, qsCatName(category), ""))
	}
	return items, len(items), len(threads) >= 20, nil
}

func qsLookup(tid string) (qsThread, bool) {
	qsMemoMu.Lock()
	t, ok := qsMemo[tid]
	qsMemoMu.Unlock()
	if ok {
		return t, true
	}
	for p := 1; p <= 4; p++ {
		threads, err := qsPage("post.getIndexList", fmt.Sprintf("page=%d&perpage=20", p))
		if err != nil || len(threads) == 0 {
			break
		}
		for _, th := range threads {
			if th.tid == tid {
				return th, true
			}
		}
	}
	return qsThread{}, false
}

func qsDetail(slot, rawID string) (map[string]interface{}, error) {
	t, ok := qsLookup(strings.TrimSpace(rawID))
	if !ok {
		return nil, fmt.Errorf("qishe: thread not found")
	}
	remark := "图文"
	if t.video != "" {
		remark = "🎬 可播"
	}
	cat := t.forum
	if cat == "" {
		cat = "妻社"
	}
	d := makeDrama(slot, t.tid, t.title, t.cover, remark, cat, t.message)
	if t.video != "" {
		g2SetChapters(d, []interface{}{g2Chapter(slot, t.tid, t.tid, "正片", 1)})
	} else {
		// Text/image post: no playable chapter.
		d["chapters"] = []interface{}{}
		d["episodeList"] = []interface{}{}
		d["episodes"] = 0
		d["episodeCount"] = 0
		d["chapterCount"] = 0
		d["totalEpisode"] = 0
		d["totalEpisodes"] = 0
	}
	return d, nil
}

func qsResolve(slot, rawID string) (map[string]interface{}, error) {
	t, ok := qsLookup(strings.TrimSpace(rawID))
	if !ok || t.video == "" {
		return nil, fmt.Errorf("qishe: no video in thread")
	}
	format, proto := "mp4", "file"
	if strings.Contains(strings.ToLower(t.video), ".m3u8") {
		format, proto = "m3u8", "hls"
	}
	return playResult(t.video, slot, rawID, qsPlayUA, qsShell+"/", format, proto), nil
}

func qsSearch(slot, kw string) ([]interface{}, error) {
	// The jar's searchContent returns an empty list — the plugin API
	// exposes no search endpoint.
	return []interface{}{}, nil
}

func init() {
	registerSite("cloudfront", &siteImpl{
		Key:        "qishe",
		Name:       "妻社",
		DefaultCat: "home",
		HomeURL:    qsHost + "/",
		Categories: qsCategories,
		Catalog:    qsCatalog,
		Detail:     qsDetail,
		Resolve:    qsResolve,
		Search:     qsSearch,
		Headers:    qsHeaders,
	})
}
