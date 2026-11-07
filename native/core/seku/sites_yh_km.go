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
	"sort"
	"strings"
	"sync"
	"time"
)

// ---- kuaimao （快猫）: maomi-family signed + AES-CBC envelope API ----
// Recipe: yuhuo km module (q3.b request builder / q3.a ApiService),
// live-verified 2026-10-24 against the HTTP-DNS-discovered host:
// boot/list/listHot/getUserInfo all return code=0 with same-day content,
// covers are real images. Play CDN hosts (video.km7p7.xyz / vv.4455sh.com /
// 119.28.204.36) were NOT reachable from the build sandbox egress
// (fake-IP DNS + direct connect refused, same class as yh_bale), so the
// play leg is code-proven but byte-unverified from here — see the port
// report. Play URLs come straight from the API (mv_play_url) with the
// app's own host rewrite applied verbatim:
//   contains 47.244.98.179:8089 -> http://47.244.98.179:8089 => https://vv.4455sh.com
//   else http://119.28.204.36   => https://video.km7p7.xyz
//
// Request: POST {base}<path>, form fields:
//   data = UPPERHEX( AES-128-CBC/PKCS5( JSON(payload), key, iv ) )
//   + fixed device fields (_device_id/_app_version/_device_type/
//     _sdk_version/_device_version)
//   sig  = MD5( sorted "k=v&..." (values URL-encoded) + "maomi_pass_xyz" )
// Response body is the same AES-CBC hex envelope -> {code,message,data}.
// Base discovery: GET http://119.28.28.28/d?dn=api.cheetah888.com ->
// plain IP list -> http://<ip>:8099 (the h6.b default 150.109.45.166:8099
// currently refuses; discovery is the app's own mechanism).

const yhKMSlot = "yh_km"
const yhKMUA = "okhttp/4.9.0"

var yhKMKey = []byte("625202f9149maomi")
var yhKMIV = []byte("5efd3f6060emaomi")

var yhKMDevice = map[string]string{
	"_device_id":     "ffffffff-da25-75a2-0000-00007ac5aad8",
	"_app_version":   "1.1.7",
	"_device_type":   "SM-A205GN",
	"_sdk_version":   "23",
	"_device_version": "28",
}

var (
	yhKMBaseMu sync.Mutex
	yhKMBase   string
	yhKMCache  sync.Map // mv_id -> map[string]interface{} (raw list item)
)

func yhKMDiscover() string {
	yhKMBaseMu.Lock()
	defer yhKMBaseMu.Unlock()
	if yhKMBase != "" {
		return yhKMBase
	}
	yhKMBase = yhKMFirstCandidate()
	return yhKMBase
}

func yhKMFirstCandidate() string {
	if cands := yhKMCandidates(); len(cands) > 0 {
		return cands[0]
	}
	return "http://150.109.45.166:8099"
}

func yhKMCandidates() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(ip string) {
		ip = strings.TrimSpace(ip)
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		out = append(out, "http://"+ip+":8099")
	}
	req, _ := http.NewRequest("GET", "http://119.28.28.28/d?dn=api.cheetah888.com", nil)
	if resp, err := (&http.Client{Timeout: 10 * time.Second, Transport: sharedTransport}).Do(req); err == nil {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		for _, p := range strings.FieldsFunc(string(b), func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == ' ' }) {
			add(p)
		}
	}
	add("150.109.45.166")
	return out
}

func yhKMSetBase(base string) {
	yhKMBaseMu.Lock()
	yhKMBase = base
	yhKMBaseMu.Unlock()
}

func yhKMPad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhKMUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

func yhKMEnc(plain []byte) string {
	block, _ := aes.NewCipher(yhKMKey)
	padded := yhKMPad(plain)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, yhKMIV).CryptBlocks(out, padded)
	return strings.ToUpper(hex.EncodeToString(out))
}

func yhKMDec(hexs string) ([]byte, error) {
	raw, err := hex.DecodeString(hexs)
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(yhKMKey)
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhKMIV).CryptBlocks(out, raw)
	return yhKMUnpad(out), nil
}

