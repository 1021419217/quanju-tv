package seku

import (
	"fmt"
	"math/rand"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ---- pron91 91pron (yh_pron91) — 经典 91porn HTML 抓取, 非 WP/远程配置 ----
//
// 配方出欲火 APK jadx 实证 (ui/pron91 + y4 ApiService/HtmlParse/NetHelper):
//   base = https://w1116.a2507app.com (y4.b, 写死, 非远程配置下发)
//   GET {base}/v.php?category=<rf|ori>&viewtype=basic&page=<n>&m=&session_language=cn_CN
//   头 User-Agent: 91appnew (RankFragment 的 @Header 参数)
// 列表解析 (RankFragment.a, jsoup 选择器原文解码后):
//   容器 class "well well-sm videos-text-align"; 标题 class
//   "video-title title-truncate m-t-5"; 封面 img src; 时长 span.duration;
//   a href 取 viewkey ("=" 之后); 简介取 "添加时间"/"Added" 之后的子串.
// 播放 (PlayActivity + x4.a WebViewClient):
//   GET {base}/view_video.php?viewkey=<key>, 头 User-Agent: 91appnew 与
//   x-forwarded-for: <随机 IP>; WebView 拦截含 "/mp43/" 的请求即为播放地址.
//   本实现直接在详情页 HTML 中提取含 /mp43/ 的地址, 无则取首个 .mp4 链接.
// 分类与 App 导航 1:1: Pron91Activity 挂 RankFragment.m("rf") 与
//   RankFragment.m("ori") 两个页签; 标签名按 91porn 自身分类语义译.
// 付费: 该站无 coins/price 字段, 列表即免费内容, 无付费过滤项.
// 验真 (2026-10-24 本机, urllib 出口; curl 直连被沙箱 fake-IP 挡, 见下):
//   v.php?category=rf 本机实拉 200 / 79,268 B 91porn 首页 HTML, 含上述
//   容器/标题/封面/时长/a[href] 锚点; 播放页提取逻辑按 jadx 拦截串实现,
//   播放字节待 Go 冒烟补验 (见文件头外回执).

const yhPron91Slot = "yh_pron91"
const yhPron91Base = "https://w1116.a2507app.com"
const yhPron91UA = "91appnew"

var (
	yhPron91CacheMu sync.Mutex
	yhPron91Cache   = map[string]map[string]interface{}{}
)

var yhPron91Cats = []struct{ id, name string }{
	{"rf", "最新"},
	{"ori", "原创"},
}

func yhPron91Headers() map[string]string {
	return map[string]string{
		"User-Agent":      yhPron91UA,
		"Accept-Language": "zh-CN,zh;q=0.9",
		"Referer":         yhPron91Base + "/",
	}
}

func yhPron91RandIP() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%d.%d.%d.%d", r.Intn(255), r.Intn(255), r.Intn(255), r.Intn(255))
}

func yhPron91Get(rawurl string) ([]byte, error) {
	h := yhPron91Headers()
	h["x-forwarded-for"] = yhPron91RandIP()
	var body []byte
	var code int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("GET", rawurl, nil, h)
		if err == nil && code == 200 {
			return body, nil
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("yh_pron91 http %d", code)
}

var (
	yhPron91TitleRe   = regexp.MustCompile(`(?s)video-title title-truncate m-t-5[^>]*>(.*?)<`)
	yhPron91ImgRe     = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	yhPron91DurRe     = regexp.MustCompile(`(?s)<span[^>]*duration[^>]*>(.*?)</span>`)
	yhPron91ViewkeyRe = regexp.MustCompile(`viewkey=([A-Za-z0-9_-]+)`)
	yhPron91VideoARe  = regexp.MustCompile(`(?s)<a[^>]*viewkey=[^>]*>\s*<img[^>]+src="([^"]+)"`)
	yhPron91TagRe     = regexp.MustCompile(`<[^>]+>`)
)

func yhPron91Clean(s string) string {
	s = yhPron91TagRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&nbsp;", " ")
	return strings.TrimSpace(s)
}

func yhPron91Cards(slot, html string) []interface{} {
	out := []interface{}{}
	// 按容器切块: 每个 "well well-sm videos-text-align" 块即一视频.
	// 块内以 viewkey 锚定取 id 与封面 (块首 <a viewkey> 包 <img>),
	// 避免标题窗口向前误取翻页导航的 img/a (实测教训).
	const marker = "well well-sm videos-text-align"
	var starts []int
	for off := 0; ; {
		j := strings.Index(html[off:], marker)
		if j < 0 {
			break
		}
		starts = append(starts, off+j)
		off += j + len(marker)
	}
	for i, st := range starts {
		end := len(html)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		seg := html[st:end]
		title := ""
		if mm := yhPron91TitleRe.FindStringSubmatch(seg); mm != nil {
			title = yhPron91Clean(mm[1])
		}
		viewkey := ""
		if mm := yhPron91ViewkeyRe.FindStringSubmatch(seg); mm != nil {
			viewkey = mm[1]
		}
		if viewkey == "" || title == "" {
			continue
		}
		img := ""
		if mm := yhPron91VideoARe.FindStringSubmatch(seg); mm != nil {
			img = mm[1]
		} else {
			// 兜底: 块内首张非导航图标 (排除 arrowright 等站内 UI 图)
			for _, mm := range yhPron91ImgRe.FindAllStringSubmatch(seg, -1) {
				if !strings.Contains(mm[1], "arrowright") && !strings.Contains(mm[1], "/images/") {
					img = mm[1]
					break
				}
			}
		}
		dur := ""
		if mm := yhPron91DurRe.FindStringSubmatch(seg); mm != nil {
			dur = yhPron91Clean(mm[1])
		}
		info := ""
		if j := strings.Index(seg, "添加时间"); j >= 0 {
			info = yhPron91Clean(seg[j:])
		} else if j := strings.Index(seg, "Added"); j >= 0 {
			info = yhPron91Clean(seg[j:])
		}
		if len(info) > 120 {
			info = info[:120]
		}
		c := makeDrama(slot, viewkey, title, img, dur, "", info)
		out = append(out, c)
		yhPron91CacheMu.Lock()
		yhPron91Cache[viewkey] = map[string]interface{}{"title": title, "cover": img, "duration": dur, "info": info}
		yhPron91CacheMu.Unlock()
	}
	return out
}

func yhPron91Categories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range yhPron91Cats {
		out = append(out, categoryObj(c.id, c.name))
	}
	return out
}

