package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// 站方 CDN 会在传一段之后直接掐断连接。本地中转必须从断点续拉，
// 让播放器收到完整文件，而不是一个「传输中断」。
func TestNativeRelayResumesAfterUpstreamCut(t *testing.T) {
	const total = 120000
	const chunk = 30000
	payload := make([]byte, total)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		start := 0
		if value := request.Header.Get("Range"); strings.HasPrefix(value, "bytes=") {
			raw := strings.TrimSuffix(strings.TrimPrefix(value, "bytes="), "-")
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(writer, "bad range", http.StatusBadRequest)
				return
			}
			start = parsed
		}
		if start >= total {
			writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start > 0 {
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, total-1, total))
			writer.Header().Set("Content-Length", strconv.Itoa(total-start))
			writer.WriteHeader(http.StatusPartialContent)
		} else {
			writer.Header().Set("Content-Length", strconv.Itoa(total))
			writer.WriteHeader(http.StatusOK)
		}
		end := min(start+chunk, total)
		_, _ = writer.Write(payload[start:end])
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		if end < total {
			// 模拟 CDN 掐断：直接断开连接，客户端读到 unexpected EOF。
			panic(http.ErrAbortHandler)
		}
	}))
	defer server.Close()

	stream := &nativeStreamServer{downloader: &Downloader{client: server.Client()}, sessions: map[string]*nativeStreamSession{}, address: "http://127.0.0.1"}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/x.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := stream.nativeRequest(request)
	if err != nil {
		t.Fatalf("首包失败: %v", err)
	}
	defer response.Body.Close()
	session := &nativeStreamSession{assets: map[string]nativeStreamAsset{"x": {kind: "media", address: server.URL + "/x.mp4"}}}
	recorder := httptest.NewRecorder()
	stream.nativeRelayBody(context.Background(), recorder, http.MethodGet, request, session, "x", request.URL.String(), response, bufio.NewReader(response.Body), nativeStreamDiagnostic{})

	body := recorder.Body.Bytes()
	if len(body) != total {
		t.Fatalf("续传后长度不对: got %d want %d", len(body), total)
	}
	if string(body) != string(payload) {
		t.Fatalf("续传内容错位")
	}
}

// 上游不支持 Range、每次都回整个文件时，续传要先跳过已写部分，不能重复写。
func TestNativeRelaySkipsWhenServerIgnoresRange(t *testing.T) {
	const total = 40000
	payload := make([]byte, total)
	for i := range payload {
		payload[i] = byte('A' + i%26)
	}
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		writer.Header().Set("Content-Type", "video/mp4")
		writer.Header().Set("Content-Length", strconv.Itoa(total))
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(payload)
	}))
	defer server.Close()

	stream := &nativeStreamServer{downloader: &Downloader{client: server.Client()}, sessions: map[string]*nativeStreamSession{}, address: "http://127.0.0.1"}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/y.mp4", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := stream.nativeRequest(request)
	if err != nil {
		t.Fatalf("首包失败: %v", err)
	}
	defer response.Body.Close()
	reader, closer, err := stream.nativeResume(context.Background(), request, request.URL, 10000)
	if err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	defer closer()
	sample := make([]byte, 10)
	if _, err := reader.Read(sample); err != nil {
		t.Fatalf("读取续传内容失败: %v", err)
	}
	if string(sample) != string(payload[10000:10010]) {
		t.Fatalf("没有跳过已写部分: %q", sample)
	}
	if calls != 2 {
		t.Fatalf("调用次数异常: %d", calls)
	}
}

// rangePayloadServer 提供带 Range 的完整文件；cutAfter > 0 时每次只给这么点就断。
func rangePayloadServer(payload []byte, cutAfter int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		start := 0
		if value := request.Header.Get("Range"); strings.HasPrefix(value, "bytes=") {
			raw := strings.TrimSuffix(strings.TrimPrefix(value, "bytes="), "-")
			parsed, err := strconv.Atoi(raw)
			if err != nil {
				http.Error(writer, "bad range", http.StatusBadRequest)
				return
			}
			start = parsed
		}
		if start >= len(payload) {
			writer.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		if start > 0 {
			writer.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)-start))
			writer.WriteHeader(http.StatusPartialContent)
		} else {
			writer.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			writer.WriteHeader(http.StatusOK)
		}
		end := len(payload)
		if cutAfter > 0 && start+cutAfter < end {
			end = start + cutAfter
		}
		_, _ = writer.Write(payload[start:end])
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		if end < len(payload) {
			panic(http.ErrAbortHandler)
		}
	}))
}

