// 临时排障工具：用 Chrome TLS 指纹请求媒体地址，确认 CDN 是否按指纹拦截。
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("usage: probe <url> <referer> [insecure]")
		os.Exit(2)
	}
	target, referer := os.Args[1], os.Args[2]
	options := []tlsclient.HttpClientOption{
		tlsclient.WithClientProfile(profiles.Chrome_150),
		tlsclient.WithRandomTLSExtensionOrder(),
		tlsclient.WithDisableHttp3(),
		tlsclient.WithTimeoutSeconds(25),
		tlsclient.WithCookieJar(tlsclient.NewCookieJar()),
		tlsclient.WithNotFollowRedirects(),
	}
	if len(os.Args) > 3 && os.Args[3] == "insecure" {
		options = append(options, tlsclient.WithInsecureSkipVerify())
	}
	client, err := tlsclient.NewHttpClient(tlsclient.NewNoopLogger(), options...)
	if err != nil {
		fmt.Println("client error:", err)
		os.Exit(1)
	}
	request, err := fhttp.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		fmt.Println("request error:", err)
		os.Exit(1)
	}
	request.Header = fhttp.Header{
		"user-agent":       {"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"},
		"accept":           {"*/*"},
		"accept-language":  {"zh-CN,zh;q=0.9"},
		"referer":          {referer},
		"range":            {"bytes=0-131071"},
		"sec-fetch-dest":   {"video"},
		"sec-fetch-mode":   {"no-cors"},
		"sec-fetch-site":   {"cross-site"},
		fhttp.HeaderOrderKey: {"user-agent", "accept", "accept-language", "referer", "range", "sec-fetch-dest", "sec-fetch-mode", "sec-fetch-site"},
	}
	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		fmt.Printf("DO ERROR after %s: %v\n", time.Since(start).Round(time.Millisecond), err)
		os.Exit(1)
	}
	defer response.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(response.Body, 200<<10))
	fmt.Printf("status=%d type=%q bytes=%d elapsed=%s readErr=%v\n",
		response.StatusCode, response.Header.Get("content-type"), len(body),
		time.Since(start).Round(time.Millisecond), readErr)
	for name, values := range response.Header {
		switch name {
		case "X-Video-Notice", "X-Video-Notice-Code", "Server", "Content-Length", "Content-Range":
			fmt.Printf("  %s: %v\n", name, values)
		}
	}
	if len(body) > 0 {
		head := body
		if len(head) > 32 {
			head = head[:32]
		}
		fmt.Printf("  head=%q\n", head)
	}
}