func yhPron91Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	cat := category
	if cat == "" {
		cat = "rf"
	}
	u := fmt.Sprintf("%s/v.php?category=%s&viewtype=basic&page=%d&m=&session_language=cn_CN",
		yhPron91Base, url.QueryEscape(cat), page)
	body, err := yhPron91Get(u)
	if err != nil {
		return nil, 0, false, err
	}
	items := yhPron91Cards(slot, string(body))
	return items, 0, len(items) > 0, nil
}

func yhPron91Detail(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhPron91Slot+":")
	yhPron91CacheMu.Lock()
	cached, ok := yhPron91Cache[vid]
	yhPron91CacheMu.Unlock()
	title, cover, desc := vid, "", ""
	if ok {
		title = g5StrVal(cached["title"])
		cover = g5StrVal(cached["cover"])
		desc = g5StrVal(cached["info"])
		if d := g5StrVal(cached["duration"]); d != "" {
			desc = strings.TrimSpace(d + " " + desc)
		}
	} else {
		// 未经过 Catalog 的直接详情: 拉播放页取 <title> 与首图
		u := fmt.Sprintf("%s/view_video.php?viewkey=%s", yhPron91Base, url.QueryEscape(vid))
		if body, err := yhPron91Get(u); err == nil {
			if m := regexp.MustCompile(`(?s)<title>(.*?)</title>`).FindSubmatch(body); m != nil {
				title = yhPron91Clean(string(m[1]))
			}
			if m := yhPron91ImgRe.FindSubmatch(body); m != nil {
				cover = string(m[1])
			}
		}
	}
	d := makeDrama(slot, vid, title, cover, "", "", desc)
	g5SetChapters(d, slot, vid, []string{vid}, []string{"正片"})
	return d, nil
}

var (
	yhPron91Mp43Re = regexp.MustCompile(`https?://[^\s"'<>]+/mp43/[^\s"'<>]+`)
	yhPron91Mp4Re  = regexp.MustCompile(`https?://[^\s"'<>]+\.mp4[^\s"'<>]*`)
)

func yhPron91Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vid = strings.TrimPrefix(vid, yhPron91Slot+":")
	u := fmt.Sprintf("%s/view_video.php?viewkey=%s", yhPron91Base, url.QueryEscape(vid))
	body, err := yhPron91Get(u)
	if err != nil {
		return nil, err
	}
	html := string(body)
	play := ""
	if m := yhPron91Mp43Re.FindString(html); m != "" {
		play = m
	} else if m := yhPron91Mp4Re.FindString(html); m != "" {
		play = m
	}
	if play == "" {
		return nil, fmt.Errorf("yh_pron91: no play url for %s", vid)
	}
	play = strings.ReplaceAll(play, "&amp;", "&")
	format := "mp4"
	if strings.Contains(play, ".m3u8") {
		format = "m3u8"
	}
	return playResult(play, slot, rawID, yhPron91UA, yhPron91Base+"/", format, "mp4"), nil
}

func yhPron91Search(slot, kw string) ([]interface{}, error) {
	return nil, fmt.Errorf("yh_pron91: no search in app (Pron91Activity has no search entry)")
}

func init() {
	registerSite(yhPron91Slot, &siteImpl{
		Key:        yhPron91Slot,
		Name:       "91pron",
		DefaultCat: "rf",
		HomeURL:    yhPron91Base + "/",
		Headers:    yhPron91Headers,
		Categories: yhPron91Categories,
		Catalog:    yhPron91Catalog,
		Detail:     yhPron91Detail,
		Resolve:    yhPron91Resolve,
		Search:     yhPron91Search,
	})
}
