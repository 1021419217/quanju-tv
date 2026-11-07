package seku

// ---- G5 group: hstv / one / yuuhc / setang (encrypted & shell sites) ----
//
// Ports of the user's py spiders (final authority), recipes in
// batch/recipes_py.json. Slots per the G5 assignment:
//   hstv  -> yeguo  (alias hstv)
//   one   -> heguo  (alias one)
//   yuuhc -> huaguo (alias yuuhc)   [impl in g5_shell.go]
//   setang-> niuguo (alias setang)  [impl in g5_shell.go]
//
// Every crypto chain was verified independently in Go against the py
// spiders' archived artifacts BEFORE these providers were written:
//   hstv : CryptoJS EVP_BytesToKey(MD5)+AES-256-CBC decrypt of the
//          archived resp_js.json matches hstv_work/list_hot_p1.json.
//   one  : AES-128-CBC form crypto cross-checked both directions with
//          pycryptodome; MD5 double-sign matches the py formula.
//   yuuhc: tail-regex material location + share-XOR key + AES-256-GCM
//          reproduces yuuhc_work/{home,list1,video1}_dec.html byte-exact.
//   setang: blob state machine + length-classified trial decrypt
//          reproduces galaxy_work/{f18,fid19,t45}.html byte-exact.
//
// Pay boundaries (卡06, kept verbatim from the py spiders):
//   hstv: list/search drop vip items (_free_only); the hd （会员专享）
//         category is not registered at all.
//   one : model 6 （点播） exposes ONLY preview_video, with the coin
//         count labelled in remarks and a purchase note in the title;
//         models 3/5 are free in full. Non-playable models (1/2/4/7)
//         are filtered out of lists/search.
// Deviation from py, deliberate: hstv detail "tags" arrives as a plain
// string; py's " ".join(tags) would space out every character. Go uses
// the string as-is (list form still joins with spaces).

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	crand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"math/big"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func init() {
	registerSite("yeguo", g5HSTVImpl(), "hstv")
	// v6: heguo no longer registers the simplified ONE below — the
	// slot now carries the faithful port of the user's uploaded py
	// (sites_one.go). The g5One* implementation stays in this file,
	// unregistered, as the archived v1-v5 version.
	registerSite("huaguo", g5YuuhcImpl(), "yuuhc")
	registerSite("niuguo", g5SetangImpl(), "setang")
}

// ---------- shared crypto / shaping helpers ----------

func g5EVPBytesToKey(pw, salt []byte, n int) []byte {
	var d, prev []byte
	for len(d) < n {
		h := md5.New()
		h.Write(prev)
		h.Write(pw)
		h.Write(salt)
		prev = h.Sum(nil)
		d = append(d, prev...)
	}
	return d[:n]
}

func g5AESCBCDecrypt(ct, key, iv []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("bad ct length")
	}
	out := make([]byte, len(ct))
	cipher.NewCBCDecrypter(b, iv).CryptBlocks(out, ct)
	pad := int(out[len(out)-1])
	if pad < 1 || pad > aes.BlockSize || pad > len(out) {
		return nil, fmt.Errorf("bad pkcs7 padding")
	}
	return out[:len(out)-pad], nil
}

func g5AESCBCEncrypt(pt, key, iv []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	pad := aes.BlockSize - len(pt)%aes.BlockSize
	p := make([]byte, len(pt)+pad)
	copy(p, pt)
	for i := len(pt); i < len(p); i++ {
		p[i] = byte(pad)
	}
	out := make([]byte, len(p))
	cipher.NewCBCEncrypter(b, iv).CryptBlocks(out, p)
	return out, nil
}

func g5CryptoJSDecrypt(b64text, passphrase string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64text))
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(raw, []byte("Salted__")) || len(raw) < 16 {
		return nil, fmt.Errorf("bad ciphertext format")
	}
	salt, ct := raw[8:16], raw[16:]
	d := g5EVPBytesToKey([]byte(passphrase), salt, 48)
	return g5AESCBCDecrypt(ct, d[:32], d[32:48])
}

func g5GCMOpen(key, iv, aad, ctTag []byte) ([]byte, error) {
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return g.Open(nil, iv, ctTag, aad)
}

func g5MD5Hex(s string) string { return fmt.Sprintf("%x", md5.Sum([]byte(s))) }

// g5StrVal renders a decoded-JSON scalar the way py's str() would read
// it in these spiders (ids, remarks): integral floats without decimals.
func g5StrVal(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case json.Number:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}

func g5MapStr(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := g5StrVal(v); s != "" {
				return s
			}
		}
	}
	return ""
}

func g5MapInt(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int(t), true
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err == nil {
			return n, true
		}
	}
	return 0, false
}

func g5Truthy(v interface{}) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "1" || strings.EqualFold(t, "true")
	}
	return false
}

