package seku

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
)

// ---- Batch2: thdh 探花短视频 (ZakaThdhApp) ----
// App-style JSON API restored from the class: host pool in ZakaSec
// strings + config ext host. Endpoints:
//   GET /index/video.php?type=list&page=N[&free=1]
//   GET /index/video.php?type=detail&id=N  (video_play.php is member-gated)
// Config marks the site searchable=0, so Search returns empty.
// VIP-locked items are listed with their lock state; Resolve reports
// the lock instead of fabricating a stream.

var z2ThdhHosts = []string{
	"https://thdhhaj.xyz", // config ext host (primary)
	"https://thdhran.xyz",
	"https://thdhjtd.xyz",
	"https://thdhaci.com",
	"https://thdhasw.com",
	"https://thdhadi.com",
}

var (
	z2ThdhMu   sync.Mutex
	z2ThdhHost string
)

func z2ThdhHeaders() map[string]string {
	return map[string]string{
		"User-Agent":       z2UA,
		"Accept":           "application/json, text/plain, */*",
		"Accept-Language":  "zh-CN,zh;q=0.9",
		"X-Requested-With": "XMLHttpRequest",
	}
}

func z2ThdhGet(path string) ([]byte, error) {
	z2ThdhMu.Lock()
	host := z2ThdhHost
	z2ThdhMu.Unlock()
	try := func(h string) ([]byte, error) {
		body, code, _, err := httpGet(h+path, z2ThdhHeaders())
		if err != nil {
			return nil, err
		}
		if code != 200 {
			return nil, fmt.Errorf("http %d", code)
		}
		return body, nil
	}
	if host != "" {
		return try(host)
	}
	var lastErr error
	for _, h := range z2ThdhHosts {
		body, err := try(h)
		if err == nil {
			z2ThdhMu.Lock()
			z2ThdhHost = h
			z2ThdhMu.Unlock()
			return body, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("z2_thdh all hosts failed: %v", lastErr)
}

type z2ThdhItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	CoverURL    string `json:"cover_url"`
	Duration    int    `json:"duration"`
	AccessType  string `json:"access_type"`
	Locked      bool   `json:"locked"`
	PreviewSecs int    `json:"preview_seconds"`
	ViewCount   int    `json:"view_count"`
	CategoryID  int    `json:"category_id"`
}

type z2ThdhListResp struct {
	Code int `json:"code"`
	Data struct {
		List  []z2ThdhItem `json:"list"`
		Total int          `json:"total"`
		Page  int          `json:"page"`
	} `json:"data"`
}

func z2ThdhCategories(slot string) []interface{} {
	return []interface{}{
		categoryObj("latest", "最新"),
		categoryObj("free", "免费专区"),
	}
}

func z2ThdhCatalog(slot, category string, page int) ([]interface{}, int, bool, error) {
	path := "/index/video.php?type=list&page=" + strconv.Itoa(page)
	if category == "free" {
		path += "&free=1"
	}
	body, err := z2ThdhGet(path)
	if err != nil {
		return nil, 0, false, err
	}
	var resp z2ThdhListResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, false, fmt.Errorf("z2_thdh list json: %v", err)
	}
	// 站点 2026-11 起把成功码从 0 改成了 1，data.list 照常有内容。
	// 只认 code==0 会把整站判成失败（体检里表现为「目录 0 · list code 1」），
	// 因此以「有没有拿到列表」为准，code 只在列表为空时作为错误信息。
	if resp.Code != 0 && len(resp.Data.List) == 0 {
		return nil, 0, false, fmt.Errorf("z2_thdh list code %d", resp.Code)
	}
	items := []interface{}{}
	for _, it := range resp.Data.List {
		remark := fmt.Sprintf("%d秒", it.Duration)
		if it.Locked {
			remark += " · VIP"
		}
		items = append(items, makeDrama(slot, strconv.Itoa(it.ID), it.Title, it.CoverURL, remark, "探花短视频", ""))
	}
	return items, resp.Data.Total, len(resp.Data.List) > 0, nil
}

func z2ThdhFindStream(v interface{}) string {
	switch t := v.(type) {
	case map[string]interface{}:
		for _, k := range []string{"play_url", "playUrl", "url", "video_url", "m3u8"} {
			if s, ok := t[k].(string); ok && (strings.Contains(s, ".m3u8") || strings.Contains(s, ".mp4")) {
				return s
			}
		}
		for _, val := range t {
			if s := z2ThdhFindStream(val); s != "" {
				return s
			}
		}
	case []interface{}:
		for _, val := range t {
			if s := z2ThdhFindStream(val); s != "" {
				return s
			}
		}
	}
	return ""
}

// detail lives on video.php?type=detail (video_play.php is the member
// play gate and rejects anonymous calls). Response shape:
// {code,msg,data:{video:{...},access:{allow,reason,...},...}}
func z2ThdhDetailRaw(rawID string) (map[string]interface{}, error) {
	body, err := z2ThdhGet("/index/video.php?type=detail&id=" + rawID)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int                    `json:"code"`
		Data map[string]interface{} `json:"data"`
		Msg  string                 `json:"msg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("z2_thdh detail json: %v", err)
	}
	if resp.Data == nil {
		return nil, fmt.Errorf("z2_thdh detail empty: %s", resp.Msg)
	}
	return resp.Data, nil
}

func z2ThdhDetail(slot, rawID string) (map[string]interface{}, error) {
	title, cover, desc := "", "", ""
	if st := getStore(slot).findDrama(rawID); st != nil {
		title, _ = st["title"].(string)
		cover, _ = st["cover"].(string)
	}
	if data, err := z2ThdhDetailRaw(rawID); err == nil {
		if video, ok := data["video"].(map[string]interface{}); ok {
			if s, ok := video["title"].(string); ok && s != "" {
				title = s
			}
			if s, ok := video["cover_url"].(string); ok && s != "" {
				cover = s
			}
			if s, ok := video["intro"].(string); ok {
				desc = s
			}
		}
	}
	return makeDrama(slot, rawID, title, cover, "正片", "探花短视频", desc), nil
}

func z2ThdhResolve(slot, rawID string) (map[string]interface{}, error) {
	data, err := z2ThdhDetailRaw(rawID)
	if err != nil {
		return nil, err
	}
	if access, ok := data["access"].(map[string]interface{}); ok {
		if allow, ok := access["allow"].(bool); ok && !allow {
			reason, _ := access["reason"].(string)
			return nil, fmt.Errorf("z2_thdh 本片需解锁(%s)，匿名无播放地址", reason)
		}
	}
	u := z2ThdhFindStream(data)
	if u == "" {
		return nil, fmt.Errorf("z2_thdh 取播放地址失败")
	}
	format, proto := "m3u8", "hls"
	if !strings.Contains(u, ".m3u8") {
		format, proto = "mp4", "file"
	}
	return playResult(u, slot, rawID, z2UA, z2ThdhHosts[0]+"/", format, proto), nil
}

func init() {
	registerSite("z2_thdh", &siteImpl{
		Key: "z2_thdh", Name: "探花短视频", DefaultCat: "latest", HomeURL: z2ThdhHosts[0] + "/",
		Categories: z2ThdhCategories,
		Catalog:    z2ThdhCatalog,
		Detail:     z2ThdhDetail,
		Resolve:    z2ThdhResolve,
		Search:     func(slot, kw string) ([]interface{}, error) { return []interface{}{}, nil },
		Headers:    z2ThdhHeaders,
	}, "thdh")
}
