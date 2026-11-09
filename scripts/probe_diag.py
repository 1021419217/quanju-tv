"""一次性海外机房探针：91 借道九色可行性 / kissav 镜像 / hsex 500 与 429。

只在 GitHub Actions 里手动运行，输出直接落在日志里，不进 App。
"""
import concurrent.futures as cf
import html
import os
import re
import subprocess
import sys
import urllib.parse

UA_IPHONE = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_7 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.6 Mobile/15E148 Safari/604.1"
UA_CHROME = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"


def curl(url, *, ua=UA_IPHONE, referer=None, headers=None, out=None, timeout=25, method="GET"):
    cmd = ["curl", "-sSkL", "-m", str(timeout), "-A", ua,
           "-H", "Accept: */*", "-H", "Accept-Language: zh-CN,zh;q=0.9"]
    if referer:
        cmd += ["-e", referer]
    for k, v in (headers or {}).items():
        cmd += ["-H", f"{k}: {v}"]
    if out:
        cmd += ["-o", out, "-w", "%{http_code} %{size_download} %{time_total}"]
    else:
        cmd += ["-w", "%{http_code} %{size_download} %{time_total}"]
    cmd.append(url)
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout + 8)
    return r.stdout.strip(), r.stderr.strip()[-200:]


def text_of(path):
    try:
        return open(path, encoding="utf-8", errors="ignore").read()
    except OSError:
        return ""


def section(name):
    print("\n" + "#" * 20 + " " + name + " " + "#" * 20, flush=True)


# ---------------------------------------------------------------- 91
def p91_detail(url, key, ua=UA_CHROME):
    out = f"/tmp/p91d_{key}.html"
    stat, err = curl(url, ua=ua, out=out)
    body = text_of(out)
    print(f"  detail {url} -> {stat} err={err}")
    m = re.search(r"strencode2\(\s*['\"]([^'\"]+)['\"]", body)
    if not m:
        # 有些渲染把地址直接写进 source
        u = re.search(r"https?://[^\"'\s<>]+?\.(?:mp4|m3u8)[^\"'\s<>]*", body)
        return html.unescape(u.group(0)) if u else "", body
    dec = urllib.parse.unquote(html.unescape(m.group(1)))
    u = re.search(r"https?://[^\"'\s<>]+?\.(?:mp4|m3u8)[^\"'\s<>]*", dec)
    return (html.unescape(u.group(0)) if u else ""), body


def probe_91():
    section("91：直链域名 + 同源九色可播性")
    keys = ["e7f9b65d89f5238b6211", "7dff460579f327b234a2"]
    p91_mirrors = ["https://91porny.com", "https://91porn.mom", "https://91porn.is"]
    jiuse_mirrors = ["https://91porny.com", "https://91porn.mom", "https://91porn.is"]
    direct = ""
    for base in p91_mirrors:
        for key in keys:
            u, _ = p91_detail(f"{base}/view_video.php?viewkey={key}", key)
            if u:
                direct = u
                print(f"  DIRECT host={urllib.parse.urlparse(u).netloc} path={urllib.parse.urlparse(u).path}")
                print(f"  DIRECT query keys={list(urllib.parse.parse_qs(urllib.parse.urlparse(u).query).keys())}")
                break
        if direct:
            break
    if direct:
        ref = "https://91porny.com/view_video.php?viewkey=" + keys[0]
        for label, extra in [("range+ref", {"Range": "bytes=0-65535"}),
                             ("full+ref", {}),
                             ("http1.1", {})]:
            hdrs = dict(extra)
            if label == "http1.1":
                hdrs["Accept-Encoding"] = "identity"
            st, err = curl(direct, ua=UA_IPHONE, referer=ref, headers=hdrs,
                           out="/tmp/p91.bin")
            print(f"  direct {label}: {st} err={err}")
    # 决定性验证：同一个 viewkey 能否在九色拿到正经 HLS
    for base in jiuse_mirrors:
        for key in keys:
            page = f"{base}/video/view/{key}"
            out = "/tmp/jiuse_v.html"
            st, err = curl(page, ua=UA_CHROME, out=out)
            body = text_of(out)
            src = re.search(r'data-src="([^"]+)"', body)
            vip = "vip-layer" in body
            print(f"  JIUSE {base}/video/view/{key} -> {st} bytes={len(body)} "
                  f"data-src={bool(src)} vip={vip} colVideoList={body.count('colVideoList')}")
            if src:
                u = html.unescape(src.group(1))
                if u.startswith("/"):
                    u = base + u
                st2, _ = curl(u, ua=UA_CHROME, referer=page, out="/tmp/jiuse_p.m3u8")
                pl = text_of("/tmp/jiuse_p.m3u8")
                print(f"    playlist -> {st2} EXTINF={pl.count('#EXTINF')} "
                      f"vnotice={'__vnotice' in pl} head={pl[:80]!r}")
                ts = re.search(r"^https?://[^\s]+", pl, re.M)
                if ts:
                    st3, _ = curl(ts.group(0), ua=UA_CHROME, referer=page,
                                  out="/tmp/jiuse_t.ts")
                    print(f"    first segment -> {st3}")
            break


