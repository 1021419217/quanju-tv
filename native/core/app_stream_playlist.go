package core

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 一次播放最多刷新几次播放列表。
//
// HLS 源的分片地址带签名（kissav 的 auth_key、部分站的 st/e 参数），有效期
// 通常十几分钟。一部片播到后半段时，整批分片会同时回 403，播放器表现为
// 「突然卡住 / 直接报错」。刷新一次就带上了新签名，不需要重新起播。
const nativeMaxPlaylistRefreshes = 4

// nativeSegmentKey 是分片的身份：只取 host + path，丢掉 query。
// 签名都在 query 里，每次刷新都会变，而 path 稳定不变，正好用来对上号。
func nativeSegmentKey(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host + parsed.Path
}

// nativeSegmentNameKey 只取分片文件名。有些站（好色TV）重取播放列表后
// 连目录都会换（路径里带时间戳），host+path 就对不上了；同一份列表里
// index0.ts / index1.ts 这类名字是稳定的，用它做二级匹配。
func nativeSegmentNameKey(address string) string {
	parsed, err := url.Parse(strings.TrimSpace(address))
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" {
		return ""
	}
	return parts[len(parts)-1]
}

// nativeSegmentAddresses 把一份播放列表摊成「分片身份 → 最新地址」。
func nativeSegmentAddresses(text, base string) map[string]string {
	result := make(map[string]string, 64)
	parsedBase, _ := url.Parse(base)
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		address := trimmed
		if parsedBase != nil {
			if relative, err := url.Parse(trimmed); err == nil {
				address = parsedBase.ResolveReference(relative).String()
			}
		}
		if key := nativeSegmentKey(address); key != "" {
			result[key] = address
		}
	}
	return result
}

// nativeRefreshSegments 重新取一份播放列表，把会话里已有分片的地址换成新的。
//
// 只换地址、不改资源 ID：播放器拿到的本地分片地址自始至终不变，所以播放
// 位置、缓冲都不用重建。返回是否真的换掉了（一个都没换就不值得重试）。
func (stream *nativeStreamServer) nativeRefreshSegments(ctx context.Context, session *nativeStreamSession) bool {
	session.mu.Lock()
	refresh := session.refreshPlaylist
	count := session.playlistRefreshes
	if refresh == nil || count >= nativeMaxPlaylistRefreshes {
		session.mu.Unlock()
		return false
	}
	session.playlistRefreshes++
	session.mu.Unlock()

	refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	text, err := refresh(refreshCtx)
	if err != nil {
		return false
	}
	text = strings.TrimPrefix(strings.TrimSpace(strings.TrimPrefix(text, "\ufeff")), "\ufeff")
	if !strings.HasPrefix(text, "#EXTM3U") {
		return false
	}

	base := ""
	session.mu.Lock()
	for _, asset := range session.assets {
		if asset.kind == "entry_playlist" || asset.kind == "child_playlist" {
			base = asset.address
			break
		}
	}
	session.mu.Unlock()
	fresh := nativeSegmentAddresses(text, base)
	if len(fresh) == 0 {
		return false
	}
	freshByName := make(map[string]string, len(fresh))
	for _, address := range fresh {
		if name := nativeSegmentNameKey(address); name != "" {
			freshByName[name] = address
		}
	}

	replaced := 0
	session.mu.Lock()
	for id, asset := range session.assets {
		if asset.kind != "segment" && asset.kind != "key" && asset.kind != "map" {
			continue
		}
		key := nativeSegmentKey(asset.address)
		address, ok := fresh[key]
		if !ok {
			// 目录变了（好色TV 的时间戳目录）：退到文件名匹配。
			address, ok = freshByName[nativeSegmentNameKey(asset.address)]
		}
		if !ok || address == asset.address {
			continue
		}
		asset.address = address
		session.assets[id] = asset
		replaced++
	}
	session.mu.Unlock()
	if replaced > 0 {
		stream.nativeLog(
			nativeStreamDiagnostic{session: session.diagnosticID},
			"playlist_refresh", "",
			fmt.Sprintf("segments=%d replaced=%d", len(fresh), replaced), 0, "",
		)
	}
	return replaced > 0
}

// nativeRetryAfterRefresh 刷新播放列表后用新地址重发当前分片请求。
// 请求头沿用原请求（UA / Referer / Range / Accept），只换 URL。
func (stream *nativeStreamServer) nativeRetryAfterRefresh(ctx context.Context, upstream *http.Request, session *nativeStreamSession, assetKey string) (*http.Response, *http.Request, error) {
	if !stream.nativeRefreshSegments(ctx, session) {
		return nil, nil, fmt.Errorf("播放列表没有可替换的分片地址")
	}
	session.mu.Lock()
	asset, found := session.assets[assetKey]
	session.mu.Unlock()
	if !found || strings.TrimSpace(asset.address) == "" {
		return nil, nil, fmt.Errorf("刷新后找不到该分片")
	}
	target, err := url.Parse(asset.address)
	if err != nil {
		return nil, nil, err
	}
	retry := upstream.Clone(ctx)
	retry.URL = target
	retry.Host = ""
	response, err := stream.nativeRequest(retry)
	if err != nil {
		return nil, nil, err
	}
	if response.StatusCode >= 400 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
		response.Body.Close()
		return nil, nil, &nativeUpstreamStatus{status: response.StatusCode}
	}
	return response, retry, nil
}
