package seku

// ---- 黄瓜视频 cucumber (yh_cucumber, type 16/36 两变体同站) ----
//
// Recipe: jadx j1/c CucumberInterceptor + j1/a ApiService + j1/e NetHelper
// + cucumber fragments, decoded verbatim (ad.r.o XOR key "7bksfs51ss4qxjck").
// Base (launcher defaults): https://api66.capcom.work / https://api88.awk2.work;
// Retrofit hard base: https://api55.wtttli.com; domain/list server list:
// https://api55.nmrgqb.com. Failover tries bases in that order.
//
// Signing (j1/c, line-for-line):
//   headers: release:410, origin:https://cuke01.live, pkg:cuke0001,
//     terminal:4, x-auth-key:feb48d9bffffebb322a773d8f213eebm,
//     seq:<token>, x-auth-nonce:0-9999 random, x-auth-timestamp:ms,
//     x-auth-sign:URLEncode(Base64(HmacSHA1(signText, "cuke@majiaSecret#12021#0811"))),
//     authorization:<HG_TOKEN> (see note below).
//   signText = METHOD + path + "?" + "Authorization=" + "&Seq={seq}" +
//     "&X-Auth-Key={key}&X-Auth-Nonce={nonce}&X-Auth-TimeStamp={ts}" +
//     (POST form fields, in order, "&k=v" — none of this site's calls use
//     form bodies; movlist/movlist2 send a JSON body, which the
//     interceptor does NOT include because it is not a FormBody) +
//     sorted "&k=v" over query params + pkg/release/terminal.
//   NOTE (jadx literal bug, CONFIRMED live 2026-10-24): j1/c gates the
//   authorization header on `!TextUtils.isEmpty("")`, which is always
//   false, so the shipped app NEVER sends it — and that is load-bearing:
//   sending `authorization: <HG_TOKEN>` to api55.wtttli.com returns
//   HTTP 666 with an empty body, while omitting it returns 200 code 0
//   (same signature, same session). So this port NEVER sends
//   authorization, exactly like the shipped app. HG_TOKEN = h6.b.P =
//   06b90feb397e9067c7f023133ef8d46c086660c339c2ae872ab2d259e3b6bfec980dab80894b9c3764
//   is recorded here for reference only and is NOT transmitted.
//
// seq (j1/c): initial 8f966d17ae1749699c124e1f1ca89662C30mC30mCJ8sC3G;
// jadx refreshes it ONLY from /active/log responses (and resets it to
// e73515db77425c61f69e1507523be812 before such calls, with a random
// X-Forwarded-For). This port refreshes from ANY response's seq header
// when present (superset, matches the task recipe). Live 2026-10-24:
// api55 returned no seq header on any tested endpoint and the initial
// seq was accepted for every call, so rotation is currently unobserved
// but harmless.
//
// Response (j1/c): JSON with encryptedData (≠ "----") is
// Base64 -> AES/ECB/PKCS7 decrypted with key "V%4SdZ^ElKdOVU0m" and the
// plaintext JSON replaces "data". Verified: every live response
// decrypted cleanly (code 0).
//
// Covers (h6.e.t + k1.d, verbatim):
//   http...                      -> as-is
//   "img..."                     -> https://gvc.n90yl.work/ + path
//   "/jqn..." or "/yvn..."       -> sekiro host (b.N) + path; live
//                                  domain/list videoCoverAddr2 =
//                                  https://hgustp.sekiro.info (launcher
//                                  default was https://shyv2.sekiro.info)
//   anything else (e.g. /bifpqrskbawkt/...) -> https://hghktp-bp-lk.hlvsja.com + path,
//                                  and those bytes are SINGLE-BYTE XOR
//                                  obfuscated: byte 0 is the key, every
//                                  following byte XOR key = plaintext
//                                  (k1.d OkHttpStreamFetcher verbatim).
//   Actor/column photos: bean getters return b.O + path, b.O = viewPicture
//   (live: https://gvc.imgcuke.work/img/; launcher defaults
//   https://gv6.baidutie.work/img, https://gv6.n90yl.work/img).
// CoverFetch implements all of the above, including host fallbacks for
// the picture/sekiro pools.
//
// Play (jadx truth, NOT the report's sekiro gloss): Video.getPlayUrl()
// = h6.b.M + address.{1080P,720P,480P,360P,240P}; h6.b.M default
// https://hgapi7.scxhht.com, launcher/domain overrides it with
// viewVideoM3u8[0] = https://hgapi-pnt017.soymfb.com (live 2026-10-24).
// The detail call is GET /mov/browse2?movId=, whose address paths are
// SIGNED (…m3u8?timestamp=…&s=…&version=410); list-level /index.m3u8
// paths returned 403 on that host, so Resolve ALWAYS goes through
// browse2. sekiro hosts are cover gateways only (b.N, see above);
// viewmov/newmov/lusir/tucao are /ranking/{key} keys, not sekiro
// method names. j1/d's playlist fixup (http seg- line -> master.m3u8)
// targets a playlist shape not seen in live browse2 responses (segments
// there are absolute https seg- URLs that play directly), so it is
// documented, not applied.
//
// LIVE VERIFICATION (2026-10-24, sandbox egress via slow proxy, ~10-30s
// per call; signed client uses 60s timeout, never the jina relay):
//   api66.capcom.work root: Spring 404 JSON, timestamp 2026-10-24 (host
//     alive); a signed /home/defaultchannels on api66 was truncated by
//     the proxy mid-body (IncompleteRead) — failover to api55 covered it.
//   api55.wtttli.com signed: defaultchannels code 0 (11 channels, see
//     yhCucumberChanSnap), selection/query + query2 code 0 with
//     decrypted sections, module/list code 0 (topics+subclasses),
//     ranking/viewmov type=1 code 0, actor/list code 0 (三上悠亚 et al),
//     actor/detail + actor/movie/list code 0, es/mov/search q=继弟
//     code 0, mov/browse2 code 0, module/movlist2 nav 979 code 0,
//     home/movlist ch33 code 0 (count 1000), shortvideo/listOrderByTime
//     code 0, es/mov/similar code 0.
//   Covers: hghktp cover XOR-unmasked -> FF D8 FF E0 JPEG (196,393 B);
//     sekiro /jqn cover -> 89 50 4E 47 PNG (640,599 B).
//   Play: browse2 shikanl URL on hgapi-pnt017.soymfb.com -> 200
//     #EXTM3U playlist (AES-128 EXT-X-KEY + seg-*.ts lines on
//     hghkaev-pnt017.yunruiml.com); first segment Range GET -> 206
//     (AES-encrypted TS bytes, as the playlist declares).
//   NOT verified live from here: api88.awk2.work signed (proxy timeouts;
//     kept as 2nd failover base, not claimed alive), actor photo bytes
//     (gvc.imgcuke.work/img -> 404, gv6.baidutie -> 403, gv6.n90yl /
//     gvc.n90yl timeouts for the sampled 三上悠亚 photo — CoverFetch
//     tries the whole picture pool), api66/api88 signed endpoints
//     (proxy truncation/timeout, see above), vipFlag==1 filtering
//     (no vipFlag==1 item appeared in any live sample; filter is
//     code-only and untriggered).
// Paid rule: vipFlag==1 items are dropped from catalogs and refused in
// Resolve (no price/money fields exist on this API; vipFlag is the
// marker). All live samples were vipFlag 0.

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const yhCucumberSlot = "yh_cucumber"

