package seku

import (
	"fmt"
	"strconv"
	"strings"
)

// ---- senlin 森林资源 (yh_senlin): MacCMS multi-source aggregator ----
//
// One siteImpl aggregating every live MacCMS collection API from the
// senlin module (欲火 §5.3). Each source gets its own mcAPIImpl; the
// aggregator prefixes category ids and vod ids with the source index
// ("<idx>:<raw>") so Catalog/Detail/Resolve can route back to the
// owning source. Search merges all sources, skipping failures.
// Play-group / episode handling stays inside mcAPIImpl (raw ids may
// carry a "#<episodeIndex>" suffix after the source prefix, e.g.
// "0:221817#2").

const yhSenlinUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36"

type yhSenlinSource struct {
	key  string
	name string
	cfg  *mcAPIConfig
	impl *siteImpl
}

var yhSenlinSources []*yhSenlinSource

func yhSenlinPrefix(idx int, raw string) string {
	return fmt.Sprintf("%d:%s", idx, raw)
}

// yhSenlinSplitPrefix parses "<idx>:<rest>"; no valid prefix -> idx 0.
func yhSenlinSplitPrefix(s string) (int, string) {
	if i := strings.Index(s, ":"); i > 0 {
		if n, err := strconv.Atoi(s[:i]); err == nil && n >= 0 && n < len(yhSenlinSources) {
			return n, s[i+1:]
		}
	}
	return 0, s
}

func yhSenlinRewriteItem(slot string, idx int, item interface{}) {
	m, ok := item.(map[string]interface{})
	if !ok {
		return
	}
	raw, _ := m["vod_id"].(string)
	if raw == "" {
		return
	}
	pfx := yhSenlinPrefix(idx, raw)
	m["vod_id"] = pfx
	m["id"] = dramaID(slot, pfx)
	m["dramaId"] = dramaID(slot, pfx)
	for _, key := range []string{"chapters", "episodeList"} {
		chs, ok := m[key].([]interface{})
		if !ok {
			continue
		}
		for _, ch := range chs {
			cm, ok := ch.(map[string]interface{})
			if !ok {
				continue
			}
			if ep, ok := cm["episodeId"].(string); ok && (ep == raw || strings.HasPrefix(ep, raw+"#")) {
				pep := yhSenlinPrefix(idx, ep)
				cm["episodeId"] = pep
				cm["id"] = dramaID(slot, pep)
				cm["dramaId"] = dramaID(slot, pfx)
			}
		}
	}
}

func yhSenlinImpl() *siteImpl {
	impl := &siteImpl{
		Key: "yh_senlin", Name: "森林资源", DefaultCat: "",
		HomeURL: yhSenlinSources[0].cfg.Base + "/",
		Headers: yhSenlinSources[0].cfg.headers,
	}
	impl.Categories = func(slot string) []interface{} {
		out := []interface{}{}
		for idx, s := range yhSenlinSources {
			for _, c := range s.impl.Categories(slot) {
				cm, ok := c.(map[string]interface{})
				if !ok {
					continue
				}
				tid, _ := cm["id"].(string)
				if tid == "" {
					continue
				}
				nc := categoryObj(yhSenlinPrefix(idx, tid), stringOr(cm["name"]))
				out = append(out, nc)
			}
		}
		return out
	}
	impl.Catalog = func(slot, category string, page int) ([]interface{}, int, bool, error) {
		idx, tid := yhSenlinSplitPrefix(category)
		if category == "" {
			idx, tid = 0, ""
		}
		items, total, hasMore, err := yhSenlinSources[idx].impl.Catalog(slot, tid, page)
		if err != nil {
			return nil, 0, false, err
		}
		for _, it := range items {
			yhSenlinRewriteItem(slot, idx, it)
		}
		return items, total, hasMore, nil
	}
	impl.Detail = func(slot, rawID string) (map[string]interface{}, error) {
		idx, id := yhSenlinSplitPrefix(rawID)
		d, err := yhSenlinSources[idx].impl.Detail(slot, id)
		if err != nil {
			return nil, err
		}
		yhSenlinRewriteItem(slot, idx, d)
		return d, nil
	}
	impl.Resolve = func(slot, rawID string) (map[string]interface{}, error) {
		idx, id := yhSenlinSplitPrefix(rawID)
		return yhSenlinSources[idx].impl.Resolve(slot, id)
	}
	impl.Search = func(slot, kw string) ([]interface{}, error) {
		out := []interface{}{}
		var lastErr error
		for idx, s := range yhSenlinSources {
			items, err := s.impl.Search(slot, kw)
			if err != nil {
				lastErr = err
				continue
			}
			for _, it := range items {
				yhSenlinRewriteItem(slot, idx, it)
				out = append(out, it)
			}
		}
		if len(out) == 0 && lastErr != nil {
			return out, lastErr
		}
		return out, nil
	}
	return impl
}

func stringOr(v interface{}) string {
	s, _ := v.(string)
	return s
}

func init() {
	defs := []struct{ key, name, base string }{
		{"guangsu", "光速", "https://api.guangsuapi.com"},
		{"liangzi", "量子", "https://cj.lziapi.com"},
		{"baofeng", "暴风", "https://bfzyapi.com"},
		{"douban", "豆瓣", "https://dbzy.tv"},
		{"ikun", "iKun", "https://ikunzyapi.com"},
		{"maotai", "茅台", "https://mtzy.me"},
	}
	yhSenlinSources = nil
	for _, d := range defs {
		cfg := &mcAPIConfig{Base: d.base, UA: yhSenlinUA}
		yhSenlinSources = append(yhSenlinSources, &yhSenlinSource{
			key: d.key, name: d.name, cfg: cfg,
			impl: mcAPIImpl("yh_senlin_"+d.key, d.name, cfg),
		})
	}
	registerSite("yh_senlin", yhSenlinImpl(), "senlin")
}
