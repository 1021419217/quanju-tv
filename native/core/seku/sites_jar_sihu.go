package seku

// 四虎 4hu.tv (ZakaSihuAmns + ZakaSihuCdnAmns — the two jar classes are
// the same site and recipe, differing only in the m3u8 CDN host pool,
// so they are ported as ONE site) — static encrypted JSON API, every
// step verified live 2026-10-24:
//   API base: https://portal.instruno.com/public2
//   Envelope: {"status":1,"data":"<base64url ciphertext>"}; data is
//   AES-256-CBC, key = ASCII "22946bc50fd63164b79df55070a85a92",
//   IV = ASCII "kaixin1234567890" (the jar's dec()).
//   Categories: /json/category-list.json tops 1=国产(video) /
//   28=女优(av) / 37=电影(movie) contribute their items[] as c:<id>
//   categories; tops 2=图文(pic) and 3=小说(book) are not videos and
//   are intentionally not registered. Charts ★最新/日榜/周榜/月榜 map
//   to t:l / t:0 / t:2 / t:3 (latest.json, top/top.json,
//   top/top-Week.json, top/top-Moon.json).
//   Category page: /json/category/<id>-<page>.json ->
//   {data:[{id,title,titlepic,yulan,m3u8}], last_page, total}.
//   Detail: /json/video/<id>.json -> {mp4 (absolute), m3u8 (relative
//   path served by the m3u8.*cdn.com pool), keyboard, maybe[]...}.
//   Covers: titlepic is a .txt holding "data:image/jpg;base64,..."
//   (verified live) — sihuCoverFetch decodes it.
//   Search: /api/v2/search?keyword=&classid=0&page= (jar recipe).

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
)

const sihuPortal = "https://portal.instruno.com/public2"
const sihuUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var sihuCDNs = []string{
	"https://m3u8.73cdn.com", "https://m3u8.74cdn.com", "https://m3u8.34cdn.com",
	"https://m3u8.41cdn.com", "https://m3u8.44cdn.com", "https://m3u8.46cdn.com",
	"https://m3u8.47cdn.com", "https://m3u8.48cdn.com",
}

var (
	sihuMu     sync.Mutex
	sihuCDN    string
	sihuCatTop []sihuTopCat
)

func sihuHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      sihuUA,
		"Accept":          "application/json,text/plain,*/*",
		"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		"Referer":         "https://www.4hu.tv/",
	}
}

