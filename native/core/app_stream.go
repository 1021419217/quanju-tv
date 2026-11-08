package core

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"duanjuapp/native/core/seku"
)

type nativeStreamAsset struct {
	kind        string
	address     string
	data        []byte
	contentType string
	total       int64
	etag        string
	modified    string
}

type nativeStreamSession struct {
	credentials  *providerMediaCredentials
	mu           sync.Mutex
	assets       map[string]nativeStreamAsset
	referer      string
	key          []byte
	ctx          context.Context
	cancel       context.CancelFunc
	lastUsed     time.Time
	diagnosticID string
	adBlock      bool
	// resilient 在打开会话时定死：上游会掐长连接/中途停发，中转要走续传与看门狗。
	resilient bool
	hook      seku.StreamHook
	// fallbacks 是同一部片的备用直链（来自 providerMedia.Variants）。
	// 主链被 CDN 掐死后按序换，换成功就记住，后续请求都走这条。
	fallbacks []string
	// reload 让站源重新解析一次播放地址：91porn 的直链带时间戳签名，
	// 播到一半签名过期时只能重新取一条，靠它接着播下去。
	reload func(context.Context) (string, error)
	// fallbackCursor 记录备用直链已经试到第几条，避免反复撞同一条死链。
	fallbackCursor int
	// reloads 是一次播放里已经重新解析过几次，防止详情页被反复打。
	reloads int
}

// relayCandidates 取下一批可以接手的备用地址（每条只给一次）。
func (session *nativeStreamSession) relayCandidates(current string) []string {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.fallbackCursor >= len(session.fallbacks) {
		return nil
	}
	candidates := make([]string, 0, len(session.fallbacks))
	for session.fallbackCursor < len(session.fallbacks) {
		address := session.fallbacks[session.fallbackCursor]
		session.fallbackCursor++
		if address != "" && address != current {
			candidates = append(candidates, address)
		}
	}
	return candidates
}

// promoteAddress 把换成功的新地址写回会话，后续 Range 请求直接走它。
func (session *nativeStreamSession) promoteAddress(assetKey, address string) {
	if assetKey == "" || address == "" {
		return
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	asset, found := session.assets[assetKey]
	if !found {
		return
	}
	asset.address = address
	session.assets[assetKey] = asset
}

// enableInsecureTLS 把整个会话的媒体传输切到「跳过证书校验」并返回新凭据。
// 只在真的遇到证书错误时调用一次：站方 CDN 缺中间证书，标准校验必失败但流可播。
func (session *nativeStreamSession) enableInsecureTLS() *providerMediaCredentials {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.credentials == nil || session.credentials.insecureTLS {
		return nil
	}
	clone := *session.credentials
	clone.insecureTLS = true
	session.credentials = &clone
	return &clone
}

// nativeCertificateError 判断错误是不是证书校验失败（中转层通用入口）。
func nativeCertificateError(err error) bool {
	return p91CertificateError(err)
}

// reloadAddress 让站源重新解析一条播放地址；没实现 Reload 的源返回空。
func (session *nativeStreamSession) reloadAddress(ctx context.Context) (string, error) {
	session.mu.Lock()
	reload := session.reload
	if reload == nil || session.reloads >= nativeRelayMaxReloads {
		session.mu.Unlock()
		return "", nil
	}
	session.reloads++
	session.mu.Unlock()
	reloadCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return reload(reloadCtx)
}

type nativeStreamServer struct {
	mu         sync.Mutex
	downloader *Downloader
	address    string
	sessions   map[string]*nativeStreamSession
	server     *http.Server

	// 投屏用：同一个 handler 再挂一份到 0.0.0.0，让局域网里的电视 / 投影仪
	// 按本机内网地址拉流。只在用户真的投屏时打开，停止投屏就关掉。
	castServer   *http.Server
	castListener net.Listener
	castBase     string
}

func (stream *nativeStreamServer) nativeRequest(request *http.Request) (*http.Response, error) {
	client := *stream.downloader.client
	client.Timeout = 0
	info, _ := request.Context().Value(nativeStreamDiagnosticKey{}).(nativeStreamDiagnostic)
	response, err := stream.downloader.doMediaRequestWithClient(request, &client)
	if err != nil {
		stream.nativeLogError(info, "upstream_error", request.URL.String(), err, "")
		return response, err
	}
	address := request.URL.String()
	if response.Request != nil && response.Request.URL != nil {
		address = response.Request.URL.String()
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		stream.nativeLog(info, "upstream_status", address, "", response.StatusCode, response.Header.Get("Content-Type"))
	}
	response.Body = &nativeDiagnosticBody{ReadCloser: response.Body, stream: stream, info: info, address: address}
	return response, nil
}

var nativePlaylistURI = regexp.MustCompile(`URI="([^"]+)"`)

// nativeRelayFallbackBudget 是一条地址失败后，在备用直链上最多花多少时间。
// 每条死线都要等到 12 秒响应头超时，几条串起来就是半分钟的转圈；宁可早点
// 承认这批地址不可用，把时间留给「重新解析」。
const nativeRelayFallbackBudget = 6 * time.Second

// nativeHTMLResponse 判断上游是不是把网页当成媒体返回了。
func nativeHTMLResponse(response *http.Response) bool {
	if response == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(response.Header.Get("Content-Type")), "text/html")
}

