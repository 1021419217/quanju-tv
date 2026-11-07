package seku

// ---- 腐老二 fulao2 (yh_fulao2, type 12) — HostInfo 发现 + 响应解密 ----
//
// 配方来源（jadx 逐行实证，见逆向报告 §2.5/§5.5）：
//   发现：GET 三处 host_f2_pwa_v3.json（v1/a.d/e/f）
//     https://storage.csmier.pro/host_f2_pwa_v3.json
//     https://storage.chatdig.life/host_f2_pwa_v3.json
//     https://storage.danaii.online/host_f2_pwa_v3.json
//     → Host{api[], img[], stream[], stream_v2[]}；取法（Host bean）：
//     最佳 Api = api[] 中出现次数最多的 url；最佳图同理；
//     默认 Stream = stream_v2[] 中 mode=="novip" 者，否则首项。
//     响应解密（v1/e Fl2Interceptor）：体先掐头去尾各 11 字节 →
//     AES/CBC/PKCS5 解密，key = Base64 解码 "r4noeDOmDihuKgZ8mt8RHStHbSNyA4K5otWJMGrAab0="，
//     IV = Base64 解码 "Od75YI4YjizN/loLy++5/w==" → 再掐头去尾各 32 字节得 JSON。
//   令牌：GET /v1/verify/code，头 referer: https://webal.mapmall.net（v1/a.g
//     的 @i 注解是 Header，不是 query 参数；写成 query 会回 403
//     "referer error"，2026-10-24 实测）→ 体正则 `\((.*?)\)` 取 hash；
//     POST /v1/register/guest JSON {"hash": hash}
//     → TokenBean.response.token；失败时用 t1/c.java 内置默认 token。
//   每次 API 请求（v1/e）：query 追加 token=<token>，头 origin:
//     https://webal.mapmall.net，User-Agent: iPhone Safari 16.6 串。
//   API 响应解密（v1/e else 分支）：体为 Base64 → AES/CBC/PKCS5，
//     key = "3a0fd42302f0aada8abc0529a2bde5aa"（32 字节原文作 key），
//     IV = MD5hex(X-VTag 响应头)[8:24]（v1/b.a 实证）。
//     离线验真（2026-10-24）：以该 IV 构造解 FuLao2LaunchActivity 内嵌
//     密文，解出真 TokenBean JSON（code 200 + JWT），构造逐行成立。
//   列表端点（v1/a）：
//     GET /v1/menu/{index}/layout（页1）与 /v1/menu/{index}/layout/{layout}
//       （页>1），query {page, type, top, video_type, order, timestamp(ms)}
//     GET /v1/menu/{index}/layout（分类页，index=0）
//       query {page, order:time, video_type:long, type, category, timestamp(s)}
//     GET /v1/videos/popular/lists query {type, timestamp}
//     GET /v1/videos/popular/actor query {type:actor, timestamp}
//     GET /v1/actors query {page, order, cup_range, timestamp}
//     GET /v1/actor/{id} query {page, timestamp(ms)}
//     GET /v1/category/long?timestamp(s) → categories[]
//     GET /v2/search/{uncensored|censored}/{key} query {page, timestamp(ms)}
//     GET /v1/video/info/{index}（空参）
//   播放（VideoInfo.getPlayUrl）：取 video_urls.480 否则 .240，否则 intro；
//     若非 http 开头则拼：{apiBase}/v3/web{path}&k=MD5hex(video_id+"fulao2")
//     &origin=web&token={token}&h={StreamHost}；http 开头者 URL 解码后直用。
//     播放清单体解密（v1/d FL2PlayerInterceptor，播放器层）：若 .m3u 体不以
//     #EXTM3U 开头，以 getFl2 key + MD5hex(X-VTag)[8:24] 解密——本核只返 URL，
//     该体解密归播放器拦截层（见回执待真机项）。
//   VIP 口径（App 自身 getter 行为）：VideoList.getVideos 滤 main_tag 含
//     "VIP" 者；VideoList2.getVideos 另滤 "抢先发行"；本核两处同滤。
//   封面：VideosBean.getCover——不含 http 时前缀最佳图域；VideoInfo 同理。
//
// 分类与 App 导航 1:1：无码（最新/热点/传媒/爽尻/91大神）、有码（最新/
// 黄动画/中字/抢先看/火爆）、发现（榜单 6 组 + 女优总览）、全部分类
// （/v1/category/long 动态）、女优详情下钻（dir:actor:<id>）。
//
// 验证门（2026-10-24，本机冒烟实测，非出口问题）：
//   发现链活：host 文件拉取成功，当前轮换基址为 api-al.yuytyr.online、
//   图 images.hujiayi.com、流 stream.bckbp.com（旧 api-al.xsfiv.com 仍
//   响应，但 host 文件已指向新域）；但令牌门卡住：
//     /v1/verify/code 在 referer 走头（已修正 query→头）且 origin/UA 齐
//     全、token 空/默认两种形态下均回 403 "referer error"（另试 api 域
//     自身、尾斜杠、旧域均同）；菜单以默认 token 请求回 495
//     "need encrypt"。即服务端 referer 门已较 APK 时期变化（或需
//     WebView/JS 侧值），本机未能取得新鲜 guest token，故 Catalog/
//     Detail/Resolve/Search 均为空壳（status 495，无数据），封面魔数
//     未验。代码逐行实证、离线解密已验（见上）；待服务端门变化或真机
//     WebView 流取得 hash 后，同一代码路径即可通。不把此门写成站已死。

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

