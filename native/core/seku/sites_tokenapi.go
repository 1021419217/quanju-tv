package seku

import (
	"bytes"
	"crypto/aes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// 令牌 API 族共享引擎（盒子AI / 樱桃 Xme 同族，配方出 ZakaTV jar 的
// ZakaHzaiAmns / ZakaXmeAmns 反编译）：
//   - 游客开户：unique_code 本地生成 → POST /api/ai/register
//     {pid:0, unique_code, model:android, channelCode} 得游客号；
//     盒子AI 再走第二步 POST {prefix}/ai/register 换 VIP 令牌。
//   - 响应 unwrap：明文 JSON 直用；否则 base64 → AES/ECB 解密，
//     密钥 "=^%7@84%0^!8892-"（两站同钥，jar 常量实测）。
//   - 封面 enpic 是普通 URL，但图片字节整幅 XOR 0x88 混淆：
//     CoverFetch 取回后若不是图片魔数就全字节 XOR 0x88 再验。
//   - 分类由 API 索引动态给出（cateParentList），即目标站导航本身。
//
// 2026-10-24 探活现状（如实记录）：盒子AI 三主机对 /api/ai/register
// 与根路径均只回 "404 Not Found!"（API 家族下线状）；樱桃 register
// 回 HTTP 500。两站均按 jar 完整配方移植落码，待服务端恢复后可用，
// 本机未能活体验通——见批次报告_fix1.md。

type tokenAPIConfig struct {
	Slot        string
	Name        string
	Note        string
	Hosts       []string
	ChannelCode string
	Prefix      string // 盒子AI /api/vip/v1；樱桃 /api
	TwoStep     bool   // 盒子AI 两步开户
	Dark        bool   // 盒子AI 有暗网区（dark_ 分类）
	PrefixedIDs bool   // 盒子AI vod_id 带 v/d 前缀；樱桃裸 vid
}

type tokenAPIState struct {
	mu         sync.Mutex
	hostIdx    int
	uniqueCode string
	token      string
	guestUser  string
	guestPass  string
	cats       []tokenAPICat
	catsAt     time.Time
}

var tokenAPIStates = map[string]*tokenAPIState{}

func tokenAPIStateFor(slot string) *tokenAPIState {
	st, ok := tokenAPIStates[slot]
	if !ok {
		st = &tokenAPIState{}
		tokenAPIStates[slot] = st
	}
	return st
}

var tokenAPIClient = &http.Client{Timeout: 25 * time.Second, Transport: sharedTransport}

const tokenAPIUA = "Mozilla/5.0 (Linux; Android 13; SM-G991B) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

var tokenAPIKey = []byte("=^%7@84%0^!8892-")

func tokenAPIUnwrap(s string) string {
	t := strings.TrimSpace(s)
	if len(t) > 1 && t[0] == '"' && t[len(t)-1] == '"' {
		t = t[1 : len(t)-1]
	}
	t = strings.ReplaceAll(t, `\/`, "/")
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return t
	}
	if strings.Contains(t, "Not Found") || strings.Contains(t, "<html") || strings.Contains(t, "<!DOCTYPE") {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(t))
	if err != nil {
		if raw, err = base64.URLEncoding.DecodeString(strings.TrimSpace(t)); err != nil {
			return ""
		}
	}
	block, err := aes.NewCipher(tokenAPIKey)
	if err != nil || len(raw) == 0 || len(raw)%aes.BlockSize != 0 {
		return ""
	}
	out := make([]byte, len(raw))
	for i := 0; i < len(raw); i += aes.BlockSize {
		block.Decrypt(out[i:i+aes.BlockSize], raw[i:i+aes.BlockSize])
	}
	// PKCS#7 去填充
	if n := len(out); n > 0 {
		pad := int(out[n-1])
		if pad >= 1 && pad <= aes.BlockSize && pad <= n {
			out = out[:n-pad]
		}
	}
	return string(out)
}

func (c *tokenAPIConfig) host(st *tokenAPIState) string {
	if st.hostIdx < 0 || st.hostIdx >= len(c.Hosts) {
		return c.Hosts[0]
	}
	return c.Hosts[st.hostIdx]
}

// tokenAPIRaw performs one HTTP call against the state's current host.
func (c *tokenAPIConfig) raw(st *tokenAPIState, method, path string, query url.Values, body []byte) (string, error) {
	u := c.host(st) + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, u, rdr)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", tokenAPIUA)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	resp, err := tokenAPIClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ensureToken registers a guest account when no token is held.