// nativeUpstreamStatus 表示上游用状态码明确拒绝（403 多半是签名过期）。
type nativeUpstreamStatus struct{ status int }

func (err *nativeUpstreamStatus) Error() string {
	return fmt.Sprintf("上游返回 HTTP %d", err.status)
}

// nativeFetchWithFallback 在主链连不上或被站方拒绝时依次试备用直链，
// 再让站源重新解析一次（只有实现了 Reload 的源才有这一步）。
// 只返回真正能用的那条；都不行时返回最后一次错误。
func (stream *nativeStreamServer) nativeFetchWithFallback(ctx context.Context, request *http.Request, session *nativeStreamSession, address string) (*http.Response, *http.Request, string, error) {
	attempt := func(attemptCtx context.Context, target string) (*http.Response, *http.Request, error) {
		upstream, err := http.NewRequestWithContext(attemptCtx, request.Method, target, nil)
		if err != nil {
			return nil, nil, err
		}
		upstream.Header.Set("User-Agent", userAgent)
		if session.referer != "" {
			upstream.Header.Set("Referer", session.referer)
		}
		// 91 的片源 CDN 只认「像播放器」的请求：除了 Referer，还要 Accept
		// 与 identity 编码（实测不带 Referer 直接掐连接，返回 http=000）。
		upstream.Header.Set("Accept", "*/*")
		upstream.Header.Set("Accept-Encoding", "identity")
		for _, name := range []string{"Range", "If-Range"} {
			if value := request.Header.Get(name); value != "" {
				upstream.Header.Set(name, value)
			}
		}
		response, err := stream.nativeRequest(upstream)
		if err != nil {
			return nil, upstream, err
		}
		if response.StatusCode >= 400 {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
			response.Body.Close()
			return nil, upstream, &nativeUpstreamStatus{status: response.StatusCode}
		}
		// 签名过期时 91 的 CDN 先 302、跟随后给一个 200 的 HTML。客户端默认
		// 跟随重定向，所以这里看到的是「200 + text/html」。把它当成功透传给
		// 播放器，播放器只会报解码失败，也永远不会触发换线/重解析。
		if nativeHTMLResponse(response) {
			_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 32<<10))
			response.Body.Close()
			return nil, upstream, &nativeUpstreamStatus{status: http.StatusBadGateway}
		}
		return response, upstream, nil
	}
	var lastErr error
	// 备用直链和主链是同一份详情页里一起解析出来的：签名一起过期，也常被
	// 同一个 CDN 节点一起拒。连续两条明确拒绝就没必要把剩下的都试一遍。
	denied := 0
	budget, cancelBudget := context.WithTimeout(ctx, nativeRelayFallbackBudget)
	defer cancelBudget()
	for _, candidate := range session.relayCandidates(address) {
		response, upstream, err := attempt(budget, candidate)
		if err == nil {
			return response, upstream, candidate, nil
		}
		lastErr = err
		if status, ok := err.(*nativeUpstreamStatus); ok && (status.status == http.StatusForbidden || status.status == http.StatusUnauthorized || status.status == http.StatusNotFound) {
			denied++
			if denied >= 2 {
				break
			}
		}
		if budget.Err() != nil {
			break
		}
	}
	// 重新解析用原始 ctx：上面那点预算是给「试旧地址」的，不能连累这一步。
	if reissued, err := session.reloadAddress(ctx); err == nil && reissued != "" && reissued != address {
		response, upstream, retryErr := attempt(ctx, reissued)
		if retryErr == nil {
			return response, upstream, reissued, nil
		}
		lastErr = retryErr
	}
	if lastErr == nil {
		lastErr = errors.New("没有可用的备用线路")
	}
	return nil, nil, "", lastErr
}

