package seku

// ---- melon (yh_melon, type 52) — SGP-style signed + AES envelope ----
//
// Recipe: yuhuo melon module, jadx deep-read 2026-10-24 (h4.a ApiService
// + h4.b MelonUtils + h4.c MeloneInterceptor + melon fragments).
// Base: https://api1.rdhxqtpj.cc (h6.b f8267q0). Picture host:
// https://pic.hdhwqx.cn (list banner str_value is already absolute).
//
// Request (form POST, h4.b.d/e + fragment wrapper):
//   outer = {timestamp:<sec>, _ver:"v2", client:"pwa",
//            data: Base64(AES-128-CBC/PKCS7(bizJSON,
//                  key "f2b3c051c97f3e21", iv "2330de0e2a8536f7")),
//            sign: MD5( hex(SHA256( "client=pwa&data="+data+
//                  "&timestamp="+ts+"2a3b8963abcb17eaa4a40c97cd9ece68" )))}
//   biz defaults (h4.b.e, fresh sid per call): oauth_id/device_id =
//     "841c93e12c80a2cb8840a5ef7ff20a9f", bundle_id "com.pwa.hlbdy",
//     version "1.8.9", oauth_type "web", language "zh", via "pwa",
//     token "", trace_id "", aff_x_code "", sid <fresh UUID-like>,
//     user_agent <iPhone 18_5 UA>, user_type "normal".
// Response (h4.c): {errcode, timestamp, data:<Base64 AES same key/iv>}
//   -> business JSON. List responses: {data:{list[]}}; detail:
//   {data:{cur{...content HTML...}, prev, next}}; errors:
//   {data:[], status:0, msg:<text>}.
//
// Endpoints (h4.a, all form POST):
//   /api.php/api/contents/list_contents  biz {mid, page, limit:"30"}
//     (+ order for the 热门 tabs, see below)
//   /api.php/api/contents/detail_content biz {cid}
//   /api.php/api/contents/search         biz {page, limit:"30", kwy}
//
// Categories vs App nav (HomeFragment tabs + HotFragment tabs):
//   首页 (mid "0") / 今日看料 (1217) / 黑料大事 (5778) /
//   热门吃瓜 (1) / 每日热瓜 (24058) / Ai短剧 (34261) / AV解说 (27171)
//   + 热门子榜： 最新 (newest) / 最热 (hottest) / 最多点赞 (favors) /
//   推荐 (featured). The App's HotFragment passes the order value through
//   the same HomeTypeFragment slot as the channel value, so the order
//   tabs are implemented as biz {mid:<order>, page, limit} — the same
//   shape the App sends; if the server ever rejects it, the fall-back
//   shape {mid:"", order:<order>} is one line away.
// List cards: data.list[] {cid, title, created, view, type, fields[]};
//   cover = fields[] entry named "banner" (first http str_value); ads
//   are list items with cid <= 0 (fields ads_field) and are dropped.
// Detail: cur.content HTML holds the video: <video src="...m3u8?
//   auth_key=..."> (same op.udhhzr.cn CDN family as hl365). Resolve
//   re-fetches detail and returns that src verbatim.
//
// Covers (g6/a Zcp91 fetcher, used by every melon list image via
// h6.e.P): the bytes served by pic.hdhwqx.cn are NOT a plain image —
// they are AES-128-CBC/NoPadding encrypted (key \"f5d965df75336270\",
// iv \"97b60394abc2fbe1\", decoded from ad.r literals in g6/a).
// CoverFetch below decrypts; verified 2026-10-24: the first list
// cover decrypts to a real JFIF JPEG (1300x625). Plain images
// (already magic-headed) pass through untouched.
// Live verification 2026-10-24 (this sandbox, egress proxy):
//   list_contents mid=0: HTTP 200, errcode 0, 30 items (17 real + 13 ads
//     with cid<=0, dropped); real cover on pic.hdhwqx.cn.
//   detail_content cid=224026: HTTP 200, cur.title/content present,
//     <video src> m3u8 with auth_key extracted (op.udhhzr.cn).
//   search: implemented per App (kwy) but the server answered
//     "您操作太快了，休息一下再来" on every attempt from this exit
//     (fresh sid + 45s gap included) — search is RATE-LIMITED from
//     this egress, not protocol-broken; it stays wired and surfaces
//     the server message.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const yhMelonBase = "https://api1.rdhxqtpj.cc"
const yhMelonUA = "Mozilla/5.0 (Linux; Android 13; SM-A205GN) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Mobile Safari/537.36"
const yhMelonAppUA = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1"
const yhMelonKey = "f2b3c051c97f3e21"
const yhMelonIV = "2330de0e2a8536f7"
const yhMelonSecret = "2a3b8963abcb17eaa4a40c97cd9ece68"

