package core

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const huangguoBrowserProfile = "Chrome 150"

type browserHTTPClient interface {
	Do(*fhttp.Request) (*fhttp.Response, error)
	CloseIdleConnections()
}

type huangguoBrowserTransport struct {
	base      http.RoundTripper
	router    *proxyRouter
	hosts     map[string]bool
	insecure  bool
	record    func(diagnosticEvent)
	mu        sync.Mutex
	clients   map[string]browserHTTPClient
	newClient func(string) (browserHTTPClient, error)
	// custom 是运行期登记的自定义源 host（registerHost）。这类站点形态各异，
	// 浏览器指纹并非总是更优：实测部分站点（如 hanjuds 一类普通 CMS）对 utls
	// 指纹直接返回 403 Cloudflare 验证页，而标准 Go 传输可以正常拿到 200。
	// 因此它们先走标准传输，只有真被验证页拦住（challenged）才升级到浏览器指纹。
	custom   map[string]bool
	upgraded map[string]bool
}

func newHuangguoBrowserTransport(base http.RoundTripper, downloader *Downloader) *huangguoBrowserTransport {
	configured, _ := url.Parse(downloader.providerBaseURL(sourceHuangguoVideo))
	transport := &huangguoBrowserTransport{
		base: base, router: downloader.proxyRouter, hosts: browserTransportHosts(configured.Host),
		insecure: downloader.cfg.InsecureTLS, record: downloader.recordDiagnostic,
		clients: make(map[string]browserHTTPClient), custom: make(map[string]bool),
		upgraded: make(map[string]bool),
	}
	transport.newClient = transport.createClient
	return transport
}

func browserTransportHosts(huangguoVideoHost string) map[string]bool {
	hosts := map[string]bool{"huangguo.video": true}
	if huangguoVideoHost != "" {
		hosts[strings.ToLower(huangguoVideoHost)] = true
	}
	for _, spec := range duanjuProviderCatalog {
		if !spec.Browser {
			continue
		}
		if parsed, err := url.Parse(spec.Base); err == nil && parsed.Host != "" {
			hosts[strings.ToLower(parsed.Host)] = true
		}
	}
	return hosts
}

func (transport *huangguoBrowserTransport) matches(request *http.Request) bool {
	if !browserEligibleRequest(request) {
		return false
	}
	host := strings.ToLower(request.URL.Hostname())
	transport.mu.Lock()
	after := transport.hosts[strings.ToLower(request.URL.Host)] || transport.hosts[host]
	if !after {
		// 自定义源 host 只有在被验证页拦住过之后才用指纹传输。
		after = transport.upgraded[host]
	}
	transport.mu.Unlock()
	if after {
		return true
	}
	if browserSuffixHost(host) {
		return true
	}
	return browserFingerprintReferer(request)
}

// browserFingerprintSuffixes 是「媒体 CDN 按 TLS 指纹拦截」的域名后缀。
//
// 91porn 的片源不在主站，而在 91p52.com 的一批随机子域（v9./ccm./…）上。
// 站方对普通 Go/curl 的 ClientHello 直接 RST，浏览器却能秒开——和 theporny
// 是同一类反爬（按 JA3/JA4 拒绝），所以这里也交给 Chrome 150 指纹客户端。
// 主站 91porn.com 不在列表里：它现在能出列表，换指纹反而可能触发验证页。
var browserFingerprintSuffixes = []string{"91p52.com"}

// browserFingerprintReferers 是「片源域名会换、但播放页固定」的站点。
//
// 91porn 的直链早期落在 *.91p52.com，2026 年起换到 la.btc620.com、ccm.91p52.com
// 等一批镜像；九色（91porny.com）的片源在 cdn2.jiuse3.cloud。这些 CDN 全按
// ClientHello 指纹放行：普通 Go 客户端要么被 RST，要么收到 200 的占位内容
// （九色是单分片 6 秒 + __vnotice=1002）。域名一批批换，硬编码后缀永远追不上，
// 所以改成按 Referer 认站：只要请求来自这两个播放页，媒体请求一律走 Chrome 指纹。
//
// 主站自身（91porn.com / 91porny.com 的 HTML 页面）不在此列——它们用标准传输
// 就能出页面，换指纹反而可能触发验证页。
// 91porn 主站不在这个列表里。2026-11-09 用 curl（标准 TLS，非浏览器指纹）
// 请求 la.btc620.com 的直链，带 Referer 拿到 206；而 tls-client 的 HTTP/2
// 对同一个 CDN 会吃 PROTOCOL_ERROR。所以 91 的媒体请求交给标准客户端，
// 和浏览器外那条能跑通的链路保持一致；旧域名 *.91p52.com 仍按后缀走指纹。
var browserFingerprintReferers = []string{"91porny.com"}