// stubbornServer 声明完整长度但一个字节都不给就断，模拟「连上了却立刻被掐」。
func stubbornServer(total int) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(total))
		writer.Header().Set("Content-Type", "video/mp4")
		writer.WriteHeader(http.StatusOK)
		if flusher, ok := writer.(http.Flusher); ok {
			flusher.Flush()
		}
		panic(http.ErrAbortHandler)
	}))
}

func relayFixture(t *testing.T, address string, session *nativeStreamSession) (*nativeStreamServer, *http.Request, *http.Response, *httptest.ResponseRecorder) {
	t.Helper()
	stream := &nativeStreamServer{downloader: &Downloader{client: http.DefaultClient}, sessions: map[string]*nativeStreamSession{}, address: "http://127.0.0.1"}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := stream.nativeRequest(request)
	if err != nil {
		t.Fatalf("首包失败: %v", err)
	}
	t.Cleanup(func() { response.Body.Close() })
	if session.assets == nil {
		session.assets = map[string]nativeStreamAsset{}
	}
	session.assets["x"] = nativeStreamAsset{kind: "media", address: address}
	return stream, request, response, httptest.NewRecorder()
}

// 有些 CDN 回 206 却不带 Content-Range。以前会直接判「续传缺少 Content-Range」放弃，
// 现在按「从 offset 开始」乐观续，能救回这类节点。
func TestNativeResumeToleratesMissingContentRange(t *testing.T) {
	payload := make([]byte, 20000)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// 故意不回 Content-Range
		writer.WriteHeader(http.StatusPartialContent)
		_, _ = writer.Write(payload[8000:])
	}))
	defer server.Close()

	stream := &nativeStreamServer{downloader: &Downloader{client: server.Client()}}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/m.mp4", nil)
	reader, closer, err := stream.nativeResumeFrom(context.Background(), request, server.URL+"/m.mp4", 8000)
	if err != nil {
		t.Fatalf("缺 Content-Range 时不应放弃: %v", err)
	}
	defer closer()
	sample := make([]byte, 8)
	if _, err := io.ReadFull(reader, sample); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if string(sample) != string(payload[8000:8008]) {
		t.Fatalf("续传起点错误: %q", sample)
	}
}

// 主链一直被掐、备用直链能用时，必须换到备用线路把片子传完。
func TestNativeRelaySwitchesToFallback(t *testing.T) {
	total := 60000
	payload := make([]byte, total)
	for i := range payload {
		payload[i] = byte('A' + i%26)
	}
	dead := stubbornServer(total) // 声明全长却一个字节都不给，等于连上就断
	defer dead.Close()
	good := rangePayloadServer(payload, 0)
	defer good.Close()

	session := &nativeStreamSession{fallbacks: []string{good.URL + "/f.mp4"}}
	stream, request, response, recorder := relayFixture(t, dead.URL+"/d.mp4", session)
	stream.downloader.client = dead.Client()

	stream.nativeRelayBody(context.Background(), recorder, http.MethodGet, request, session, "x", dead.URL+"/d.mp4", response, bufio.NewReader(response.Body), nativeStreamDiagnostic{})

	body := recorder.Body.Bytes()
	if len(body) != total {
		t.Fatalf("换线后长度不对: got %d want %d", len(body), total)
	}
	if !strings.HasPrefix(string(body), string(payload[:8])) {
		t.Fatalf("内容错位")
	}
	if session.assets["x"].address != good.URL+"/f.mp4" {
		t.Fatalf("换线后没记住新地址: %s", session.assets["x"].address)
	}
}

// 备用线路也死时，站源重新解析出的新地址要能接手（签名过期只能这么救）。
func TestNativeRelayReloadsAddress(t *testing.T) {
	total := 30000
	payload := make([]byte, total)
	for i := range payload {
		payload[i] = byte('0' + i%10)
	}
	dead := stubbornServer(total)
	defer dead.Close()
	good := rangePayloadServer(payload, 0)
	defer good.Close()

	calls := 0
	session := &nativeStreamSession{reload: func(context.Context) (string, error) {
		calls++
		return good.URL + "/fresh.mp4", nil
	}}
	stream, request, response, recorder := relayFixture(t, dead.URL+"/old.mp4", session)
	stream.downloader.client = dead.Client()

	stream.nativeRelayBody(context.Background(), recorder, http.MethodGet, request, session, "x", dead.URL+"/old.mp4", response, bufio.NewReader(response.Body), nativeStreamDiagnostic{})

	if len(recorder.Body.Bytes()) != total {
		t.Fatalf("重新解析后长度不对: got %d want %d", len(recorder.Body.Bytes()), total)
	}
	if calls == 0 {
		t.Fatalf("应调用重新解析")
	}
}