// Caller holds st.mu.
func (c *tokenAPIConfig) ensureToken(st *tokenAPIState) error {
	if st.token != "" {
		return nil
	}
	if st.uniqueCode == "" {
		ms := time.Now().UnixMilli() & 0xffffffff
		st.uniqueCode = "tvbox" + strconv.FormatInt(ms, 16) + strconv.FormatInt(rand.Int63n(1<<62), 16)
	}
	var lastErr error
	for attempt := 0; attempt < len(c.Hosts); attempt++ {
		body, _ := json.Marshal(map[string]interface{}{
			"pid": 0, "unique_code": st.uniqueCode, "model": "android", "channelCode": c.ChannelCode,
		})
		raw, err := c.raw(st, "POST", "/api/ai/register", nil, body)
		if err == nil {
			if txt := tokenAPIUnwrap(raw); txt != "" {
				var resp map[string]interface{}
				if json.Unmarshal([]byte(txt), &resp) == nil {
					data := taMap(resp["data"])
					if data != nil {
						if uc := g5StrVal(data["unique_code"]); uc != "" {
							st.uniqueCode = uc
						}
						ui := taMap(data["userInfo"])
						guestTok := g5StrVal(data["token"])
						if ui != nil {
							st.guestUser = g5StrVal(ui["username"])
							st.guestPass = g5StrVal(ui["plain_passwd"])
							if st.guestPass == "" {
								st.guestPass = g5StrVal(ui["passwd"])
							}
							if st.guestPass == "" {
								st.guestPass = st.guestUser
							}
							if uc := g5StrVal(ui["unique_code"]); uc != "" {
								st.uniqueCode = uc
							}
						}
						if !c.TwoStep {
							st.token = guestTok
						} else if st.guestUser != "" {
							body2, _ := json.Marshal(map[string]interface{}{
								"username": st.guestUser, "passwd": st.guestPass,
								"unique_code": st.uniqueCode, "model": "android", "channelCode": c.ChannelCode,
							})
							raw2, err2 := c.raw(st, "POST", c.Prefix+"/ai/register", nil, body2)
							if err2 == nil {
								if txt2 := tokenAPIUnwrap(raw2); txt2 != "" {
									var resp2 map[string]interface{}
									if json.Unmarshal([]byte(txt2), &resp2) == nil {
										st.token = g5StrVal(taMap(resp2["data"])["token"])
									}
								}
							}
						}
						if st.token != "" {
							return nil
						}
					}
				}
			}
			lastErr = fmt.Errorf("register: no token in response")
		} else {
			lastErr = err
		}
		st.hostIdx = (st.hostIdx + 1) % len(c.Hosts)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("register failed")
	}
	return lastErr
}

func taMap(v interface{}) map[string]interface{} {
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func taArr(v interface{}) []interface{} {
	if a, ok := v.([]interface{}); ok {
		return a
	}
	return nil
}

func taInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	}
	return 0
}

func tokenAPIAuthFail(resp map[string]interface{}) bool {
	if resp == nil {
		return true
	}
	code, has := resp["code"]
	if !has {
		return false
	}
	n := taInt(code)
	if n == 401 || n == 403 {
		return true
	}
	if n == 0 && strings.Contains(fmt.Sprint(resp), "令牌") {
		return true
	}
	return false
}

// apiCall performs an authenticated API call with the jar's retry
// semantics: rotate host on empty response, re-register once on
// auth failure.
func (c *tokenAPIConfig) apiCall(method, path string, params url.Values, bodyObj map[string]interface{}) (map[string]interface{}, error) {
	st := tokenAPIStateFor(c.Slot)
	st.mu.Lock()
	defer st.mu.Unlock()
	do := func() (map[string]interface{}, string, error) {
		if err := c.ensureToken(st); err != nil {
			return nil, "", err
		}
		q := url.Values{}
		for k, vs := range params {
			for _, v := range vs {
				q.Add(k, v)
			}
		}
		q.Set("accessToken", st.token)
		var body []byte
		if bodyObj != nil {
			bo := map[string]interface{}{}
			for k, v := range bodyObj {
				bo[k] = v
			}
			bo["accessToken"] = st.token
			body, _ = json.Marshal(bo)
		}
		raw, err := c.raw(st, method, path, q, body)
		if err != nil {
			return nil, "", err
		}
		txt := tokenAPIUnwrap(raw)
		if txt == "" {
			return nil, "", nil
		}
		var resp map[string]interface{}
		if err := json.Unmarshal([]byte(txt), &resp); err != nil {
			return nil, "", nil
		}
		return resp, txt, nil
	}
	resp, _, err := do()
	if err == nil && resp == nil {
		st.hostIdx = (st.hostIdx + 1) % len(c.Hosts)
		resp, _, err = do()
	}
	if err == nil && tokenAPIAuthFail(resp) {
		st.token = ""
		resp, _, err = do()
	}
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("%s: empty api response", c.Slot)
	}
	return resp, nil
}