func browserFingerprintReferer(request *http.Request) bool {
	if request == nil || request.URL == nil {
		return false
	}
	target := strings.ToLower(request.URL.Hostname())
	referer := strings.TrimSpace(request.Header.Get("Referer"))
	if referer == "" {
		return false
	}
	parsed, err := url.Parse(referer)
	if err != nil || parsed.Host == "" {
		return false
	}
	from := strings.ToLower(parsed.Hostname())
	matched := false
	for _, suffix := range browserFingerprintReferers {
		if from == suffix || strings.HasSuffix(from, "."+suffix) {
			matched = true
			break
		}
	}
	if !matched {
		return false
	}
	// 同一个站的主站页面请求保持标准传输。
	for _, suffix := range browserFingerprintReferers {
		if target == suffix || strings.HasSuffix(target, "."+suffix) {
			return false
		}
	}
	return true
}

func browserSuffixHost(host string) bool {
	for _, suffix := range browserFingerprintSuffixes {
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return true
		}
	}
	return false
}

// browserEligibleRequest 只有 GET/HEAD 的 http(s) 请求才适合改换指纹传输。
func browserEligibleRequest(request *http.Request) bool {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		return false
	}
	return request.URL.Scheme == "http" || request.URL.Scheme == "https"
}

// isCustomHost 判断 host 是否由 registerHost 运行期登记（自定义源）。
func (transport *huangguoBrowserTransport) isCustomHost(request *http.Request) bool {
	host := strings.ToLower(request.URL.Hostname())
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.custom[host] || transport.custom[strings.ToLower(request.URL.Host)]
}

// promoted 判断自定义源 host 是否已确认需要浏览器指纹（曾被验证页拦住过）。
func (transport *huangguoBrowserTransport) promoted(request *http.Request) bool {
	host := strings.ToLower(request.URL.Hostname())
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return transport.upgraded[host]
}

func (transport *huangguoBrowserTransport) promote(host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	transport.mu.Lock()
	transport.upgraded[host] = true
	transport.mu.Unlock()
}

// browserChallengeStatus 判断是否值得换浏览器指纹再试一次。
// 只认 403：Cloudflare 的 managed challenge 固定用 403 + `cf-mitigated: challenge`，
// 而站点本身的权限拒绝也是 403——后者多花一次请求就返回，代价可接受；
// 429/503 属于限流与维护，换指纹只会加重负担，不重试。
func browserChallengeStatus(response *http.Response) bool {
	return response != nil && response.StatusCode == http.StatusForbidden
}

// registerHost 动态把一个 host 加入浏览器指纹传输候选集。
// 自定义源（含「加速链接+真实网址」拼接形态的 Cloudflare 前缀加速源）在新增、更新与
// 加载时调用它。这类 host 与内置 Browser 源不同：先按标准 Go 传输取一次，
// 只有返回 Cloudflare 验证页时才升级到 Chrome 指纹传输，并记住该 host，
// 之后同进程内一直用指纹传输。
func (transport *huangguoBrowserTransport) registerHost(host string) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return
	}
	transport.mu.Lock()
	if !transport.hosts[host] {
		transport.custom[host] = true
	}
	transport.mu.Unlock()
}

// forceInsecure 让指纹客户端跳过证书校验，同时保留 Chrome 指纹。
//
// 站方 CDN 证书链不完整时，标准传输会先报错、再被上层切成「宽松传输」。
// 如果那一步直接把指纹传输换成普通 Go Transport，指纹就丢了——对这个靠
// 指纹放行的 CDN 等于二次失败。所以宽松模式也要在指纹客户端内部打开。
func (transport *huangguoBrowserTransport) forceInsecure() {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	if transport.insecure {
		return
	}
	transport.insecure = true
	for key, client := range transport.clients {
		client.CloseIdleConnections()
		delete(transport.clients, key)
	}
}

func (transport *huangguoBrowserTransport) createClient(proxy string) (browserHTTPClient, error) {
	idleTimeout := 90 * time.Second
	options := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(profiles.Chrome_150),
		tlsclient.WithRandomTLSExtensionOrder(),
		tlsclient.WithDisableHttp3(),
		tlsclient.WithNotFollowRedirects(),
		tlsclient.WithTimeoutSeconds(45),
		tlsclient.WithProxyUrl(proxy),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
		tlsclient.WithTransportOptions(&tlsclient.TransportOptions{
			IdleConnTimeout: &idleTimeout, MaxIdleConns: 8, MaxIdleConnsPerHost: 4,
			MaxResponseHeaderBytes: 1 << 20,
			// Android 端纯 Go 不加载系统信任库，tls-client 的 RootCAs 为空时
			// utls 回落系统池会报 unknown authority。显式挂上「系统信任库 +
			// 打包 CA 全集」，与标准传输（app_runtime.go）保持一致的信任锚，
			// 修复黄果站点目录与播放请求在 Android 上握手失败的问题。
			RootCAs: systemCertPoolWithBundle(),
		}),
	}
	if transport.insecure {
		options = append(options, tlsclient.WithInsecureSkipVerify())
	}
	return tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
}

