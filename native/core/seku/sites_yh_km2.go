package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ---- xin-kuaimao （新快猫， type 21）: maomi-family signed + AES-CBC envelope ----
// Recipe: yuhuo km2 module, jadx s3 package read line by line (NOT by km
// analogy — the two builders differ in three places, all reproduced here):
//   * s3.a.c (video/area endpoints) vs s3.a.a (brothel/info only):
//       form = {api_token:"", data:HEX, device:"android",
//               device_type:"MI8", device_version:"8.0.0", version_code:"2"}
//       a() is the ONLY form that carries the built-in JWT as api_token;
//       it is used solely by /api/brothel/info (excluded below).
//   * signature (s3.a.a/c, identical in both): over the JSON payload keys
//       in INSERTION order (org.json order, not sorted): "&k=v&k=v..." with
//       String values URLEncoder-encoded, leading "&" stripped, then the
//       128-char salt below appended with no separator; signature =
//       MD5(...).toUpperCase(), inserted into the JSON as "signature"
//       BEFORE encryption. (km sorts keys and appends maomi_pass_xyz;
//       km2 does neither — that is the family difference.)
//   * encryption (s3.a.b/e/f): AES-128-CBC/PKCS5, key = e(getNewKm2) =
//       "x;j/6olSp})&{ZJD", IV = f(getNewKm1) = "znbV%$JN5olCpt<c"
//       (both already 16 chars, so the pad/truncate-to-16 is a no-op);
//       ciphertext as UPPERHEX. Response is the same hex envelope,
//       decrypted by the s3.c interceptor (hex-decode -> AES-CBC decrypt).
//   * endpoints (s3.b, the complete table): /api/video/index,
//       /api/video/info, /api/area/index, /api/brothel/index,
//       /api/brothel/info. There is NO /api/photos endpoint in km2 and
//       NO search endpoint; search below is a client-side scan of the
//       three video feeds (same approach as yh_km).
//   * base (s3.d + Km2LaunchActivity): h6.b default
//       http://150.109.44.66:8099; launch fallbacks 119.28.228.253 and the
//       HTTP-DNS answer for dn=app.newkm-azuercdn.com. Verified live
//       2026-10-24 at http://16.163.155.131:8099 (that day's HTTP-DNS
//       answer); the Go client tries HTTPDNS first, then the verified IP,
//       then the two launch fallbacks, and caches the first base that
//       returns code 200.
//
// EXCLUDED BY PRODUCT RULE (documented, not an oversight):
//   * brothel dimension (/api/brothel/index, /api/brothel/info and the
//       /api/area/index city list that feeds it): offline-service listings
//       (MMInfo carries phone/qq/wechat/price fields) — same exclusion as
//       the sex8/fengliu brothel family. The km2 "picture" slot (type 46)
//       is exactly this brothel/MM list (Km2LaunchActivity isMM=true ->
//       MMActivity -> /api/brothel/index); it is NOT a photos endpoint,
//       so no sites_yh_km2pic.go is produced — the type-46 content is the
//       excluded brothel set, not a video/picture set.
//   * VIP/gold content: KMVideoList.getList() drops type!=1, gold>0 and
//       is_vip!="0" items; the Go card filter replicates that verbatim.
//       Detail is_vip is NOT used as a filter (live sample: list item
//       is_vip=0 but its detail reports is_vip=1 while m3u8_url is still
//       served — detail is_vip does not gate playback here).
//
// JWT FINDING (live-tested 2026-10-24, do NOT "fix" by sending it):
//   * The built-in JWT (s3.a.a, exp=1668724920, user_id 5310840) is
//       REJECTED by the server today: /api/video/index with it in
//       api_token returns code 403 "登陆失效，请重新登陆！".
//   * The video endpoints are built by s3.a.c, which sends api_token=""
//       — and that form returns code 200. So the expired JWT is simply
//       not sent for any video call; it is only ever used by the
//       excluded brothel/info call.
//   * /api/config/index (the launch host probe) returns HTTP 500 for
//       both the real JWT and a garbage token, so it is not a token
//       discriminator — the app itself treats >300 as a dead host.
//
// LIVE VERIFICATION 2026-10-24 (base http://16.163.155.131:8099, Go smoke
// in go_build_km2, all legs below against this build's own code):
//   * Categories: App navigation 1:1 — DisCoverFragment tabs
//       热门(type 3) / 发现(type 2) / 精选(type 1). Each page-1 feed
//       returns 11 items (incl. type!=1 ads, filtered to free videos).
//   * Catalog: type 3 page 2 returns 11 more items with a different first
//       id (pagination works); list scan pages 1-2: 22 items = 2 ads +
//       20 free, 0 gold, 0 vip.
//   * Detail: video 100001705 -> title/nickname/praise_num 1796,
//       cover, m3u8_url + normal_url present, vip_url empty.
//   * Cover: real JPEG (FF D8 FF E0, JFIF) fetched from the API cover URL.
//   * Resolve: m3u8 playlist is a real VOD HLS list (EXT-X-KEY AES-128);
//       first segment (2,124,032 B) AES-decrypted with the playlist key
//       starts 47 40 00 10 (TS sync) — playback byte-verified.
//   * Search: no server endpoint; client-side scan (same as yh_km).
//   * Server success code is 200 (NOT km's 0) with msg "查询成功".
//
// Symbol prefix: every package-level symbol here is yhKM2* (the guide's
// collision rule for co-existing port files).

