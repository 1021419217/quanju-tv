package core

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/url"
	"strings"
)

type nativeStreamDiagnosticKey struct{}

type nativeStreamDiagnostic struct {
	session string
	asset   string
}

func nativeStreamID(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:6])
}

func nativeStreamTarget(address string) (string, string) {
	parsed, err := url.Parse(address)
	if err != nil {
		return "", "invalid"
	}
	parts := strings.Split(parsed.EscapedPath(), "/")
	for i, part := range parts {
		if len(part) > 48 {
			parts[i] = "[redacted-" + nativeStreamID(part) + "]"
		}
	}
	return parsed.Hostname(), truncate(strings.Join(parts, "/"), 512)
}

func nativeStreamError(err error) string {
	if err == nil {
		return "none"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "deadline"
	}
	var certificate *tls.CertificateVerificationError
	if errors.As(err, &certificate) {
		return "tls"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "dns"
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return "connect"
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected_eof"
	}
	// io_error 太笼统：用户只看到「诊断：传输中断」，没法区分是链路被
	// 重置、对端提前关闭，还是纯粹的读错误。把常见的连接层错误细分出来，
	// 下一轮反馈就能直接判断是网络问题还是代码问题，不用再靠猜。
	text := strings.ToLower(err.Error())
	switch {
	case strings.Contains(text, "connection reset") || strings.Contains(text, "reset by peer"):
		return "connection_reset"
	case strings.Contains(text, "broken pipe") || strings.Contains(text, "connection closed"):
		return "connection_closed"
	case errors.Is(err, io.EOF):
		return "eof"
	case strings.Contains(text, "http2") || strings.Contains(text, "protocol_error"):
		return "http2_error"
	}
	return "io_error"
}

func (stream *nativeStreamServer) nativeLog(info nativeStreamDiagnostic, event, address, detail string, status int, contentType string) {
	host, target := nativeStreamTarget(address)
	mediaType, _, _ := mime.ParseMediaType(contentType)
	stream.downloader.recordDiagnostic(diagnosticEvent{
		Event: "stream." + event, Level: "warning", Host: host, HTTPStatus: status, ResponseType: mediaType,
		Message: strings.TrimSpace(fmt.Sprintf("session=%s asset=%s path=%s %s", info.session, info.asset, target, detail)),
	})
}

func (stream *nativeStreamServer) nativeLogError(info nativeStreamDiagnostic, event, address string, err error, contentType string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	stream.nativeLog(info, event, address, "error="+nativeStreamError(err), 0, contentType)
}

func nativePlaylistKind(body string) string {
	if strings.Contains(body, "#EXT-X-STREAM-INF:") || strings.Contains(body, "#EXT-X-I-FRAME-STREAM-INF:") {
		return "master_playlist"
	}
	return "media_playlist"
}

type nativeDiagnosticBody struct {
	io.ReadCloser
	stream  *nativeStreamServer
	info    nativeStreamDiagnostic
	address string
}

func (body *nativeDiagnosticBody) Read(buffer []byte) (int, error) {
	n, err := body.ReadCloser.Read(buffer)
	if err != nil && err != io.EOF {
		body.stream.nativeLogError(body.info, "upstream_read_error", body.address, err, "")
	}
	return n, err
}