var yhCucumberBases = []string{
	"https://api66.capcom.work",
	"https://api88.awk2.work",
	"https://api55.wtttli.com",
	"https://api55.nmrgqb.com",
}

const yhCucumberAuthKey = "feb48d9bffffebb322a773d8f213eebm"
const yhCucumberHGToken = "06b90feb397e9067c7f023133ef8d46c086660c339c2ae872ab2d259e3b6bfec980dab80894b9c3764"

var yhCucumberHMACKey = []byte("cuke@majiaSecret#12021#0811")
var yhCucumberAESKey = []byte("V%4SdZ^ElKdOVU0m")

// Stream host: domain/list viewVideoM3u8[0] live 2026-10-24, then h6.b default.
const yhCucumberStream = "https://hgapi-pnt017.soymfb.com"
const yhCucumberStreamFallback = "https://hgapi7.scxhht.com"

// Cover hosts (see header).
const yhCucumberSekiro = "https://hgustp.sekiro.info"
const yhCucumberSekiroAlt = "https://shyv2.sekiro.info"
const yhCucumberAESCover = "https://hghktp-bp-lk.hlvsja.com"
const yhCucumberAESCoverAlt = "https://hghktp-pnt017.spsctd.com"
const yhCucumberImgPrefix = "https://gvc.n90yl.work/"

var yhCucumberPicHosts = []string{
	"https://gvc.imgcuke.work/img/",
	"https://gv6.baidutie.work/img/",
	"https://gv6.n90yl.work/img/",
	"https://gvc.n90yl.work/img/",
}

var (
	yhCucumberSeqMu sync.Mutex
	yhCucumberSeq   = "8f966d17ae1749699c124e1f1ca89662C30mC30mCJ8sC3G"
	// Signed calls use a dedicated client (60s: this sandbox's proxy is
	// slow; devices are fast). Signed URLs must never go through the
	// shared jina relay, so httpDo/httpGet are deliberately NOT used.
	yhCucumberClient      = &http.Client{Timeout: 60 * time.Second, Transport: sharedTransport}
	yhCucumberCoverClient = &http.Client{Timeout: 30 * time.Second, Transport: sharedTransport}
)

type yhCucumberChan struct {
	parentModule int
	channelType  int
	hasSel       bool
	name         string
}

// Live snapshot 2026-10-24 (GET /home/defaultchannels, api55). Used when
// the live channel fetch fails; Categories() prefers the live list.
var yhCucumberChanSnap = []yhCucumberChan{
	{25, 1, true, "国产"},
	{36, 1, true, "传媒机构"},
	{23, 1, true, "日本AV"},
	{27, 1, true, "动漫"},
	{24, 1, true, "欧美"},
	{26, 1, true, "三级"},
	{33, 1, false, "主播"},
	{34, 2, false, "UP人气热播"},
	{34, 2, false, "UP本周推荐"},
	{41, 2, false, "AI精彩影片"},
	{41, 1, true, "月度精选"},
}

var yhCucumberRanks = []struct{ key, name string }{
	{"viewmov", "观影榜"},
	{"newmov", "新片榜"},
	{"lusir", "撸sir榜"},
	{"tucao", "吐槽榜"},
}

