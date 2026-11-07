package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// ---- kuaimao-pic （快猫图片， type 24）: km maomi-family signed + AES-CBC ----
// Recipe: yuhuo km module, jadx q3 package read line by line:
//   * ApiService (q3.a.d): POST to the ABSOLUTE URL
//       http://119.28.65.192:8090/api/photos/listAll  (it overrides the
//       video base; the video host does NOT serve this path — verified
//       2026-10-24: the live km video host 18.167.17.100:8099 answers
//       HTTP 404 for /api/photos/listAll).
//   * Request (PicItemFragment.j, inline — NOT q3.b.a): payload JSON in
//       this exact field order: {"mc_id":"<cat>","page":N,"perPage":20},
//       encrypted with the km key/IV (see below) as UPPERHEX; device
//       fields are the PICTURE set (different from the video set in
//       yh_km.go): _device_id=868152038015467, _app_version=1.1.2,
//       _device_type=MuMu, _sdk_version=23, _device_version=6.0.1;
//       sig = MD5(sorted "k=v&..." with values URL-encoded +
//       "maomi_pass_xyz") in lowercase hex (q3.b.b, same as yh_km).
//   * Response: PicList {code, message, data:{count, page, pageCount,
//       list:[{mc_id, mp_id, mp_title, mp_content:[image URLs],
//       mp_created, mp_updated}]}}. The album IS the list item: the App
//       shows mp_content[0..2] as the card's three covers
//       (PicListAdapter) and opens the full mp_content array in
//       PicBrowseActivity — there is NO separate detail endpoint.
//   * Categories (PicActivity tabs 1:1, fragment order = tab order):
//       1099910 推女郎写真 / 1099912 兔宝宝 / 1099916 假面女皇 /
//       1099915 Pantyhose / 1099908 美媛馆新刊 / 1099901 V女郎 /
//       1099902 推女神 / 1099903 爱蜜社 / 1099904 秀人网 /
//       1099905 嗲囡囡.
//
// DISPOSITION vs yh_km (evidence, not a guess): the picture API is the
// same km engine (same q3.b encryption, same maomi_pass_xyz signature,
// same module, same request builder family) but a DISTINCT content type
// (photo albums, not videos), a DISTINCT host (dedicated :8090 photos
// host, not the video host), a DISTINCT device fingerprint and its own
// 10-category navigation. It is therefore shipped as its own file/slot
// (yh_kmpic) rather than folded into yh_km's video categories —
// folding would mix two hosts, two device sets and two content types
// into one Catalog. The shared-engine evidence above is why the two
// files' crypto is identical by construction.
//
// VERIFICATION STATUS — READ BEFORE INTEGRATING: this leg is
// CODE-PROVEN BUT LIVE-UNVERIFIED from the build sandbox, and the file
// says so instead of claiming a pass:
//   * 2026-10-24, three POST attempts to 119.28.65.192:8090 (plus the
//       same form to :8099 and :80 on that IP) all ended with the
//       connection closed without any HTTP response — not a 403/404,
//       not a timeout, the host sends nothing. The identical form
//       against the live video host DID reach a server (HTTP 404),
//       which proves the request construction is routable; the photos
//       host itself is unresponsive from this egress.
//   * Per the porting guide this is recorded as "出口未验" (host
//       unresponsive from here), NOT as "station dead": the same class
//       of first-connect flakiness was seen on km2 before it answered.
//       Re-verify from a phone/usable egress before treating the slot
//       as live; the code needs no change if the host answers, since
//       every field above is copied verbatim from jadx.
//   * Success-code convention for this endpoint is unconfirmed (the km
//       video family uses code 0; km2 uses 200): the call accepts
//       either 0 or 200 and reports any other code with its message.
//   * Photo-album Detail/Resolve shape: Detail returns the album with
//       one chapter per image (图1..图N); Resolve returns the image
//       URL. The chapter/URL plumbing follows the bale Detail pattern;
//       host-side album display is unconfirmed until the first live
//       response lands (field names are bean-verbatim, so the parse
//       itself is not a guess).
//
// Symbol prefix: every package-level symbol here is yhKMPic*.

const yhKMPicSlot = "yh_kmpic"
const yhKMPicUA = "okhttp/4.9.0"
const yhKMPicBase = "http://119.28.65.192:8090"