const yhFL2Slot = "yh_fulao2"
const yhFL2UA = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.6 Mobile/15E148 Safari/604.1"
const yhFL2Origin = "https://webal.mapmall.net"
const yhFL2DefaultAPI = "https://api-al.xsfiv.com"
const yhFL2DefaultImg = "https://images.sfc188.club"
const yhFL2RespKey = "3a0fd42302f0aada8abc0529a2bde5aa"
const yhFL2PlayKey = "db6f7f9e5d7a770e0e3497a7d7a077f5"
const yhFL2HostKeyB64 = "r4noeDOmDihuKgZ8mt8RHStHbSNyA4K5otWJMGrAab0="
const yhFL2HostIVB64 = "Od75YI4YjizN/loLy++5/w=="
const yhFL2FallbackToken = "TlRJMlpkVGtsemEwYlJsY01KdjdCeitLTGtUUDUxTXNQS2pnRktDTisvT2xrM0xBTUx6NVhPZmExRUxsSUttcmtucmc4dndDR3djZmJhZGR1d0FZWlAvL25mVWM0N1NQUURTTVg3c2RaSzZKMTRqVE1tTTM"

var yhFL2HostURLs = []string{
	"https://storage.csmier.pro/host_f2_pwa_v3.json",
	"https://storage.chatdig.life/host_f2_pwa_v3.json",
	"https://storage.danaii.online/host_f2_pwa_v3.json",
}

var (
	yhFL2Mu        sync.Mutex
	yhFL2Ready     bool
	yhFL2API       string
	yhFL2Img       string
	yhFL2Stream    string
	yhFL2Token     string
	yhFL2InitErr   error
	yhFL2VerifyErr error
)

func yhFL2VerifyFailed() error {
	yhFL2Mu.Lock()
	defer yhFL2Mu.Unlock()
	if yhFL2Token == yhFL2FallbackToken {
		return yhFL2VerifyErr
	}
	return nil
}

func yhFL2MD5IV(seed string) []byte {
	sum := md5.Sum([]byte(seed))
	return []byte(hex.EncodeToString(sum[:])[8:24])
}

func yhFL2Unpad(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	n := int(b[len(b)-1])
	if n < 1 || n > 16 || n > len(b) {
		return b
	}
	return b[:len(b)-n]
}

func yhFL2Decrypt(key, iv, raw []byte) ([]byte, error) {
	if len(raw) == 0 || len(raw)%16 != 0 {
		return nil, fmt.Errorf("yh_fulao2: bad ciphertext len %d", len(raw))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, raw)
	return yhFL2Unpad(out), nil
}

// yhFL2DecryptB64：响应体 Base64 → AES/CBC 解密（v1/b.b 原路）。
func yhFL2DecryptB64(key, iv []byte, body string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(body))
	if err != nil {
		return "", err
	}
	plain, err := yhFL2Decrypt(key, iv, raw)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func yhFL2Headers() map[string]string {
	return map[string]string{
		"User-Agent": yhFL2UA,
		"origin":     yhFL2Origin,
	}
}