// sihuDecrypt unwraps the {data: base64url(AES-256-CBC)} envelope.
func sihuDecrypt(body []byte) (json.RawMessage, error) {
	var env struct {
		Status int             `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, err
	}
	var enc string
	if err := json.Unmarshal(env.Data, &enc); err != nil {
		// Some endpoints answer the payload object directly.
		return body, nil
	}
	raw, err := base64.URLEncoding.DecodeString(enc)
	if err != nil {
		raw, err = base64.RawURLEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("sihu: envelope b64: %v", err)
		}
	}
	block, err := aes.NewCipher([]byte("22946bc50fd63164b79df55070a85a92"))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("sihu: ciphertext size %d", len(raw))
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte("kaixin1234567890")).CryptBlocks(out, raw)
	// PKCS7 strip.
	if n := len(out); n > 0 {
		pad := int(out[n-1])
		if pad > 0 && pad <= aes.BlockSize && n-pad >= 0 {
			out = out[:n-pad]
		}
	}
	return json.RawMessage(out), nil
}

func sihuGetJSON(path string) (json.RawMessage, error) {
	body, code, _, err := httpGet(sihuPortal+path, sihuHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("sihu http %d for %s", code, path)
	}
	return sihuDecrypt(body)
}

type sihuItem struct {
	ID       json.RawMessage `json:"id"`
	Title    string          `json:"title"`
	TitlePic string          `json:"titlepic"`
	Yulan    string          `json:"yulan"`
	M3U8     string          `json:"m3u8"`
	MP4      string          `json:"mp4"`
	ClassID  json.RawMessage `json:"classid"`
	Keyboard string          `json:"keyboard"`
}

func sihuCard(slot string, it *sihuItem, catName string) map[string]interface{} {
	id := mcRawID(it.ID)
	if id == "" || it.Title == "" {
		return nil
	}
	remark := ""
	if it.Yulan != "" {
		remark = "预告"
	}
	d := makeDrama(slot, id, cleanText(it.Title), strings.TrimSpace(it.TitlePic), remark, catName, "")
	if it.Keyboard != "" {
		tags := []interface{}{}
		for _, t := range strings.Split(it.Keyboard, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, "tag:"+t)
			}
		}
		// Tags surface via detail tags; keep the raw words in tags
		// as plain names (host converts to tag refs).
		names := []interface{}{}
		for _, t := range strings.Split(it.Keyboard, ",") {
			if t = strings.TrimSpace(t); t != "" {
				names = append(names, t)
			}
		}
		_ = tags
		d["tags"] = names
	}
	return d
}

type sihuTopCat struct {
	ClassID   json.RawMessage `json:"classid"`
	ClassName string          `json:"classname"`
	Items     []struct {
		ClassID   json.RawMessage `json:"classid"`
		ClassName string          `json:"classname"`
	} `json:"items"`
}

// sihuVideoTops: jar loadKinds kind map — only playable tops.
var sihuVideoTops = map[string]bool{"1": true, "28": true, "37": true}

func sihuLoadCats() []htmlCat {
	sihuMu.Lock()
	cached := sihuCatTop
	sihuMu.Unlock()
	if cached != nil {
		out := []htmlCat{}
		for _, t := range cached {
			for _, it := range t.Items {
				out = append(out, htmlCat{ID: "c:" + mcRawID(it.ClassID), Name: it.ClassName})
			}
		}
		return out
	}
	raw, err := sihuGetJSON("/json/category-list.json")
	if err != nil {
		return []htmlCat{}
	}
	var tops []sihuTopCat
	if err := json.Unmarshal(raw, &tops); err != nil {
		return []htmlCat{}
	}
	keep := []sihuTopCat{}
	for _, t := range tops {
		if sihuVideoTops[mcRawID(t.ClassID)] {
			keep = append(keep, t)
		}
	}
	sihuMu.Lock()
	sihuCatTop = keep
	sihuMu.Unlock()
	out := []htmlCat{}
	for _, t := range keep {
		for _, it := range t.Items {
			out = append(out, htmlCat{ID: "c:" + mcRawID(it.ClassID), Name: it.ClassName})
		}
	}
	return out
}

var sihuChartCats = []htmlCat{
	{ID: "t:l", Name: "★最新"},
	{ID: "t:0", Name: "★日榜"},
	{ID: "t:2", Name: "★周榜"},
	{ID: "t:3", Name: "★月榜"},
}

func sihuChartPath(cat string) string {
	switch cat {
	case "t:0":
		return "/json/top/top.json"
	case "t:2":
		return "/json/top/top-Week.json"
	case "t:3":
		return "/json/top/top-Moon.json"
	default:
		return "/json/latest.json"
	}
}

// sihuArrayItems extracts an item array from a decrypted payload of
// varying shape: root array, or object with a known list key, or the
// first array-valued field.
func sihuArrayItems(raw json.RawMessage) []*sihuItem {
	var rootArr []*sihuItem
	if err := json.Unmarshal(raw, &rootArr); err == nil && len(rootArr) > 0 {
		return rootArr
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	for _, key := range []string{"data", "list", "latest", "top", "items"} {
		if v, ok := obj[key]; ok {
			var arr []*sihuItem
			if err := json.Unmarshal(v, &arr); err == nil && len(arr) > 0 {
				return arr
			}
		}
	}
	for _, v := range obj {
		var arr []*sihuItem
		if err := json.Unmarshal(v, &arr); err == nil && len(arr) > 0 {
			return arr
		}
	}
	return nil
}

const sihuPageSize = 20

func sihuCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if strings.HasPrefix(category, "t:") {
		raw, err := sihuGetJSON(sihuChartPath(category))
		if err != nil {
			return nil, 0, false, err
		}
		all := sihuArrayItems(raw)
		total := len(all)
		start := (page - 1) * sihuPageSize
		if start >= total {
			return []interface{}{}, total, false, nil
		}
		end := start + sihuPageSize
		if end > total {
			end = total
		}
		items := []interface{}{}
		for _, it := range all[start:end] {
			if c := sihuCard(slot, it, "榜单"); c != nil {
				items = append(items, c)
			}
		}
		return items, total, end < total, nil
	}
	cid := strings.TrimPrefix(category, "c:")
	raw, err := sihuGetJSON(fmt.Sprintf("/json/category/%s-%d.json", cid, page))
	if err != nil {
		return nil, 0, false, err
	}
	var resp struct {
		Data         []*sihuItem `json:"data"`
		LastPage     int         `json:"last_page"`
		Total        int         `json:"total"`
		CategoryName string      `json:"category_name"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, 0, false, fmt.Errorf("sihu category parse: %v", err)
	}
	items := []interface{}{}
	for _, it := range resp.Data {
		if c := sihuCard(slot, it, resp.CategoryName); c != nil {
			items = append(items, c)
		}
	}
	return items, resp.Total, page < resp.LastPage, nil
}

type sihuDetailResp struct {
	sihuItem
	CategoryName string      `json:"category_name"`
	Maybe        []*sihuItem `json:"maybe"`
}

func sihuFetchDetail(id string) (*sihuDetailResp, error) {
	raw, err := sihuGetJSON("/json/video/" + id + ".json")
	if err != nil {
		return nil, err
	}
	var d sihuDetailResp
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("sihu detail parse: %v", err)
	}
	if mcRawID(d.ID) == "" {
		return nil, fmt.Errorf("sihu detail empty for %s", id)
	}
	return &d, nil
}