func newNativeStreamServer(d *Downloader) (*nativeStreamServer, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, errors.New("无法初始化本机播放器")
	}
	stream := &nativeStreamServer{downloader: d, address: "http://" + listener.Addr().String(), sessions: map[string]*nativeStreamSession{}}
	server := &http.Server{Handler: http.HandlerFunc(stream.nativeServe), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384}
	stream.server = server
	go func() { _ = server.Serve(listener) }()
	return stream, nil
}

func (stream *nativeStreamServer) nativeOpen(media providerMedia, adBlock bool) (string, string) {
	tokenBytes := make([]byte, 24)
	if _, err := rand.Read(tokenBytes); err != nil {
		panic(err)
	}
	token := hex.EncodeToString(tokenBytes)
	ctx, cancel := context.WithCancel(providerMediaContext(context.Background(), media.credentials))
	session := &nativeStreamSession{assets: map[string]nativeStreamAsset{}, referer: media.Referer, key: media.HLSKey, ctx: ctx, cancel: cancel, lastUsed: time.Now(), credentials: media.credentials, diagnosticID: nativeStreamID(token), adBlock: adBlock, hook: media.Hook, reload: media.Reload}
	if media.credentials != nil {
		session.resilient = media.credentials.resilient
	}
	for _, variant := range media.Variants {
		if address := strings.TrimSpace(variant.URL); address != "" && address != media.URL {
			session.fallbacks = append(session.fallbacks, address)
		}
	}
	stream.mu.Lock()
	for id, old := range stream.sessions {
		if time.Since(old.lastUsed) > 10*time.Minute {
			stream.nativeCancel(old, "idle_expired")
			delete(stream.sessions, id)
		}
	}
	if len(stream.sessions) >= 8 {
		oldest := ""
		for id, old := range stream.sessions {
			if oldest == "" || old.lastUsed.Before(stream.sessions[oldest].lastUsed) {
				oldest = id
			}
		}
		stream.nativeCancel(stream.sessions[oldest], "capacity_eviction")
		delete(stream.sessions, oldest)
	}
	stream.sessions[token] = session
	stream.mu.Unlock()
	entry := nativeStreamAsset{kind: "media", address: media.URL, contentType: "video/mp4"}
	parsedURL, parseErr := url.Parse(media.URL)
	isHLS := media.Playlist != "" || (parseErr == nil && strings.HasSuffix(strings.ToLower(parsedURL.Path), ".m3u8"))
	if isHLS {
		entry.contentType = "application/vnd.apple.mpegurl"
	}
	if media.Playlist != "" {
		entry.data = []byte(media.Playlist)
		entry.contentType = "application/vnd.apple.mpegurl"
	}
	if isHLS || strings.Contains(entry.contentType, "mpegurl") {
		entry.kind = "entry_playlist"
	}
	return stream.nativeAsset(token, session, entry), token
}

func (stream *nativeStreamServer) nativeCancel(session *nativeStreamSession, reason string) {
	if reason != "release" {
		stream.nativeLog(nativeStreamDiagnostic{session: session.diagnosticID}, "session_cancel", "", "reason="+reason, 0, "")
	}
	session.cancel()
}

func (stream *nativeStreamServer) nativeRelease(token string) {
	stream.mu.Lock()
	if session := stream.sessions[token]; session != nil {
		stream.nativeCancel(session, "release")
		delete(stream.sessions, token)
	}
	stream.mu.Unlock()
}

func (stream *nativeStreamServer) nativeAsset(token string, session *nativeStreamSession, asset nativeStreamAsset) string {
	return stream.nativeAssetAt(stream.address, token, session, asset)
}

// nativeAssetAt 用调用方给的基址生成资源地址。本机播放走 127.0.0.1，
// 投屏时必须换成「手机内网 IP + 投屏端口」，否则电视拿到的是它自己的
// localhost；同一个会话因此在两个监听上共用资源表，只有前缀不同。
func (stream *nativeStreamServer) nativeAssetAt(base, token string, session *nativeStreamSession, asset nativeStreamAsset) string {
	digest := sha256.Sum256([]byte(asset.address + "\x00" + asset.contentType))
	extension := ".ts"
	if parsed, err := url.Parse(asset.address); err == nil {
		switch candidate := strings.ToLower(path.Ext(parsed.Path)); candidate {
		case ".m3u8", ".ts", ".m4s", ".mp4", ".aac", ".m4a", ".mp3", ".vtt", ".webvtt", ".key":
			extension = candidate
		}
	}
	switch asset.contentType {
	case "application/vnd.apple.mpegurl":
		extension = ".m3u8"
	case "application/octet-stream":
		extension = ".key"
	case "video/mp4":
		extension = ".mp4"
	}
	id := hex.EncodeToString(digest[:12]) + extension
	session.mu.Lock()
	if previous, found := session.assets[id]; !found || len(previous.data) == 0 || len(asset.data) > 0 {
		session.assets[id] = asset
	}
	session.mu.Unlock()
	return base + "/" + token + "/" + id
}