func yhCucumberSign(method, path string, query map[string]string, form [][2]string) (string, string, string, string) {
	yhCucumberSeqMu.Lock()
	seq := yhCucumberSeq
	yhCucumberSeqMu.Unlock()
	nonce := strconv.Itoa(rand.Intn(10000))
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	var sb strings.Builder
	sb.WriteString(method)
	sb.WriteString(path)
	sb.WriteString("?Authorization=&Seq=")
	sb.WriteString(seq)
	sb.WriteString("&X-Auth-Key=")
	sb.WriteString(yhCucumberAuthKey)
	sb.WriteString("&X-Auth-Nonce=")
	sb.WriteString(nonce)
	sb.WriteString("&X-Auth-TimeStamp=")
	sb.WriteString(ts)
	for _, kv := range form {
		sb.WriteString("&")
		sb.WriteString(kv[0])
		sb.WriteString("=")
		sb.WriteString(kv[1])
	}
	tree := map[string]string{}
	for k, v := range query {
		tree[k] = v
	}
	tree["pkg"] = "cuke0001"
	tree["release"] = "410"
	tree["terminal"] = "4"
	keys := make([]string, 0, len(tree))
	for k := range tree {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sb.WriteString("&")
		sb.WriteString(k)
		sb.WriteString("=")
		sb.WriteString(tree[k])
	}
	mac := hmac.New(sha1.New, yhCucumberHMACKey)
	mac.Write([]byte(sb.String()))
	sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	return sign, nonce, ts, seq
}

func yhCucumberUnpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

// yhCucumberCallOnce performs one signed request against one base.
func yhCucumberCallOnce(base, method, path string, query map[string]string, jsonBody map[string]interface{}) (map[string]interface{}, error) {
	q := map[string]string{}
	for k, v := range query {
		q[k] = v
	}
	sign, nonce, ts, seq := yhCucumberSign(method, path, q, nil)
	vals := url.Values{}
	for k, v := range q {
		vals.Set(k, v)
	}
	u := base + path
	if enc := vals.Encode(); enc != "" {
		u += "?" + enc
	}
	var body io.Reader
	if jsonBody != nil {
		jb, _ := json.Marshal(jsonBody)
		body = bytes.NewReader(jb)
	}
	req, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("release", "410")
	req.Header.Set("origin", "https://cuke01.live")
	req.Header.Set("seq", seq)
	req.Header.Set("x-auth-key", yhCucumberAuthKey)
	req.Header.Set("pkg", "cuke0001")
	req.Header.Set("terminal", "4")
	req.Header.Set("x-auth-nonce", nonce)
	req.Header.Set("x-auth-sign", sign)
	req.Header.Set("x-auth-timestamp", ts)
	// NO authorization header: live-verified to trigger HTTP 666 (see header).
	req.Header.Set("User-Agent", "okhttp/4.9.0")
	if jsonBody != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := yhCucumberClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if s := resp.Header.Get("seq"); s != "" {
		yhCucumberSeqMu.Lock()
		yhCucumberSeq = s
		yhCucumberSeqMu.Unlock()
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("yh_cucumber http %d", resp.StatusCode)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, fmt.Errorf("yh_cucumber parse: %v", err)
	}
	if enc, ok := obj["encryptedData"].(string); ok && enc != "" && enc != "----" {
		ct, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			return nil, fmt.Errorf("yh_cucumber encryptedData b64: %v", err)
		}
		block, err := aes.NewCipher(yhCucumberAESKey)
		if err != nil {
			return nil, err
		}
		if len(ct)%aes.BlockSize != 0 {
			return nil, fmt.Errorf("yh_cucumber encryptedData bad length")
		}
		pt := make([]byte, len(ct))
		for i := 0; i < len(ct); i += aes.BlockSize {
			block.Decrypt(pt[i:i+aes.BlockSize], ct[i:i+aes.BlockSize])
		}
		pt = yhCucumberUnpad(pt)
		var data interface{}
		if err := json.Unmarshal(pt, &data); err != nil {
			return nil, fmt.Errorf("yh_cucumber decrypt parse: %v", err)
		}
		obj["data"] = data
	}
	if c, ok := obj["code"].(float64); ok && c != 0 {
		return nil, fmt.Errorf("yh_cucumber code %v: %s", c, g5StrVal(obj["msg"]))
	}
	return obj, nil
}