const yhKM2Slot = "yh_km2"
const yhKM2UA = "okhttp/4.9.0"

var yhKM2Key = []byte("x;j/6olSp})&{ZJD")
var yhKM2IV = []byte("znbV%$JN5olCpt<c")

// yhKM2Salt is s3.a's long obfuscated suffix, decoded from ad.r.o
// (XOR key "7bksfs51ss4qxjck") — the exact 128 chars appended to the
// signature string. Kept as a double-quoted literal because it contains
// a backtick.
var yhKM2Salt = "ohI}-bFpD*z8)W7~REusVa]U`YKQ=[C1&XZ.\"n5:dl<{?@J6NkO+f%c^\"$tevxB>j2M_9;G#y3Tw|gL/HS,Pqr0!Ami(49Y_.~Tan#z{5ZLO,_E(7!vJ^HC5_{Xq5$z*"

var (
	yhKM2BaseMu sync.Mutex
	yhKM2Base   string
	yhKM2Cache  sync.Map // video id -> map[string]interface{} (raw list/detail item)
)

func yhKM2Pad16(s string) []byte {
	b := []byte(s)
	if len(b) > 16 {
		return b[:16]
	}
	for len(b) < 16 {
		b = append(b, '0')
	}
	return b
}

func yhKM2Pad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhKM2Unpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

func yhKM2Enc(plain []byte) string {
	block, _ := aes.NewCipher(yhKM2Pad16(string(yhKM2Key)))
	padded := yhKM2Pad(plain)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, yhKM2Pad16(string(yhKM2IV))).CryptBlocks(out, padded)
	return strings.ToUpper(hex.EncodeToString(out))
}

func yhKM2Dec(hexs string) ([]byte, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexs))
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(yhKM2Pad16(string(yhKM2Key)))
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhKM2Pad16(string(yhKM2IV))).CryptBlocks(out, raw)
	return yhKM2Unpad(out), nil
}

// yhKM2JavaEncode mirrors java.net.URLEncoder.encode (UTF-8) for the
// signature string: Go's QueryEscape already matches for digits, but
// Java also escapes '~' (%7E) and keeps '*' — fix the one divergence.
func yhKM2JavaEncode(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "~", "%7E")
}

// yhKM2Pair is one JSON payload field in insertion order (org.json order
// matters for the signature, so pairs are a slice, not a map).
type yhKM2Pair struct {
	key string
	val string // already JSON-rendered: digits bare, strings quoted by caller
	raw string // signature value: int as digits, string as-is
}

func yhKM2IntPair(k string, v int) yhKM2Pair {
	return yhKM2Pair{key: k, val: strconv.Itoa(v), raw: strconv.Itoa(v)}
}

func yhKM2StrPair(k, v string) yhKM2Pair {
	return yhKM2Pair{key: k, val: strconv.Quote(v), raw: v}
}

// yhKM2Form builds the s3.a.c form for a payload (signature + AES hex).
func yhKM2Form(pairs []yhKM2Pair) map[string]string {
	parts := []string{}
	for _, pr := range pairs {
		parts = append(parts, pr.key+"="+yhKM2JavaEncode(pr.raw))
	}
	sig := fmt.Sprintf("%X", md5.Sum([]byte(strings.Join(parts, "&")+yhKM2Salt)))
	var sb strings.Builder
	sb.WriteString("{")
	for i, pr := range pairs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(strconv.Quote(pr.key) + ":" + pr.val)
	}
	sb.WriteString(`,"signature":` + strconv.Quote(sig) + "}")
	return map[string]string{
		"api_token":      "",
		"data":           yhKM2Enc([]byte(sb.String())),
		"device":         "android",
		"device_type":    "MI8",
		"device_version": "8.0.0",
		"version_code":   "2",
	}
}

