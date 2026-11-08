package core

import (
	"bufio"
	"context"
	"fmt"
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
	recorder := httptest.NewRecorder()
	stream.nativeRelayBody(context.Background(), recorder, http.MethodGet, request, request.URL, response, bufio.NewReader(response.Body), nativeStreamDiagnostic{})

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