// yhCucumberCall tries each base in order until one returns code 0.
func yhCucumberCall(method, path string, query map[string]string, jsonBody map[string]interface{}) (map[string]interface{}, error) {
	var lastErr error
	for _, base := range yhCucumberBases {
		obj, err := yhCucumberCallOnce(base, method, path, query, jsonBody)
		if err == nil {
			return obj, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// yhCucumberCover builds a full cover URL per h6.e.t (see header).
func yhCucumberCover(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http") {
		return p
	}
	if strings.HasPrefix(p, "img") {
		return yhCucumberImgPrefix + p
	}
	if strings.HasPrefix(p, "/jqn") || strings.HasPrefix(p, "/yvn") {
		return yhCucumberSekiro + p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return yhCucumberAESCover + p
}

// yhCucumberPic builds an actor/column photo URL per the bean getters
// (b.O + path, b.O = viewPicture).
func yhCucumberPic(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return yhCucumberPicHosts[0] + strings.TrimPrefix(p, "/")
}

func yhCucumberIsImage(b []byte) bool {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xD8 {
		return true
	}
	if len(b) >= 4 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return true
	}
	if len(b) >= 4 && string(b[:4]) == "RIFF" {
		return true
	}
	if len(b) >= 3 && b[0] == 'G' && b[1] == 'I' && b[2] == 'F' {
		return true
	}
	return false
}

func yhCucumberFetchCover(u string) ([]byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "okhttp/4.9.0")
	req.Header.Set("Referer", "https://cuke01.live/")
	resp, err := yhCucumberCoverClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("yh_cucumber cover http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// yhCucumberCoverFetch: hghktp covers are XOR-unmasked (k1.d); picture
// and sekiro pools fall back across their hosts until image magic appears.
func yhCucumberCoverFetch(u string) ([]byte, error) {
	if strings.Contains(u, "hghktp") {
		raw, err := yhCucumberFetchCover(u)
		if err != nil {
			// Try the alternate AES cover host.
			alt := strings.Replace(u, yhCucumberAESCover, yhCucumberAESCoverAlt, 1)
			if alt != u {
				raw, err = yhCucumberFetchCover(alt)
			}
			if err != nil {
				return nil, err
			}
		}
		if len(raw) > 1 {
			key := raw[0]
			out := make([]byte, len(raw)-1)
			for i, b := range raw[1:] {
				out[i] = b ^ key
			}
			if yhCucumberIsImage(out) {
				return out, nil
			}
			return out, nil
		}
		return raw, nil
	}
	// Host-pool fallback for picture/sekiro URLs.
	var pool []string
	if strings.Contains(u, "sekiro.info") {
		pool = []string{yhCucumberSekiro, yhCucumberSekiroAlt}
	} else {
		for _, h := range yhCucumberPicHosts {
			if strings.Contains(u, strings.TrimSuffix(h, "/")) || strings.Contains(u, h) {
				pool = yhCucumberPicHosts
				break
			}
		}
	}
	if pool != nil {
		// Split the URL into host prefix + path.
		var path string
		for _, h := range pool {
			trim := strings.TrimSuffix(h, "/")
			if i := strings.Index(u, trim); i >= 0 {
				path = u[i+len(trim):]
				break
			}
		}
		if path != "" {
			var firstErr error
			for _, h := range pool {
				b, err := yhCucumberFetchCover(strings.TrimSuffix(h, "/") + path)
				if err != nil {
					if firstErr == nil {
						firstErr = err
					}
					continue
				}
				if yhCucumberIsImage(b) {
					return b, nil
				}
			}
			if firstErr != nil {
				return nil, firstErr
			}
			return nil, fmt.Errorf("yh_cucumber cover: no image magic in pool")
		}
	}
	return yhCucumberFetchCover(u)
}

func yhCucumberVideoID(rec map[string]interface{}) string {
	if s := g5StrVal(rec["movId"]); s != "" {
		return s
	}
	return g5StrVal(rec["id"])
}

func yhCucumberVideoTitle(rec map[string]interface{}) string {
	if s := g5StrVal(rec["movName"]); s != "" {
		return s
	}
	return g5StrVal(rec["name"])
}

// yhCucumberVideoCover: cover may be a plain path, a covers object, or
// (actor/movie/list) a JSON string of a covers object.
func yhCucumberVideoCover(rec map[string]interface{}) string {
	switch c := rec["cover"].(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(c), "{") {
			var oc map[string]interface{}
			if err := json.Unmarshal([]byte(c), &oc); err == nil {
				if s := g5StrVal(oc["horizontal_large"]); s != "" {
					return yhCucumberCover(s)
				}
			}
		}
		if c != "" {
			return yhCucumberCover(c)
		}
	case map[string]interface{}:
		if s := g5StrVal(c["horizontal_large"]); s != "" {
			return yhCucumberCover(s)
		}
	}
	if oc := yhMap(rec["allCovers"]); oc != nil {
		if s := g5StrVal(oc["horizontal_large"]); s != "" {
			return yhCucumberCover(s)
		}
	}
	if oc := yhMap(rec["url"]); oc != nil {
		if s := g5StrVal(oc["horizontal_large"]); s != "" {
			return yhCucumberCover(s)
		}
	}
	return ""
}

func yhCucumberVideoDesc(rec map[string]interface{}) string {
	parts := []string{}
	if s := g5StrVal(rec["movDesc"]); s != "" {
		parts = append(parts, s)
	}
	if tags := yhArr(rec["tags"]); len(tags) > 0 {
		names := []string{}
		for _, t := range tags {
			if tm := yhMap(t); tm != nil {
				if n := g5StrVal(tm["name"]); n != "" {
					names = append(names, n)
				}
			}
		}
		if len(names) > 0 {
			parts = append(parts, strings.Join(names, " / "))
		}
	}
	if tags := yhArr(rec["relTagName"]); len(tags) > 0 && len(parts) == 0 {
		names := []string{}
		for _, t := range tags {
			if tm := yhMap(t); tm != nil {
				if n := g5StrVal(tm["name"]); n != "" {
					names = append(names, n)
				}
			}
		}
		if len(names) > 0 {
			parts = append(parts, strings.Join(names, " / "))
		}
	}
	return strings.Join(parts, "\n")
}

func yhCucumberIsPaid(rec map[string]interface{}) bool {
	if v, ok := rec["vipFlag"].(float64); ok && v != 0 {
		return true
	}
	if v, ok := rec["isLimit"].(float64); ok && v != 0 {
		return true
	}
	return false
}

func yhCucumberCard(slot string, rec map[string]interface{}, catName string) (map[string]interface{}, bool) {
	id := yhCucumberVideoID(rec)
	title := yhCucumberVideoTitle(rec)
	if id == "" || title == "" {
		return nil, false
	}
	if yhCucumberIsPaid(rec) {
		return nil, false
	}
	remark := g5StrVal(rec["mins"])
	if remark == "" {
		remark = g5StrVal(rec["movScore"])
	}
	if pc := g5StrVal(rec["strPlayCnt"]); pc != "" && remark == "" {
		remark = pc + "播放"
	}
	return makeDrama(slot, id, title, yhCucumberVideoCover(rec), remark, catName, yhCucumberVideoDesc(rec)), true
}

func yhCucumberCards(slot string, recs []interface{}, catName string) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		rec := yhMap(e)
		if rec == nil {
			continue
		}
		if c, ok := yhCucumberCard(slot, rec, catName); ok {
			if !seen[g5StrVal(rec["movId"])+"/"+g5StrVal(rec["id"])] {
				seen[g5StrVal(rec["movId"])+"/"+g5StrVal(rec["id"])] = true
				out = append(out, c)
			}
		}
	}
	return out
}