var yhMelonChannels = []struct{ id, name string }{
	{"0", "首页"}, {"1217", "今日看料"}, {"5778", "黑料大事"},
	{"1", "热门吃瓜"}, {"24058", "每日热瓜"}, {"34261", "Ai短剧"},
	{"27171", "AV解说"},
}

var yhMelonOrders = []struct{ id, name string }{
	{"newest", "最新"}, {"hottest", "最热"}, {"favors", "最多点赞"}, {"featured", "推荐"},
}

func yhMelonHeaders() map[string]string {
	return map[string]string{
		"User-Agent":   yhMelonUA,
		"Content-Type": "application/x-www-form-urlencoded",
	}
}

func yhMelonSID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	hexs := "0123456789abcdef"
	enc := func(v byte) string { return string([]byte{hexs[v>>4], hexs[v&15]}) }
	// App shape: %s-%s-4%s-%s-%s over hex nibbles of random bytes.
	return enc(b[0]) + enc(b[1]) + enc(b[2]) + enc(b[3]) + "-" +
		enc(b[4]) + enc(b[5]) + "-4" + enc(b[6])[1:] + enc(b[7]) + "-" +
		enc(b[8]) + enc(b[9]) + "-" + enc(b[10]) + enc(b[11]) + enc(b[12]) +
		enc(b[13]) + enc(b[14]) + enc(b[15])
}

func yhMelonPad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhMelonUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n >= 1 && n <= 16 && n <= len(b) {
		return b[:len(b)-n]
	}
	return b
}

func yhMelonEnc(plain []byte) (string, error) {
	block, err := aes.NewCipher([]byte(yhMelonKey))
	if err != nil {
		return "", err
	}
	ct := make([]byte, len(yhMelonPad(plain)))
	cipher.NewCBCEncrypter(block, []byte(yhMelonIV)).CryptBlocks(ct, yhMelonPad(plain))
	return base64.StdEncoding.EncodeToString(ct), nil
}

func yhMelonDec(b64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher([]byte(yhMelonKey))
	if err != nil {
		return nil, err
	}
	if len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_melon: bad aes length")
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte(yhMelonIV)).CryptBlocks(out, raw)
	return yhMelonUnpad(out), nil
}