func g5StrList(v interface{}) []string {
	out := []string{}
	if arr, ok := v.([]interface{}); ok {
		for _, e := range arr {
			if s := g5StrVal(e); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

// g5SetChapters replaces a drama's chapter list (aligned with the
// original core's Chapter model, same shape as makeDrama's default).
func g5SetChapters(d map[string]interface{}, slot, rawID string, epIDs, names []string) {
	chapters := []interface{}{}
	for i, epID := range epIDs {
		name := "正片"
		if i < len(names) && names[i] != "" {
			name = names[i]
		}
		chapters = append(chapters, map[string]interface{}{
			"episodeId": epID,
			"id":        dramaID(slot, epID),
			"dramaId":   dramaID(slot, rawID),
			"sourceId":  slot,
			"source":    slot,
			"title":     name,
			"name":      name,
			"index":     i + 1,
			"episode":   i + 1,
			"url":       "",
			"videoUrl":  "",
			"mediaSize": 0,
		})
	}
	if len(chapters) == 0 {
		return
	}
	d["chapters"] = chapters
	d["episodeList"] = chapters
	d["episodes"] = len(chapters)
	d["episodeCount"] = len(chapters)
	d["chapterCount"] = len(chapters)
	d["totalEpisode"] = len(chapters)
	d["totalEpisodes"] = len(chapters)
}

// g5EpIDs builds episode ids: bare rawID for a single episode, else
// rawID#n (1-based), the suffix splitEpisodeSuffix understands.
func g5EpIDs(rawID string, n int) []string {
	out := []string{}
	for i := 0; i < n; i++ {
		if n == 1 {
			out = append(out, rawID)
		} else {
			out = append(out, fmt.Sprintf("%s#%d", rawID, i+1))
		}
	}
	return out
}

var (
	g5ScriptStyleRe = regexp.MustCompile(`(?is)<(script|style)[^>]*>.*?</(script|style)>`)
	g5TagRe         = regexp.MustCompile(`(?s)<[^>]+>`)
	g5SpaceRe       = regexp.MustCompile(`\s+`)
)

// g5StripHTML mirrors py one._strip_html (tags out, unescape, <=500).
func g5StripHTML(s string) string {
	s = g5ScriptStyleRe.ReplaceAllString(s, " ")
	s = g5TagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	s = g5SpaceRe.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 500 {
		s = string(r[:500])
	}
	return s
}

func g5UUID4() string {
	var b [16]byte
	if _, err := io.ReadFull(crand.Reader, b[:]); err != nil {
		// crypto/rand failure is not expected; fall back to time entropy.
		n := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(n >> (8 * (i % 8)))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ================= hstv (SevenVideo white-label, CryptoJS envelope) =================

const (
	g5HSTVUA         = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	g5HSTVReferer    = "https://hstv.fun/"
	g5HSTVPassphrase = "xxx" // SITE.passphrase, from the site's public JS bundle
	g5HSTVToken      = "your_default_token"
	g5HSTVPageSize   = 24
)

var g5HSTVHosts = []string{
	"https://v2.cdn199.com",
	"https://v2.kekecdn.net",
	"https://v2.luchu.org",
	"https://v2.madou.ws",
	"https://v2.papapa.biz",
	"https://v2.tianmtv.com",
	"https://v2.xiaoshuo.info",
	"https://v2.xiaoshuo.la",
}

// py CATS: the site "视频" page table verbatim (35 entries) minus hd
// （会员专享， pay), myfavs (login), forum (image/text), language (setting).
var g5HSTVCats = []struct{ id, name string }{
	{"hot", "当前最热"}, {"recent", "最近更新"}, {"high", "站长推荐"},
	{"recent_hot", "最近加精"}, {"10mins", "10分钟以上"}, {"20mins", "20分钟以上"},
	{"month_discuss", "本月讨论"}, {"month_favs", "本月收藏"}, {"favs", "收藏最多"},
	{"month_hot", "本月最热"}, {"last_month_hot", "上月最热"},
	{"c0", "国产传媒全集"}, {"c1", "麻豆视频"}, {"c2", "91制片厂"},
	{"c3", "玩偶姐姐"}, {"c4", "小鸟酱专题"}, {"c5", "糖心Vlog"},
	{"c6", "天美传媒"}, {"c7", "蜜桃传媒"}, {"c8", "皇家华人"},
	{"c9", "星空传媒"}, {"ca", "精东影业"}, {"cc", "成人头条"},
	{"cd", "乌鸦传媒"}, {"cl", "开心鬼传媒"}, {"japan", "日本高清AV"},
	{"banana", "汝工作室"}, {"swag", "台湾Swag全集"}, {"torture_all", "重口系列"},
	{"gcsm", "国产SM[全集]"}, {"pissvids", "欧美重口 (pissvids)"},
}

var (
	g5HSTVMu   sync.Mutex
	g5HSTVUUID string
)

func g5HSTVUUIDOnce() string {
	g5HSTVMu.Lock()
	defer g5HSTVMu.Unlock()
	if g5HSTVUUID == "" {
		n, err := crand.Int(crand.Reader, big.NewInt(9000))
		r := int64(1000)
		if err == nil {
			r += n.Int64()
		}
		g5HSTVUUID = "U" + time.Now().Format("20060102150405") + strconv.FormatInt(r, 10)
	}
	return g5HSTVUUID
}

func g5HSTVHeaders() map[string]string {
	return map[string]string{
		"Content-Type":  "application/json",
		"User-Agent":    g5HSTVUA,
		"Authorization": "Bearer " + g5HSTVToken,
		"Origin":        "https://hstv.fun",
		"Referer":       g5HSTVReferer,
		"Accept":        "application/json, text/plain, */*",
		"Connection":    "close",
	}
}

func g5HSTVFetch(host, path string, payload map[string]interface{}) (interface{}, error) {
	env := map[string]interface{}{}
	for k, v := range payload {
		env[k] = v
	}
	env["url"] = path
	env["deviceInfo"] = map[string]interface{}{}
	env["app"] = "hstv.fun"
	env["isStandalone"] = false
	env["theLink"] = "novaluenull"
	env["uuid"] = g5HSTVUUIDOnce()
	env["version"] = "260901"
	body, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	raw, code, _, err := httpDo("POST", host+"/js", bytes.NewReader(body), g5HSTVHeaders())
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("hstv http %d", code)
	}
	var resp struct {
		R string `json:"r"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || resp.R == "" {
		return nil, fmt.Errorf("hstv blocked or unexpected response")
	}
	pt, err := g5CryptoJSDecrypt(resp.R, g5HSTVPassphrase)
	if err != nil {
		return nil, err
	}
	var out interface{}
	if err := json.Unmarshal(pt, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// g5HSTVAPI mirrors py _api: sequential rotation over all hosts, 6
// rounds, 0.4s between failures (concurrent bursts get cut by the
// egress middlebox; sequential succeeds intermittently).
func g5HSTVAPI(path string, payload map[string]interface{}) (interface{}, error) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	var lastErr error
	for round := 0; round < 6; round++ {
		for _, host := range g5HSTVHosts {
			v, err := g5HSTVFetch(host, path, payload)
			if err == nil {
				return v, nil
			}
			lastErr = err
			time.Sleep(400 * time.Millisecond)
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("hstv api unreachable")
}

func g5HSTVCatName(id string) string {
	for _, c := range g5HSTVCats {
		if c.id == id {
			return c.name
		}
	}
	return ""
}

// g5HSTVVod shapes one list item (py _vod); ok=false when the item is
// vip or lacks id/title — the _free_only pay boundary.
func g5HSTVVod(slot string, it map[string]interface{}, catName string) (map[string]interface{}, bool) {
	if g5Truthy(it["vip"]) {
		return nil, false
	}
	id := g5MapStr(it, "vId", "id")
	name := g5MapStr(it, "title", "title_en")
	if id == "" || name == "" {
		return nil, false
	}
	thumbs := g5StrList(it["thumbnails"])
	if len(thumbs) == 0 {
		thumbs = g5StrList(it["thumbNails"])
	}
	pic := ""
	if len(thumbs) > 0 {
		pic = thumbs[0]
	}
	return makeDrama(slot, id, cleanText(name), pic, g5MapStr(it, "durationStr"), catName, ""), true
}

func g5HSTVFreeList(slot string, v interface{}, catName string) ([]interface{}, int) {
	arr, _ := v.([]interface{})
	items := []interface{}{}
	for _, e := range arr {
		if m, ok := e.(map[string]interface{}); ok {
			if d, ok := g5HSTVVod(slot, m, catName); ok {
				items = append(items, d)
			}
		}
	}
	return items, len(arr)
}

func g5HSTVCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	tid := category
	if tid == "" {
		tid = "hot"
	}
	v, err := g5HSTVAPI(fmt.Sprintf("/sevenVideos?page=%d&type=%s", page, tid), nil)
	if err != nil {
		return nil, 0, false, err
	}
	items, rawCount := g5HSTVFreeList(slot, v, g5HSTVCatName(tid))
	hasMore := rawCount >= g5HSTVPageSize
	total := rawCount
	if hasMore {
		total = 999999
	}
	return items, total, hasMore, nil
}

func g5HSTVDetailData(rawID string) (map[string]interface{}, error) {
	v, err := g5HSTVAPI("/sevenVideos/"+rawID, map[string]interface{}{
		"url_search": "", "token": "ufd",
	})
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("hstv detail shape")
	}
	return m, nil
}

func g5HSTVPlayURLs(d map[string]interface{}) []string {
	urls := g5StrList(d["m3u8s"])
	if m := g5MapStr(d, "m3u8"); m != "" {
		urls = append([]string{m}, urls...)
	}
	return urls
}

func g5HSTVDetail(slot, rawID string) (map[string]interface{}, error) {
	id, _ := splitEpisodeSuffix(rawID)
	d, err := g5HSTVDetailData(id)
	if err != nil {
		return nil, err
	}
	thumbs := g5StrList(d["thumbnails"])
	pic := ""
	if len(thumbs) > 0 {
		pic = thumbs[0]
	}
	// tags: list in some responses, plain string in others (see header).
	var desc string
	switch t := d["tags"].(type) {
	case []interface{}:
		desc = strings.Join(g5StrList(t), " ")
	case string:
		desc = t
	}
	title := g5MapStr(d, "title")
	dr := makeDrama(slot, id, cleanText(title), pic, g5MapStr(d, "durationStr"),
		g5MapStr(d, "videoType"), desc)
	if actor := g5MapStr(d, "user"); actor != "" {
		dr["actor"] = actor
		dr["vod_actor"] = actor
	}
	urls := g5HSTVPlayURLs(d)
	if len(urls) > 0 {
		names := []string{}
		for i := range urls {
			names = append(names, fmt.Sprintf("线路%d", i+1))
		}
		g5SetChapters(dr, slot, id, g5EpIDs(id, len(urls)), names)
	}
	return dr, nil
}

func g5HSTVResolve(slot, rawID string) (map[string]interface{}, error) {
	id, epIdx := splitEpisodeSuffix(rawID)
	d, err := g5HSTVDetailData(id)
	if err != nil {
		return nil, err
	}
	urls := g5HSTVPlayURLs(d)
	if len(urls) == 0 {
		return nil, fmt.Errorf("hstv no play url")
	}
	if epIdx < 1 || epIdx > len(urls) {
		epIdx = 1
	}
	return playResult(urls[epIdx-1], slot, rawID, g5HSTVUA, g5HSTVReferer, "m3u8", "hls"), nil
}

func g5HSTVSearch(slot, kw string) ([]interface{}, error) {
	v, err := g5HSTVAPI("/searchSevenVideos", map[string]interface{}{
		"page": 1, "keywords": kw,
		"filterCategory": "all", "filterTimeInDays": "all",
		"filterViews": "all", "filterDurationInMins": "all",
		"sortBy": "relevance", "keywordLogic": "and",
	})
	if err != nil {
		return []interface{}{}, err
	}
	items, _ := g5HSTVFreeList(slot, v, "搜索")
	return items, nil
}

func g5HSTVImpl() *siteImpl {
	return &siteImpl{
		Key: "hstv", Name: "HSTV", DefaultCat: "hot", HomeURL: "https://hstv.fun/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range g5HSTVCats {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog: g5HSTVCatalog,
		Detail:  g5HSTVDetail,
		Resolve: g5HSTVResolve,
		Search:  g5HSTVSearch,
		Headers: func() map[string]string {
			return map[string]string{"User-Agent": g5HSTVUA, "Referer": g5HSTVReferer}
		},
	}
}

// ================= ONE （一个） App API: AES form + MD5 double sign + bootstrap =================

const (
	g5OneUA          = "Mozilla/5.0 (Linux; Android 13; SGT-AL10 Build/HUAWEISGT-AL10) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36"
	g5OneSalt        = "m4n2hjPeYWkD6tFpqKF^3HO^h24P@idT"
	g5OneAppVersion  = "2.5.9.2"
	g5OnePageSize    = 20
	g5OneFallbackPic = "https://img807.hkq57tt.com/storage/promote/4/6698951c18f3f.png"
)

var (
	g5OneKey = []byte("l*bv%Ziq000Biaog")
	g5OneIV  = []byte("8597506002939249")
)

var g5OneAPIHostsSeed = []string{
	"https://api.5urjzs.com",
	"https://api.phplhr.com",
	"https://api.mgzsua.com",
	"https://api.h52ft6.com",
	"https://api.xeajfv.com",
	"https://917api.deyhhc3.com",
	"https://917api.ecmxb7e.com",
	"https://917api.5ebd5d.com",
	"https://917api.4c86d03.com",
	"https://917api.26bb4xt.com",
}

var g5OneCats = []struct{ id, name string }{
	{"daily", "每日上新"}, {"movie", "每日电影"}, {"demand", "点播预告"},
	{"audio", "音频专区"}, {"actors", "女优"},
}

var g5OneActorNavsFallback = []struct {
	id   int
	name string
}{
	{1, "日本"}, {2, "中国"}, {3, "美国"}, {4, "俄罗斯"}, {5, "日本（男）"},
}

// playable content models (py PLAYABLE): 3 video, 5 audio, 6 demand.
func g5OnePlayable(mid int) bool { return mid == 3 || mid == 5 || mid == 6 }

type g5OneState struct {
	mu           sync.Mutex
	uuid         string
	token        string
	domains      map[string][]string
	apiHosts     []string
	hostIdx      int
	booted       bool
	audioCache   []map[string]interface{}
	audioLoaded  bool
	demandCache  []map[string]interface{}
	demandLoaded bool
}

var g5One = &g5OneState{}

func (s *g5OneState) setupLocked() {
	if s.uuid != "" {
		return
	}
	s.uuid = g5UUID4()
	s.domains = map[string][]string{
		"video":    {"https://46e9nz.mfpt8g.com", "https://f86xxm.upmf83.com", "https://eruj5e.fwn9vj.com", "https://emg7wn.scycjz.com", "https://pu63rg.upmf83.com"},
		"image":    {"https://8r57s992801jp.gdliren123.com"},
		"audio":    {"https://46e9nz.mfpt8g.com", "https://f86xxm.upmf83.com"},
		"resource": {"https://img807.hkq57tt.com"},
	}
	s.apiHosts = append([]string{}, g5OneAPIHostsSeed...)
}

// headersLocked builds the signed header set (py _headers).
func (s *g5OneState) headersLocked(withToken bool) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	vals := map[string]string{
		"brand": "HUAWEI", "ip": "0.0.0.0", "model": "SGT-AL10",
		"platform": "1", "timestamp": ts, "user-key": "", "uuid": s.uuid,
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = vals[k]
	}
	h := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
		"User-Agent":   g5OneUA,
		"uuid":         s.uuid,
		"timestamp":    ts,
		"ip":           "0.0.0.0",
		"user-key":     "",
		"platform":     "1",
		"brand":        "HUAWEI",
		"model":        "SGT-AL10",
		"app-version":  g5OneAppVersion,
		"sign":         g5MD5Hex(g5MD5Hex(strings.Join(parts, ".")) + g5OneSalt),
	}
	if withToken && s.token != "" {
		h["token"] = s.token
	}
	return h
}

func (s *g5OneState) decodeResp(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '{' {
		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err == nil {
			return m
		}
		return nil
	}
	ct, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil
	}
	pt, err := g5AESCBCDecrypt(ct, g5OneKey, g5OneIV)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(pt, &m); err != nil {
		return nil
	}
	return m
}

// postLocked mirrors py _post: encrypted form body, host ring starting
// at hostIdx, first decodable response wins and advances hostIdx.
func (s *g5OneState) postLocked(path string, fields url.Values, withToken bool) map[string]interface{} {
	form := fields.Encode()
	ct, err := g5AESCBCEncrypt([]byte(form), g5OneKey, g5OneIV)
	if err != nil {
		return nil
	}
	body := base64.StdEncoding.EncodeToString(ct)
	n := len(s.apiHosts)
	for hi := 0; hi < n; hi++ {
		host := s.apiHosts[(s.hostIdx+hi)%n]
		raw, code, _, err := httpDo("POST", host+path, strings.NewReader(body), s.headersLocked(withToken))
		if err != nil || code >= 400 {
			continue
		}
		if j := s.decodeResp(raw); j != nil {
			s.hostIdx = (s.hostIdx + hi) % n
			return j
		}
	}
	return nil
}

func (s *g5OneState) bootstrapLocked() bool {
	j := s.postLocked("/v2.5/bootstrap", url.Values{}, false)
	if j == nil {
		return false
	}
	code, _ := g5MapInt(j, "code")
	data, _ := j["data"].(map[string]interface{})
	if code != 200 || data == nil {
		return false
	}
	if user, _ := data["user"].(map[string]interface{}); user != nil {
		if tok := g5MapStr(user, "token"); tok != "" {
			s.token = tok
		}
	}
	if dom, _ := data["domain"].(map[string]interface{}); dom != nil {
		for _, k := range []string{"video", "image", "audio", "resource"} {
			if lst := g5StrList(dom[k]); len(lst) > 0 {
				cleaned := []string{}
				for _, x := range lst {
					cleaned = append(cleaned, strings.TrimRight(x, "/"))
				}
				s.domains[k] = cleaned
			}
		}
		if lst := g5StrList(dom["api"]); len(lst) > 0 {
			extra := []string{}
			for _, x := range lst {
				extra = append(extra, strings.TrimRight(x, "/"))
			}
			merged := append([]string{}, extra...)
			for _, h := range s.apiHosts {
				found := false
				for _, e := range extra {
					if e == h {
						found = true
					}
				}
				if !found {
					merged = append(merged, h)
				}
			}
			s.apiHosts = merged
			s.hostIdx = 0
		}
	}
	s.booted = true
	return true
}

// apiLocked mirrors py _api: bootstrap once; on 401/450/451 reset,
// re-bootstrap and resend once.
func (s *g5OneState) apiLocked(path string, fields url.Values) map[string]interface{} {
	s.setupLocked()
	if !s.booted {
		s.bootstrapLocked()
	}
	j := s.postLocked(path, fields, true)
	if j != nil {
		if code, ok := g5MapInt(j, "code"); ok && (code == 401 || code == 450 || code == 451) {
			s.booted = false
			s.bootstrapLocked()
			j = s.postLocked(path, fields, true)
		}
	}
	return j
}

func (s *g5OneState) mediaLocked(kind, path string, idx int) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "http") {
		return path
	}
	hosts := s.domains[kind]
	if len(hosts) == 0 {
		return path
	}
	return hosts[idx%len(hosts)] + path
}

// picLocked mirrors py _pic (resource-domain plaintext cover default).
func (s *g5OneState) picLocked(item map[string]interface{}) string {
	p := g5MapStr(item, "thumbnail", "thumb")
	if p == "" {
		return g5OneFallbackPic
	}
	return s.mediaLocked("resource", p, 0)
}

// vodLocked mirrors py _vod, including the model-6 coin/preview remark.
func (s *g5OneState) vodLocked(slot string, item map[string]interface{}, catName string) map[string]interface{} {
	mid, _ := g5MapInt(item, "model_id")
	name := g5MapStr(item, "title", "video_name")
	remarks := g5MapStr(item, "published_at")
	if len(remarks) > 10 {
		remarks = remarks[:10]
	}
	if mid == 6 {
		bits := []string{}
		if ln := g5MapStr(item, "length"); ln != "" {
			bits = append(bits, "正片"+ln)
		}
		if coin, present := item["coin"]; present && g5StrVal(coin) != "" {
			bits = append(bits, g5StrVal(coin)+"金币")
		}
		if lf := item["is_limit_free"]; g5StrVal(lf) == "1" {
			bits = append(bits, "限时免费(预告)")
		} else {
			bits = append(bits, "预告")
		}
		remarks = strings.TrimSpace(remarks + " " + strings.Join(bits, "·"))
	} else if mid == 5 {
		remarks = strings.TrimSpace(remarks + " 音频")
	}
	return makeDrama(slot, g5MapStr(item, "id"), cleanText(name), s.picLocked(item), remarks, catName, "")
}

func g5OneDataMaps(j map[string]interface{}) []map[string]interface{} {
	out := []map[string]interface{}{}
	if j == nil {
		return out
	}
	switch d := j["data"].(type) {
	case []interface{}:
		for _, e := range d {
			if m, ok := e.(map[string]interface{}); ok {
				out = append(out, m)
			}
		}
	case map[string]interface{}:
		if lst, ok := d["list"].([]interface{}); ok {
			for _, e := range lst {
				if m, ok := e.(map[string]interface{}); ok {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

func g5OneToday() string { return time.Now().Format("2006-01-02") }

func (s *g5OneState) audioAllLocked() []map[string]interface{} {
	if !s.audioLoaded {
		j := s.apiLocked("/v2.5/audio/list", url.Values{})
		s.audioCache = g5OneDataMaps(j)
		s.audioLoaded = true
	}
	return s.audioCache
}

func g5OneCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	s := g5One
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setupLocked()
	tid := category
	if tid == "" {
		tid = "daily"
	}
	items := []interface{}{}
	switch {
	case tid == "daily":
		day := time.Now().AddDate(0, 0, -(page - 1)).Format("2006-01-02")
		j := s.apiLocked("/v2.5/article/day", url.Values{"published_at": {day}})
		var info []interface{}
		if j != nil {
			if data, _ := j["data"].(map[string]interface{}); data != nil {
				info, _ = data["info"].([]interface{})
			}
		}
		for _, e := range info {
			if m, ok := e.(map[string]interface{}); ok {
				if mid, _ := g5MapInt(m, "model_id"); g5OnePlayable(mid) {
					items = append(items, s.vodLocked(slot, m, "每日上新"))
				}
			}
		}
		return items, len(items), len(items) > 0, nil
	case tid == "movie":
		j := s.apiLocked("/v2.5/article/discovery", url.Values{
			"model_id": {"3"}, "published_at": {g5OneToday()},
			"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(g5OnePageSize)},
		})
		for _, m := range g5OneDataMaps(j) {
			items = append(items, s.vodLocked(slot, m, "每日电影"))
		}
		return items, len(items), len(items) > 0, nil
	case tid == "demand":
		if !s.demandLoaded {
			j := s.apiLocked("/v2.5/article/discovery", url.Values{
				"model_id": {"6"}, "published_at": {g5OneToday()},
				"page": {"1"}, "size": {strconv.Itoa(g5OnePageSize)},
			})
			s.demandCache = g5OneDataMaps(j)
			s.demandLoaded = true
		}
		data := s.demandCache
		start := (page - 1) * g5OnePageSize
		if start < len(data) {
			end := start + g5OnePageSize
			if end > len(data) {
				end = len(data)
			}
			for _, m := range data[start:end] {
				items = append(items, s.vodLocked(slot, m, "点播预告"))
			}
		}
		return items, len(data), start+g5OnePageSize < len(data), nil
	case tid == "audio":
		data := s.audioAllLocked()
		start := (page - 1) * g5OnePageSize
		if start < len(data) {
			end := start + g5OnePageSize
			if end > len(data) {
				end = len(data)
			}
			for _, x := range data[start:end] {
				rem := strings.TrimSpace(g5MapStr(x, "author") + " " + g5MapStr(x, "published_at")[:min(10, len(g5MapStr(x, "published_at")))])
				items = append(items, makeDrama(slot, "audio:"+g5MapStr(x, "id"),
					cleanText(g5MapStr(x, "title")), g5OneFallbackPic, rem, "音频专区", ""))
			}
		}
		return items, len(data), start+g5OnePageSize < len(data), nil
	case tid == "actors":
		if page > 1 {
			return items, 0, false, nil
		}
		navs := []struct {
			id   string
			name string
		}{}
		j := s.apiLocked("/v2.5/actor/getMainNavigations", url.Values{})
		if lst := g5OneDataMaps(j); len(lst) > 0 {
			for _, n := range lst {
				navs = append(navs, struct {
					id   string
					name string
				}{g5MapStr(n, "id"), g5MapStr(n, "name")})
			}
		} else {
			for _, n := range g5OneActorNavsFallback {
				navs = append(navs, struct {
					id   string
					name string
				}{strconv.Itoa(n.id), n.name})
			}
		}
		for _, n := range navs {
			items = append(items, makeDrama(slot, "anav:"+n.id, n.name, "", "女优导航", "女优", ""))
		}
		return items, len(items), false, nil
	case strings.HasPrefix(tid, "anav:"):
		j := s.apiLocked("/v2.5/actor/getMainNavigationActors", url.Values{
			"page": {strconv.Itoa(page)}, "size": {"30"},
			"main_navigation_id": {strings.TrimPrefix(tid, "anav:")}, "tag_ids": {""},
		})
		for _, a := range g5OneDataMaps(j) {
			rem := g5MapStr(a, "latest_published_at")
			if len(rem) > 10 {
				rem = rem[:10]
			}
			pic := s.mediaLocked("resource", g5MapStr(a, "thumbnail", "thumb"), 0)
			items = append(items, makeDrama(slot, "actor:"+g5MapStr(a, "id"),
				cleanText(g5MapStr(a, "main_name", "name")), pic, rem, "女优", ""))
		}
		return items, len(items), len(items) > 0, nil
	case strings.HasPrefix(tid, "actor:"):
		j := s.apiLocked("/v2.5/actor/getActorArticles", url.Values{
			"id":   {strings.TrimPrefix(tid, "actor:")},
			"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(g5OnePageSize)},
		})
		for _, m := range g5OneDataMaps(j) {
			if mid, _ := g5MapInt(m, "model_id"); g5OnePlayable(mid) {
				items = append(items, s.vodLocked(slot, m, "女优"))
			}
		}
		return items, len(items), len(items) > 0, nil
	}
	return items, 0, false, nil
}

type g5OneLine struct {
	name   string
	url    string
	format string // "mp4" | "m3u8" | "mp3"
}

// fetchDetailLocked mirrors py detailContent/_article_detail/
// _audio_detail: returns the shaped drama plus its play lines.
func (s *g5OneState) fetchDetailLocked(slot, rawID string) (map[string]interface{}, []g5OneLine, error) {
	vid := rawID
	if strings.HasPrefix(vid, "audio:") {
		aid := strings.TrimPrefix(vid, "audio:")
		for _, x := range s.audioAllLocked() {
			if g5MapStr(x, "id") == aid {
				u := s.mediaLocked("audio", g5MapStr(x, "video"), 0)
				rem := g5MapStr(x, "published_at")
				if len(rem) > 10 {
					rem = rem[:10]
				}
				d := makeDrama(slot, vid, cleanText(g5MapStr(x, "title")), g5OneFallbackPic, rem, "音频专区", "")
				if a := g5MapStr(x, "author"); a != "" {
					d["actor"] = a
					d["vod_actor"] = a
				}
				lines := []g5OneLine{}
				if u != "" {
					lines = append(lines, g5OneLine{"音频", u, "mp3"})
					g5SetChapters(d, slot, vid, []string{vid}, []string{"音频"})
				}
				return d, lines, nil
			}
		}
		return nil, nil, fmt.Errorf("one audio not found")
	}
	if strings.HasPrefix(vid, "anav:") || strings.HasPrefix(vid, "actor:") {
		// Folder entries: no detail/play of their own (py returns []).
		return makeDrama(slot, vid, vid, "", "女优导航", "女优", ""), nil, nil
	}
	j := s.apiLocked("/v2.5/article/detail", url.Values{"id": {vid}})
	var d map[string]interface{}
	if j != nil {
		d, _ = j["data"].(map[string]interface{})
	}
	if d == nil {
		return nil, nil, fmt.Errorf("one detail empty")
	}
	mid, _ := g5MapInt(d, "model_id")
	title := g5MapStr(d, "title", "video_name")
	lines := []g5OneLine{}
	switch mid {
	case 3:
		vf := g5MapStr(d, "video_file", "video")
		n := len(s.domains["video"])
		if n > 2 {
			n = 2
		}
		for i := 0; i < n; i++ {
			if u := s.mediaLocked("video", vf, i); u != "" {
				name := "MP4备用"
				if i == 0 {
					name = "MP4高清"
				}
				format := "mp4"
				if strings.Contains(u, ".m3u8") {
					format = "m3u8"
				}
				lines = append(lines, g5OneLine{name, u, format})
			}
		}
	case 5:
		if u := s.mediaLocked("audio", g5MapStr(d, "audio_file"), 0); u != "" {
			lines = append(lines, g5OneLine{"音频", u, "mp3"})
		}
	case 6:
		// Pay boundary: preview only; the full feature is coin-purchase
		// in the app and is never assembled here.
		pv := g5MapStr(d, "preview_video", "video_file")
		if u := s.mediaLocked("video", pv, 0); u != "" {
			format := "mp4"
			if strings.Contains(u, ".m3u8") {
				format = "m3u8"
			}
			lines = append(lines, g5OneLine{"预告片", u, format})
		}
		title += "（点播预告·正片需在App内购买）"
	}
	rem := g5MapStr(d, "published_at")
	if len(rem) > 10 {
		rem = rem[:10]
	}
	desc := g5StripHTML(g5MapStr(d, "description", "content"))
	dr := makeDrama(slot, vid, cleanText(title), s.picLocked(d), rem, "", desc)
	if a := g5MapStr(d, "author", "actor"); a != "" {
		dr["actor"] = a
		dr["vod_actor"] = a
	}
	if tags := g5StrList(d["tags"]); len(tags) > 0 {
		if len(tags) > 8 {
			tags = tags[:8]
		}
		dr["tags"] = tags
	}
	if len(lines) > 0 {
		names := []string{}
		for _, l := range lines {
			names = append(names, l.name)
		}
		g5SetChapters(dr, slot, vid, g5EpIDs(vid, len(lines)), names)
	}
	return dr, lines, nil
}

func g5OneDetail(slot, rawID string) (map[string]interface{}, error) {
	s := g5One
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setupLocked()
	d, _, err := s.fetchDetailLocked(slot, rawID)
	return d, err
}

func g5OneResolve(slot, rawID string) (map[string]interface{}, error) {
	s := g5One
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setupLocked()
	id, epIdx := splitEpisodeSuffix(rawID)
	_, lines, err := s.fetchDetailLocked(slot, id)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("one no play url")
	}
	if epIdx < 1 || epIdx > len(lines) {
		epIdx = 1
	}
	l := lines[epIdx-1]
	proto := "file"
	if l.format == "m3u8" {
		proto = "hls"
	}
	return playResult(l.url, slot, rawID, g5OneUA, "", l.format, proto), nil
}

func g5OneSearch(slot, kw string) ([]interface{}, error) {
	s := g5One
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setupLocked()
	j := s.apiLocked("/v2.5/article/search", url.Values{
		"keyword": {kw}, "model_id": {"0"},
		"page": {"1"}, "size": {strconv.Itoa(g5OnePageSize)},
	})
	items := []interface{}{}
	for _, m := range g5OneDataMaps(j) {
		if mid, _ := g5MapInt(m, "model_id"); g5OnePlayable(mid) {
			items = append(items, s.vodLocked(slot, m, "搜索"))
		}
	}
	return items, nil
}

func g5OneImpl() *siteImpl {
	return &siteImpl{
		Key: "one", Name: "ONE一个", DefaultCat: "daily", HomeURL: "https://api.5urjzs.com/",
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			for _, c := range g5OneCats {
				out = append(out, categoryObj(c.id, c.name))
			}
			return out
		},
		Catalog: g5OneCatalog,
		Detail:  g5OneDetail,
		Resolve: g5OneResolve,
		Search:  g5OneSearch,
		Headers: func() map[string]string {
			return map[string]string{"User-Agent": g5OneUA}
		},
	}
}