func yhCucumberDataList(obj map[string]interface{}) []interface{} {
	return yhArr(obj["data"])
}

func yhCucumberPageInfo(obj map[string]interface{}) int {
	if n, ok := g5MapInt(obj, "pageCount"); ok {
		return n
	}
	return 0
}

var (
	yhCucumberChanMu   sync.Mutex
	yhCucumberChanList []yhCucumberChan
	yhCucumberChanAt   time.Time
)

func yhCucumberChannels() []yhCucumberChan {
	yhCucumberChanMu.Lock()
	defer yhCucumberChanMu.Unlock()
	if yhCucumberChanList != nil && time.Since(yhCucumberChanAt) < 30*time.Minute {
		return yhCucumberChanList
	}
	obj, err := yhCucumberCall("GET", "/home/defaultchannels", nil, nil)
	if err == nil {
		list := []yhCucumberChan{}
		for _, e := range yhCucumberDataList(obj) {
			m := yhMap(e)
			if m == nil {
				continue
			}
			pm, _ := g5MapInt(m, "parentModule")
			ct, _ := g5MapInt(m, "channelType")
			name := g5StrVal(m["name"])
			if pm == 0 || name == "" {
				continue
			}
			list = append(list, yhCucumberChan{pm, ct, g5Truthy(m["hasSelection"]), name})
		}
		if len(list) > 0 {
			yhCucumberChanList = list
			yhCucumberChanAt = time.Now()
			return list
		}
	}
	return yhCucumberChanSnap
}

func yhCucumberChanID(c yhCucumberChan) string {
	sel := "0"
	if c.hasSel {
		sel = "1"
	}
	return fmt.Sprintf("ch:%d:%d:%s", c.parentModule, c.channelType, sel)
}

func yhCucumberCategories(slot string) []interface{} {
	out := []interface{}{categoryObj("choice", "精选")}
	for _, c := range yhCucumberChannels() {
		out = append(out, categoryObj(yhCucumberChanID(c), c.name))
	}
	out = append(out, categoryObj("col", "专栏"))
	for _, r := range yhCucumberRanks {
		for _, t := range []struct{ id, name string }{{"1", "日榜"}, {"2", "周榜"}, {"3", "月榜"}} {
			out = append(out, categoryObj("rank:"+r.key+":"+t.id, r.name+"·"+t.name))
		}
	}
	out = append(out,
		categoryObj("rank:actors:1", "女优榜·日榜"),
		categoryObj("rank:actors:2", "女优榜·周榜"),
		categoryObj("rank:actors:3", "女优榜·月榜"),
		categoryObj("actor", "女优"),
		categoryObj("short:Time", "短剧·最新"),
		categoryObj("short:Play", "短剧·最热"),
	)
	return out
}

func yhCucumberCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	switch {
	case category == "" || category == "choice":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhCucumberCall("GET", "/home/selection/query", nil, nil)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhCucumberDataList(obj) {
			sec := yhMap(e)
			if sec == nil {
				continue
			}
			name := g5StrVal(sec["name"])
			if name == "" {
				name = "精选"
			}
			out = append(out, yhCucumberCards(slot, yhArr(sec["movList"]), name)...)
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "ch:"):
		parts := strings.Split(category, ":")
		if len(parts) != 4 {
			return nil, 0, false, fmt.Errorf("yh_cucumber bad channel %s", category)
		}
		module, _ := strconv.Atoi(parts[1])
		selType, _ := strconv.Atoi(parts[2])
		if parts[3] == "1" {
			if page > 1 {
				return []interface{}{}, 0, false, nil
			}
			obj, err := yhCucumberCall("GET", "/home/selection/query2", map[string]string{"moduleId": parts[1]}, nil)
			if err != nil {
				return nil, 0, false, err
			}
			out := []interface{}{}
			for _, e := range yhCucumberDataList(obj) {
				sec := yhMap(e)
				if sec == nil {
					continue
				}
				name := g5StrVal(sec["name"])
				if name == "" {
					name = "精选"
				}
				out = append(out, yhCucumberCards(slot, yhArr(sec["movList"]), name)...)
			}
			return out, len(out), false, nil
		}
		q := map[string]string{
			"pageNo": strconv.Itoa(page), "pageSize": "12",
			"selectionType": strconv.Itoa(selType), "moduleId": strconv.Itoa(module),
		}
		body := map[string]interface{}{}
		for k, v := range q {
			body[k] = v
		}
		obj, err := yhCucumberCall("POST", "/home/movlist", q, body)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhCucumberCards(slot, yhCucumberDataList(obj), "")
		pc := yhCucumberPageInfo(obj)
		return items, pc, pc == 0 && len(items) > 0 || page < pc, nil
	case category == "col":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		obj, err := yhCucumberCall("GET", "/module/list", nil, nil)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhCucumberDataList(obj) {
			mod := yhMap(e)
			if mod == nil {
				continue
			}
			modName := g5StrVal(mod["modName"])
			for _, s := range yhArr(mod["subclass"]) {
				sm := yhMap(s)
				if sm == nil {
					continue
				}
				navID := g5StrVal(sm["navId"])
				navCls := g5StrVal(sm["navCls"])
				name := g5StrVal(sm["navName"])
				if navID == "" || name == "" {
					continue
				}
				out = append(out, makeDrama(slot, "dir:nav:"+navID+":"+navCls, name, yhCucumberPic(g5StrVal(sm["navImage"])), modName, "专栏", g5StrVal(sm["intro"])))
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "dir:nav:"):
		parts := strings.Split(category, ":")
		if len(parts) != 4 {
			return nil, 0, false, fmt.Errorf("yh_cucumber bad nav %s", category)
		}
		navID, _ := strconv.Atoi(parts[2])
		navCls, _ := strconv.Atoi(parts[3])
		q := map[string]string{
			"pageNo": strconv.Itoa(page), "pageSize": "12",
			"navId": parts[2], "navCls": parts[3],
		}
		// App passes the @Body map with Integer values (p2.f chain);
		// string values get code -1 处理异常 (live-verified 2026-10-24).
		body := map[string]interface{}{
			"pageNo": page, "pageSize": 12, "navId": navID, "navCls": navCls,
		}
		obj, err := yhCucumberCall("POST", "/module/movlist2", q, body)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhCucumberCards(slot, yhCucumberDataList(obj), "")
		pc := yhCucumberPageInfo(obj)
		return items, pc, page < pc, nil
	case strings.HasPrefix(category, "rank:"):
		parts := strings.Split(category, ":")
		if len(parts) != 3 {
			return nil, 0, false, fmt.Errorf("yh_cucumber bad rank %s", category)
		}
		q := map[string]string{
			"pageNo": strconv.Itoa(page), "pageSize": "30", "type": parts[2],
		}
		var obj map[string]interface{}
		var err error
		if parts[1] == "actors" {
			obj, err = yhCucumberCall("GET", "/ranking/actors", q, nil)
		} else {
			obj, err = yhCucumberCall("GET", "/ranking/"+parts[1], q, nil)
		}
		if err != nil {
			return nil, 0, false, err
		}
		if parts[1] == "actors" {
			// Ranking actors use actorsId + name + movNumber (live
			// 2026-10-24), unlike /home/actor/list (id + nameCn).
			out := []interface{}{}
			seen := map[string]bool{}
			for _, e := range yhCucumberDataList(obj) {
				a := yhMap(e)
				if a == nil {
					continue
				}
				aid := g5StrVal(a["actorsId"])
				if aid == "" {
					aid = g5StrVal(a["id"])
				}
				name := g5StrVal(a["nameCn"])
				if name == "" {
					name = g5StrVal(a["name"])
				}
				if aid == "" || name == "" || seen[aid] {
					continue
				}
				seen[aid] = true
				remark := ""
				if n := g5StrVal(a["movNumber"]); n != "" {
					remark = n + "部"
				}
				if cup := g5StrVal(a["cup"]); cup != "" {
					remark = cup + "杯 " + remark
				}
				out = append(out, makeDrama(slot, "dir:actor:"+aid, name, yhCucumberPic(g5StrVal(a["photoUrl"])), remark, "女优", g5StrVal(a["briefIntroduction"])))
			}
			return out, len(out), false, nil
		}
		items := yhCucumberCards(slot, yhCucumberDataList(obj), "")
		return items, len(items), len(items) >= 30, nil
	case category == "actor":
		obj, err := yhCucumberCall("GET", "/home/actor/list", map[string]string{
			"queryType": "1", "cupType": "0",
			"pageNo": strconv.Itoa(page), "pageSize": "21",
		}, nil)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		seen := map[string]bool{}
		for _, e := range yhCucumberDataList(obj) {
			a := yhMap(e)
			if a == nil {
				continue
			}
			aid := g5StrVal(a["id"])
			name := g5StrVal(a["nameCn"])
			if aid == "" || name == "" || seen[aid] {
				continue
			}
			seen[aid] = true
			remark := g5StrVal(a["nameEn"])
			if cup := g5StrVal(a["cup"]); cup != "" {
				remark = cup + "杯"
			}
			out = append(out, makeDrama(slot, "dir:actor:"+aid, name, yhCucumberPic(g5StrVal(a["photoUrl"])), remark, "女优", g5StrVal(a["briefIntroduction"])))
		}
		pc := yhCucumberPageInfo(obj)
		return out, pc, page < pc, nil
	case strings.HasPrefix(category, "dir:actor:"):
		aid := strings.TrimPrefix(category, "dir:actor:")
		obj, err := yhCucumberCall("GET", "/actor/movie/list", map[string]string{
			"id": aid, "pageNo": strconv.Itoa(page), "queryType": "1", "pageSize": "12",
		}, nil)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhCucumberCards(slot, yhCucumberDataList(obj), "")
		pc := yhCucumberPageInfo(obj)
		return items, pc, page < pc, nil
	case strings.HasPrefix(category, "short:"):
		order := strings.TrimPrefix(category, "short:")
		obj, err := yhCucumberCall("GET", "/shortvideo/listOrderBy"+order, map[string]string{"pageNo": strconv.Itoa(page)}, nil)
		if err != nil {
			return nil, 0, false, err
		}
		out := []interface{}{}
		for _, e := range yhCucumberDataList(obj) {
			rec := yhMap(e)
			if rec == nil {
				continue
			}
			id := yhCucumberVideoID(rec)
			title := yhCucumberVideoTitle(rec)
			if id == "" || title == "" || yhCucumberIsPaid(rec) {
				continue
			}
			out = append(out, makeDrama(slot, id, title, yhCucumberCover(g5StrVal(rec["cover"])), g5StrVal(rec["mins"]), "短剧", g5StrVal(rec["movDesc"])))
		}
		return out, len(out), len(out) > 0, nil
	}
	return yhCucumberCatalog(slot, "choice", page)
}

