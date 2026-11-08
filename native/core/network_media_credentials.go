package core

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type providerMediaCredentials struct {
	cookie    string
	origin    string
	referer   string
	userAgent string
	expires   time.Time
	// insecureTLS 只给证书链不完整的站点用（缺中间证书，标准校验必失败但流可播）。
	// 由站源探测到证书错误并确认「跳过校验能出流」后显式打开，默认关闭。
	insecureTLS bool
	// sendCookie 让 Cookie 跟着每一跳走（含跨域 CDN）。站源把播放地址放在自家
	// CDN 上、且 CDN 也要校验游客 Cookie 时才打开；默认只在同源请求里带。
	sendCookie bool
	// resilient 表示上游会掐断长连接，本地中转要断点续传而不是把错误抛给播放器。
	// 只有站源显式标记时才走续传路径，其它源保持原样。
	resilient bool
}

type providerMediaCredentialsKey struct{}

func providerMediaContext(ctx context.Context, credentials *providerMediaCredentials) context.Context {
	return context.WithValue(ctx, providerMediaCredentialsKey{}, credentials)
}

func providerMediaOrigin(address *url.URL) string {
	if address == nil {
		return ""
	}
	scheme := strings.ToLower(address.Scheme)
	host := strings.ToLower(address.Host)
	if scheme == "https" && address.Port() == "443" || scheme == "http" && address.Port() == "80" {
		host = strings.TrimSuffix(host, ":"+address.Port())
	}
	return scheme + "://" + host
}

func (credentials *providerMediaCredentials) apply(request *http.Request) error {
	request.Header.Del("Cookie")
	if credentials.userAgent != "" {
		request.Header.Set("User-Agent", credentials.userAgent)
	}
	if credentials.referer != "" {
		request.Header.Set("Referer", credentials.referer)
		if address, err := url.Parse(credentials.referer); err == nil {
			request.Header.Set("Origin", providerMediaOrigin(address))
		}
	}
	if credentials.sendCookie || providerMediaOrigin(request.URL) == credentials.origin {
		if !credentials.expires.IsZero() && !time.Now().Before(credentials.expires) {
			return errors.New("播放凭证已过期，请重新解析播放")
		}
		if credentials.cookie != "" {
			request.Header.Set("Cookie", credentials.cookie)
		}
	}
	return nil
}

func (credentials *providerMediaCredentials) client(client *http.Client) *http.Client {
	scoped := *client
	scoped.Jar = nil
	if credentials.insecureTLS {
		scoped.Transport = insecureMediaTransport(client.Transport)
	}
	previousRedirect := client.CheckRedirect
	scoped.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("媒体重定向过多")
		}
		if previousRedirect != nil {
			if err := previousRedirect(request, via); err != nil {
				return err
			}
		}
		return credentials.apply(request)
	}
	return &scoped
}

// insecureMediaTransport 复制一份跳过证书校验的传输层。
// 只有站源明确标记了 insecureTLS（缺中间证书的 CDN）才会走到这里。
func insecureMediaTransport(base http.RoundTripper) http.RoundTripper {
	// 指纹传输不能在这里被换成普通 Transport：91/theporny 这类 CDN 正是按
	// TLS 指纹放行的，丢掉指纹等于白试。改成在指纹客户端内部跳过证书校验。
	if browser, ok := base.(*huangguoBrowserTransport); ok && browser != nil {
		browser.forceInsecure()
		return browser
	}
	transport, ok := base.(*http.Transport)
	if !ok || transport == nil {
		return base
	}
	cloned := transport.Clone()
	if cloned.TLSClientConfig == nil {
		cloned.TLSClientConfig = &tls.Config{}
	} else {
		cloned.TLSClientConfig = cloned.TLSClientConfig.Clone()
	}
	cloned.TLSClientConfig.InsecureSkipVerify = true
	return cloned
}