func yhKM2Candidates() []string {
	out := []string{}
	req, _ := http.NewRequest("GET", "http://119.28.28.28/d?dn=app.newkm-azuercdn.com", nil)
	if resp, err := (&http.Client{Timeout: 8 * time.Second, Transport: sharedTransport}).Do(req); err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		parts := strings.FieldsFunc(string(b), func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == ' ' })
		if len(parts) > 0 && parts[0] != "" {
			out = append(out, "http://"+parts[0]+":8099")
		}
	}
	for _, ip := range []string{"16.163.155.131", "119.28.228.253", "150.109.44.66"} {
		out = append(out, "http://"+ip+":8099")
	}
	return out
}

func yhKM2Call(path string, pairs []yhKM2Pair) (map[string]interface{}, error) {
	form := yhKM2Form(pairs)
	body := url.Values{}
	for k, v := range form {
		body.Set(k, v)
	}
	h := map[string]string{
		"User-Agent":   yhKM2UA,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	yhKM2BaseMu.Lock()
	bases := []string{}
	if yhKM2Base != "" {
		bases = append(bases, yhKM2Base)
	}
	for _, c := range yhKM2Candidates() {
		if c != yhKM2Base {
			bases = append(bases, c)
		}
	}
	yhKM2BaseMu.Unlock()
	var lastErr error
	for _, base := range bases {
		raw, code, _, err := httpDo("POST", base+path, strings.NewReader(body.Encode()), h)
		if err != nil {
			lastErr = err
			continue
		}
		if code != 200 {
			lastErr = fmt.Errorf("yh_km2 http %d", code)
			continue
		}
		trimmed := strings.TrimSpace(string(raw))
		var plain []byte
		if strings.HasPrefix(trimmed, "{") {
			plain = []byte(trimmed)
		} else {
			plain, err = yhKM2Dec(trimmed)
			if err != nil {
				lastErr = fmt.Errorf("yh_km2 decrypt: %v", err)
				continue
			}
		}
		var out map[string]interface{}
		if err := json.Unmarshal(plain, &out); err != nil {
			lastErr = fmt.Errorf("yh_km2 parse: %v", err)
			continue
		}
		if c, ok := out["code"].(float64); ok && c != 200 {
			lastErr = fmt.Errorf("yh_km2 code %v: %s", c, g5StrVal(out["msg"]))
			continue
		}
		yhKM2BaseMu.Lock()
		yhKM2Base = base
		yhKM2BaseMu.Unlock()
		return out, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("yh_km2: no base responded")
	}
	return nil, lastErr
}

// yhKM2Keep replicates KMVideoList.getList(): drop ads (type!=1) and
// paid items (gold>0 or is_vip!="0").
func yhKM2Keep(rec map[string]interface{}) bool {
	if t, ok := rec["type"].(float64); ok && t != 1 {
		return false
	}
	if g, ok := rec["gold"].(float64); ok && g > 0 {
		return false
	}
	if v := g5StrVal(rec["is_vip"]); v != "" && v != "0" {
		return false
	}
	return true
}

func yhKM2Card(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	if !yhKM2Keep(rec) {
		return nil, false
	}
	id := g5StrVal(rec["id"])
	title := g5StrVal(rec["title"])
	if id == "" || title == "" {
		return nil, false
	}
	yhKM2Cache.Store(id, rec)
	remark := g5StrVal(rec["nickname"])
	desc := ""
	if pr, ok := rec["praise_num"].(float64); ok && pr > 0 {
		desc = "点赞 " + strconv.FormatInt(int64(pr), 10)
	}
	return makeDrama(slot, id, title, g5StrVal(rec["cover"]), remark, "", desc), true
}

func yhKM2Cards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhKM2Card(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhKM2List(slot string, typ, page int) ([]interface{}, int, bool, error) {
	obj, err := yhKM2Call("/api/video/index", []yhKM2Pair{yhKM2IntPair("page", page), yhKM2IntPair("type", typ)})
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}, 0, false, nil
	}
	recs := yhArr(data["video_list"])
	if len(recs) == 0 {
		recs = yhArr(data["list"])
	}
	items := yhKM2Cards(slot, recs)
	return items, len(items), len(items) >= 10, nil
}