func yhCucumberBrowse(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	obj, err := yhCucumberCall("GET", "/mov/browse2", map[string]string{"movId": vid}, nil)
	if err != nil {
		return nil, err
	}
	items := yhCucumberDataList(obj)
	if len(items) == 0 {
		return nil, fmt.Errorf("yh_cucumber: video %s not found", vid)
	}
	rec := yhMap(items[0])
	if rec == nil {
		return nil, fmt.Errorf("yh_cucumber: bad detail for %s", vid)
	}
	return rec, nil
}

func yhCucumberActorDetail(slot, rawID string) (map[string]interface{}, error) {
	aid := strings.TrimPrefix(rawID, "dir:actor:")
	obj, err := yhCucumberCall("GET", "/home/actor/detail", map[string]string{"id": aid}, nil)
	if err != nil {
		return nil, err
	}
	var actor map[string]interface{}
	for _, e := range yhCucumberDataList(obj) {
		actor = yhMap(e)
		break
	}
	if actor == nil {
		return nil, fmt.Errorf("yh_cucumber: actor %s not found", aid)
	}
	name := g5StrVal(actor["nameCn"])
	if name == "" {
		name = g5StrVal(actor["name"])
	}
	info := fmt.Sprintf("身高：%s cm  三围：%s-%s-%s cm  罩杯：%s",
		g5StrVal(actor["height"]), g5StrVal(actor["bust"]), g5StrVal(actor["waist"]), g5StrVal(actor["hips"]), g5StrVal(actor["cup"]))
	desc := g5StrVal(actor["briefIntroduction"])
	if desc != "" {
		desc += "\n"
	}
	desc += info
	d := makeDrama(slot, rawID, name, yhCucumberPic(g5StrVal(actor["photoUrl"])), g5StrVal(actor["cup"])+"杯", "女优", desc)
	d["actor"] = name
	d["vod_actor"] = name
	works, err := yhCucumberCall("GET", "/actor/movie/list", map[string]string{
		"id": aid, "pageNo": "1", "queryType": "1", "pageSize": "12",
	}, nil)
	if err == nil {
		items := yhCucumberCards(slot, yhCucumberDataList(works), "")
		epIDs := []string{}
		epNames := []string{}
		for _, it := range items {
			if im, ok := it.(map[string]interface{}); ok {
				epIDs = append(epIDs, g5StrVal(im["vod_id"]))
				epNames = append(epNames, g5StrVal(im["title"]))
			}
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, epNames)
			d["related"] = items
		}
	}
	return d, nil
}

