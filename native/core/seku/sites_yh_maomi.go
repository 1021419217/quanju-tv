package seku

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---- maomi （猫咪）: maomi-family AES-CBC envelope API (no sign field) ----
// Recipe: yuhuo maomi module, jadx-verified line by line 2026-10-24:
//   request builder b4.b.a: params = HEX( AES-128-CBC/PKCS5( JSON, key, IV ) )
//     (b4.b emits lowercase hex; the response interceptor b4.c upper-cases
//     before decoding, so case is not significant to the server family)
//   key/IV (native SignHelper via b4.b fields f633d/f631a, order settled by
//     逆向报告 §5.1): key = getMM2 = "5efdtf6060e2o330",
//     IV = getMM1 = "" -> in the app the raw strings go straight into
//     SecretKeySpec/IvParameterSpec; the working pair used by every sibling
//     builder (q3.b/l5.d/a3.b pad with '0' to 16) is therefore
//     IV "0000000000000000". As a belt-and-braces fallback this port also
//     retries the swapped pair (key "0000000000000000", IV "5efdtf6060e2o330")
//     when the primary pair fails to decrypt a response.
//   response interceptor b4.c: body is the same AES-CBC hex envelope ->
//     plain JSON {code,message,data}.
//   ApiService b4.a endpoints used here:
//     POST /api/v2/video/index   (no params)      -> HomeData sections
//     POST /api/v2/special/more  (no params)      -> SpecialList
//     POST /api/special/video    {special_id,page}-> SearchResult
//     POST /api/video/detail     {id}             -> VideoDetail
//     POST /api/video/search     {keyword,page}   -> SearchResult
//     POST /api/v2/original/index {user_id:"8105661",type:"1",page}
//                                                 -> ShortOriginal (shorts)
//   Play: VideoDetail.data.video_item[0].file is played directly by
//     MovieDetailActivity; data.videos[] carries per-item {file,is_vip}.
//     Paid rule: is_vip==1 items are refused in Resolve (detail-level
//     evidence; list beans carry no vip flag — see verification note).
//   Covers: relative image paths are prefixed with https://mao.1f866.com
//     (HomeData.DataBean.getImage verbatim).
//   Base: h6.b URL_MM http://124.156.98.46:8089; HTTP-DNS discovery
//     http://119.28.28.28/d?dn=cdn.newmm-tengxunyun.com -> 16.162.45.151
//     (live-checked 2026-10-24 via the sandbox's working HTTP-DNS path);
//     decoded fallback IPs 124.156.119.252 / 150.109.116.53 (module strings).
//
// Verification (2026-10-24, build sandbox): the sandbox egress reaches
// none of the maomi hosts (direct connect timeout / proxy empty reply,
// DNS for the domain is fake-IP) — the same egress class that blocked
// yh_bale/yh_km play hosts. HTTP-DNS discovery itself answered
// (16.162.45.151). All legs are therefore CODE-PROVEN (jadx line-by-line)
// but BYTE-UNVERIFIED from here: Categories/Catalog/Cover/Detail/Search/
// Resolve 出口未验，待真机/可用出口补验. Do not read this as the site
// being dead: no DNS NXDOMAIN / parking evidence exists.

const yhMMSlot = "yh_maomi"
const yhMMUA = "okhttp/4.9.0"

var yhMMKeyPairs = [][2][]byte{
	{[]byte("5efdtf6060e2o330"), []byte("0000000000000000")},
	{[]byte("0000000000000000"), []byte("5efdtf6060e2o330")},
}

var (
	yhMMBaseMu sync.Mutex
	yhMMBase   string
	yhMMKeyIdx int
	yhMMCache  sync.Map // id -> yhMMPlay {url, vip}
	yhMMSecs   sync.Map // section id -> map[string]interface{} (HomeData.DataBean)
	yhMMDNSMu  sync.Mutex
	yhMMDNSIPs []string
	yhMMDNSAt  time.Time
)