func tokenAPIPickVideos(data interface{}) []map[string]interface{} {
	var out []map[string]interface{}
	var walk func(v interface{})
	walk = func(v interface{}) {
		switch t := v.(type) {
		case []interface{}:
			for _, e := range t {
				if m, ok := e.(map[string]interface{}); ok {
					if vl, ok := m["videoList"].([]interface{}); ok {
						for _, sub := range vl {
							if sm, ok := sub.(map[string]interface{}); ok {
								out = append(out, sm)
							}
						}
					} else {
						out = append(out, m)
					}
				}
			}
		case map[string]interface{}:
			for _, key := range []string{"list", "videoList", "data"} {
				if arr, ok := t[key].([]interface{}); ok {
					walk(arr)
					return
				}
			}
			// cateVideo 式：{分类名: [视频...]} 值数组全拼
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			for _, k := range keys {
				if arr, ok := t[k].([]interface{}); ok {
					walk(arr)
				}
			}
		}
	}
	walk(data)
	return out
}

func (c *tokenAPIConfig) card(v map[string]interface{}, dark bool) (map[string]interface{}, bool) {
	vid := g5StrVal(v["vid"])
	if vid == "" {
		vid = g5StrVal(v["id"])
	}
	name := g5StrVal(v["vod_name"])
	if vid == "" || name == "" {
		return nil, false
	}
	prefix := ""
	if c.PrefixedIDs {
		if dark {
			prefix = "d"
		} else {
			prefix = "v"
		}
	}
	remark := g5StrVal(v["duration"])
	if eyes := g5StrVal(v["eyes"]); eyes != "" && eyes != "0万" {
		remark = strings.TrimSpace(remark + " · 🔥" + eyes)
	}
	if remark == "" {
		if ut := g5StrVal(v["update_time"]); len(ut) >= 10 {
			remark = ut[:10]
		}
	}
	return makeDrama(c.Slot, prefix+vid, name, g5StrVal(v["enpic"]), remark, "", ""), true
}

type tokenAPICat struct{ id, name string }

func (c *tokenAPIConfig) categories() ([]tokenAPICat, error) {
	st := tokenAPIStateFor(c.Slot)
	st.mu.Lock()
	cached, at := st.cats, st.catsAt
	st.mu.Unlock()
	if cached != nil && time.Since(at) < 10*time.Minute {
		return cached, nil
	}
	cats := []tokenAPICat{{"new", "🔥最新入库"}}
	resp, err := c.apiCall("GET", c.Prefix+"/video/index", nil, nil)
	if err != nil {
		return nil, err
	}
	data := taMap(resp["data"])
	if data != nil {
		for _, e := range taArr(data["cateParentList"]) {
			m := taMap(e)
			if m == nil {
				continue
			}
			pid := taInt(m["cate_pid"])
			title := g5StrVal(m["title"])
			if title != "" {
				cats = append(cats, tokenAPICat{"cate_" + strconv.Itoa(pid), title})
			}
		}
	}
	if c.Dark {
		if dresp, derr := c.apiCall("GET", c.Prefix+"/dark/index", nil, nil); derr == nil {
			ddata := taMap(dresp["data"])
			if ddata != nil {
				for _, e := range taArr(ddata["categoryList"]) {
					m := taMap(e)
					if m == nil {
						continue
					}
					cid := taInt(m["cid"])
					title := g5StrVal(m["title"])
					if title != "" {
						cats = append(cats, tokenAPICat{"dark_" + strconv.Itoa(cid), "🕳" + title})
					}
				}
			}
		}
	}
	st.mu.Lock()
	st.cats, st.catsAt = cats, time.Now()
	st.mu.Unlock()
	return cats, nil
}