var yhKMPicKey = []byte("625202f9149maomi")
var yhKMPicIV = []byte("5efd3f6060emaomi")

var yhKMPicDevice = map[string]string{
	"_device_id":      "868152038015467",
	"_app_version":    "1.1.2",
	"_device_type":    "MuMu",
	"_sdk_version":    "23",
	"_device_version": "6.0.1",
}

var yhKMPicCache sync.Map // mp_id -> map[string]interface{} (raw album item)

func yhKMPicPad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhKMPicUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

func yhKMPicEnc(plain []byte) string {
	block, _ := aes.NewCipher(yhKMPicKey)
	padded := yhKMPicPad(plain)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, yhKMPicIV).CryptBlocks(out, padded)
	return strings.ToUpper(hex.EncodeToString(out))
}

func yhKMPicDec(hexs string) ([]byte, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexs))
	if err != nil {
		return nil, err
	}
	block, _ := aes.NewCipher(yhKMPicKey)
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhKMPicIV).CryptBlocks(out, raw)
	return yhKMPicUnpad(out), nil
}

// yhKMPicCall posts one PicItemFragment-form request and returns the
// decoded PicList envelope.
func yhKMPicCall(mcID string, page int) (map[string]interface{}, error) {
	payload := fmt.Sprintf(`{"mc_id":%s,"page":%d,"perPage":20}`, strconv.Quote(mcID), page)
	form := map[string]string{"data": yhKMPicEnc([]byte(payload))}
	for k, v := range yhKMPicDevice {
		form[k] = v
	}
	keys := []string{}
	for k := range form {
		keys = append(keys, k)
	}
	// TreeMap order in the App = sorted keys.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
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
		"User-Agent":   yhKMPicUA,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	raw, code, _, err := httpDo("POST", yhKMPicBase+"/api/photos/listAll", strings.NewReader(body.Encode()), h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_kmpic http %d", code)
	}
	trimmed := strings.TrimSpace(string(raw))
	var plain []byte
	if strings.HasPrefix(trimmed, "{") {
		plain = []byte(trimmed)
	} else {
		plain, err = yhKMPicDec(trimmed)
		if err != nil {
			return nil, fmt.Errorf("yh_kmpic decrypt: %v", err)
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_kmpic parse: %v", err)
	}
	if c, ok := out["code"].(float64); ok && c != 0 && c != 200 {
		return nil, fmt.Errorf("yh_kmpic code %v: %s", c, g5StrVal(out["message"]))
	}
	return out, nil
}

func yhKMPicCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("1099910", "推女郎写真"),
		categoryObj("1099912", "兔宝宝"),
		categoryObj("1099916", "假面女皇"),
		categoryObj("1099915", "Pantyhose"),
		categoryObj("1099908", "美媛馆新刊"),
		categoryObj("1099901", "V女郎"),
		categoryObj("1099902", "推女神"),
		categoryObj("1099903", "爱蜜社"),
		categoryObj("1099904", "秀人网"),
		categoryObj("1099905", "嗲囡囡"),
	}
}

func yhKMPicCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["mp_id"])
	title := g5StrVal(rec["mp_title"])
	if id == "" || title == "" {
		return nil, false
	}
	yhKMPicCache.Store(id, rec)
	imgs := yhArr(rec["mp_content"])
	cover := ""
	if len(imgs) > 0 {
		cover = g5StrVal(imgs[0])
	}
	remark := ""
	if len(imgs) > 0 {
		remark = fmt.Sprintf("共%d图", len(imgs))
	}
	return makeDrama(slot, id, title, cover, remark, "", g5StrVal(rec["mp_created"])), true
}

func yhKMPicCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	obj, err := yhKMPicCall(category, page)
	if err != nil {
		return nil, 0, false, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}, 0, false, nil
	}
	out := []interface{}{}
	for _, e := range yhArr(data["list"]) {
		if m := yhMap(e); m != nil {
			if c, ok := yhKMPicCard(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	total := 0
	if pc := g5StrVal(data["pageCount"]); pc != "" {
		if n, err := strconv.Atoi(pc); err == nil {
			total = n
		}
	}
	hasMore := total == 0 || page < total
	return out, total, hasMore, nil
}

func yhKMPicFind(slot, mpID string) (map[string]interface{}, error) {
	if v, ok := yhKMPicCache.Load(mpID); ok {
		return v.(map[string]interface{}), nil
	}
	for _, cat := range []string{"1099910", "1099912", "1099916", "1099915", "1099908", "1099901", "1099902", "1099903", "1099904", "1099905"} {
		for p := 1; p <= 2; p++ {
			if _, _, _, err := yhKMPicCatalog(slot, cat, p); err != nil {
				break
			}
			if v, ok := yhKMPicCache.Load(mpID); ok {
				return v.(map[string]interface{}), nil
			}
		}
	}
	return nil, fmt.Errorf("yh_kmpic: album %s not found in recent lists", mpID)
}

func yhKMPicDetail(slot, rawID string) (map[string]interface{}, error) {
	rec, err := yhKMPicFind(slot, rawID)
	if err != nil {
		return nil, err
	}
	imgs := yhArr(rec["mp_content"])
	cover := ""
	if len(imgs) > 0 {
		cover = g5StrVal(imgs[0])
	}
	d := makeDrama(slot, rawID, g5StrVal(rec["mp_title"]), cover, fmt.Sprintf("共%d图", len(imgs)), "", g5StrVal(rec["mp_created"]))
	epIDs := []string{}
	epNames := []string{}
	for i := range imgs {
		epIDs = append(epIDs, fmt.Sprintf("pic:%s:%d", rawID, i))
		epNames = append(epNames, fmt.Sprintf("图%d", i+1))
	}
	if len(epIDs) > 0 {
		g5SetChapters(d, slot, rawID, epIDs, epNames)
	}
	return d, nil
}

func yhKMPicResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	// Episode ids are "pic:<mp_id>:<index>"; a bare mp_id resolves to
	// its first image.
	mpID, idx := vid, 0
	if strings.HasPrefix(vid, "pic:") {
		rest := strings.TrimPrefix(vid, "pic:")
		if j := strings.LastIndex(rest, ":"); j >= 0 {
			mpID = rest[:j]
			if n, err := strconv.Atoi(rest[j+1:]); err == nil {
				idx = n
			}
		} else {
			mpID = rest
		}
	}
	rec, err := yhKMPicFind(slot, mpID)
	if err != nil {
		return nil, err
	}
	imgs := yhArr(rec["mp_content"])
	if idx < 0 || idx >= len(imgs) {
		return nil, fmt.Errorf("yh_kmpic: image %d out of range for %s", idx, mpID)
	}
	u := g5StrVal(imgs[idx])
	if u == "" {
		return nil, fmt.Errorf("yh_kmpic: empty image url for %s", mpID)
	}
	return playResult(u, slot, rawID, yhKMPicUA, yhKMPicBase+"/", "jpg", ""), nil
}

func yhKMPicSearch(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	seen := map[string]bool{}
	lkw := strings.ToLower(kw)
	// No server-side search endpoint in q3.a; scan category page 1-2,
	// stop at 20 hits — same shape as yh_km.
	for _, cat := range []string{"1099910", "1099912", "1099916", "1099915", "1099908", "1099901", "1099902", "1099903", "1099904", "1099905"} {
		for p := 1; p <= 2 && len(out) < 20; p++ {
			items, _, _, err := yhKMPicCatalog(slot, cat, p)
			if err != nil {
				break
			}
			for _, e := range items {
				m, ok := e.(map[string]interface{})
				if !ok {
					continue
				}
				id := g5StrVal(m["vod_id"])
				if id == "" || seen[id] || !strings.Contains(strings.ToLower(g5StrVal(m["title"])), lkw) {
					continue
				}
				seen[id] = true
				out = append(out, e)
			}
		}
	}
	return out, nil
}

func yhKMPicHeaders() map[string]string {
	return map[string]string{"User-Agent": yhKMPicUA}
}

func init() {
	registerSite(yhKMPicSlot, &siteImpl{
		Key:        yhKMPicSlot,
		Name:       "快猫图片",
		DefaultCat: "1099910",
		HomeURL:    yhKMPicBase + "/",
		Headers:    yhKMPicHeaders,
		Categories: yhKMPicCategories,
		Catalog:    yhKMPicCatalog,
		Detail:     yhKMPicDetail,
		Resolve:    yhKMPicResolve,
		Search:     yhKMPicSearch,
	})
	// Integration: append "yh_kmpic" to slotOrder (registry.go tail) and
	// add the builtin_config entry AFTER a usable-egress live check —
	// see the header verification note.
}