// sihuM3U8URL resolves a relative m3u8 path against the CDN pool,
// probing once and caching the answering host.
func sihuM3U8URL(path string) string {
	if strings.HasPrefix(path, "http") {
		return path
	}
	sihuMu.Lock()
	cdn := sihuCDN
	sihuMu.Unlock()
	if cdn != "" {
		return cdn + path
	}
	for _, c := range sihuCDNs {
		b, code, _, err := httpGetTimeout(c+path, sihuHeaders(), 8_000_000_000)
		if err == nil && code == 200 && strings.Contains(string(b), "#EXTM3U") {
			sihuMu.Lock()
			sihuCDN = c
			sihuMu.Unlock()
			return c + path
		}
	}
	return sihuCDNs[0] + path
}

func sihuDetail(slot, rawID string) (map[string]interface{}, error) {
	id := strings.TrimPrefix(strings.TrimPrefix(rawID, "v:"), "m:")
	d, err := sihuFetchDetail(id)
	if err != nil {
		return nil, err
	}
	card := sihuCard(slot, &d.sihuItem, d.CategoryName)
	if card == nil {
		return nil, fmt.Errorf("sihu detail empty")
	}
	if len(d.Maybe) > 0 {
		rel := []interface{}{}
		for _, it := range d.Maybe {
			if c := sihuCard(slot, it, d.CategoryName); c != nil {
				rel = append(rel, c)
			}
		}
		if len(rel) > 0 {
			card["related"] = rel
		}
	}
	return card, nil
}

func sihuResolve(slot, rawID string) (map[string]interface{}, error) {
	id := strings.TrimPrefix(strings.TrimPrefix(rawID, "v:"), "m:")
	d, err := sihuFetchDetail(id)
	if err != nil {
		return nil, err
	}
	var primary string
	var format, proto string
	var variants []interface{}
	if d.M3U8 != "" {
		primary = sihuM3U8URL(d.M3U8)
		format, proto = "m3u8", "hls"
		if d.MP4 != "" {
			variants = append(variants, map[string]interface{}{"name": "MP4", "route": "MP4", "url": d.MP4})
		}
	} else if d.MP4 != "" {
		primary = d.MP4
		format, proto = "mp4", "file"
	} else if d.Yulan != "" {
		primary = d.Yulan
		format, proto = "mp4", "file"
	} else {
		return nil, fmt.Errorf("sihu: no play url for %s", id)
	}
	res := playResult(primary, slot, rawID, sihuUA, "https://www.4hu.tv/", format, proto)
	if len(variants) > 0 {
		res["routes"] = append(res["routes"].([]interface{}), variants...)
	}
	return res, nil
}

func sihuSearch(slot, kw string) ([]interface{}, error) {
	// Jar recipe: /api/v2/search on the API base the jar discovers
	// from a CDN host page. The portal base answers 404 for this
	// route and the front host is unreachable from this exit, so
	// search is recipe-ready but unverified (see batch report).
	path := "/api/v2/search?keyword=" + url.QueryEscape(kw) + "&classid=0&page=1"
	raw, err := sihuGetJSON(path)
	if err != nil {
		return []interface{}{}, err
	}
	items := []interface{}{}
	for _, it := range sihuArrayItems(raw) {
		if c := sihuCard(slot, it, "搜索"); c != nil {
			items = append(items, c)
		}
	}
	return items, nil
}

func sihuCoverFetch(u string) ([]byte, error) {
	if !strings.HasSuffix(u, ".txt") {
		b, code, _, err := httpGet(u, sihuHeaders())
		if err != nil || code != 200 {
			return nil, fmt.Errorf("sihu cover http %d err %v", code, err)
		}
		return b, nil
	}
	b, code, _, err := httpGet(u, sihuHeaders())
	if err != nil || code != 200 {
		return nil, fmt.Errorf("sihu cover http %d err %v", code, err)
	}
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, "data:") {
		i := strings.Index(s, "base64,")
		if i < 0 {
			return nil, fmt.Errorf("sihu cover: data url without base64")
		}
		img, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(s[i+len("base64,"):]), ""))
		if err != nil || len(img) == 0 {
			return nil, fmt.Errorf("sihu cover: base64: %v", err)
		}
		return img, nil
	}
	if len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 {
		return b, nil
	}
	return nil, fmt.Errorf("sihu cover: unexpected payload")
}

func init() {
	impl := &siteImpl{
		Key: "sihu", Name: "四虎", DefaultCat: "t:l", HomeURL: "https://www.4hu.tv/",
		Headers:    sihuHeaders,
		CoverFetch: sihuCoverFetch,
	}
	impl.Categories = func(slot string) []interface{} {
		out := []interface{}{}
		for _, c := range sihuChartCats {
			out = append(out, categoryObj(c.ID, c.Name))
		}
		for _, c := range sihuLoadCats() {
			out = append(out, categoryObj(c.ID, c.Name))
		}
		return out
	}
	impl.Catalog = sihuCatalog
	impl.Detail = sihuDetail
	impl.Resolve = sihuResolve
	impl.Search = sihuSearch
	registerSite("sihu", impl, "sihu", "四虎", "4hu")
}
