package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const legacyFrontendURL = "https://d2pypzndaqisk.cloudfront.net"

var apiFallbackHosts = []string{
	defaultAPIBase,
	"https://d18ka9rqpfd3lo.cloudfront.net",
	"https://d37n0wjehmw08f.cloudfront.net",
}

func (d *Downloader) apiEndpoint(ctx context.Context) (string, error) {
	d.apiMu.Lock()
	defer d.apiMu.Unlock()
	if d.apiBase != "" {
		return d.apiBase, nil
	}
	configured := strings.TrimRight(firstNonEmpty(d.cfg.APIBase, defaultAPIBase), "/")
	parsed, err := url.Parse(configured)
	if err != nil || !isProviderHTTPMediaURL(configured) {
		return "", errors.New("API 地址无效")
	}
	if !strings.HasSuffix(strings.ToLower(parsed.Hostname()), ".cloudfront.net") {
		d.apiBase = configured
		return configured, nil
	}
	candidates := append([]string{configured}, apiFallbackHosts...)
	seen := map[string]bool{}
	var lastErr error
	for pass := 0; pass < 2; pass++ {
		if pass == 1 {
			candidates, err = d.discoverAPIHosts(ctx)
			if err != nil {
				lastErr = err
				break
			}
		}
		var pending []string
		for _, candidate := range candidates {
			if seen[candidate] || time.Now().Before(d.apiFailures[candidate]) {
				continue
			}
			seen[candidate] = true
			pending = append(pending, candidate)
		}
		if len(pending) == 0 {
			continue
		}
		if err := ctx.Err(); err != nil {
			return "", err
		}
		// 并发探测：三个 cloudfront 候选里经常只有一路可达，串行探测会把
		// 4 秒超时叠加成十几秒，前台等不及就报「黄果旧版加载失败」。
		type probeResult struct {
			host string
			err  error
		}
		results := make(chan probeResult, len(pending))
		probeCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
		for _, candidate := range pending {
			go func(host string) {
				results <- probeResult{host: host, err: d.probeAPIHost(probeCtx, host)}
			}(candidate)
		}
		for range pending {
			result := <-results
			if result.err == nil {
				cancel()
				d.apiBase = result.host
				return result.host, nil
			}
			lastErr = result.err
		}
		cancel()
	}
	return "", fmt.Errorf("黄果原 API 的候选域名均不可用: %w", publicError(lastErr))
}

func (d *Downloader) resetAPIEndpoint(failed string) {
	d.apiMu.Lock()
	if d.apiFailures == nil {
		d.apiFailures = map[string]time.Time{}
	}
	d.apiFailures[failed] = time.Now().Add(time.Minute)
	if d.apiBase == failed {
		d.apiBase = ""
	}
	d.apiMu.Unlock()
}

func (d *Downloader) probeAPIHost(ctx context.Context, host string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, host+"/api/app/ping/check", nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", userAgent)
	response, err := d.doCatalogRequestWithTimeout(request, 4*time.Second)
	if err != nil {
		return publicError(err)
	}
	defer response.Body.Close()
	var envelope struct {
		Code int `json:"code"`
	}
	if response.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(response.Body, 64*1024)).Decode(&envelope) != nil || envelope.Code != 200 {
		return fmt.Errorf("%s API 健康检查失败 (HTTP %d)", request.URL.Hostname(), response.StatusCode)
	}
	return nil
}

func (d *Downloader) discoverAPIHosts(ctx context.Context) ([]string, error) {
	body, err := d.fetchLegacyFrontendScript(ctx)
	if err != nil {
		return nil, err
	}
	if hosts := parseAPIHosts(body); len(hosts) > 0 {
		return hosts, nil
	}
	return nil, errors.New("官网前端未提供可识别的 API 地址，请稍后重试或更新应用")
}

func (d *Downloader) fetchLegacyFrontendScript(ctx context.Context) (string, error) {
	page, err := d.fetchProviderText(ctx, legacyFrontendURL+"/", legacyFrontendURL+"/")
	if err != nil {
		return "", err
	}
	scripts := regexp.MustCompile(`(?i)<script\b[^>]*\bsrc=["']([^"']+)["']`).FindAllStringSubmatch(page, -1)
	for _, script := range scripts {
		if !strings.Contains(script[1], "/main-") || !strings.HasSuffix(script[1], ".js") {
			continue
		}
		assetURL := resolveProviderURL(legacyFrontendURL+"/", script[1])
		asset, err := url.Parse(assetURL)
		if err != nil || asset.Hostname() != "d2pypzndaqisk.cloudfront.net" {
			continue
		}
		body, err := d.fetchProviderText(ctx, assetURL, legacyFrontendURL+"/")
		if err != nil {
			return "", err
		}
		return body, nil
	}
	return "", errors.New("黄果官网未提供可识别的接口脚本")
}

func parseAPIHosts(script string) []string {
	matches := regexp.MustCompile(`https://[a-z0-9]+\.cloudfront\.net`).FindAllString(script, -1)
	seen := map[string]bool{}
	var hosts []string
	for _, host := range matches {
		if host != legacyFrontendURL && !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
			if len(hosts) == 8 {
				break
			}
		}
	}
	return hosts
}