# ------------------------------------------------------------- kissav
def probe_kissav():
    section("kissav：镜像可达性 + EOF 位置")
    hosts = ["kissavs.com", "www.kissavs.com", "kissav.com", "kissav.tv",
             "kissav.me", "kissav.cc", "kissav.to", "kissav.is", "kissav.us",
             "kissav.net", "kissav.la", "kissav.ru"]
    def one(host):
        url = f"https://{host}/av/updates/"
        out = f"/tmp/k_{host.replace('.', '_')}.html"
        st, err = curl(url, ua=UA_IPHONE, out=out, timeout=18)
        body = text_of(out)
        return host, st, err, len(body), body.count("video-img-box")
    with cf.ThreadPoolExecutor(6) as ex:
        for host, st, err, size, cards in ex.map(one, hosts):
            print(f"  {host:22s} stat={st:24s} bytes={size:7d} cards={cards:3d} err={err}")
    # 对主域名看 TLS / HTTP2 细节
    print("  -- kissavs.com 详细握手（curl -v 片段）")
    r = subprocess.run(["curl", "-sv", "-m", "20", "-A", UA_IPHONE,
                        "-o", "/tmp/kv.html", "https://kissavs.com/av/updates/"],
                       capture_output=True, text=True, timeout=30)
    for line in r.stderr.splitlines():
        if any(k in line for k in ("Trying", "Connected", "ALPN", "SSL", "TLS", "HTTP/", "error", "EOF", "closed", "subject", "issuer")):
            print("    " + line[:180])


# --------------------------------------------------------------- hsex
def probe_hsex():
    section("hsex：429 限流 / CDN 500 / 作者页")
    base = "https://hsex.icu"
    # 连续请求看何时 429
    print("  -- 连续请求列表页，观察 429 与 Retry-After")
    for i in range(1, 7):
        out = f"/tmp/h_list_{i}.html"
        st, err = curl(f"{base}/list-{i}.htm", ua=UA_IPHONE, out=out)
        body = text_of(out)
        print(f"    page{i}: {st} bytes={len(body)} cards={body.count('class=\"thumbnail\"')}")
    # 429 响应头
    r = subprocess.run(["curl", "-sSk", "-m", "20", "-D", "-", "-o", "/dev/null",
                        "-A", UA_IPHONE, f"{base}/list-1.htm"],
                       capture_output=True, text=True, timeout=30)
    print("  -- 响应头片段：")
    for line in r.stdout.splitlines()[:18]:
        print("    " + line[:180])
    # 详情页 → m3u8 → 分片
    det = f"{base}/video-1257475.htm"
    out = "/tmp/h_d.html"
    st, err = curl(det, ua=UA_IPHONE, out=out)
    body = text_of(out)
    print(f"  detail -> {st} bytes={len(body)}")
    src = re.search(r'(?is)<source[^>]+src="(https?://[^"]+\.m3u8[^"]*)"', body)
    author = re.findall(r'href="user\.htm\?author=([^"&]+)"', body)
    print(f"  source={src.group(1)[:140] if src else 'none'}")
    print(f"  author links={author[:5]}")
    if src:
        u = html.unescape(src.group(1))
        print(f"  -- m3u8 host={urllib.parse.urlparse(u).netloc} path={urllib.parse.urlparse(u).path[:120]}")
        for label, hdrs, ua in [("iphone+ref", {}, UA_IPHONE),
                                ("iphone+noref", {}, UA_IPHONE),
                                ("chrome+ref", {}, UA_CHROME),
                                ("chrome+identity", {"Accept-Encoding": "identity"}, UA_CHROME)]:
            st2, err2 = curl(u, ua=ua, referer=(det if "noref" not in label else None),
                             headers=hdrs, out="/tmp/h_p.m3u8")
            pl = text_of("/tmp/h_p.m3u8")
            print(f"    playlist {label:18s}: {st2} bytes={len(pl)} "
                  f"EXTINF={pl.count('#EXTINF')} head={pl[:60]!r} err={err2}")
            first = re.search(r"^(?!#)(?:https?://)?[^\s]+\.ts[^\s]*", pl, re.M)
            if first:
                seg = first.group(0)
                if seg.startswith("http"):
                    segurl = seg
                else:
                    segurl = urllib.parse.urljoin(u, seg if not seg.startswith("/") else "/" + seg.lstrip("/"))
                st3, err3 = curl(segurl, ua=ua, referer=det, out="/tmp/h_t.ts")
                print(f"      first segment -> {st3} err={err3} url={segurl[:110]}")
        keytag = re.search(r'URI="([^"]+)"', pl)
        if keytag:
            ku = urllib.parse.urljoin(u, keytag.group(1))
            st4, _ = curl(ku, ua=UA_IPHONE, referer=det, out="/tmp/h_k.key")
            print(f"      key -> {st4}")
    # 作者页结构
    if author:
        a = author[0]
        au = f"{base}/user.htm?author={urllib.parse.quote(a)}"
        st5, err5 = curl(au, ua=UA_IPHONE, out="/tmp/h_a.html")
        ab = text_of("/tmp/h_a.html")
        print(f"  -- author page {a!r} -> {st5} bytes={len(ab)} "
              f"thumbnails={ab.count('class=\"thumbnail\"')} video-links={len(set(re.findall(r'video-(\\d+)\\.htm', ab)))}")
        print(f"     title={re.findall(r'<title[^>]*>(.*?)</title>', ab, re.S)[:1]}")
        print(f"     pagination hints={re.findall(r'href=\"([^\"]*user\\.htm[^\"]*)\"', ab)[:8]}")
        for p in (2, 3):
            st6, _ = curl(au + f"&page={p}", ua=UA_IPHONE, out="/tmp/h_a2.html")
            ab2 = text_of("/tmp/h_a2.html")
            ids2 = set(re.findall(r"video-(\d+)\.htm", ab2))
            print(f"     page={p} -> {st6} bytes={len(ab2)} ids={len(ids2)}")


if __name__ == "__main__":
    which = sys.argv[1:] or ["91", "kissav", "hsex"]
    for name in which:
        try:
            {"91": probe_91, "kissav": probe_kissav, "hsex": probe_hsex}[name]()
        except Exception as e:  # noqa: BLE001
            print(f"  !! {name} 探针异常: {type(e).__name__}: {e}", flush=True)