func (transport *huangguoBrowserTransport) client(request *http.Request) (browserHTTPClient, error) {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	proxy, err := transport.router.proxy(request)
	if err != nil {
		return nil, err
	}
	proxyURL := ""
	if proxy != nil {
		proxyURL = proxy.String()
	}
	key := request.URL.Scheme + "://" + strings.ToLower(request.URL.Host) + "|" + proxyURL
	if client := transport.clients[key]; client != nil {
		return client, nil
	}
	client, err := transport.newClient(proxyURL)
	if err != nil {
		return nil, err
	}
	if len(transport.clients) >= 8 {
		for key, old := range transport.clients {
			old.CloseIdleConnections()
			delete(transport.clients, key)
			break
		}
	}
	transport.clients[key] = client
	return client, nil
}

func huangguoBrowserHeaders(request *http.Request) fhttp.Header {
	headers := fhttp.Header{
		"sec-ch-ua":                 {`"Chromium";v="150", "Google Chrome";v="150", "Not_A Brand";v="24"`},
		"sec-ch-ua-mobile":          {"?0"},
		"sec-ch-ua-platform":        {`"macOS"`},
		"upgrade-insecure-requests": {"1"},
		"user-agent":                {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"},
		"accept":                    {"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
		"sec-fetch-site":            {"same-origin"},
		"sec-fetch-mode":            {"navigate"},
		"sec-fetch-user":            {"?1"},
		"sec-fetch-dest":            {"document"},
		"accept-language":           {"zh-CN,zh;q=0.9"},
		fhttp.HeaderOrderKey:        {"sec-ch-ua", "sec-ch-ua-mobile", "sec-ch-ua-platform", "upgrade-insecure-requests", "user-agent", "accept", "sec-fetch-site", "sec-fetch-mode", "sec-fetch-user", "sec-fetch-dest", "referer", "accept-encoding", "accept-language", "cookie"},
	}
	if mode := request.Header.Get("Sec-Fetch-Mode"); mode != "" && mode != "navigate" {
		headers["accept"] = []string{firstNonEmpty(request.Header.Get("Accept"), "*/*")}
		headers["sec-fetch-mode"] = []string{mode}
		headers["sec-fetch-dest"] = []string{firstNonEmpty(request.Header.Get("Sec-Fetch-Dest"), "empty")}
		delete(headers, "upgrade-insecure-requests")
		delete(headers, "sec-fetch-user")
	}
	for key, values := range request.Header {
		lower := strings.ToLower(key)
		if _, fixed := headers[lower]; fixed || lower == "host" || lower == "connection" {
			continue
		}
		headers[lower] = append([]string(nil), values...)
	}
	if referer := request.Header.Get("Referer"); referer == "" {
		headers["sec-fetch-site"] = []string{"none"}
	} else if parsed, err := url.Parse(referer); err == nil && !strings.EqualFold(parsed.Host, request.URL.Host) {
		headers["sec-fetch-site"] = []string{"cross-site"}
	}
	// 媒体请求要装成「video 元素在拉流」，不能沿用「导航到网页」那套头：
	// accept=text/html + sec-fetch-dest=document 对 CDN 来说是明显异常，
	// 有的节点据此直接断流。只有真带了 Range 或声明 video 的才算媒体请求。
	if isMediaFingerprintRequest(request) {
		headers["accept"] = []string{"*/*"}
		headers["sec-fetch-dest"] = []string{"video"}
		headers["sec-fetch-mode"] = []string{"no-cors"}
		headers["sec-fetch-site"] = []string{"cross-site"}
		delete(headers, "upgrade-insecure-requests")
		delete(headers, "sec-fetch-user")
	}
	return headers
}

// isMediaFingerprintRequest 判断这次请求是不是播放器在拉媒体流。
func isMediaFingerprintRequest(request *http.Request) bool {
	if request.Header.Get("Range") != "" {
		return true
	}
	if strings.EqualFold(request.Header.Get("Sec-Fetch-Dest"), "video") {
		return true
	}
	accept := strings.ToLower(request.Header.Get("Accept"))
	return strings.Contains(accept, "video/") || strings.Contains(accept, "audio/")
}

func standardBrowserHeaders(headers fhttp.Header) http.Header {
	result := make(http.Header, len(headers))
	for key, values := range headers {
		result[http.CanonicalHeaderKey(key)] = append([]string(nil), values...)
	}
	return result
}

func (transport *huangguoBrowserTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// 自定义源 host：先用标准传输（能带上规则里的 请求头/UA，且不少站点对
	// utls 指纹直接回 403 验证页），只有真的撞上验证页才升级到浏览器指纹。
	if browserEligibleRequest(request) && transport.isCustomHost(request) && !transport.promoted(request) {
		response, err := transport.base.RoundTrip(request)
		if err != nil || !browserChallengeStatus(response) {
			return response, err
		}
		status := response.StatusCode
		mitigated := response.Header.Get("Cf-Mitigated")
		if response.Body != nil {
			response.Body.Close()
		}
		transport.promote(request.URL.Hostname())
		message := "自定义源返回 403，已改用浏览器指纹重试"
		if strings.Contains(strings.ToLower(mitigated), "challenge") {
			message = "自定义源被 Cloudflare 验证页拦截，已改用浏览器指纹重试"
		}
		transport.recordEvent(request, status, message)
		return transport.browserRoundTrip(request)
	}
	if !transport.matches(request) {
		return transport.base.RoundTrip(request)
	}
	return transport.browserRoundTrip(request)
}

// recordEvent 在切换传输通道时留一条诊断，便于用户定位站点拦截行为。
func (transport *huangguoBrowserTransport) recordEvent(request *http.Request, status int, message string) {
	if transport.record == nil || request == nil || request.URL == nil {
		return
	}
	source := sourceHuangguoVideo
	if transport.isCustomHost(request) {
		source = ""
	}
	transport.record(diagnosticEvent{Event: "network.browser_request", Level: "warning", Source: source,
		Host: request.URL.Hostname(), HTTPStatus: status, Client: huangguoBrowserProfile,
		ResponseType: "text/html", Message: message})
}

func (transport *huangguoBrowserTransport) browserRoundTrip(request *http.Request) (*http.Response, error) {
	client, err := transport.client(request)
	if err != nil {
		return nil, fmt.Errorf("黄果浏览器客户端初始化失败：%w", publicError(err))
	}
	upstream, err := fhttp.NewRequestWithContext(request.Context(), request.Method, request.URL.String(), request.Body)
	if err != nil {
		return nil, err
	}
	upstream.Header = huangguoBrowserHeaders(request)
	upstream.Host = request.Host
	upstream.ContentLength = request.ContentLength
	upstream.GetBody = request.GetBody
	response, err := client.Do(upstream)
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, publicError(err)
	}
	result := &http.Response{
		Status: response.Status, StatusCode: response.StatusCode,
		Proto: response.Proto, ProtoMajor: response.ProtoMajor, ProtoMinor: response.ProtoMinor,
		Header: standardBrowserHeaders(response.Header), Body: response.Body,
		ContentLength: response.ContentLength, TransferEncoding: response.TransferEncoding,
		Close: response.Close, Uncompressed: response.Uncompressed,
		Trailer: standardBrowserHeaders(response.Trailer), Request: request,
	}
	if state := response.TLS; state != nil {
		result.TLS = &tls.ConnectionState{Version: state.Version, HandshakeComplete: state.HandshakeComplete,
			DidResume: state.DidResume, CipherSuite: state.CipherSuite, NegotiatedProtocol: state.NegotiatedProtocol,
			ServerName: state.ServerName, PeerCertificates: state.PeerCertificates, VerifiedChains: state.VerifiedChains}
	}
	if transport.record != nil {
		level := "info"
		if result.StatusCode >= 400 || strings.EqualFold(result.Header.Get("Cf-Mitigated"), "challenge") {
			level = "warning"
		}
		source := sourceHuangguoVideo
		if transport.isCustomHost(request) {
			source = ""
		}
		transport.record(diagnosticEvent{Event: "network.browser_request", Level: level, Source: source,
			Host: request.URL.Hostname(), HTTPStatus: result.StatusCode, Client: huangguoBrowserProfile,
			Protocol: result.Proto, CFRay: truncate(result.Header.Get("Cf-Ray"), 128),
			ResponseType: truncate(result.Header.Get("Content-Type"), 128), Message: "黄果浏览器指纹请求完成"})
	}
	return result, nil
}

func (transport *huangguoBrowserTransport) CloseIdleConnections() {
	transport.mu.Lock()
	for key, client := range transport.clients {
		client.CloseIdleConnections()
		delete(transport.clients, key)
	}
	transport.mu.Unlock()
	if closer, ok := transport.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