func yhKMCall(path string, payload map[string]interface{}) (map[string]interface{}, error) {
	bases := []string{}
	seen := map[string]bool{}
	push := func(b string) {
		if b != "" && !seen[b] {
			seen[b] = true
			bases = append(bases, b)
		}
	}
	push(yhKMDiscover())
	for _, b := range yhKMCandidates() {
		push(b)
	}
	var lastErr error
	for _, b := range bases {
		out, err := yhKMCallOnce(b, path, payload)
		if err == nil {
			yhKMSetBase(b)
			return out, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("yh_km: no base available")
	}
	return nil, lastErr
}

func yhKMCallOnce(base, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	jb, _ := json.Marshal(payload)
	form := map[string]string{"data": yhKMEnc(jb)}
	for k, v := range yhKMDevice {
		form[k] = v
	}
	keys := []string{}
	for k := range form {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := []string{}
	for _, k := range keys {
		parts = append(parts, k+"="+url.QueryEscape(form[k]))
	}
	form["sig"] = fmt.Sprintf("%x", md5.Sum([]byte(strings.Join(parts, "&")+"maomi_pass_xyz")))
	body := url.Values{}
	for k, v := range form {
		body.Set(k, v)
	}
	h := map[string]string{
		"User-Agent":   yhKMUA,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	raw, code, _, err := httpDo("POST", base+path, strings.NewReader(body.Encode()), h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_km http %d", code)
	}
	trimmed := strings.TrimSpace(string(raw))
	var plain []byte
	if strings.HasPrefix(trimmed, "{") {
		plain = []byte(trimmed)
	} else {
		plain, err = yhKMDec(trimmed)
		if err != nil {
			return nil, fmt.Errorf("yh_km decrypt: %v", err)
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_km parse: %v", err)
	}
	if c, ok := out["code"].(float64); ok && c != 0 {
		return nil, fmt.Errorf("yh_km code %v: %s", c, g5StrVal(out["message"]))
	}
	return out, nil
}

func yhKMPlayRewrite(u string) string {
	if strings.Contains(u, "47.244.98.179:8089") {
		return strings.Replace(u, "http://47.244.98.179:8089", "https://vv.4455sh.com", 1)
	}
	return strings.Replace(u, "http://119.28.204.36", "https://video.km7p7.xyz", 1)
}

func yhKMCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	if ad, ok := rec["is_cat_ads"].(float64); ok && ad == 1 {
		return nil, false
	}
	id := g5StrVal(rec["mv_id"])
	title := g5StrVal(rec["mv_title"])
	if id == "" || title == "" {
		return nil, false
	}
	yhKMCache.Store(id, rec)
	remark := g5StrVal(rec["mu_name"])
	desc := ""
	if like := g5StrVal(rec["mv_like"]); like != "" {
		desc = "点赞 " + like
	}
	return makeDrama(slot, id, title, g5StrVal(rec["mv_img_url"]), remark, "", desc), true
}

func yhKMCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhKMCard(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhKMList(slot, path string, page int) ([]interface{}, int, bool, error) {
	obj, err := yhKMCall(path, map[string]interface{}{"page": page, "perPage": 20})
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}, 0, false, nil
	}
	items := yhKMCards(slot, yhArr(data["list"]))
	return items, len(items), len(items) >= 20, nil
}

func yhKMCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("new", "最新"),
		categoryObj("hot", "热门"),
	}
}

func yhKMCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if category == "hot" {
		return yhKMList(slot, "/api/videos/listHot", page)
	}
	return yhKMList(slot, "/api/videos/listAll", page)
}

func yhKMFind(slot, mvID string) (map[string]interface{}, error) {
	if v, ok := yhKMCache.Load(mvID); ok {
		return v.(map[string]interface{}), nil
	}
	for _, path := range []string{"/api/videos/listAll", "/api/videos/listHot"} {
		for p := 1; p <= 5; p++ {
			if _, _, _, err := yhKMList(slot, path, p); err != nil {
				break
			}
			if v, ok := yhKMCache.Load(mvID); ok {
				return v.(map[string]interface{}), nil
			}
		}
	}
	return nil, fmt.Errorf("yh_km: video %s not found in recent lists (no detail API; scanned recent lists only)", mvID)
}

func yhKMDetail(slot, rawID string) (map[string]interface{}, error) {
	rec, err := yhKMFind(slot, rawID)
	if err != nil {
		return nil, err
	}
	title := g5StrVal(rec["mv_title"])
	desc := g5StrVal(rec["mv_created"])
	d := makeDrama(slot, rawID, title, g5StrVal(rec["mv_img_url"]), g5StrVal(rec["mu_name"]), "", desc)
	if actor := g5StrVal(rec["mu_name"]); actor != "" {
		d["actor"] = actor
		d["vod_actor"] = actor
	}
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	return d, nil
}

func yhKMResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	rec, err := yhKMFind(slot, vid)
	if err != nil {
		return nil, err
	}
	u := yhKMPlayRewrite(g5StrVal(rec["mv_play_url"]))
	if u == "" {
		return nil, fmt.Errorf("yh_km: no play url for %s", vid)
	}
	return playResult(u, slot, rawID, yhKMUA, yhKMDiscover()+"/", "mp4", ""), nil
}

func yhKMSearch(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	seen := map[string]bool{}
	lkw := strings.ToLower(kw)
	// No server-side search endpoint exists in the app; scan the newest
	// feed (<=10 pages) and the hot feed (<=3 pages), stop at 20 hits.
	for _, sp := range []struct {
		path  string
		pages int
	}{{"/api/videos/listAll", 10}, {"/api/videos/listHot", 3}} {
		path := sp.path
		for p := 1; p <= sp.pages && len(out) < 20; p++ {
			obj, err := yhKMCall(path, map[string]interface{}{"page": p, "perPage": 20})
			if err != nil {
				break
			}
			data := yhMap(obj["data"])
			if data == nil {
				break
			}
			for _, e := range yhArr(data["list"]) {
				m := yhMap(e)
				if m == nil {
					continue
				}
				id := g5StrVal(m["mv_id"])
				hay := strings.ToLower(g5StrVal(m["mv_title"]) + " " + g5StrVal(m["mu_name"]))
				if id == "" || seen[id] || !strings.Contains(hay, lkw) {
					continue
				}
				seen[id] = true
				if c, ok := yhKMCard(slot, m); ok {
					out = append(out, c)
				}
			}
		}
	}
	return out, nil
}

func yhKMHeaders() map[string]string {
	return map[string]string{"User-Agent": yhKMUA}
}

func init() {
	registerSite(yhKMSlot, &siteImpl{
		Key:        yhKMSlot,
		Name:       "🔞快猫",
		DefaultCat: "new",
		HomeURL:    "http://api.cheetah888.com/",
		Headers:    yhKMHeaders,
		Categories: yhKMCategories,
		Catalog:    yhKMCatalog,
		Detail:     yhKMDetail,
		Resolve:    yhKMResolve,
		Search:     yhKMSearch,
	})
	// Integration: append "yh_km" to slotOrder (registry.go tail) and
	// add the builtin_config entry — see port_out/kuaimao/集成说明.md.
}