func (stream *nativeStreamServer) nativeRewrite(token string, session *nativeStreamSession, body, base string) (string, error) {
	return stream.nativeRewriteAt(stream.address, token, session, body, base)
}

// nativeRewriteAt 的 publicBase 决定播放列表里写回的绝对地址前缀：本机播放是
// 127.0.0.1，投屏请求进来时用请求的 Host（内网 IP），两种客户端各拿各的。
func (stream *nativeStreamServer) nativeRewriteAt(publicBase, token string, session *nativeStreamSession, body, base string) (string, error) {
	var output []string
	nextPlaylist := false
	for _, line := range strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(line)
		rewrite := func(reference string, key bool, contentType string) string {
			if strings.HasPrefix(reference, "data:") {
				return reference
			}
			parsed, err := url.Parse(base)
			if err != nil {
				return ""
			}
			relative, err := url.Parse(reference)
			if err != nil {
				return ""
			}
			address := parsed.ResolveReference(relative).String()
			if !isProviderHTTPMediaURL(address) {
				return ""
			}
			kind := "segment"
			switch contentType {
			case "application/vnd.apple.mpegurl":
				kind = "child_playlist"
			case "application/octet-stream":
				kind = "key"
			case "video/mp4":
				kind = "map"
			}
			asset := nativeStreamAsset{kind: kind, address: address, contentType: contentType}
			if key && len(session.key) == 16 {
				asset.data = append([]byte{}, session.key...)
				asset.contentType = "application/octet-stream"
			}
			return stream.nativeAssetAt(publicBase, token, session, asset)
		}
		if text != "" && !strings.HasPrefix(text, "#") {
			contentType := ""
			if nextPlaylist {
				contentType = "application/vnd.apple.mpegurl"
			}
			line = rewrite(text, false, contentType)
			nextPlaylist = false
			if line == "" {
				return "", errors.New("播放列表中的媒体地址无效")
			}
		} else if strings.Contains(text, "URI=") {
			invalid := false
			line = nativePlaylistURI.ReplaceAllStringFunc(line, func(match string) string {
				reference := nativePlaylistURI.FindStringSubmatch(match)[1]
				key := strings.HasPrefix(text, "#EXT-X-KEY:") || strings.HasPrefix(text, "#EXT-X-SESSION-KEY:")
				contentType := ""
				switch {
				case key:
					contentType = "application/octet-stream"
				case strings.HasPrefix(text, "#EXT-X-MAP:"):
					contentType = "video/mp4"
				case strings.HasPrefix(text, "#EXT-X-MEDIA:"), strings.HasPrefix(text, "#EXT-X-I-FRAME-STREAM-INF:"), strings.HasPrefix(text, "#EXT-X-RENDITION-REPORT:"):
					contentType = "application/vnd.apple.mpegurl"
				}
				updated := rewrite(reference, key, contentType)
				if updated == "" {
					invalid = true
				}
				return "URI=\"" + updated + "\""
			})
			if invalid {
				return "", errors.New("播放列表中的附属地址无效")
			}
		}
		if strings.HasPrefix(text, "#EXT-X-STREAM-INF:") {
			nextPlaylist = true
		}
		output = append(output, line)
	}
	return strings.Join(output, "\n"), nil
}