func (c *tokenAPIConfig) catalog(catID string, page int) ([]map[string]interface{}, int, bool, error) {
	if page < 1 {
		page = 1
	}
	var path string
	params := url.Values{}
	dark := false
	switch {
	case catID == "" || catID == "new":
		path = c.Prefix + "/video/newVideoList"
		params.Set("page", strconv.Itoa(page))
		params.Set("limit", "24")
	case strings.HasPrefix(catID, "cate_"):
		path = c.Prefix + "/video/cateVideoList"
		params.Set("cate_pid", strings.TrimPrefix(catID, "cate_"))
		params.Set("page", strconv.Itoa(page))
	case strings.HasPrefix(catID, "dark_") && c.Dark:
		path = c.Prefix + "/dark/videoByCid"
		params.Set("cid", strings.TrimPrefix(catID, "dark_"))
		params.Set("page", strconv.Itoa(page))
		params.Set("limit", "24")
		dark = true
	default:
		path = c.Prefix + "/video/newVideoList"
		params.Set("page", strconv.Itoa(page))
		params.Set("limit", "24")
	}
	resp, err := c.apiCall("GET", path, params, nil)
	if err != nil {
		return nil, 0, false, err
	}
	vids := tokenAPIPickVideos(resp["data"])
	items := make([]map[string]interface{}, 0, len(vids))
	for _, v := range vids {
		if item, ok := c.card(v, dark); ok {
			items = append(items, item)
		}
	}
	total := 0
	if dm := taMap(resp["data"]); dm != nil {
		total = taInt(dm["total"])
	}
	return items, total, len(vids) >= 24, nil
}

func (c *tokenAPIConfig) stripID(rawID string) (vid string, dark bool) {
	if c.PrefixedIDs && len(rawID) > 1 {
		if rawID[0] == 'd' {
			return rawID[1:], true
		}
		if rawID[0] == 'v' {
			return rawID[1:], false
		}
	}
	return rawID, false
}

func (c *tokenAPIConfig) detail(rawID string) (map[string]interface{}, error) {
	vid, dark := c.stripID(rawID)
	path := c.Prefix + "/video/detail"
	if dark && c.Dark {
		path = c.Prefix + "/dark/detail"
	}
	params := url.Values{}
	params.Set("vid", vid)
	resp, err := c.apiCall("GET", path, params, nil)
	if err != nil {
		return nil, err
	}
	data := taMap(resp["data"])
	if data == nil {
		return nil, fmt.Errorf("%s: detail empty for %s", c.Slot, rawID)
	}
	video := taMap(data["video"])
	if video == nil {
		video = data
	}
	name := g5StrVal(video["vod_name"])
	playURL := g5StrVal(video["play_url"])
	if playURL == "" {
		playURL = g5StrVal(video["playUrl"])
	}
	d := map[string]interface{}{"name": name}
	if pic := g5StrVal(video["enpic"]); pic != "" {
		d["pic"] = pic
	}
	zone := c.Name
	if dark {
		zone = "暗网重口"
	}
	desc := name + "\n播放源：" + zone + " · 播放 " + g5StrVal(video["eyes"]) + "\n更新：" + g5StrVal(video["update_time"])
	d["desc"] = desc
	if ut := g5StrVal(video["update_time"]); len(ut) >= 4 {
		d["year"] = ut[:4]
	}
	if playURL != "" {
		g5SetChapters(d, c.Slot, rawID, []string{playURL}, []string{name})
	}
	rel := tokenAPIPickVideos(data["relatedVideoList"])
	related := make([]interface{}, 0, len(rel))
	for i, v := range rel {
		if i >= 12 {
			break
		}
		if item, ok := c.card(v, dark); ok {
			related = append(related, item)
		}
	}
	if len(related) > 0 {
		d["related"] = related
	}
	return d, nil
}