// yhFL2Discover：拉 host 文件并取最佳 Api/Img/Stream（v1/e + Host bean）。
func yhFL2Discover() (api, img, stream string, err error) {
	for _, u := range yhFL2HostURLs {
		b, code, _, gerr := httpGet(u, yhFL2Headers())
		if gerr != nil || code != 200 || len(b) <= 22 {
			continue
		}
		inner := string(b[11 : len(b)-11])
		key, kerr := base64.StdEncoding.DecodeString(yhFL2HostKeyB64)
		iv, ierr := base64.StdEncoding.DecodeString(yhFL2HostIVB64)
		if kerr != nil || ierr != nil {
			continue
		}
		plain, derr := yhFL2DecryptB64(key, iv, inner)
		if derr != nil || len(plain) <= 64 {
			continue
		}
		plain = plain[32 : len(plain)-32]
		var host struct {
			API []struct {
				URL string `json:"url"`
			} `json:"api"`
			Img []struct {
				URL string `json:"url"`
			} `json:"img"`
			Stream []struct {
				URL string `json:"url"`
			} `json:"stream"`
			StreamV2 []struct {
				Mode string `json:"mode"`
				URL  string `json:"url"`
			} `json:"stream_v2"`
		}
		if jerr := json.Unmarshal([]byte(plain), &host); jerr != nil {
			continue
		}
		best := func(urls []string) string {
			counts := map[string]int{}
			bestURL, bestN := "", 0
			for _, s := range urls {
				counts[s]++
				if counts[s] > bestN {
					bestN, bestURL = counts[s], s
				}
			}
			return bestURL
		}
		apiURLs, imgURLs := []string{}, []string{}
		for _, e := range host.API {
			apiURLs = append(apiURLs, e.URL)
		}
		for _, e := range host.Img {
			imgURLs = append(imgURLs, e.URL)
		}
		api, img = best(apiURLs), best(imgURLs)
		stream = ""
		for _, e := range host.StreamV2 {
			if e.Mode == "novip" {
				stream = e.URL
				break
			}
		}
		if stream == "" && len(host.StreamV2) > 0 {
			stream = host.StreamV2[0].URL
		}
		if api != "" {
			return api, img, stream, nil
		}
	}
	return "", "", "", fmt.Errorf("yh_fulao2: host discovery failed")
}

var yhFL2VerifyRe = regexp.MustCompile(`\((.*?)\)`)

// yhFL2Ensure：发现 + 令牌（失败时退默认基址/默认 token，不中断）。
func yhFL2Reset() {
	yhFL2Mu.Lock()
	yhFL2Ready = false
	yhFL2Mu.Unlock()
}

func yhFL2Ensure() {
	yhFL2Mu.Lock()
	defer yhFL2Mu.Unlock()
	if yhFL2Ready {
		return
	}
	yhFL2API, yhFL2Img, yhFL2Stream = yhFL2DefaultAPI, yhFL2DefaultImg, ""
	if api, img, stream, err := yhFL2Discover(); err == nil {
		yhFL2API = api
		if img != "" {
			yhFL2Img = img
		}
		yhFL2Stream = stream
	} else {
		yhFL2InitErr = err
	}
	yhFL2Token = yhFL2FallbackToken
	tryVerify := func(referer string) (string, bool) {
		vh := yhFL2Headers()
		vh["referer"] = referer
		if b, code, _, err := httpGet(yhFL2API+"/v1/verify/code", vh); err == nil && code == 200 {
			if m := yhFL2VerifyRe.FindSubmatch(b); len(m) == 2 {
				return string(m[1]), true
			}
		}
		return "", false
	}
	hash, ok := tryVerify(yhFL2Origin)
	if !ok {
		hash, ok = tryVerify(yhFL2API + "/")
	}
	if !ok {
		yhFL2VerifyErr = fmt.Errorf("yh_fulao2: verify rejected (referer gate)")
		return
	}
	yhFL2VerifyErr = nil
	body, _ := json.Marshal(map[string]string{"hash": hash})
	h := yhFL2Headers()
	h["Content-Type"] = "application/json; charset=utf-8"
	if rb, code2, hdr2, err2 := httpDo("POST", yhFL2API+"/v1/register/guest", bytes.NewReader(body), h); err2 == nil && code2 == 200 {
		if obj, perr := yhFL2ParseAPI(rb, hdr2.Get("X-VTag")); perr == nil {
			if tok := g5StrVal(yhMap(obj["response"])["token"]); tok != "" {
				yhFL2Token = tok
				yhFL2Ready = true
			}
		}
	}
}