// yhMelonCall posts one contents endpoint and returns the decrypted
// business JSON (or the decrypted error envelope, for the caller to read).
func yhMelonCall(path string, biz map[string]string) (map[string]interface{}, error) {
	full := map[string]string{
		"oauth_id":   "841c93e12c80a2cb8840a5ef7ff20a9f",
		"device_id":  "841c93e12c80a2cb8840a5ef7ff20a9f",
		"bundle_id":  "com.pwa.hlbdy",
		"version":    "1.8.9",
		"oauth_type": "web",
		"language":   "zh",
		"via":        "pwa",
		"token":      "",
		"trace_id":   "",
		"aff_x_code": "",
		"sid":        yhMelonSID(),
		"user_agent": yhMelonAppUA,
		"user_type":  "normal",
	}
	for k, v := range biz {
		full[k] = v
	}
	jb, _ := json.Marshal(full)
	data, err := yhMelonEnc(jb)
	if err != nil {
		return nil, err
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sum := sha256.Sum256([]byte("client=pwa&data=" + data + "&timestamp=" + ts + yhMelonSecret))
	sign := fmt.Sprintf("%x", md5.Sum([]byte(hex.EncodeToString(sum[:]))))
	form := url.Values{
		"timestamp": {ts},
		"_ver":      {"v2"},
		"client":    {"pwa"},
		"data":      {data},
		"sign":      {sign},
	}.Encode()
	body, code, _, err := httpDo("POST", yhMelonBase+path, strings.NewReader(form), yhMelonHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_melon http %d", code)
	}
	var env map[string]interface{}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("yh_melon parse: %v", err)
	}
	enc, ok := env["data"].(string)
	if !ok {
		return env, nil
	}
	plain, err := yhMelonDec(enc)
	if err != nil {
		return nil, err
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(plain, &obj); err != nil {
		return nil, fmt.Errorf("yh_melon plain parse: %v", err)
	}
	return obj, nil
}

func yhMelonCover(rec map[string]interface{}) string {
	for _, e := range yhArr(rec["fields"]) {
		f := yhMap(e)
		if f == nil {
			continue
		}
		if g5StrVal(f["name"]) != "banner" {
			continue
		}
		if sv := g5StrVal(f["str_value"]); strings.HasPrefix(sv, "http") {
			return sv
		}
	}
	for _, e := range yhArr(rec["fields"]) {
		f := yhMap(e)
		if f == nil {
			continue
		}
		if sv := g5StrVal(f["str_value"]); strings.HasPrefix(sv, "http") {
			return sv
		}
	}
	return ""
}

func yhMelonCoverFetch(u string) ([]byte, error) {
	body, code, _, err := httpGet(u, yhMelonHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_melon cover http %d", code)
	}
	if len(body) >= 4 && (body[0] == 0xFF && body[1] == 0xD8 || body[0] == 0x89 && body[1] == 0x50 || body[0] == 'R' && body[1] == 'I' && body[2] == 'F' && body[3] == 'F' || body[0] == 'G' && body[1] == 'I' && body[2] == 'F') {
		return body, nil
	}
	block, err := aes.NewCipher([]byte("f5d965df75336270"))
	if err != nil {
		return nil, err
	}
	trimmed := body[:len(body)/aes.BlockSize*aes.BlockSize]
	out := make([]byte, len(trimmed))
	cipher.NewCBCDecrypter(block, []byte("97b60394abc2fbe1")).CryptBlocks(out, trimmed)
	return out, nil
}

func yhMelonCards(slot string, list []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range list {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		cid, hasCid := g5MapInt(rec, "cid")
		if !hasCid || cid <= 0 {
			continue // ads_field items (cid <= 0) are dropped
		}
		id := strconv.Itoa(cid)
		title := g5StrVal(rec["title"])
		if title == "" || seen[id] {
			continue
		}
		seen[id] = true
		remark := g5StrVal(rec["created"])
		if v, ok := g5MapInt(rec, "view"); ok && v > 0 {
			remark = strings.TrimSpace(remark + " " + strconv.Itoa(v) + "播放")
		}
		out = append(out, makeDrama(slot, id, title, yhMelonCover(rec), remark, "", ""))
	}
	return out
}

func yhMelonCategories(slot string) []interface{} {
	out := []interface{}{}
	for _, c := range yhMelonChannels {
		out = append(out, categoryObj("ch:"+c.id, c.name))
	}
	for _, o := range yhMelonOrders {
		out = append(out, categoryObj("hot:"+o.id, "热门·"+o.name))
	}
	return out
}

func yhMelonCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	biz := map[string]string{
		"page": strconv.Itoa(page), "limit": "30",
	}
	switch {
	case category == "" || strings.HasPrefix(category, "ch:"):
		mid := "0"
		if strings.HasPrefix(category, "ch:") {
			mid = strings.TrimPrefix(category, "ch:")
		}
		biz["mid"] = mid
	case strings.HasPrefix(category, "hot:"):
		// Hot tabs ride the same mid slot as channels in the App.
		biz["mid"] = strings.TrimPrefix(category, "hot:")
	default:
		biz["mid"] = category
	}
	obj, err := yhMelonCall("/api.php/api/contents/list_contents", biz)
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		// Error envelope: {data:[], status:0, msg}
		return nil, 0, false, fmt.Errorf("yh_melon: %v", obj["msg"])
	}
	items := yhMelonCards(slot, yhArr(data["list"]))
	return items, 0, len(items) >= 30, nil
}