func (c *tokenAPIConfig) resolve(playID string) (map[string]interface{}, error) {
	if strings.HasPrefix(playID, "http") {
		return playResult(playID, c.Slot, playID, tokenAPIUA, "", "", "mp4"), nil
	}
	// 章节 id 不是 http 时按 jar playerContent 回查详情取 play_url
	d, err := c.detail(playID)
	if err != nil {
		return nil, err
	}
	if chs, ok := d["chapters"].([]interface{}); ok && len(chs) > 0 {
		if ch, ok := chs[0].(map[string]interface{}); ok {
			if u := g5StrVal(ch["episodeId"]); strings.HasPrefix(u, "http") {
				return playResult(u, c.Slot, playID, tokenAPIUA, "", "", "mp4"), nil
			}
		}
	}
	return nil, fmt.Errorf("%s: no play_url for %s", c.Slot, playID)
}

func (c *tokenAPIConfig) search(keyword string, page int) ([]map[string]interface{}, error) {
	if page < 1 {
		page = 1
	}
	resp, err := c.apiCall("POST", c.Prefix+"/video/search", nil, map[string]interface{}{
		"keyword": keyword, "page": page, "limit": 24,
	})
	if err != nil {
		return nil, err
	}
	vids := tokenAPIPickVideos(resp["data"])
	items := make([]map[string]interface{}, 0, len(vids))
	for _, v := range vids {
		if item, ok := c.card(v, false); ok {
			items = append(items, item)
		}
	}
	return items, nil
}

// coverFetch 解 enpic 混淆：整幅 XOR 0x88（jar $HzaiProxy/$XmeProxy 的
// deXor，常量 -120 即 0x88）；本身已是图片则原样返回。
func (c *tokenAPIConfig) coverFetch(rawURL string) ([]byte, string, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", tokenAPIUA)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	resp, err := tokenAPIClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 12<<20))
	if err != nil {
		return nil, "", err
	}
	if len(b) == 0 {
		return nil, "", fmt.Errorf("empty cover")
	}
	if mime := sniffImageMime(b); mime != "" {
		return b, mime, nil
	}
	if len(b) >= 8 {
		out := make([]byte, len(b))
		for i := range b {
			out[i] = b[i] ^ 0x88
		}
		if mime := sniffImageMime(out); mime != "" {
			return out, mime, nil
		}
	}
	return b, "image/jpeg", nil
}

func sniffImageMime(b []byte) string {
	switch {
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "image/jpeg"
	case len(b) >= 4 && b[0] == 0x89 && b[1] == 0x50 && b[2] == 0x4e && b[3] == 0x47:
		return "image/png"
	case len(b) >= 3 && string(b[:3]) == "GIF":
		return "image/gif"
	case len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP":
		return "image/webp"
	}
	return ""
}

func tokenAPIImpl(cfg *tokenAPIConfig) *siteImpl {
	return &siteImpl{
		Key:        cfg.Slot,
		Name:       cfg.Name,
		DefaultCat: "new",
		HomeURL:    cfg.Hosts[0],
		Categories: func(slot string) []interface{} {
			out := []interface{}{}
			cats, err := cfg.categories()
			if err != nil || len(cats) == 0 {
				return []interface{}{categoryObj("new", "🔥最新入库")}
			}
			for _, ct := range cats {
				out = append(out, categoryObj(ct.id, ct.name))
			}
			return out
		},
		Catalog: func(slot, category string, page int) ([]interface{}, int, bool, error) {
			items, total, hasMore, err := cfg.catalog(category, page)
			if err != nil {
				return nil, 0, false, err
			}
			out := make([]interface{}, 0, len(items))
			for _, it := range items {
				out = append(out, it)
			}
			return out, total, hasMore, nil
		},
		Detail:  func(slot, rawID string) (map[string]interface{}, error) { return cfg.detail(rawID) },
		Resolve: func(slot, rawID string) (map[string]interface{}, error) { return cfg.resolve(rawID) },
		Search: func(slot, kw string) ([]interface{}, error) {
			items, err := cfg.search(kw, 1)
			if err != nil {
				return nil, err
			}
			out := make([]interface{}, 0, len(items))
			for _, it := range items {
				out = append(out, it)
			}
			return out, nil
		},
		CoverFetch: func(rawURL string) ([]byte, error) {
			b, _, err := cfg.coverFetch(rawURL)
			return b, err
		},
	}
}

// 盒子AI（hzai）与樱桃（xme）原配置已按用户 2026-10-24「坏站不进
// 成品」通则撤下：hzai 三主机 register 全 404（服务端下线状）、xme
// register 500，同属服务端坏。令牌 API 引擎留树存档，无站点引用；
// 两站配方（主机/channelCode/前缀）在 git 历史的本文件旧版可查。