type yhMMPlay struct {
	URL string
	VIP bool
}

func yhMMDiscover() []string {
	yhMMDNSMu.Lock()
	if time.Since(yhMMDNSAt) < 10*time.Minute && len(yhMMDNSIPs) > 0 {
		cached := append([]string{}, yhMMDNSIPs...)
		yhMMDNSMu.Unlock()
		return append(cached,
			"http://124.156.98.46:8089",
			"http://124.156.119.252:8089",
			"http://150.109.116.53:8089",
		)
	}
	yhMMDNSMu.Unlock()
	out := []string{}
	req, err := http.NewRequest("GET", "http://119.28.28.28/d?dn=cdn.newmm-tengxunyun.com", nil)
	if err == nil {
		resp, err := (&http.Client{Timeout: 10 * time.Second, Transport: sharedTransport}).Do(req)
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			for _, p := range strings.FieldsFunc(string(b), func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == ' ' }) {
				if p != "" && p != "0" {
					out = append(out, "http://"+p+":8089")
				}
			}
		}
	}
	if len(out) > 0 {
		yhMMDNSMu.Lock()
		yhMMDNSIPs = append([]string{}, out...)
		yhMMDNSAt = time.Now()
		yhMMDNSMu.Unlock()
	}
	out = append(out,
		"http://124.156.98.46:8089",
		"http://124.156.119.252:8089",
		"http://150.109.116.53:8089",
	)
	return out
}

func yhMMPad(b []byte) []byte {
	n := 16 - len(b)%16
	out := append([]byte{}, b...)
	for i := 0; i < n; i++ {
		out = append(out, byte(n))
	}
	return out
}

func yhMMUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

func yhMMEnc(plain []byte, pair int) string {
	block, _ := aes.NewCipher(yhMMKeyPairs[pair][0])
	padded := yhMMPad(plain)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, yhMMKeyPairs[pair][1]).CryptBlocks(out, padded)
	return hex.EncodeToString(out)
}

func yhMMDec(hexs string, pair int) ([]byte, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(hexs))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_maomi: bad cipher len")
	}
	block, _ := aes.NewCipher(yhMMKeyPairs[pair][0])
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, yhMMKeyPairs[pair][1]).CryptBlocks(out, raw)
	return yhMMUnpad(out), nil
}

func yhMMPost(base, path string, payload map[string]interface{}) (map[string]interface{}, error) {
	var bodyStr string
	if payload != nil {
		jb, _ := json.Marshal(payload)
		bodyStr = url.Values{"params": {yhMMEnc(jb, yhMMKeyIdx)}}.Encode()
	}
	h := map[string]string{
		"User-Agent":   yhMMUA,
		"Content-Type": "application/x-www-form-urlencoded",
	}
	raw, code, _, err := httpDo("POST", base+path, strings.NewReader(bodyStr), h)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("yh_maomi http %d", code)
	}
	trimmed := strings.TrimSpace(string(raw))
	var plain []byte
	if strings.HasPrefix(trimmed, "{") {
		plain = []byte(trimmed)
	} else {
		plain, err = yhMMDec(trimmed, yhMMKeyIdx)
		if err != nil && yhMMKeyIdx == 0 {
			// swapped-pair fallback (see header note)
			if p2, err2 := yhMMDec(trimmed, 1); err2 == nil {
				var probe map[string]interface{}
				if json.Unmarshal(p2, &probe) == nil {
					yhMMKeyIdx = 1
					plain, err = p2, nil
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("yh_maomi decrypt: %v", err)
		}
	}
	var out map[string]interface{}
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("yh_maomi parse: %v", err)
	}
	if c, ok := out["code"].(float64); ok && c != 0 {
		return nil, fmt.Errorf("yh_maomi code %v: %s", c, g5StrVal(out["message"]))
	}
	return out, nil
}

