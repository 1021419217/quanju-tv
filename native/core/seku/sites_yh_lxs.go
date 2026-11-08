package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- luxiansheng （撸先生）: signed + AES-CFB envelope JSON API ----
// Recipe: yuhuo lxs module (w3.c LxsUtils / w3.a ApiService / lxs fragments),
// fully reversed and live-verified 2026-10-24 (config/list/detail/actors/
// search/play all pulled locally; paid items confirmed walled server-side).
//
//	POST {base}/api.php?t=<millis>  form: client=pwa, timestamp=<sec>, data, sign
//	data = lowerhex( AES-256-CFB/NoPadding( JSON(biz), key, iv=key[:16] ) )
//	sign = MD5( hex(SHA256( "client=pwa&data="+data+"&timestamp="+ts+SECRET )) )
//	resp = {"errcode":0,"data":"<upper hex>"} -> same AES decrypt -> business JSON
//
// biz defaults: oauth_id/bundleId com.tvlutv/version 2.4.0/oauth_type web/
// app_type rn/img_type new/client pwa.
// ops: config {code:homePage, mod:down} -> data.avTag/xiaoTag (live category
// table, names/tags/types used verbatim so categories stay 1:1 with the
// site nav); list {code:search, mod:<av|xiao>, tag, page}; detail
// {code:detail, mod:<mv_type>, id}; actors {mod:actor, filter:"", page};
// actor videos {mod:actor, code:eq, id, actor:name, page}; search
// {mod:search, type:<av|xiao>, code:index, order:new, sort:desc, tag:kw}.
// Play: detail.line[].line.s720||s240 with host rewrite hf-<x>. -> hf-long.
// Paid rule: list items with price>0 are dropped; a paid detail never
// resolves (server withholds usable lines behind a purchase wall anyway).
// Covers: cover bytes on kfws.xeqyy.com are AES-128-CBC/NoPadding
// encrypted at rest (the app's Glide fetcher x3/d decrypts inline: key
// "f5d965df75336270", IV "97b60394abc2fbe1", PKCS#7-style pad tail).
// vod_pic therefore keeps the RAW url and yhLxsCoverFetch (registered
// as the impl's CoverFetch) fetches + decrypts; hosts must route cover
// loads for this slot through seku.Cover, as with the other
// byte-transformed-cover sites (e.g. sjCoverFetch in sites_g1.go).

const yhLxsSlot = "yh_lxs"
const yhLxsBase = "https://api.yewlowxe.cc"
const yhLxsUA = "Mozilla/5.0 (Linux; Android 13; SM-A205GN) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"

var yhLxsKey = []byte("f95277cfbbimgkcusimcuekd3b777pwa")
var yhLxsIV = []byte("f95277cfbbimgkcu")

const yhLxsSecret = "8DH2lusir4W6uhqFjbCkudNDosdfghjH"

var yhLxsDefaults = map[string]string{
	"oauth_id":   "zwmLGswGWjXy61JY_1749114098063",
	"bundleId":   "com.tvlutv",
	"version":    "2.4.0",
	"oauth_type": "web",
	"app_type":   "rn",
	"img_type":   "new",
	"client":     "pwa",
}

var yhLxsHostRe = regexp.MustCompile(`hf-[^.]+\.`)

func yhLxsEnc(plain []byte) string {
	block, _ := aes.NewCipher(yhLxsKey)
	out := make([]byte, len(plain))
	cipher.NewCFBEncrypter(block, yhLxsIV).XORKeyStream(out, plain)
	return hex.EncodeToString(out)
}

func yhLxsDec(hexs string) ([]byte, error) {
	raw, err := hex.DecodeString(hexs)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(yhLxsKey)
	out := make([]byte, len(raw))
	cipher.NewCFBDecrypter(block, yhLxsIV).XORKeyStream(out, raw)
	return out, nil
}

