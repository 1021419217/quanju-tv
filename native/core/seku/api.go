package seku

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Adapter API consumed by the host core (package core). All slot ids
// here are the legacy quanju slot ids the providers registered under;
// the host maps its public source ids to these slots and rewrites the
// ids inside returned payloads. Payloads keep the map shapes the
// providers have always produced (drama maps from makeDrama, chapter
// maps, play-result maps) — conversion to host types is the host
// adapter's job.

// Slots returns the registered legacy slot ids in presentation order.
func Slots() []string { return allSlots() }

// HasSlot reports whether a legacy slot is registered.
func HasSlot(slot string) bool { return implForSlot(slot) != nil }

// SiteName returns the provider's content name (may carry a 🔞 prefix).
func SiteName(slot string) string {
	if impl := implForSlot(slot); impl != nil {
		return impl.Name
	}
	return ""
}

// SiteKey returns the provider's internal key, e.g. "mirror".
func SiteKey(slot string) string {
	if impl := implForSlot(slot); impl != nil {
		return impl.Key
	}
	return ""
}

// DefaultCategory returns the slot's default category id.
func DefaultCategory(slot string) string {
	if impl := implForSlot(slot); impl != nil {
		return impl.DefaultCat
	}
	return ""
}

// Category is one entry of a provider category list.
type Category struct {
	ID   string
	Name string
}

// Categories returns the slot's category list without the legacy
// "__all" marker entry (the host shows real site names, so the marker
// is unnecessary) and without duplicates.
func Categories(slot string) []Category {
	out := []Category{}
	seen := map[string]bool{}
	for _, c := range providerCategories(slot) {
		m, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if id == "" || id == markerCatID || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Category{ID: id, Name: name})
	}
	return out
}

func toMaps(items []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]interface{}); ok {
			out = append(out, m)
		}
	}
	return out
}

func rawIDOf(item map[string]interface{}) string {
	if s, ok := item["vod_id"].(string); ok && s != "" {
		return s
	}
	if s, ok := item["id"].(string); ok {
		if _, raw, found := strings.Cut(s, ":"); found {
			return raw
		}
	}
	return ""
}

func annotateKind(item map[string]interface{}) {
	raw := rawIDOf(item)
	switch {
	case strings.HasPrefix(raw, "idx:"), strings.HasPrefix(raw, "dir:"), strings.HasPrefix(raw, "tag:"):
		item["kind"] = "folder"
		item["folderId"] = raw
	default:
		item["kind"] = "video"
	}
}

// Catalog fetches one live catalog page. A returned page is also
// merged into the local store, mirroring the standalone core's dual
// mode (several providers read back their own detail data through
// the store).
func Catalog(slot, category string, page int) ([]map[string]interface{}, int, bool, error) {
	items, total, hasMore, err := providerCatalog(slot, category, page)
	if err != nil {
		return nil, 0, false, err
	}
	if len(items) > 0 {
		st := getStore(slot)
		storeMu.Lock()
		st.mergePage(category, page, items, hasMore)
		saveStore(st)
		storeMu.Unlock()
	}
	out := toMaps(items)
	for _, item := range out {
		annotateKind(item)
	}
	return out, total, hasMore, nil
}

// Search runs the slot's own search.
func Search(slot, kw string) ([]map[string]interface{}, error) {
	items, err := providerSearch(slot, kw)
	if err != nil {
		return nil, err
	}
	out := toMaps(items)
	for _, item := range out {
		annotateKind(item)
	}
	return out, nil
}

// Detail returns the provider's drama map (chapters under "chapters").
func Detail(slot, rawID string) (map[string]interface{}, error) {
	d, err := providerDetail(slot, rawID)
	if err != nil {
		return nil, err
	}
	annotateKind(d)
	if _, ok := d["backdrop"]; !ok {
		d["backdrop"] = ""
	}
	if _, ok := d["stills"]; !ok {
		if v, ok := d["screenshots"]; ok {
			d["stills"] = v
		} else {
			d["stills"] = []interface{}{}
		}
	}
	if rel, ok := d["related"].([]interface{}); ok {
		for _, item := range toMaps(rel) {
			annotateKind(item)
		}
	} else if _, ok := d["related"]; !ok {
		d["related"] = []interface{}{}
	}
	return d, nil
}

// Resolve returns the provider's play-result map ("url", "headers", ...).
func Resolve(slot, rawID string) (map[string]interface{}, error) {
	return providerResolve(slot, rawID)
}

// Headers returns the slot's default request headers, if any.
func Headers(slot string) map[string]string { return providerHeaders(slot) }

// Cover fetches a cover image the way the provider prescribes: its
// registered CoverFetch when present (byte-transformed covers),
// otherwise a plain GET carrying the provider's headers.
func Cover(slot, rawURL string) ([]byte, error) {
	impl := implForSlot(slot)
	if impl != nil && impl.CoverFetch != nil {
		return impl.CoverFetch(rawURL)
	}
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range providerHeaders(slot) {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Mozilla/5.0 (Linux; Android 13; Pixel 7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Mobile Safari/537.36")
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: sharedTransport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cover http %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 24<<20))
}

// IsMarkerCategory reports the legacy marker id.
func IsMarkerCategory(id string) bool { return id == markerCatID }

// CleanName strips the adult-tag prefix used in provider names.
func CleanName(name string) string { return strings.TrimPrefix(name, "🔞") }