// Categories are the App's DisCoverFragment tabs 1:1 (order preserved).
func yhKM2Categories(slot string) []interface{} {
	return []interface{}{
		categoryObj("3", "热门"),
		categoryObj("2", "发现"),
		categoryObj("1", "精选"),
	}
}

func yhKM2Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	typ := 3
	if t, err := strconv.Atoi(category); err == nil && t >= 1 && t <= 3 {
		typ = t
	}
	return yhKM2List(slot, typ, page)
}

func yhKM2Info(rawID string) (map[string]interface{}, error) {
	obj, err := yhKM2Call("/api/video/info", []yhKM2Pair{yhKM2StrPair("video_id", rawID)})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_km2: empty detail for %s", rawID)
	}
	vi := yhMap(data["video_info"])
	if vi == nil {
		return nil, fmt.Errorf("yh_km2: no video_info for %s", rawID)
	}
	return vi, nil
}

func yhKM2Detail(slot, rawID string) (map[string]interface{}, error) {
	vi, err := yhKM2Info(rawID)
	if err != nil {
		return nil, err
	}
	if g, ok := vi["gold"].(float64); ok && g > 0 {
		return nil, fmt.Errorf("yh_km2: paid video %s (gold %v) not served", rawID, g)
	}
	title := g5StrVal(vi["title"])
	if title == "" {
		if v, ok := yhKM2Cache.Load(rawID); ok {
			title = g5StrVal(v.(map[string]interface{})["title"])
		}
	}
	d := makeDrama(slot, rawID, title, g5StrVal(vi["cover"]), g5StrVal(vi["nickname"]), "", "")
	if actor := g5StrVal(vi["nickname"]); actor != "" {
		d["actor"] = actor
		d["vod_actor"] = actor
	}
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	return d, nil
}

func yhKM2Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	vi, err := yhKM2Info(vid)
	if err != nil {
		return nil, err
	}
	// App default (VideoPlayActivity.f3706f=false): m3u8_url; the
	// MineFragment radio toggle to normal_url is a user setting, not a
	// second line — fall back to normal_url only if m3u8 is absent.
	u := g5StrVal(vi["m3u8_url"])
	format, protocol := "m3u8", "hls"
	if u == "" {
		u = g5StrVal(vi["normal_url"])
		format, protocol = "mp4", ""
	}
	if u == "" {
		return nil, fmt.Errorf("yh_km2: no play url for %s", vid)
	}
	return playResult(u, slot, rawID, yhKM2UA, yhKM2Base+"/", format, protocol), nil
}

func yhKM2Search(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	seen := map[string]bool{}
	lkw := strings.ToLower(kw)
	// No server-side search endpoint exists in s3.b; scan the three
	// feeds (<=5 pages each), stop at 20 hits — same shape as yh_km.
	for _, typ := range []int{3, 2, 1} {
		for p := 1; p <= 5 && len(out) < 20; p++ {
			items, _, _, err := yhKM2List(slot, typ, p)
			if err != nil {
				break
			}
			for _, e := range items {
				m, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				id := g5StrVal(m["vod_id"])
				hay := strings.ToLower(g5StrVal(m["title"]) + " " + g5StrVal(m["remark"]))
				if id == "" || seen[id] || !strings.Contains(hay, lkw) {
					continue
				}
				seen[id] = true
				out = append(out, e)
			}
		}
	}
	return out, nil
}

func yhKM2Headers() map[string]string {
	return map[string]string{"User-Agent": yhKM2UA}
}

func init() {
	registerSite(yhKM2Slot, &siteImpl{
		Key:        yhKM2Slot,
		Name:       "🔞新快猫",
		DefaultCat: "3",
		HomeURL:    "http://app.newkm-azuercdn.com/",
		Headers:    yhKM2Headers,
		Categories: yhKM2Categories,
		Catalog:    yhKM2Catalog,
		Detail:     yhKM2Detail,
		Resolve:    yhKM2Resolve,
		Search:     yhKM2Search,
	})
	// Integration: append "yh_km2" to slotOrder (registry.go tail) and
	// add the builtin_config entry; brothel/MM (type 46) is deliberately
	// NOT registered — see the header note.
}