func yhLxsCall(biz map[string]string) (map[string]interface{}, error) {
	b := map[string]string{}
	for k, v := range yhLxsDefaults {
		b[k] = v
	}
	for k, v := range biz {
		b[k] = v
	}
	// biz values that are pure numbers in the app (page) travel as JSON
	// strings in the app too (HashMap<String,String>), keep them strings.
	jb, _ := json.Marshal(b)
	data := yhLxsEnc(jb)
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256([]byte("client=pwa&data=" + data + "&timestamp=" + ts + yhLxsSecret))
	sign := fmt.Sprintf("%x", md5.Sum([]byte(hex.EncodeToString(sum[:]))))
	form := url.Values{
		"client":    {"pwa"},
		"timestamp": {ts},
		"data":      {data},
		"sign":      {sign},
	}.Encode()
	u := fmt.Sprintf("%s/api.php?t=%d", yhLxsBase, time.Now().UnixMilli())
	h := map[string]string{
		"User-Agent":   yhLxsUA,
		"Content-Type": "application/x-www-form-urlencoded",
		"Referer":      yhLxsBase + "/",
	}
	var body []byte
	var code int
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		body, code, _, err = httpDo("POST", u, strings.NewReader(form), h)
		if err == nil && code == 200 {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_lxs http %d", code)
	}
	var outer map[string]interface{}
	if err := json.Unmarshal(body, &outer); err != nil {
		return nil, fmt.Errorf("yh_lxs outer parse: %v", err)
	}
	encHex := g5StrVal(outer["data"])
	if encHex == "" {
		return outer, nil
	}
	plain, err := yhLxsDec(encHex)
	if err != nil {
		return nil, fmt.Errorf("yh_lxs decrypt: %v", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_lxs parse: %v", err)
	}
	return out, nil
}

type yhLxsTag struct{ Type, Tag, Name string }

var (
	yhLxsTagsMu sync.Mutex
	yhLxsTags   []yhLxsTag
)

func yhLxsLoadTags() []yhLxsTag {
	yhLxsTagsMu.Lock()
	defer yhLxsTagsMu.Unlock()
	if yhLxsTags != nil {
		return yhLxsTags
	}
	obj, err := yhLxsCall(map[string]string{"code": "homePage", "mod": "down"})
	if err != nil {
		return nil
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil
	}
	out := []yhLxsTag{}
	for _, key := range []string{"xiaoTag", "avTag"} {
		for _, e := range yhArr(data[key]) {
			m := yhMap(e)
			if m == nil {
				continue
			}
			t := yhLxsTag{Type: g5StrVal(m["type"]), Tag: g5StrVal(m["tag"]), Name: g5StrVal(m["name"])}
			if t.Type != "" && t.Tag != "" && t.Name != "" {
				out = append(out, t)
			}
		}
	}
	yhLxsTags = out
	return out
}

func yhLxsHeaders() map[string]string {
	return map[string]string{"User-Agent": yhLxsUA, "Referer": yhLxsBase + "/"}
}

// ---- cover fetch: kfws covers are AES-128-CBC encrypted (see header) ----

var yhLxsImgKey = []byte("f5d965df75336270")
var yhLxsImgIV = []byte("97b60394abc2fbe1")

func yhLxsImgMagic(data []byte) bool {
	switch {
	case len(data) >= 2 && data[0] == 0xFF && data[1] == 0xD8: // JPEG
		return true
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return true
	case len(data) >= 4 && string(data[:4]) == "GIF8":
		return true
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return true
	case len(data) >= 2 && string(data[:2]) == "BM":
		return true
	}
	return false
}

func yhLxsCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpDo("GET", u, nil, yhLxsHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 || len(body) == 0 {
		return nil, fmt.Errorf("yh_lxs cover http %d", code)
	}
	if yhLxsImgMagic(body) || !strings.Contains(u, "kfws.xeqyy.com") ||
		len(body)%aes.BlockSize != 0 {
		return body, nil
	}
	block, err := aes.NewCipher(yhLxsImgKey)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(body))
	cipher.NewCBCDecrypter(block, yhLxsImgIV).CryptBlocks(out, body)
	if pad := int(out[len(out)-1]); pad > 0 && pad <= aes.BlockSize && pad <= len(out) {
		ok := true
		for _, b := range out[len(out)-pad:] {
			if int(b) != pad {
				ok = false
				break
			}
		}
		if ok {
			out = out[:len(out)-pad]
		}
	}
	if yhLxsImgMagic(out) {
		return out, nil
	}
	return body, nil // undecodable: pass raw through, as the app does
}

func yhLxsFree(rec map[string]interface{}) bool {
	if p, ok := rec["price"].(float64); ok && p > 0 {
		return false
	}
	return true
}

func yhLxsCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" {
		return nil, false
	}
	if !yhLxsFree(rec) {
		return nil, false
	}
	cover := g5StrVal(rec["cover_full"])
	if cover == "" {
		cover = g5StrVal(rec["cover_thumb"])
	}
	if strings.Contains(cover, "/ads/") {
		return nil, false // pinned ad creative, not a video
	}
	mod := g5StrVal(rec["mv_type"])
	if mod == "" {
		mod = "xiao"
	}
	remark := ""
	if d, ok := rec["duration"].(float64); ok && d > 0 {
		remark = fmt.Sprintf("%d:%02d", int(d)/60, int(d)%60)
	}
	desc := g5StrVal(rec["tags"])
	return makeDrama(slot, mod+":"+id, title, cover, remark, "", desc), true
}

func yhLxsCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhLxsCard(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhLxsPaged(slot string, obj map[string]interface{}, page int) ([]interface{}, int, bool, error) {
	items := yhLxsCards(slot, yhArr(obj["data"]))
	total := len(items)
	hasMore := false
	if pg := yhMap(obj["page"]); pg != nil {
		if t, ok := pg["total"].(float64); ok {
			total = int(t)
		}
		if ps, ok := pg["pagesize"].(float64); ok && ps > 0 {
			hasMore = float64(page*int(ps)) < float64(total)
		}
	}
	return items, total, hasMore, nil
}

func yhLxsCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, t := range yhLxsLoadTags() {
		label := t.Name
		if t.Type == "av" {
			label = "AV·" + t.Name
		}
		out = append(out, categoryObj(t.Type+"|"+t.Tag, label))
	}
	out = append(out, categoryObj("actors", "女优"))
	return out
}

func yhLxsCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	pg := strconv.Itoa(page)
	switch {
	case category == "actors":
		obj, err := yhLxsCall(map[string]string{"mod": "actor", "filter": "", "page": pg})
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(obj["data"]) {
			m := yhMap(e)
			if m == nil {
				continue
			}
			aid := g5StrVal(m["id"])
			name := g5StrVal(m["name"])
			if aid == "" || name == "" {
				continue
			}
			out = append(out, makeDrama(slot, "actor:"+aid+"|"+name, name, g5StrVal(m["cover"]), "女优", "女优", ""))
		}
		return out, len(out), len(out) >= 100, nil
	case strings.HasPrefix(category, "actor:"):
		rest := strings.TrimPrefix(category, "actor:")
		parts := strings.SplitN(rest, "|", 2)
		aid := parts[0]
		name := aid
		if len(parts) == 2 {
			name = parts[1]
		}
		obj, err := yhLxsCall(map[string]string{"mod": "actor", "code": "eq", "id": aid, "actor": name, "page": pg})
		if err != nil {
			return nil, 0, false, err
		}
		return yhLxsPaged(slot, obj, page)
	default:
		parts := strings.SplitN(category, "|", 2)
		if len(parts) != 2 {
			tags := yhLxsLoadTags()
			if len(tags) == 0 {
				return []interface{}{}, 0, false, nil
			}
			parts = []string{tags[0].Type, tags[0].Tag}
		}
		obj, err := yhLxsCall(map[string]string{"code": "search", "mod": parts[0], "tag": parts[1], "page": pg})
		if err != nil {
			return nil, 0, false, err
		}
		return yhLxsPaged(slot, obj, page)
	}
}

func yhLxsDetailRaw(mod, id string) (map[string]interface{}, error) {
	return yhLxsCall(map[string]string{"code": "detail", "mod": mod, "id": id})
}

func yhLxsPaid(obj map[string]interface{}) bool {
	data := yhMap(obj["data"])
	if data == nil {
		return false
	}
	if p, ok := data["price"].(float64); ok && p > 0 {
		if f, ok := data["isfree"].(float64); ok && f == 2 {
			return true
		}
		if ip, ok := data["is_pay"].(float64); ok && ip == 1 {
			return true
		}
		// detail price is in smaller units (list 500 -> detail 5);
		// combined with the privilege wall below it still counts paid.
		if priv := yhMap(data["privilege"]); priv != nil {
			if look := yhMap(priv["can_change_look"]); look != nil {
				if msg := g5StrVal(look["msg"]); strings.Contains(msg, "付费") {
					return true
				}
			}
		}
	}
	return false
}

func yhLxsPlayURL(obj map[string]interface{}) string {
	for _, e := range yhArr(obj["line"]) {
		m := yhMap(e)
		if m == nil {
			continue
		}
		inner := yhMap(m["line"])
		if inner == nil {
			continue
		}
		u := g5StrVal(inner["s720"])
		if u == "" {
			u = g5StrVal(inner["s240"])
		}
		if u != "" {
			return yhLxsHostRe.ReplaceAllString(u, "hf-long.")
		}
	}
	return ""
}

func yhLxsDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "actor:") {
		items, total, _, err := yhLxsCatalog(slot, rawID, 1)
		if err != nil {
			return nil, err
		}
		_ = total
		rest := strings.TrimPrefix(rawID, "actor:")
		name := rest
		if i := strings.Index(rest, "|"); i >= 0 {
			name = rest[i+1:]
		}
		d := makeDrama(slot, rawID, name, "", "女优", "女优", "")
		epIDs := []string{}
		epNames := []string{}
		for _, it := range items {
			if im, ok := it.(map[string]interface{}); ok {
				epIDs = append(epIDs, g5StrVal(im["vod_id"]))
				epNames = append(epNames, g5StrVal(im["title"]))
			}
		}
		if len(epIDs) > 0 {
			// episode ids are drama ids (slot-prefixed); store raw tails
			raw := []string{}
			for _, eid := range epIDs {
				raw = append(raw, eid)
			}
			g5SetChapters(d, slot, rawID, raw, epNames)
			d["related"] = items
		}
		return d, nil
	}
	parts := strings.SplitN(rawID, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("yh_lxs bad id %s", rawID)
	}
	obj, err := yhLxsDetailRaw(parts[0], parts[1])
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	title := rawID
	cover := ""
	desc := ""
	remark := ""
	if data != nil {
		if t := g5StrVal(data["title"]); t != "" {
			title = t
		}
		cover = g5StrVal(data["cover_full"])
		if cover == "" {
			cover = g5StrVal(data["cover_thumb"])
		}
		desc = g5StrVal(data["desc"])
		if tags := yhArr(data["tags"]); len(tags) > 0 {
			ts := []string{}
			for _, t := range tags {
				if s := g5StrVal(t); s != "" {
					ts = append(ts, s)
				}
			}
			if len(ts) > 0 {
				remark = strings.Join(ts, " ")
			}
		}
		if yhLxsPaid(obj) {
			remark = "付费"
		}
	}
	d := makeDrama(slot, rawID, title, cover, remark, "", desc)
	if !yhLxsPaid(obj) {
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	}
	return d, nil
}

func yhLxsResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	parts := strings.SplitN(vid, ":", 2)
	if len(parts) != 2 {
		// chapter ids arrive as drama ids; fall back to stored raw form
		return nil, fmt.Errorf("yh_lxs bad resolve id %s", rawID)
	}
	obj, err := yhLxsDetailRaw(parts[0], parts[1])
	if err != nil {
		return nil, err
	}
	if yhLxsPaid(obj) {
		return nil, fmt.Errorf("yh_lxs: paid content is not resolved")
	}
	u := yhLxsPlayURL(obj)
	if u == "" {
		return nil, fmt.Errorf("yh_lxs: no play url for %s", rawID)
	}
	return playResult(u, slot, rawID, yhLxsUA, yhLxsBase+"/", "m3u8", "hls"), nil
}

func yhLxsSearch(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	var lastErr error
	for _, typ := range []string{"xiao", "av"} {
		obj, err := yhLxsCall(map[string]string{
			"mod": "search", "type": typ, "code": "index",
			"order": "new", "sort": "desc", "tag": kw, "page": "1",
		})
		if err != nil {
			lastErr = err
			continue
		}
		out = append(out, yhLxsCards(slot, yhArr(obj["data"]))...)
	}
	if len(out) == 0 && lastErr != nil {
		return out, lastErr
	}
	return out, nil
}

func init() {
	registerSite(yhLxsSlot, &siteImpl{
		Key:        yhLxsSlot,
		Name:       "撸先生",
		DefaultCat: "",
		HomeURL:    yhLxsBase + "/",
		Headers:    yhLxsHeaders,
		Categories: yhLxsCategories,
		Catalog:    yhLxsCatalog,
		Detail:     yhLxsDetail,
		Resolve:    yhLxsResolve,
		Search:     yhLxsSearch,
		CoverFetch: yhLxsCoverFetch,
	})
	// NOTE for integration: also append "yh_lxs" to slotOrder in
	// registry.go and add the builtin_config entry (see 集成说明 in
	// port_out/luxiansheng/集成说明.md). registerSite alone makes the
	// slot dispatchable; slotOrder drives enumeration.
}