// yhFL2ParseAPI：先明文 JSON，否则以 X-VTag 派生 IV 解密（v1/e else 分支）。
func yhFL2ParseAPI(body []byte, vtag string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "{") {
		var obj map[string]interface{}
		if err := json.Unmarshal(body, &obj); err == nil {
			return obj, nil
		}
	}
	plain, err := yhFL2DecryptB64([]byte(yhFL2RespKey), yhFL2MD5IV(vtag), trimmed)
	if err != nil {
		return nil, fmt.Errorf("yh_fulao2 decrypt: %v", err)
	}
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(plain), &obj); err != nil {
		return nil, fmt.Errorf("yh_fulao2 parse: %v", err)
	}
	return obj, nil
}

func yhFL2Call(path string, q url.Values) (map[string]interface{}, http.Header, error) {
	yhFL2Ensure()
	if verr := yhFL2VerifyFailed(); verr != nil {
		return nil, nil, verr
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("token", yhFL2Token)
	u := yhFL2API + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	b, code, hdr, err := httpGet(u, yhFL2Headers())
	if err != nil {
		return nil, nil, err
	}
	if code == 495 {
		yhFL2Reset()
		yhFL2Ensure()
		q.Set("token", yhFL2Token)
		u = yhFL2API + path
		if len(q) > 0 {
			u += "?" + q.Encode()
		}
		b, code, hdr, err = httpGet(u, yhFL2Headers())
		if err != nil {
			return nil, nil, err
		}
	}
	if code != 200 {
		return nil, hdr, fmt.Errorf("yh_fulao2 http %d", code)
	}
	obj, err := yhFL2ParseAPI(b, hdr.Get("X-VTag"))
	return obj, hdr, err
}

func yhFL2Cover(p string) string {
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "http") {
		return p
	}
	yhFL2Ensure()
	return strings.TrimSuffix(yhFL2Img, "/") + "/" + strings.TrimPrefix(p, "/")
}

func yhFL2Tags(v interface{}) string {
	parts := []string{}
	for _, e := range yhArr(v) {
		parts = append(parts, g5StrVal(e))
	}
	return strings.Join(parts, "")
}

func yhFL2Card(slot string, rec map[string]interface{}) (map[string]interface{}, bool) {
	id := g5StrVal(rec["video_id"])
	title := g5StrVal(rec["video_title"])
	if id == "" || title == "" {
		return nil, false
	}
	tags := yhFL2Tags(rec["main_tag"])
	if strings.Contains(tags, "VIP") || strings.Contains(tags, "抢先发行") {
		return nil, false
	}
	remark := tags
	if remark == "" {
		remark = yhFL2Tags(rec["second_tag"])
	}
	desc := g5StrVal(rec["video_description"])
	if desc == "" {
		desc = g5StrVal(rec["actor"])
	}
	return makeDrama(slot, id, title, yhFL2Cover(g5StrVal(rec["cover"])), remark, "", desc), true
}