func (stream *nativeStreamServer) nativeServe(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
	if len(parts) != 2 {
		http.NotFound(writer, request)
		return
	}
	stream.mu.Lock()
	session := stream.sessions[parts[0]]
	if session != nil {
		session.lastUsed = time.Now()
	}
	stream.mu.Unlock()
	if session == nil {
		http.Error(writer, "播放已结束", http.StatusGone)
		return
	}
	session.mu.Lock()
	asset, found := session.assets[parts[1]]
	session.mu.Unlock()
	info := nativeStreamDiagnostic{session: session.diagnosticID, asset: asset.kind}
	if !found {
		stream.nativeLog(info, "asset_missing", "", "", 0, "")
		http.NotFound(writer, request)
		return
	}
	ctx, cancel := context.WithCancel(providerMediaContext(request.Context(), session.credentials))
	// 证书链不完整时会把 ctx 换成「宽松传输」的版本，所以 cancel/stop 要用闭包延迟取最新值。
	defer func() { cancel() }()
	ctx = context.WithValue(ctx, nativeStreamDiagnosticKey{}, info)
	stop := context.AfterFunc(session.ctx, cancel)
	defer func() { stop() }()
	writer.Header().Set("Cache-Control", "no-store")
	// 播放列表里写回的绝对地址跟着请求来源走：本机播放是 127.0.0.1，投屏时
	// 电视按内网地址请求，只有用请求 Host 才能让两边都拉到同一份资源。
	publicBase := stream.address
	if host := request.Host; host != "" {
		publicBase = "http://" + host
	}
	if len(asset.data) > 0 && asset.total > int64(len(asset.data)) {
		if stream.nativeServePrefix(ctx, writer, request, session, asset) {
			return
		}
		asset.data = nil
	}
	if len(asset.data) > 0 {
		if strings.Contains(asset.contentType, "mpegurl") {
			body, err := stream.nativeRewriteAt(publicBase, parts[0], session, nativeAdCleaned(session, string(asset.data), asset.address), asset.address)
			if err != nil {
				stream.nativeLog(info, "playlist_rewrite", asset.address, "kind="+nativePlaylistKind(string(asset.data))+" error="+nativeStreamError(err), 0, "")
				http.Error(writer, err.Error(), http.StatusBadGateway)
				return
			}
			writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			if request.Method == http.MethodGet {
				_, _ = io.WriteString(writer, body)
			}
		} else {
			writer.Header().Set("Content-Type", asset.contentType)
			if asset.etag != "" {
				writer.Header().Set("ETag", asset.etag)
			}
			http.ServeContent(writer, request, parts[1], time.Time{}, bytes.NewReader(asset.data))
		}
		return
	}
	fetchAddress := asset.address
	if session.hook != nil {
		if signed, err := session.hook.SignSegment(fetchAddress); err == nil {
			fetchAddress = signed
		}
	}
	upstream, err := http.NewRequestWithContext(ctx, request.Method, fetchAddress, nil)
	if err != nil {
		http.Error(writer, "媒体地址无效", http.StatusBadGateway)
		return
	}
	upstream.Header.Set("User-Agent", userAgent)
	upstream.Header.Set("Referer", session.referer)
	for _, name := range []string{"Range", "If-Range"} {
		if value := request.Header.Get(name); value != "" {
			upstream.Header.Set(name, value)
		}
	}
	response, err := stream.nativeRequest(upstream)
	// 站方 CDN 证书链不完整（缺中间证书）时标准校验必失败，但流本身可播。
	// 3.16~3.19 靠起播前探测来发现这件事；现在不探测了，就在真遇到证书错误时
	// 把整个会话切到宽松传输再试一次 —— 只影响这个会话、只影响这一个源。
	if err != nil && nativeCertificateError(err) {
		if credentials := session.enableInsecureTLS(); credentials != nil {
			stream.nativeLog(info, "tls_fallback", fetchAddress, "证书校验失败，切换宽松传输重试", 0, "")
			retryCtx, retryCancel := context.WithCancel(providerMediaContext(request.Context(), credentials))
			retryCtx = context.WithValue(retryCtx, nativeStreamDiagnosticKey{}, info)
			retryStop := context.AfterFunc(session.ctx, retryCancel)
			retryRequest := upstream.Clone(retryCtx)
			if retryResponse, retryErr := stream.nativeRequest(retryRequest); retryErr == nil {
				cancel()
				stop()
				ctx, cancel, stop = retryCtx, retryCancel, retryStop
				upstream, response, err = retryRequest, retryResponse, nil
			} else {
				retryCancel()
				retryStop()
			}
		}
	}
	// 主链连不上或是被站方明确拒（403 多半是签名过期），给可续传的源换一次地址：
	// 备用直链 → 重新解析。其它站源不满足条件，行为与原来完全一致。
	// 换线（备用直链 → 重新解析）与「续传」是两件事：
	// 续传只对会掐长连接的源有意义，而换线对所有源都必要——详情页给出的
	// 第一条地址经常是死节点，不换就只能一直对着它重试。3.22 把两者绑在
	// resilient 上，关掉续传时把换线也一起关了，直接导致「10 个 9 个播不出」。
	if err != nil || (response != nil && response.StatusCode >= 400) {
		if retry, retryRequest, retryAddress, retryErr := stream.nativeFetchWithFallback(ctx, request, session, fetchAddress); retryErr == nil {
			if err != nil {
				err = nil
			} else {
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
				response.Body.Close()
			}
			response, upstream, fetchAddress = retry, retryRequest, retryAddress
			session.promoteAddress(parts[1], retryAddress)
		}
	}
	if err != nil {
		http.Error(writer, "读取媒体失败，请重试", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		err := stream.downloader.catalogResponseError(upstream, response, body)
		http.Error(writer, err.Error(), response.StatusCode)
		return
	}
	contentType := strings.ToLower(response.Header.Get("Content-Type"))
	finalURL := upstream.URL
	if response.Request != nil && response.Request.URL != nil {
		finalURL = response.Request.URL
	}
	contentIsMP4 := strings.Contains(contentType, "video/mp4")
	playlist := strings.Contains(contentType, "mpegurl") ||
		(strings.HasSuffix(strings.ToLower(finalURL.Path), ".m3u8") && !contentIsMP4)
	var responseBody io.ReadCloser = response.Body
	if session.resilient {
		// CDN 有时不断开、只是不再发数据：十几秒没有新字节就主动断开重连，
		// 否则播放器会一直转圈到超时（用户看到的「缓冲很久最后播不了」）。
		watch := newNativeIdleReader(response.Body)
		defer watch.stop()
		responseBody = watch
	}
	reader := bufio.NewReader(responseBody)
	// Peek 会一直阻塞到上游吐出首字节。慢节点上这几秒就是纯等待，所以明确是
	// mp4 直链（Content-Type 或地址后缀）时直接放行，不做 m3u8 嗅探。
	plainMP4 := contentIsMP4 || strings.HasSuffix(strings.ToLower(finalURL.Path), ".mp4") || strings.HasSuffix(strings.ToLower(finalURL.Path), ".m4v")
	if request.Method == http.MethodGet && !playlist && !plainMP4 {
		// 只读 16 字节判断是不是 m3u8：原来 Peek(512) 在慢速首包时会一直等，
		// 明明已经有数据了却一个字节都不往播放器写。
		peek, _ := reader.Peek(16)
		bodyIsPlaylist := strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(string(peek), "\ufeff")), "#EXTM3U")
		if bodyIsPlaylist {
			playlist = true
		}
	}
	if playlist && request.Method == http.MethodHead {
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		writer.WriteHeader(http.StatusOK)
		return
	}
	if playlist && request.Method == http.MethodGet {
		body, err := io.ReadAll(io.LimitReader(reader, 4<<20+1))
		if err != nil || len(body) > 4<<20 {
			stream.nativeLogError(info, "playlist_read_failed", finalURL.String(), err, contentType)
			http.Error(writer, "播放列表过大或读取失败", http.StatusBadGateway)
			return
		}
		text := nativeAdCleaned(session, strings.TrimSpace(strings.TrimPrefix(string(body), "\ufeff")), finalURL.String())
		if !strings.HasPrefix(text, "#EXTM3U") {
			stream.nativeLog(info, "playlist_invalid", finalURL.String(), "", 0, contentType)
			http.Error(writer, "播放列表无效", http.StatusBadGateway)
			return
		}
		rewritten, err := stream.nativeRewriteAt(publicBase, parts[0], session, text, finalURL.String())
		if err != nil {
			stream.nativeLog(info, "playlist_rewrite", finalURL.String(), "kind="+nativePlaylistKind(text)+" error="+nativeStreamError(err), 0, contentType)
			http.Error(writer, err.Error(), http.StatusBadGateway)
			return
		}
		writer.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		_, _ = io.WriteString(writer, rewritten)
		return
	}
	for _, name := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"} {
		if value := response.Header.Get(name); value != "" {
			writer.Header().Set(name, value)
		}
	}
	writer.WriteHeader(response.StatusCode)
	if request.Method == http.MethodGet {
		// 标记了 resilient 的源（站方 CDN 会掐长连接）走断点续传中转：
		// 上游断开时从断点再拉一段，播放器不会看到「传输中断」。
		if session.resilient {
			stream.nativeRelayBody(ctx, writer, request.Method, upstream, session, parts[1], finalURL.String(), response, reader, info)
			return
		}
		_, _ = io.Copy(writer, reader)
	}
}
