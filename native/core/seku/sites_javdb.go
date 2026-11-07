package seku

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
)

const slotJavDB = "javdb"
const javdbHost = "https://javdb.com"
const javdbUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var javdbCats = []struct{ id, name string }{
	{"new", "最新"},
	{"censored", "有码"},
	{"uncensored", "无码"},
	{"western", "欧美"},
	{"fc2", "FC2"},
	{"anime", "动漫"},
}

func init() {
	registerSite(slotJavDB, &siteImpl{
		Key: "javdb", Name: "JavDB磁力", DefaultCat: "new", HomeURL: javdbHost + "/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range javdbCats {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog: javdbCatalog,
		Detail:  javdbDetail,
		Resolve: func(slot, rawID string) (map[string]interface{}, error) {
			return nil, fmt.Errorf("JavDB 为磁力站，没有在线播放；请在详情页把磁力存到 115，再到 115 区播放")
		},
		Search: javdbSearch,
		Headers: func() map[string]string {
			return map[string]string{"User-Agent": javdbUA}
		},
	}, "javdb")
}

func javdbHeaders() map[string]string {
	return map[string]string{"User-Agent": javdbUA, "Referer": javdbHost + "/"}
}

func javdbListURL(category string, page int) string {
	base := javdbHost + "/"
	switch {
	case category == "new" || category == "":
		base = javdbHost + "/"
	case strings.HasPrefix(category, "tag:"):
		return javdbSearchURL(strings.TrimPrefix(category, "tag:"), page)
	default:
		base = javdbHost + "/" + category
	}
	if page > 1 {
		sep := "?"
		if strings.Contains(base, "?") {
			sep = "&"
		}
		base += sep + fmt.Sprintf("page=%d", page)
	}
	return base
}

func javdbSearchURL(keyword string, page int) string {
	u := javdbHost + "/search?q=" + url.QueryEscape(keyword) + "&f=all"
	if page > 1 {
		u += fmt.Sprintf("&page=%d", page)
	}
	return u
}

var (
	javdbItemRe  = regexp.MustCompile(`(?s)<div class="item">\s*<a href="/v/([A-Za-z0-9]+)" class="box" title="([^"]*)">(.*?)</a>\s*</div>`)
	javdbCoverRe = regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
	javdbTitleRe = regexp.MustCompile(`(?s)<div class="video-title"><strong>([^<]*)</strong>(.*?)</div>`)
	javdbMetaRe  = regexp.MustCompile(`(?s)<div class="meta">\s*([^<]+?)\s*</div>`)
	javdbScoreRe = regexp.MustCompile(`([0-9]+\.[0-9]+), by`)
)

func parseJavdbList(slot, body string) []interface{} {
	out := []interface{}{}
	for _, m := range javdbItemRe.FindAllStringSubmatch(body, -1) {
		rawID, block := m[1], m[3]
		title := html.UnescapeString(m[2])
		code, rest := "", title
		if tm := javdbTitleRe.FindStringSubmatch(block); tm != nil {
			code = strings.TrimSpace(html.UnescapeString(tm[1]))
			rest = strings.TrimSpace(html.UnescapeString(stripTags(tm[2])))
			if code != "" {
				title = code + " " + rest
			}
		}
		cover := ""
		if cm := javdbCoverRe.FindStringSubmatch(block); cm != nil {
			cover = cm[1]
		}
		remark := ""
		if mm := javdbMetaRe.FindStringSubmatch(block); mm != nil {
			remark = strings.TrimSpace(mm[1])
		}
		if sm := javdbScoreRe.FindStringSubmatch(block); sm != nil {
			if remark != "" {
				remark += " · "
			}
			remark += "★" + sm[1]
		}
		out = append(out, makeDrama(slot, rawID, title, cover, remark, "JavDB", ""))
	}
	return out
}

func javdbCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	body, code, _, err := httpGet(javdbListURL(category, page), javdbHeaders())
	if err != nil {
		return nil, 0, false, err
	}
	if code != 200 {
		return nil, 0, false, fmt.Errorf("JavDB 列表返回 %d", code)
	}
	items := parseJavdbList(slot, string(body))
	return items, len(items), len(items) > 0, nil
}

func javdbSearch(slot, kw string) ([]interface{}, error) {
	body, code, _, err := httpGet(javdbSearchURL(kw, 1), javdbHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("JavDB 搜索返回 %d", code)
	}
	return parseJavdbList(slot, string(body)), nil
}

var (
	javdbDetailTitleRe = regexp.MustCompile(`(?s)<h2 class="title is-4">(.*?)</h2>`)
	javdbPageTitleRe   = regexp.MustCompile(`(?s)<title>\s*(.*?)\s*\|`)
	javdbDetailCoverRe = regexp.MustCompile(`<img class="video-cover"[^>]+src="([^"]+)"`)
	javdbPanelRe       = regexp.MustCompile(`(?s)<div class="panel-block[^"]*">(.*?)</div>`)
)

func javdbDetail(slot, rawID string) (map[string]interface{}, error) {
	body, code, _, err := httpGet(javdbHost+"/v/"+rawID, javdbHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("JavDB 详情返回 %d", code)
	}
	text := string(body)
	title := ""
	if tm := javdbDetailTitleRe.FindStringSubmatch(text); tm != nil {
		title = strings.Join(strings.Fields(html.UnescapeString(stripTags(tm[1]))), " ")
	}
	if title == "" {
		if tm := javdbPageTitleRe.FindStringSubmatch(text); tm != nil {
			title = strings.Join(strings.Fields(html.UnescapeString(tm[1])), " ")
		}
	}
	cover := ""
	if cm := javdbDetailCoverRe.FindStringSubmatch(text); cm != nil {
		cover = cm[1]
	}
	info := map[string]string{}
	tags := []interface{}{}
	for _, pm := range javdbPanelRe.FindAllStringSubmatch(text, -1) {
		plain := strings.TrimSpace(html.UnescapeString(stripTags(pm[1])))
		key, value, found := strings.Cut(plain, ":")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		info[key] = value
		if key == "Tags" {
			for _, t := range strings.Split(value, ",") {
				if t = strings.TrimSpace(t); t != "" {
					tags = append(tags, t)
				}
			}
		}
	}
	descLines := []string{}
	for _, key := range []string{"ID", "Released Date", "Duration", "Maker", "Publisher", "Series", "Rating", "Actor(s)"} {
		if v := info[key]; v != "" {
			descLines = append(descLines, key+": "+v)
		}
	}
	descLines = append(descLines, "磁力站：本页不提供在线播放，选一条磁力「存到 115」，下载完成后在 115 区播放。")
	magnets := parseMagnets(text)
	d := makeDrama(slot, rawID, title, cover, info["Released Date"], "JavDB", strings.Join(descLines, "\n"))
	d["chapters"] = []interface{}{}
	d["episodeList"] = []interface{}{}
	d["episodeCount"] = 0
	d["chapterCount"] = 0
	d["totalEpisode"] = 0
	d["totalEpisodes"] = 0
	d["tags"] = tags
	d["magnets"] = magnetMaps(magnets)
	return d, nil
}