func yhFL2Cards(slot string, recs []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[string]bool{}
	for _, e := range recs {
		if m := yhMap(e); m != nil {
			if c, ok := yhFL2Card(slot, m); ok && !seen[g5StrVal(m["video_id"])] {
				seen[g5StrVal(m["video_id"])] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// yhFL2ListVideos：VideoList 形态 {response{videos[]|data[], total_results}}。
func yhFL2ListVideos(slot string, obj map[string]interface{}) []interface{} {
	res := yhMap(obj["response"])
	if res == nil {
		return []interface{}{}
	}
	list := yhArr(res["videos"])
	if len(list) == 0 {
		list = yhArr(res["data"])
	}
	return yhFL2Cards(slot, list)
}

// yhFL2MenuSections：VideoList2 形态 {response[{data[]}]}，展平。
func yhFL2MenuSections(slot string, obj map[string]interface{}) []interface{} {
	out := []interface{}{}
	for _, sec := range yhArr(obj["response"]) {
		out = append(out, yhFL2Cards(slot, yhArr(yhMap(sec)["data"]))...)
	}
	return out
}

func yhFL2Categories(slot string) []interface{} {
	cats := []interface{}{
		categoryObj("menu:5:uncover:time::96", "无码·最新"),
		categoryObj("menu:7:uncover:views:day:19", "无码·热点"),
		categoryObj("menu:11:uncover:time::23", "无码·传媒"),
		categoryObj("menu:12:uncover:time::24", "无码·爽尻"),
		categoryObj("menu:14:uncover:time::114", "无码·91大神"),
		categoryObj("menu:4:cover:time::16", "有码·最新"),
		categoryObj("menu:18:cover:views:day:186", "有码·黄动画"),
		categoryObj("menu:8:cover:time::21", "有码·中字"),
		categoryObj("menu:16:cover:time::159", "有码·抢先看"),
		categoryObj("menu:6:cover:time::18", "有码·火爆"),
		categoryObj("pop:av_guide", "榜单·AV指南"),
		categoryObj("pop:new", "榜单·最新上片"),
		categoryObj("pop:original_videos", "榜单·原创"),
		categoryObj("pop:uncover", "榜单·无码"),
		categoryObj("pop:cover", "榜单·有码"),
		categoryObj("actors", "女优总览"),
		categoryObj("types", "全部分类"),
	}
	return cats
}

func yhFL2MenuCatalog(slot string, parts []string, page int) ([]interface{}, int, bool, error) {
	index, typ, order, top, layout := parts[1], parts[2], parts[3], parts[4], parts[5]
	q := url.Values{}
	q.Set("page", fmt.Sprintf("%d", page))
	q.Set("type", typ)
	q.Set("top", top)
	q.Set("video_type", "long")
	q.Set("order", order)
	q.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	if page == 1 {
		obj, _, err := yhFL2Call(fmt.Sprintf("/v1/menu/%s/layout", index), q)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhFL2MenuSections(slot, obj)
		return items, len(items), len(items) > 0, nil
	}
	obj, _, err := yhFL2Call(fmt.Sprintf("/v1/menu/%s/layout/%s", index, layout), q)
	if err != nil {
		return nil, 0, false, err
	}
	items := yhFL2ListVideos(slot, obj)
	return items, len(items), len(items) > 0, nil
}

func yhFL2Catalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	if strings.HasPrefix(category, "menu:") {
		parts := strings.Split(category, ":")
		if len(parts) == 6 {
			return yhFL2MenuCatalog(slot, parts, page)
		}
	}
	switch {
	case strings.HasPrefix(category, "pop:"):
		q := url.Values{}
		q.Set("type", strings.TrimPrefix(category, "pop:"))
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		obj, _, err := yhFL2Call("/v1/videos/popular/lists", q)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhFL2ListVideos(slot, obj)
		return items, len(items), false, nil
	case category == "actors" || strings.HasPrefix(category, "actors:"):
		cup, order := "", "views"
		if strings.HasPrefix(category, "actors:") {
			if p := strings.Split(strings.TrimPrefix(category, "actors:"), ":"); len(p) >= 1 {
				cup = p[0]
			}
			if p := strings.Split(strings.TrimPrefix(category, "actors:"), ":"); len(p) >= 2 {
				order = p[1]
			}
		}
		q := url.Values{}
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("order", order)
		q.Set("cup_range", cup)
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		obj, _, err := yhFL2Call("/v1/actors", q)
		if err != nil {
			return nil, 0, false, err
		}
		res := yhMap(obj["response"])
		out := []interface{}{}
		seen := map[string]bool{}
		if res != nil {
			for _, e := range yhArr(res["actors"]) {
				b := yhMap(e)
				if b == nil {
					continue
				}
				aid := g5StrVal(b["actor_id"])
				name := g5StrVal(b["actor_name"])
				if aid == "" || name == "" || seen[aid] {
					continue
				}
				seen[aid] = true
				out = append(out, makeDrama(slot, "dir:actor:"+aid, name, yhFL2Cover(g5StrVal(b["actor_url"])), "", "女优", ""))
			}
		}
		return out, len(out), len(out) > 0, nil
	case strings.HasPrefix(category, "dir:actor:"):
		q := url.Values{}
		q.Set("page", fmt.Sprintf("%d", page))
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
		obj, _, err := yhFL2Call("/v1/actor/"+strings.TrimPrefix(category, "dir:actor:"), q)
		if err != nil {
			return nil, 0, false, err
		}
		items := yhFL2ListVideos(slot, obj)
		return items, len(items), len(items) > 0, nil
	case category == "types":
		if page > 1 {
			return []interface{}{}, 0, false, nil
		}
		q := url.Values{}
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
		obj, _, err := yhFL2Call("/v1/category/long", q)
		if err != nil {
			return nil, 0, false, err
		}
		res := yhMap(obj["response"])
		out := []interface{}{}
		if res != nil {
			for _, e := range yhArr(res["categories"]) {
				c := yhMap(e)
				if c == nil {
					continue
				}
				cid := g5StrVal(c["category_id"])
				name := g5StrVal(c["category_name"])
				if cid == "" || name == "" {
					continue
				}
				out = append(out, categoryObj("cat:"+cid, name))
			}
		}
		return out, len(out), false, nil
	case strings.HasPrefix(category, "cat:"):
		cid := strings.TrimPrefix(category, "cat:")
		for _, typ := range []string{"uncover", "cover"} {
			q := url.Values{}
			q.Set("page", fmt.Sprintf("%d", page))
			q.Set("order", "time")
			q.Set("video_type", "long")
			q.Set("type", typ)
			q.Set("category", cid)
			q.Set("timestamp", fmt.Sprintf("%d", time.Now().Unix()))
			obj, err := func() (map[string]interface{}, error) {
				o, _, e := yhFL2Call("/v1/menu/0/layout", q)
				return o, e
			}()
			if err != nil {
				continue
			}
			items := yhFL2MenuSections(slot, obj)
			if len(items) > 0 {
				return items, len(items), len(items) > 0, nil
			}
		}
		return []interface{}{}, 0, false, nil
	}
	return yhFL2MenuCatalog(slot, []string{"", "5", "uncover", "time", "", "96"}, page)
}

func yhFL2Detail(slot, rawID string) (map[string]interface{}, error) {
	if strings.HasPrefix(rawID, "dir:actor:") {
		items, _, _, err := yhFL2Catalog(slot, rawID, 1)
		if err != nil {
			return nil, err
		}
		d := makeDrama(slot, rawID, strings.TrimPrefix(rawID, "dir:actor:"), "", "女优", "女优", "")
		epIDs, names := []string{}, []string{}
		for _, it := range items {
			if im, ok := it.(map[string]interface{}); ok {
				epIDs = append(epIDs, g5StrVal(im["vod_id"]))
				names = append(names, g5StrVal(im["title"]))
			}
		}
		if len(epIDs) > 0 {
			g5SetChapters(d, slot, rawID, epIDs, names)
			d["related"] = items
		}
		return d, nil
	}
	obj, _, err := yhFL2Call("/v1/video/info/"+rawID, nil)
	if err != nil {
		return nil, err
	}
	res := yhMap(obj["response"])
	if res == nil {
		return nil, fmt.Errorf("yh_fulao2: video info empty for %s", rawID)
	}
	title := g5StrVal(res["video_title"])
	cover := g5StrVal(res["cover_url"])
	if cover == "" {
		cover = g5StrVal(res["cover"])
	}
	desc := g5StrVal(res["video_description"])
	if desc == "" {
		desc = strings.Join(func() []string {
			out := []string{}
			for _, e := range yhArr(res["video_tags"]) {
				out = append(out, g5StrVal(e))
			}
			return out
		}(), " ")
	}
	d := makeDrama(slot, rawID, title, yhFL2Cover(cover), g5StrVal(res["video_number"]), "", desc)
	// 选集 = 清晰度线（480P/240P/预览），与 nana/lutube 同族形态。
	names, ids := []string{}, []string{}
	for _, q := range []struct {
		key, name string
	}{{"480", "480P"}, {"240", "240P"}, {"intro", "预览"}} {
		if v := yhMap(res["video_urls"]); v != nil && g5StrVal(v[q.key]) != "" {
			names = append(names, q.name)
			ids = append(ids, rawID+"$q:"+q.key)
		}
	}
	if len(ids) == 0 {
		ids, names = []string{rawID}, []string{"正片"}
	}
	g5SetChapters(d, slot, rawID, ids, names)
	return d, nil
}

func yhFL2PlayURL(vid, quality string) (string, error) {
	yhFL2Ensure()
	obj, _, err := yhFL2Call("/v1/video/info/"+vid, nil)
	if err != nil {
		return "", err
	}
	res := yhMap(obj["response"])
	if res == nil {
		return "", fmt.Errorf("yh_fulao2: video info empty for %s", vid)
	}
	urls := yhMap(res["video_urls"])
	if urls == nil {
		return "", fmt.Errorf("yh_fulao2: no video_urls for %s", vid)
	}
	pick := func() string {
		if quality != "" {
			if s := g5StrVal(urls[quality]); s != "" {
				return s
			}
		}
		if s := g5StrVal(urls["480"]); s != "" {
			return s
		}
		if s := g5StrVal(urls["240"]); s != "" {
			return s
		}
		return g5StrVal(urls["intro"])
	}
	str := pick()
	if str == "" {
		return "", fmt.Errorf("yh_fulao2: no play url for %s", vid)
	}
	if !strings.HasPrefix(str, "http") {
		sum := md5.Sum([]byte(vid + "fulao2"))
		str = yhFL2API + "/v3/web" + str + "&k=" + hex.EncodeToString(sum[:]) +
			"&origin=web&token=" + yhFL2Token + "&h=" + yhFL2Stream
	} else if u, err := url.QueryUnescape(str); err == nil && strings.Contains(str, "%") {
		str = u
	}
	return str, nil
}

func yhFL2Resolve(slot, rawID string) (map[string]interface{}, error) {
	vid, quality := rawID, ""
	if i := strings.Index(rawID, "$"); i >= 0 {
		vid, quality = rawID[:i], rawID[i+1:]
	}
	if strings.HasPrefix(quality, "q:") {
		quality = strings.TrimPrefix(quality, "q:")
	}
	u, err := yhFL2PlayURL(vid, quality)
	if err != nil {
		return nil, err
	}
	format, protocol := "mp4", ""
	if strings.Contains(u, ".m3u8") {
		format, protocol = "m3u8", "hls"
	}
	return playResult(u, slot, rawID, yhFL2UA, yhFL2Origin+"/", format, protocol), nil
}

func yhFL2Search(slot, kw string) ([]interface{}, error) {
	out := []interface{}{}
	seen := map[string]bool{}
	anyOK := false
	var firstErr error
	for _, typ := range []string{"uncensored", "censored"} {
		q := url.Values{}
		q.Set("page", "1")
		q.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
		obj, _, err := yhFL2Call("/v2/search/"+typ+"/"+url.PathEscape(kw), q)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		anyOK = true
		for _, it := range yhFL2ListVideos(slot, obj) {
			if im, ok := it.(map[string]interface{}); ok {
				id := g5StrVal(im["vod_id"])
				if !seen[id] {
					seen[id] = true
					out = append(out, it)
				}
			}
		}
	}
	if !anyOK && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}

func init() {
	registerSite(yhFL2Slot, &siteImpl{
		Key:        yhFL2Slot,
		Name:       "腐老二",
		DefaultCat: "menu:5:uncover:time::96",
		HomeURL:    yhFL2DefaultAPI + "/",
		Headers:    yhFL2Headers,
		Categories: yhFL2Categories,
		Catalog:    yhFL2Catalog,
		Detail:     yhFL2Detail,
		Resolve:    yhFL2Resolve,
		Search:     yhFL2Search,
	})
	// Integration: append "yh_fulao2" to slotOrder (registry.go tail) and
	// add the builtin_config entry — see port_out integration notes.
}