func yhMelonDetailData(cid string) (map[string]interface{}, error) {
	obj, err := yhMelonCall("/api.php/api/contents/detail_content", map[string]string{"cid": cid})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_melon: %v", obj["msg"])
	}
	cur := yhMap(data["cur"])
	if cur == nil {
		return nil, fmt.Errorf("yh_melon: empty detail for %s", cid)
	}
	return cur, nil
}

// yhMelonVideoSrc extracts the first <video src="...">, as in h4.b.k.
func yhMelonVideoSrc(html string) string {
	idx := strings.Index(strings.ToLower(html), "<video")
	if idx < 0 {
		return ""
	}
	seg := html[idx:]
	if end := strings.Index(seg, ">"); end >= 0 {
		seg = seg[:end]
	}
	for _, key := range []string{`src="`, `src='`} {
		if i := strings.Index(seg, key); i >= 0 {
			rest := seg[i+len(key):]
			if j := strings.IndexAny(rest, `"'`); j >= 0 {
				return rest[:j]
			}
		}
	}
	return ""
}

func yhMelonDetail(slot, rawID string) (map[string]interface{}, error) {
	cid := rawID
	if i := strings.Index(rawID, "@@"); i >= 0 {
		cid = rawID[:i]
	}
	cur, err := yhMelonDetailData(cid)
	if err != nil {
		return nil, err
	}
	title := g5StrVal(cur["title"])
	cover := ""
	content := g5StrVal(cur["content"])
	// Poster: the <video pic="..."> attribute when present.
	if idx := strings.Index(content, "pic=\""); idx >= 0 {
		rest := content[idx+5:]
		if j := strings.Index(rest, "\""); j >= 0 {
			cover = rest[:j]
		}
	}
	desc := yhMelonStripHTML(content)
	d := makeDrama(slot, rawID, title, cover, g5StrVal(cur["created"]), "", desc)
	g5SetChapters(d, slot, rawID, []string{cid}, []string{"正片"})
	return d, nil
}

func yhMelonStripHTML(s string) string {
	var b bytes.Buffer
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
			b.WriteByte(' ')
		default:
			if !inTag {
				b.WriteRune(r)
			}
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	if len(out) > 600 {
		out = out[:600] + "…"
	}
	return out
}

func yhMelonResolve(slot, rawID string) (map[string]interface{}, error) {
	cid := rawID
	if i := strings.Index(rawID, "@@"); i >= 0 {
		cid = rawID[:i]
	}
	cur, err := yhMelonDetailData(cid)
	if err != nil {
		return nil, err
	}
	u := yhMelonVideoSrc(g5StrVal(cur["content"]))
	if u == "" {
		return nil, fmt.Errorf("yh_melon: no video in detail %s", cid)
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhMelonUA, yhMelonBase+"/", format, protocol), nil
}

func yhMelonSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhMelonCall("/api.php/api/contents/search", map[string]string{
		"page": "1", "limit": "30", "kwy": kw,
	})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_melon: %v", obj["msg"])
	}
	return yhMelonCards(slot, yhArr(data["list"])), nil
}

func init() {
	registerSite("yh_melon", &siteImpl{
		Key:        "yh_melon",
		Name:       "melon",
		DefaultCat: "ch:0",
		HomeURL:    yhMelonBase + "/",
		Categories: yhMelonCategories,
		Catalog:    yhMelonCatalog,
		Detail:     yhMelonDetail,
		Resolve:    yhMelonResolve,
		Search:     yhMelonSearch,
		Headers:    yhMelonHeaders,
		CoverFetch: yhMelonCoverFetch,
	}, "yh_melon", "melon")
}