func yhMMCall(path string, payload map[string]interface{}) (map[string]interface{}, error) {
	yhMMBaseMu.Lock()
	base := yhMMBase
	yhMMBaseMu.Unlock()
	if base != "" {
		if obj, err := yhMMPost(base, path, payload); err == nil {
			return obj, nil
		} else {
			yhMMBaseMu.Lock()
			if yhMMBase == base {
				yhMMBase = ""
			}
			yhMMBaseMu.Unlock()
		}
	}
	cands := yhMMDiscover()
	type yhMMResult struct {
		base string
		obj  map[string]interface{}
		err  error
	}
	ch := make(chan yhMMResult, len(cands))
	for _, cand := range cands {
		go func(c string) {
			obj, err := yhMMPost(c, path, payload)
			ch <- yhMMResult{base: c, obj: obj, err: err}
		}(cand)
	}
	var lastErr error
	for range cands {
		r := <-ch
		if r.err == nil {
			yhMMBaseMu.Lock()
			yhMMBase = r.base
			yhMMBaseMu.Unlock()
			return r.obj, nil
		}
		lastErr = r.err
	}
	return nil, lastErr
}

func yhMMCover(img string) string {
	if img == "" || strings.HasPrefix(img, "http") {
		return img
	}
	if strings.HasPrefix(img, "/") {
		return "https://mao.1f866.com" + img
	}
	return "https://mao.1f866.com/" + img
}

func yhMMVideoCard(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["video_id"])
	if id == "" {
		id = g5StrVal(rec["id"])
	}
	title := g5StrVal(rec["video_name"])
	if title == "" {
		title = g5StrVal(rec["name"])
	}
	if id == "" || title == "" {
		return nil, false
	}
	cover := yhMMCover(g5StrVal(rec["image"]))
	if cover == "" {
		cover = yhMMCover(g5StrVal(rec["ximage"]))
	}
	remark := ""
	if r := g5StrVal(rec["rate"]); r != "" && r != "0" {
		remark = "评分 " + r
	}
	return makeDrama(slot, id, title, cover, remark, "", ""), true
}

func yhMMVideoCards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhMMVideoCard(slot, m); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

func yhMMHome(slot string) ([]interface{}, error) {
	obj, err := yhMMCall("/api/v2/video/index", nil)
	if err != nil {
		return nil, err
	}
	secs := yhArr(obj["data"])
	for _, e := range secs {
		if m := yhMap(e); m != nil {
			if id := g5StrVal(m["id"]); id != "" {
				yhMMSecs.Store(id, m)
			}
		}
	}
	return secs, nil
}

func yhMMCategories(slot string) []interface{} {
	out := []interface{}{}
	secs, homeErr := yhMMHome(slot)
	if homeErr == nil {
		for _, e := range secs {
			m := yhMap(e)
			if m == nil {
				continue
			}
			id, name := g5StrVal(m["id"]), g5StrVal(m["name"])
			if id != "" && name != "" {
				out = append(out, categoryObj("sec:"+id, name))
			}
		}
	}
	if homeErr == nil && len(out) == 0 {
		out = append(out, categoryObj("home", "首页"))
	}
	out = append(out,
		categoryObj("short", "短视频"),
		categoryObj("special", "专题"),
	)
	return out
}

func yhMMCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "short":
		obj, err := yhMMCall("/api/v2/original/index", map[string]interface{}{
			"user_id": "8105661", "type": "1", "page": page,
		})
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return []interface{}{}, 0, false, nil
		}
		out := []interface{}{}
		for _, e := range yhArr(data["data"]) {
			m := yhMap(e)
			if m == nil {
				continue
			}
			// ShortOriginal.DataBeanX keeps only type=="1" items (bean verbatim).
			if t := g5StrVal(m["type"]); t != "" && t != "1" {
				continue
			}
			id := g5StrVal(m["id"])
			title := g5StrVal(m["title"])
			if id == "" || title == "" {
				continue
			}
			play := g5StrVal(m["m3u8"])
			if play == "" {
				play = g5StrVal(m["video"])
			}
			if play != "" {
				yhMMCache.Store("short:"+id, yhMMPlay{URL: play})
			}
			out = append(out, makeDrama(slot, "short:"+id, title, yhMMCover(g5StrVal(m["video_img"])), g5StrVal(m["plays"]), "短视频", g5StrVal(m["content"])))
		}
		total, _ := g5MapInt(data, "total_page")
		return out, len(out), total > 0 && page < total, nil
	case category == "special":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhMMCall("/api/v2/special/more", nil)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhArr(obj["data"]) {
			m := yhMap(e)
			if m == nil {
				continue
			}
			id, name := g5StrVal(m["id"]), g5StrVal(m["name"])
			if id == "" || name == "" {
				continue
			}
			out = append(out, makeDrama(slot, "dir:special:"+id, name, yhMMCover(g5StrVal(m["image"])), g5StrVal(m["label"]), "专题", g5StrVal(m["describe"])))
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "dir:special:"):
		obj, err := yhMMCall("/api/special/video", map[string]interface{}{
			"special_id": strings.TrimPrefix(category, "dir:special:"), "page": page,
		})
		if err != nil {
			return nil, 0, false, err
		}
		data := yhMap(obj["data"])
		if data == nil {
			return []interface{}{}, 0, false, nil
		}
		items := yhMMVideoCards(slot, yhArr(data["data"]))
		total, _ := g5MapInt(data, "total_page")
		return items, len(items), total > 0 && page < total, nil
	case strings.HasPrefix(category, "sec:"):
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		id := strings.TrimPrefix(category, "sec:")
		if v, ok := yhMMSecs.Load(id); ok {
			return yhMMVideoCards(slot, yhArr(v.(map[string]interface{})["videos"])), 0, false, nil
		}
		if _, err := yhMMHome(slot); err != nil {
			return nil, 0, false, err
		}
		if v, ok := yhMMSecs.Load(id); ok {
			return yhMMVideoCards(slot, yhArr(v.(map[string]interface{})["videos"])), 0, false, nil
		}
		return nil, 0, false, fmt.Errorf("yh_maomi: section %s not found", id)
	default: // "home" fallback: flatten all home sections (first page only)
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		secs, err := yhMMHome(slot)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		seen := map[string]bool{}
		for _, e := range secs {
			m := yhMap(e)
			if m == nil {
				continue
			}
			for _, c := range yhMMVideoCards(slot, yhArr(m["videos"])) {
				id := g5StrVal(yhMap(c)["vod_id"])
				if !seen[id] {
					seen[id] = true
					out = append(out, c)
				}
			}
		}
		return out, len(out), false, nil
	}
}

func yhMMDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "short:") {
		title, cover, desc := "短视频"+strings.TrimPrefix(rawID, "short:"), "", ""
		if st := getStore(slot).findDrama(rawID); st != nil {
			title = g5StrVal(st["title"])
			cover = g5StrVal(st["cover"])
			desc = g5StrVal(st["desc"])
		}
		d := makeDrama(slot, rawID, title, cover, "", "短视频", desc)
		g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
		return d, nil
	}
	if strings.HasPrefix(rawID, "dir:special:") {
		items, _, _, err := yhMMCatalog(slot, rawID, 1)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, strings.TrimPrefix(rawID, "dir:special:"), "", "", "专题", "")
		if len(items) > 0 {
			d["related"] = items
		}
		return d, nil
	}
	obj, err := yhMMCall("/api/video/detail", map[string]interface{}{"id": rawID})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return nil, fmt.Errorf("yh_maomi: detail empty for %s", rawID)
	}
	title := g5StrVal(data["name"])
	desc := g5StrVal(data["description"])
	cover := ""
	if st := getStore(slot).findDrama(rawID); st != nil {
		cover = g5StrVal(st["cover"])
		if title == "" {
			title = g5StrVal(st["title"])
		}
	}
	d := makeDrama(slot, rawID, title, cover, g5StrVal(data["author"]), "", desc)
	if a := g5StrVal(data["author"]); a != "" {
		d["actor"] = a
		d["vod_actor"] = a
	}
	// Episodes: videos[] items carry their own file + is_vip; video_item
	// is the single-item form MovieDetailActivity plays (index 0).
	epIDs, epNames := []string{}, []string{}
	vip := g5Truthy(data["is_vip"])
	if vids := yhArr(data["videos"]); len(vids) > 0 {
		for _, e := range vids {
			m := yhMap(e)
			if m == nil {
				continue
			}
			epID := "ep:" + g5StrVal(m["id"])
			yhMMCache.Store(epID, yhMMPlay{URL: g5StrVal(m["file"]), VIP: g5Truthy(m["is_vip"]) || vip})
			epIDs = append(epIDs, epID)
			epNames = append(epNames, fmt.Sprintf("第%d集", len(epIDs)))
		}
	} else if items := yhArr(data["video_item"]); len(items) > 0 {
		m := yhMap(items[0])
		if m != nil {
			yhMMCache.Store(rawID, yhMMPlay{URL: g5StrVal(m["file"]), VIP: vip})
			epIDs = append(epIDs, rawID)
			epNames = append(epNames, "正片")
		}
	}
	if vip && len(epIDs) == 0 {
		return nil, fmt.Errorf("yh_maomi: %s is VIP-only, refused", rawID)
	}
	if len(epIDs) > 0 {
		g5SetChapters(d, slot, rawID, epIDs, epNames)
	}
	return d, nil
}

func yhMMResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	if v, ok := yhMMCache.Load(vid); ok {
		p := v.(yhMMPlay)
		if p.VIP {
			return nil, fmt.Errorf("yh_maomi: %s is VIP content, refused", vid)
		}
		if p.URL != "" {
			format, protocol := "mp4", ""
			if strings.Contains(p.URL, ".m3u8") {
				format, protocol = "m3u8", "hls"
			}
			return playResult(p.URL, slot, rawID, yhMMUA, "https://mao.1f866.com/", format, protocol), nil
		}
	}
	// Episode ids (ep:<n>) resolve through their parent detail cache only;
	// plain ids fetch the detail on demand.
	if strings.HasPrefix(vid, "ep:") || strings.HasPrefix(vid, "dir:") {
		return nil, fmt.Errorf("yh_maomi: play url for %s not cached (open detail first)", vid)
	}
	if _, err := yhMMDetail(slot, vid); err != nil {
		return nil, err
	}
	if v, ok := yhMMCache.Load(vid); ok {
		p := v.(yhMMPlay)
		if p.VIP {
			return nil, fmt.Errorf("yh_maomi: %s is VIP content, refused", vid)
		}
		if p.URL != "" {
			format, protocol := "mp4", ""
			if strings.Contains(p.URL, ".m3u8") {
				format, protocol = "m3u8", "hls"
			}
			return playResult(p.URL, slot, rawID, yhMMUA, "https://mao.1f866.com/", format, protocol), nil
		}
	}
	return nil, fmt.Errorf("yh_maomi: no play url for %s", vid)
}

func yhMMSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhMMCall("/api/video/search", map[string]interface{}{"keyword": kw, "page": 1})
	if err != nil {
		return nil, err
	}
	data := yhMap(obj["data"])
	if data == nil {
		return []interface{}{}, nil
	}
	return yhMMVideoCards(slot, yhArr(data["data"])), nil
}

func yhMMHeaders() map[string]string {
	return map[string]string{"User-Agent": yhMMUA}
}

func init() {
	registerSite(yhMMSlot, &siteImpl{
		Key:        yhMMSlot,
		Name:       "🔞猫咪",
		DefaultCat: "home",
		HomeURL:    "http://124.156.98.46:8089/",
		Headers:    yhMMHeaders,
		Categories: yhMMCategories,
		Catalog:    yhMMCatalog,
		Detail:     yhMMDetail,
		Resolve:    yhMMResolve,
		Search:     yhMMSearch,
	})
}