func yhCucumberDetail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "dir:actor:") {
		return yhCucumberActorDetail(slot, rawID)
	}
	if strings.HasPrefix(rawID, "dir:nav:") {
		parts := strings.Split(rawID, ":")
		if len(parts) != 4 {
			return nil, fmt.Errorf("yh_cucumber bad nav %s", rawID)
		}
		obj, err := yhCucumberCall("GET", "/module/nav", map[string]string{"navId": parts[2]}, nil)
		if err != nil {
			return nil, err
		}
		var nav map[string]interface{}
		for _, e := range yhCucumberDataList(obj) {
			nav = yhMap(e)
			break
		}
		name := parts[2]
		cover := ""
		desc := ""
		if nav != nil {
			name = g5StrVal(nav["navName"])
			cover = yhCucumberPic(g5StrVal(nav["photoAddr"]))
			desc = g5StrVal(nav["intro"])
		}
		d := makeDrama(slot, rawID, name, cover, "专栏", "专栏", desc)
		items, _, _, err := yhCucumberCatalog(slot, rawID, 1)
		if err == nil && len(items) > 0 {
			epIDs := []string{}
			epNames := []string{}
			for _, it := range items {
				if im, ok := it.(map[string]interface{}); ok {
					epIDs = append(epIDs, g5StrVal(im["vod_id"]))
					epNames = append(epNames, g5StrVal(im["title"]))
				}
			}
			g5SetChapters(d, slot, rawID, epIDs, epNames)
			d["related"] = items
		}
		return d, nil
	}
	rec, err := yhCucumberBrowse(slot, rawID)
	if err != nil {
		return nil, err
	}
	title := yhCucumberVideoTitle(rec)
	cover := yhCucumberVideoCover(rec)
	desc := yhCucumberVideoDesc(rec)
	detail := fmt.Sprintf("%s | %s | %s | %s", g5StrVal(rec["movScore"]), g5StrVal(rec["strPlayCnt"]), g5StrVal(rec["gmtCreate"]), g5StrVal(rec["movSnOri"]))
	if desc != "" {
		desc += "\n"
	}
	desc += detail
	d := makeDrama(slot, rawID, title, cover, g5StrVal(rec["mins"]), "", desc)
	actors := []string{}
	for _, a := range yhArr(rec["actorList"]) {
		if am := yhMap(a); am != nil {
			if n := g5StrVal(am["nameCn"]); n != "" {
				actors = append(actors, n)
			}
		}
	}
	if s := g5StrVal(rec["actor"]); s != "" {
		actors = append(actors, s)
	}
	if len(actors) > 0 {
		d["actor"] = strings.Join(actors, ", ")
		d["vod_actor"] = strings.Join(actors, ", ")
	}
	if g5StrVal(rec["director"]) != "" {
		d["director"] = g5StrVal(rec["director"])
	}
	g5SetChapters(d, slot, rawID, []string{rawID}, []string{"正片"})
	if sim, err := yhCucumberCall("GET", "/es/mov/similar", map[string]string{"movId": yhCucumberVideoID(rec)}, nil); err == nil {
		if items := yhCucumberCards(slot, yhCucumberDataList(sim), ""); len(items) > 0 {
			d["related"] = items
		}
	}
	return d, nil
}

func yhCucumberAddress(rec map[string]interface{}) string {
	addr := yhMap(rec["address"])
	if addr == nil {
		return ""
	}
	for _, q := range []string{"1080P", "720P", "480P", "360P", "240P"} {
		if s := g5StrVal(addr[q]); s != "" {
			return s
		}
	}
	return ""
}

func yhCucumberPlaylistOK(u string) bool {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "okhttp/4.9.0")
	req.Header.Set("Referer", "https://cuke01.live/")
	resp, err := yhCucumberCoverClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		if resp != nil {
			resp.Body.Close()
		}
		return false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return bytes.Contains(b, []byte("#EXTM3U"))
}

func yhCucumberResolve(slot, rawID string) (map[string]interface{}, error) {
	vid := rawID
	if i := strings.Index(vid, "$"); i >= 0 {
		vid = vid[i+1:]
	}
	rec, err := yhCucumberBrowse(slot, vid)
	if err != nil {
		return nil, err
	}
	if yhCucumberIsPaid(rec) {
		return nil, fmt.Errorf("yh_cucumber: video %s is vip/limited, not resolved", vid)
	}
	path := yhCucumberAddress(rec)
	if path == "" {
		return nil, fmt.Errorf("yh_cucumber: no play url for %s", vid)
	}
	u := path
	if !strings.HasPrefix(u, "http") {
		if !strings.HasPrefix(u, "/") {
			u = "/" + u
		}
		u = yhCucumberStream + u
		if !yhCucumberPlaylistOK(u) {
			alt := yhCucumberStreamFallback + path
			if yhCucumberPlaylistOK(alt) {
				u = alt
			}
		}
	}
	format, protocol := "m3u8", "hls"
	if strings.Contains(u, ".mp4") && !strings.Contains(u, ".m3u8") {
		format, protocol = "mp4", ""
	}
	return playResult(u, slot, rawID, "okhttp/4.9.0", yhCucumberStream+"/", format, protocol), nil
}

func yhCucumberSearch(slot, kw string) ([]interface{}, error) {
	obj, err := yhCucumberCall("GET", "/es/mov/search", map[string]string{
		"q": kw, "pageNo": "1", "pageSize": "10",
	}, nil)
	if err != nil {
		return nil, err
	}
	return yhCucumberCards(slot, yhCucumberDataList(obj), "搜索"), nil
}

func yhCucumberHeaders() map[string]string {
	return map[string]string{
		"User-Agent": "okhttp/4.9.0",
		"origin":     "https://cuke01.live",
		"release":    "410",
		"pkg":        "cuke0001",
		"terminal":   "4",
		"x-auth-key": yhCucumberAuthKey,
	}
}

func init() {
	registerSite(yhCucumberSlot, &siteImpl{
		Key:        yhCucumberSlot,
		Name:       "黄瓜视频",
		DefaultCat: "choice",
		HomeURL:    yhCucumberBases[0] + "/",
		Headers:    yhCucumberHeaders,
		Categories: yhCucumberCategories,
		Catalog:    yhCucumberCatalog,
		Detail:     yhCucumberDetail,
		Resolve:    yhCucumberResolve,
		Search:     yhCucumberSearch,
		CoverFetch: yhCucumberCoverFetch,
	})
	// Integration: append "yh_cucumber" to slotOrder (registry.go tail)
	// and add the builtin_config entry, as with yh_km / yh_bale.
}
