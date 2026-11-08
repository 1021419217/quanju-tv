package seku

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

type StreamHook interface {
	SignSegment(rawURL string) (string, error)
	RefreshPlaylist(failedURL string) (string, bool)
	ShortRefetch() bool
}

type hookCacheEntry struct {
	hook StreamHook
	at   time.Time
}

var streamHookCache = &struct {
	mu sync.Mutex
	m  map[string]hookCacheEntry
}{m: map[string]hookCacheEntry{}}

func StoreStreamHook(playlistURL string, hook StreamHook) {
	if playlistURL == "" || hook == nil {
		return
	}
	streamHookCache.mu.Lock()
	defer streamHookCache.mu.Unlock()
	now := time.Now()
	for key, entry := range streamHookCache.m {
		if now.Sub(entry.at) > 15*time.Minute {
			delete(streamHookCache.m, key)
		}
	}
	if len(streamHookCache.m) >= 64 {
		oldest := ""
		var oldestAt time.Time
		for key, entry := range streamHookCache.m {
			if oldest == "" || entry.at.Before(oldestAt) {
				oldest, oldestAt = key, entry.at
			}
		}
		delete(streamHookCache.m, oldest)
	}
	streamHookCache.m[playlistURL] = hookCacheEntry{hook: hook, at: now}
}

func HookForPlaylist(playlistURL string) StreamHook {
	if playlistURL == "" {
		return nil
	}
	streamHookCache.mu.Lock()
	defer streamHookCache.mu.Unlock()
	entry, ok := streamHookCache.m[playlistURL]
	if !ok || time.Since(entry.at) > 15*time.Minute {
		return nil
	}
	return entry.hook
}

func probePlaylist(u, referer string) bool {
	body, code, _, err := httpGet(u, map[string]string{
		"User-Agent": z2UA,
		"Referer":    referer,
	})
	if err != nil || code != 200 {
		return false
	}
	return strings.HasPrefix(strings.TrimSpace(string(body)), "#EXTM3U")
}

type jrStreamHook struct {
	detail        string
	sid           string
	mid           string
	guid          string
	sec           string
	origin        string
	origToken     string
	origExpires   int64
	origTokenPath string
}

func (h *jrStreamHook) SignSegment(rawURL string) (string, error) {
	token, expires, tokenPath, err := jrFreshTokenParts(h.mid, h.sid, h.guid, h.sec)
	if err == nil {
		return jrMkURL(h.origin, h.guid, rawURL, token, expires, tokenPath), nil
	}
	if h.origToken != "" {
		return jrMkURL(h.origin, h.guid, rawURL, h.origToken, h.origExpires, h.origTokenPath), nil
	}
	return "", err
}

func (h *jrStreamHook) RefreshPlaylist(string) (string, bool) {
	if h.detail == "" {
		return "", false
	}
	u, _, err := jrResolveSigned(h.detail)
	if err != nil || u == "" {
		return "", false
	}
	return u, true
}

func (h *jrStreamHook) ShortRefetch() bool {
	return false
}

type jrFallbackHook struct {
	detail string
	query  string
}

func (h *jrFallbackHook) SignSegment(rawURL string) (string, error) {
	if h.query != "" && !strings.Contains(rawURL, "token=") {
		sep := "?"
		if strings.Contains(rawURL, "?") {
			sep = "&"
		}
		return rawURL + sep + h.query, nil
	}
	return rawURL, nil
}

func (h *jrFallbackHook) RefreshPlaylist(string) (string, bool) {
	if h.detail == "" {
		return "", false
	}
	u, _, err := jrResolveSigned(h.detail)
	if err != nil || u == "" {
		return "", false
	}
	return u, true
}

func (h *jrFallbackHook) ShortRefetch() bool {
	return false
}

func jrMkURL(origin, guid, rawURL, token string, expires int64, tokenPath string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	path := strings.TrimPrefix(parsed.Path, "/")
	if guid != "" && !strings.HasPrefix(path, guid+"/") {
		path = guid + "/" + path
	}
	out := strings.TrimSuffix(origin, "/") + "/" + path
	out += "?token=" + url.QueryEscape(token) + "&expires=" + fmt.Sprintf("%d", expires)
	if tokenPath != "" {
		out += "&token_path=" + url.QueryEscape(tokenPath)
	}
	return out
}

type av321StreamHook struct {
	mu      sync.Mutex
	slug    string
	referer string
	tried   map[string]bool
}

func (h *av321StreamHook) SignSegment(rawURL string) (string, error) {
	return rawURL, nil
}

func (h *av321StreamHook) RefreshPlaylist(failedURL string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tried == nil {
		h.tried = map[string]bool{}
	}
	h.tried[failedURL] = true
	candidates, err := z2Av321GetVideo(h.slug)
	if err != nil {
		return "", false
	}
	for _, candidate := range candidates {
		if h.tried[candidate] {
			continue
		}
		h.tried[candidate] = true
		if probePlaylist(candidate, h.referer) {
			return candidate, true
		}
	}
	return "", false
}

func (h *av321StreamHook) ShortRefetch() bool {
	return true
}

type mmtvStreamHook struct {
	mu      sync.Mutex
	embed   string
	referer string
	backups []string
	tried   map[string]bool
}

func (h *mmtvStreamHook) SignSegment(rawURL string) (string, error) {
	return rawURL, nil
}

func (h *mmtvStreamHook) RefreshPlaylist(failedURL string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.tried == nil {
		h.tried = map[string]bool{}
	}
	h.tried[failedURL] = true
	for _, backup := range h.backups {
		if h.tried[backup] {
			continue
		}
		h.tried[backup] = true
		if probePlaylist(backup, h.referer) {
			return backup, true
		}
	}
	page, err := mmFetchEmbed(h.embed, h.referer)
	if err != nil {
		return "", false
	}
	main, backup := mmStreamsFromPage(page)
	for _, candidate := range []string{main, backup} {
		if candidate == "" || h.tried[candidate] {
			continue
		}
		h.tried[candidate] = true
		if probePlaylist(candidate, h.referer) {
			return candidate, true
		}
	}
	return "", false
}

func (h *mmtvStreamHook) ShortRefetch() bool {
	return false
}
