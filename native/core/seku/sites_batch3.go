package seku

import (
	"encoding/json"
	"fmt"
	"net/url"
)

const pgBase = "https://pigav.com"
const pgUA = "Mozilla/5.0 (Linux; Android 12) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"

func pgHeaders() map[string]string {
	return map[string]string{
		"User-Agent": pgUA,
		"Accept":     "application/json, text/plain, */*",
		"Referer":    pgBase + "/",
	}
}

type pgCategory struct {
	Label string `json:"label"`
}

type pgVideo struct {
	UUID        string     `json:"uuid"`
	Name        string     `json:"name"`
	Thumbnail   string     `json:"thumbnailPath"`
	Duration    float64    `json:"duration"`
	Description string     `json:"description"`
	Category    pgCategory `json:"category"`
	Playlists   []struct {
		PlaylistURL string `json:"playlistUrl"`
	} `json:"streamingPlaylists"`
}

type pgListResp struct {
	Total int        `json:"total"`
	Data  []*pgVideo `json:"data"`
}

func pgGet(path string, out interface{}) error {
	body, code, _, err := httpGet(pgBase+path, pgHeaders())
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("pigav http %d", code)
	}
	return json.Unmarshal(body, out)
}

func pgDrama(slot string, v *pgVideo, catName string) map[string]interface{} {
	remark := ""
	if v.Duration > 0 {
		remark = pkDur(v.Duration)
	}
	name := catName
	if name == "" {
		name = v.Category.Label
	}
	return makeDrama(slot, v.UUID, cleanText(v.Name), v.Thumbnail, remark, name, cleanText(v.Description))
}

var pgCats = []struct{ id, name, sort string }{
	{"latest", "最新", "-publishedAt"},
	{"views", "最多觀看", "-views"},
	{"likes", "最多喜歡", "-likes"},
}

func pgSortOf(cat string) string {
	for _, c := range pgCats {
		if c.id == cat {
			return c.sort
		}
	}
	return "-publishedAt"
}

func pgImpl() *siteImpl {
	impl := &siteImpl{
		Key: "pigav", Name: "PIGAV", DefaultCat: "latest", HomeURL: pgBase + "/",
		Headers: pgHeaders,
	}
	impl.Categories = func(slot string) []interface{} {
		out := []interface{}{}
		for _, c := range pgCats {
			out = append(out, categoryObj(c.id, c.name))
		}
		return out
	}
	impl.Catalog = func(slot, category string, page int) ([]interface{}, int, bool, error) {
		if page < 1 {
			page = 1
		}
		var resp pgListResp
		path := fmt.Sprintf("/api/v1/videos?start=%d&count=30&sort=%s", (page-1)*30, url.QueryEscape(pgSortOf(category)))
		if err := pgGet(path, &resp); err != nil {
			return nil, 0, false, err
		}
		items := []interface{}{}
		for _, v := range resp.Data {
			if v.UUID != "" {
				items = append(items, pgDrama(slot, v, ""))
			}
		}
		return items, resp.Total, page*30 < resp.Total, nil
	}
	impl.Detail = func(slot, rawID string) (map[string]interface{}, error) {
		var v pgVideo
		if err := pgGet("/api/v1/videos/"+url.PathEscape(rawID), &v); err != nil {
			return nil, err
		}
		if v.UUID == "" {
			return nil, fmt.Errorf("pigav detail empty")
		}
		return pgDrama(slot, &v, ""), nil
	}
	impl.Resolve = func(slot, rawID string) (map[string]interface{}, error) {
		var v pgVideo
		if err := pgGet("/api/v1/videos/"+url.PathEscape(rawID), &v); err != nil {
			return nil, err
		}
		for _, p := range v.Playlists {
			if p.PlaylistURL != "" {
				return playResult(p.PlaylistURL, slot, rawID, pgUA, pgBase+"/", "m3u8", "hls"), nil
			}
		}
		return nil, fmt.Errorf("pigav no playlist")
	}
	impl.Search = func(slot, kw string) ([]interface{}, error) {
		var resp pgListResp
		path := "/api/v1/search/videos?search=" + url.QueryEscape(kw) + "&start=0&count=30"
		if err := pgGet(path, &resp); err != nil {
			return []interface{}{}, err
		}
		items := []interface{}{}
		for _, v := range resp.Data {
			if v.UUID != "" {
				items = append(items, pgDrama(slot, v, "搜索"))
			}
		}
		return items, nil
	}
	return impl
}

func init() {
	registerSite("pigav", pgImpl(), "pigav")
}
