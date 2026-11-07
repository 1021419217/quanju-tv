package seku

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

type zkShieldTicket struct {
	cookie string
	ua     string
	saved  time.Time
	ttl    time.Duration
}

type zkShieldFailEntry struct {
	count int
	at    time.Time
}

var zkShieldMu sync.Mutex
var zkShieldOn = true
var zkShieldRelays = []string{"https://tv506.ystv.top"}
var zkShieldGateway = ""
var zkShieldPyRelay = ""
var zkShieldPyKind = map[string]int{}
var zkShieldMode = ""
var zkShieldAliases = map[string][]string{}
var zkShieldAPIPaths = map[string][]string{}
var zkShieldLastUA = ""
var zkShieldTickets = map[string]zkShieldTicket{}
var zkShieldFail = map[string]zkShieldFailEntry{}
var zkShieldMarked = map[string]time.Time{}
var zkShieldJinaDeadUntil time.Time
var zkShieldJinaLast time.Time

func zkShieldSetOn(on bool) {
	zkShieldMu.Lock()
	zkShieldOn = on
	zkShieldMu.Unlock()
}

func zkShieldSetRelays(list []string) {
	zkShieldMu.Lock()
	cp := make([]string, 0, len(list))
	for _, r := range list {
		r = strings.TrimSpace(r)
		if len(r) > 6 {
			cp = append(cp, r)
		}
	}
	zkShieldRelays = cp
	zkShieldMu.Unlock()
}

func zkShieldAddRelay(relay string) {
	relay = strings.TrimSpace(relay)
	if len(relay) <= 6 {
		return
	}
	zkShieldMu.Lock()
	for _, r := range zkShieldRelays {
		if r == relay {
			zkShieldMu.Unlock()
			return
		}
	}
	zkShieldRelays = append([]string{relay}, zkShieldRelays...)
	zkShieldMu.Unlock()
}

func zkShieldSetGateway(gateway string) {
	zkShieldMu.Lock()
	zkShieldGateway = strings.TrimSpace(gateway)
	zkShieldMu.Unlock()
}

func zkShieldSetPyRelay(base string, kind int) {
	zkShieldMu.Lock()
	zkShieldPyRelay = strings.TrimSpace(base)
	if zkShieldPyRelay != "" {
		zkShieldPyKind[zkShieldPyRelay] = kind
	}
	zkShieldMu.Unlock()
}

func zkShieldSetMode(mode string) {
	zkShieldMu.Lock()
	zkShieldMode = mode
	zkShieldMu.Unlock()
}

func zkShieldAddAliasGroup(group string) {
	parts := strings.FieldsFunc(group, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	hosts := []string{}
	for _, p := range parts {
		h := zkShieldNormHost(p)
		if len(h) > 3 {
			found := false
			for _, x := range hosts {
				if x == h {
					found = true
				}
			}
			if !found {
				hosts = append(hosts, h)
			}
		}
	}
	if len(hosts) == 0 {
		return
	}
	zkShieldMu.Lock()
	for _, h := range hosts {
		zkShieldAliases[h] = hosts
	}
	zkShieldMu.Unlock()
}

func zkShieldAddAPIPath(hosts, paths string) {
	for _, hp := range strings.FieldsFunc(hosts, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
		h := zkShieldNormHost(hp)
		if len(h) < 4 {
			continue
		}
		for _, pp := range strings.FieldsFunc(paths, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' }) {
			p := strings.ToLower(strings.TrimSpace(pp))
			if len(p) <= 1 {
				continue
			}
			zkShieldMu.Lock()
			list := zkShieldAPIPaths[h]
			found := false
			for _, x := range list {
				if x == p {
					found = true
				}
			}
			if !found {
				zkShieldAPIPaths[h] = append(list, p)
			}
			zkShieldMu.Unlock()
		}
	}
}

func zkShieldSetTicket(host, cookie, ua string, ttl time.Duration) {
	host = zkShieldNormHost(host)
	if host == "" || cookie == "" {
		return
	}
	if ttl <= 0 {
		ttl = 1500 * time.Second
	}
	zkShieldMu.Lock()
	zkShieldTickets[host] = zkShieldTicket{cookie: cookie, ua: ua, saved: time.Now(), ttl: ttl}
	if ua != "" {
		zkShieldLastUA = ua
	}
	zkShieldMu.Unlock()
}

func zkShieldClearTicket(host string) {
	host = zkShieldNormHost(host)
	zkShieldMu.Lock()
	delete(zkShieldTickets, host)
	zkShieldMu.Unlock()
}

func zkShieldNormHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(value, "http") {
		if u, err := url.Parse(value); err == nil && u.Host != "" {
			value = u.Host
		}
	}
	if i := strings.Index(value, "/"); i > 0 {
		value = value[:i]
	}
	if i := strings.Index(value, ":"); i > 0 {
		value = value[:i]
	}
	value = strings.TrimPrefix(value, "www.")
	return value
}

func zkShieldHostOf(rawurl string) string {
	if u, err := url.Parse(rawurl); err == nil && u.Hostname() != "" {
		return strings.ToLower(u.Hostname())
	}
	return zkShieldNormHost(rawurl)
}

func zkShieldLiveTicket(host string) (zkShieldTicket, bool) {
	zkShieldMu.Lock()
	defer zkShieldMu.Unlock()
	t, ok := zkShieldTickets[host]
	if !ok {
		return t, false
	}
	if time.Since(t.saved) > t.ttl {
		delete(zkShieldTickets, host)
		return t, false
	}
	return t, true
}

func zkShieldTicketHeaders(host string, headers map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		out[k] = v
	}
	zkShieldMu.Lock()
	on := zkShieldOn
	zkShieldMu.Unlock()
	if !on {
		return out
	}
	t, ok := zkShieldLiveTicket(host)
	if !ok {
		return out
	}
	merged := ""
	for k, v := range out {
		if strings.EqualFold(k, "Cookie") {
			merged = v
		}
	}
	if merged != "" {
		merged = merged + "; " + t.cookie
	} else {
		merged = t.cookie
	}
	for k := range out {
		if strings.EqualFold(k, "Cookie") {
			delete(out, k)
		}
	}
	out["Cookie"] = merged
	ua := t.ua
	if ua == "" {
		zkShieldMu.Lock()
		ua = zkShieldLastUA
		zkShieldMu.Unlock()
	}
	if ua != "" {
		for k := range out {
			if strings.EqualFold(k, "User-Agent") {
				delete(out, k)
			}
		}
		out["User-Agent"] = ua
	}
	return out
}

func zkShieldIsChallenge(body string) bool {
	if body == "" {
		return true
	}
	head := body
	if len(head) > 6000 {
		head = head[:6000]
	}
	lower := strings.ToLower(head)
	if strings.Contains(lower, "just a moment") ||
		strings.Contains(lower, "cf_chl_opt") ||
		strings.Contains(lower, "cf-browser-verification") ||
		strings.Contains(lower, "enable javascript and cookies to continue") ||
		strings.Contains(lower, "attention required") {
		return true
	}
	return len(strings.TrimSpace(body)) < 200
}

func zkShieldStem(host string) string {
	host = zkShieldNormHost(host)
	if host == "" {
		return ""
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return host
	}
	i := len(parts) - 2
	if len(parts) >= 3 {
		switch parts[i] {
		case "com", "co", "net", "org", "gov", "edu":
			i--
		}
	}
	return parts[i]
}

func zkShieldDeclaredHost(body string) string {
	lower := strings.ToLower(body)
	idx := strings.Index(lower, "rel=\"canonical\"")
	if idx < 0 {
		idx = strings.Index(lower, "rel='canonical'")
	}
	if idx < 0 {
		idx = strings.Index(lower, "property=\"og:url\"")
	}
	if idx < 0 {
		idx = strings.Index(lower, "property='og:url'")
	}
	if idx < 0 {
		return ""
	}
	end := idx + 400
	if end > len(lower) {
		end = len(lower)
	}
	seg := body[idx:end]
	hi := strings.Index(seg, "http")
	if hi < 0 {
		return ""
	}
	rest := seg[hi:]
	cut := len(rest)
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if (c == '"' || c == '\'' || c == '>' || c == ' ' || c == '/') && i > 8 {
			cut = i
			break
		}
	}
	return zkShieldStem(zkShieldHostOf(rest[:cut]))
}

func zkShieldSane(rawurl, body string) bool {
	if body == "" || len(body) < 200 {
		return false
	}
	host := zkShieldHostOf(rawurl)
	stem := zkShieldStem(host)
	lower := strings.ToLower(body)
	zkShieldMu.Lock()
	aliases := zkShieldAliases[host]
	zkShieldMu.Unlock()
	mentions := false
	declaredOK := false
	if len(aliases) == 0 {
		if len(stem) >= 3 {
			mentions = strings.Contains(lower, stem)
			declared := zkShieldDeclaredHost(body)
			declaredOK = declared == "" || strings.Contains(declared, stem)
			if !declaredOK {
				return false
			}
		} else {
			return true
		}
	} else {
		for _, a := range aliases {
			as := zkShieldStem(a)
			if len(as) >= 3 && strings.Contains(lower, as) {
				mentions = true
			}
		}
		declared := zkShieldDeclaredHost(body)
		if declared == "" {
			declaredOK = true
		} else {
			for _, a := range aliases {
				as := zkShieldStem(a)
				if len(as) >= 3 && strings.Contains(declared, as) {
					declaredOK = true
				}
			}
		}
		if !declaredOK {
			return false
		}
	}
	if len(body) <= 3000 || mentions {
		return true
	}
	return false
}

func zkShieldAPIPath(rawurl string) bool {
	if rawurl == "" {
		return false
	}
	host := zkShieldHostOf(rawurl)
	zkShieldMu.Lock()
	paths := zkShieldAPIPaths[host]
	zkShieldMu.Unlock()
	if len(paths) == 0 {
		return false
	}
	lower := strings.ToLower(rawurl)
	for _, p := range paths {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func zkShieldGood(rawurl, body string, allowFail bool) bool {
	if zkShieldIsChallenge(body) {
		return false
	}
	if zkShieldAPIPath(rawurl) {
		return len(body) > 40
	}
	return allowFail || zkShieldSane(rawurl, body)
}

func zkShieldMark(host string, ok bool) {
	if host == "" {
		return
	}
	zkShieldMu.Lock()
	if ok {
		delete(zkShieldFail, host)
	} else {
		e := zkShieldFail[host]
		e.count++
		e.at = time.Now()
		zkShieldFail[host] = e
	}
	zkShieldMu.Unlock()
}

func zkShieldDead(host string) bool {
	zkShieldMu.Lock()
	defer zkShieldMu.Unlock()
	e, ok := zkShieldFail[host]
	if !ok {
		return false
	}
	return e.count >= 3 && time.Since(e.at) <= 60*time.Second
}

func zkShieldShielded(host string) bool {
	zkShieldMu.Lock()
	defer zkShieldMu.Unlock()
	at, ok := zkShieldMarked[host]
	if !ok {
		return false
	}
	if time.Since(at) > 600*time.Second {
		delete(zkShieldMarked, host)
		return false
	}
	return true
}

func zkShieldMarkShielded(host string) {
	if host == "" {
		return
	}
	zkShieldMu.Lock()
	zkShieldMarked[host] = time.Now()
	zkShieldMu.Unlock()
}

func zkShieldEnc(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}

func zkShieldTrimRelay(relay string) string {
	relay = strings.TrimSpace(relay)
	return strings.TrimSuffix(relay, "/")
}

func zkShieldRawGet(rawurl string, headers map[string]string, referer string, timeoutMs int) string {
	if timeoutMs <= 0 {
		timeoutMs = 15000
	}
	req, err := http.NewRequest("GET", rawurl, nil)
	if err != nil {
		return ""
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	cl := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond, Transport: sharedTransport}
	resp, err := cl.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return ""
	}
	return string(b)
}

func zkShieldRelayURL(relay, rawurl, referer string) string {
	u := zkShieldTrimRelay(relay) + "/p?u=" + zkShieldEnc(rawurl)
	if referer != "" {
		u += "&r=" + zkShieldEnc(referer)
	}
	return u
}

func zkShieldRelayTimeout(timeoutMs int) int {
	if timeoutMs < 5000 {
		timeoutMs = 5000
	}
	if timeoutMs > 12000 {
		timeoutMs = 12000
	}
	if timeoutMs < 6000 {
		timeoutMs = 6000
	}
	return timeoutMs
}

func zkShieldPyPaths(base string) []string {
	zkShieldMu.Lock()
	kind, ok := zkShieldPyKind[base]
	zkShieldMu.Unlock()
	if !ok {
		return []string{"/fs", "/fetch"}
	}
	switch kind {
	case 1:
		return []string{"/fs"}
	case 2:
		return []string{"/fetch"}
	case 3:
		return []string{"/p"}
	default:
		return []string{"/fs", "/fetch"}
	}
}

func zkShieldPyFetch(base, rawurl, referer string, timeoutMs int, bytesMode bool) string {
	if timeoutMs < 3000 {
		timeoutMs = 3000
	}
	for _, path := range zkShieldPyPaths(base) {
		u := zkShieldTrimRelay(base) + path + "?u=" + zkShieldEnc(rawurl)
		if referer != "" {
			u += "&r=" + zkShieldEnc(referer)
		}
		if bytesMode {
			u += "&b=1"
		}
		body := zkShieldRawGet(u, map[string]string{
			"User-Agent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
			"Accept":     "*/*",
		}, "", timeoutMs)
		if body != "" {
			zkShieldMu.Lock()
			kind := 0
			switch path {
			case "/fs":
				kind = 1
			case "/fetch":
				kind = 2
			case "/p":
				kind = 3
			}
			if kind != 0 {
				zkShieldPyKind[base] = kind
			}
			zkShieldMu.Unlock()
			return body
		}
	}
	return ""
}

func zkShieldRender(rawurl, referer string, timeoutMs int) string {
	zkShieldMu.Lock()
	if time.Now().Before(zkShieldJinaDeadUntil) {
		zkShieldMu.Unlock()
		return ""
	}
	if gap := time.Since(zkShieldJinaLast); gap < 3*time.Second {
		wait := 3*time.Second - gap
		zkShieldMu.Unlock()
		time.Sleep(wait)
		zkShieldMu.Lock()
	}
	zkShieldJinaLast = time.Now()
	zkShieldMu.Unlock()
	to := timeoutMs
	if to < 45000 {
		to = 45000
	}
	body := zkShieldRawGet("https://r.jina.ai/"+rawurl, map[string]string{
		"User-Agent":      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		"x-respond-with":  "html",
		"X-Return-Format": "text/html",
		"X-No-Cache":      "true",
	}, referer, to)
	if body != "" && len(body) < 600 {
		lower := strings.ToLower(body)
		if strings.Contains(lower, "rate limit") || strings.Contains(lower, "too many requests") || strings.Contains(lower, "429") {
			zkShieldMu.Lock()
			zkShieldJinaDeadUntil = time.Now().Add(45 * time.Second)
			zkShieldMu.Unlock()
			return ""
		}
	}
	return body
}

type zkShieldSlotState struct {
	mu   sync.Mutex
	body string
	done bool
}

func (s *zkShieldSlotState) set(body string) {
	s.mu.Lock()
	s.body = body
	s.done = true
	s.mu.Unlock()
}

func (s *zkShieldSlotState) get() (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.body, s.done
}

func zkShieldRaceWait(slots []*zkShieldSlotState, deadline time.Time) string {
	for {
		for _, s := range slots {
			if body, _ := s.get(); body != "" {
				return body
			}
		}
		allDone := true
		for _, s := range slots {
			if _, done := s.get(); !done {
				allDone = false
			}
		}
		if allDone {
			return ""
		}
		if time.Now().After(deadline) {
			return ""
		}
		time.Sleep(40 * time.Millisecond)
	}
}

func zkShieldRaceRunning(slots []*zkShieldSlotState) bool {
	for _, s := range slots {
		if _, done := s.get(); !done {
			return true
		}
	}
	return false
}

type zkShieldDirectFunc func(rawurl string, headers map[string]string, referer string, timeoutMs int) string

func zkShieldDefaultDirect(rawurl string, headers map[string]string, referer string, timeoutMs int) string {
	if timeoutMs < 4000 {
		timeoutMs = 4000
	}
	if timeoutMs > 15000 {
		timeoutMs = 15000
	}
	return zkShieldRawGet(rawurl, headers, referer, timeoutMs)
}

func zkShieldFetchWithDirect(rawurl string, headers map[string]string, referer string, timeoutMs int, allowFail bool, direct zkShieldDirectFunc) string {
	if rawurl == "" {
		return ""
	}
	if direct == nil {
		direct = zkShieldDefaultDirect
	}
	host := zkShieldHostOf(rawurl)
	zkShieldMu.Lock()
	on := zkShieldOn
	relays := append([]string{}, zkShieldRelays...)
	gateway := zkShieldGateway
	pyRelay := zkShieldPyRelay
	mode := zkShieldMode
	zkShieldMu.Unlock()
	if mode == "" {
		if on {
			mode = "auto"
		} else {
			mode = "direct"
		}
	}
	directOnly := mode == "direct" || !on
	slots := []*zkShieldSlotState{}
	directSlot := &zkShieldSlotState{}
	slots = append(slots, directSlot)
	go func() {
		body := direct(rawurl, zkShieldTicketHeaders(host, headers), referer, timeoutMs)
		if zkShieldGood(rawurl, body, allowFail) {
			zkShieldMark(host, true)
			zkShieldMu.Lock()
			delete(zkShieldMarked, host)
			zkShieldMu.Unlock()
		} else {
			zkShieldMark(host, false)
			if zkShieldIsChallenge(body) {
				zkShieldMarkShielded(host)
			}
			body = ""
		}
		directSlot.set(body)
	}()
	if !directOnly && mode != "webview" && mode != "relay" && mode != "gateway" {
		added := 0
		for _, relay := range relays {
			if added >= 2 {
				break
			}
			if zkShieldDead(relay) {
				continue
			}
			added++
			slot := &zkShieldSlotState{}
			slots = append(slots, slot)
			relayCopy := relay
			to := zkShieldRelayTimeout(timeoutMs)
			go func() {
				body := zkShieldRawGet(zkShieldRelayURL(relayCopy, rawurl, referer), headers, "", to)
				if !zkShieldGood(rawurl, body, allowFail) {
					body = ""
				}
				slot.set(body)
			}()
		}
		if !allowFail && pyRelay != "" && !zkShieldDead(pyRelay) {
			slot := &zkShieldSlotState{}
			slots = append(slots, slot)
			pyCopy := pyRelay
			go func() {
				body := zkShieldPyFetch(pyCopy, rawurl, referer, timeoutMs, false)
				if !zkShieldGood(rawurl, body, allowFail) {
					body = ""
				}
				slot.set(body)
			}()
		}
	}
	start := time.Now()
	if body := zkShieldRaceWait(slots, start.Add(7*time.Second)); body != "" {
		return body
	}
	if directOnly {
		return ""
	}
	if mode == "webview" {
		return ""
	}
	if zkShieldRaceRunning(slots) {
		if body := zkShieldRaceWait(slots, start.Add(30*time.Second)); body != "" {
			return body
		}
	}
	if mode != "relay" {
		if gateway != "" {
			gw := strings.TrimSuffix(gateway, "/")
			to := timeoutMs
			if to < 40000 {
				to = 40000
			}
			body := zkShieldRawGet(gw+"/fetch?u="+zkShieldEnc(rawurl), nil, "", to)
			if zkShieldGood(rawurl, body, allowFail) {
				return body
			}
		}
	}
	if !allowFail {
		body := zkShieldRender(rawurl, referer, timeoutMs)
		if zkShieldGood(rawurl, body, allowFail) {
			return body
		}
	}
	return ""
}

func zkShieldFetch(rawurl string, headers map[string]string, referer string, timeoutMs int, allowFail bool) string {
	return zkShieldFetchWithDirect(rawurl, headers, referer, timeoutMs, allowFail, nil)
}

func zkShieldFetchBytes(rawurl string, headers map[string]string, referer string, timeoutMs int) []byte {
	if rawurl == "" {
		return nil
	}
	host := zkShieldHostOf(rawurl)
	body := zkShieldRawGet(rawurl, zkShieldTicketHeaders(host, headers), referer, timeoutMs)
	if body != "" {
		return []byte(body)
	}
	zkShieldMu.Lock()
	relays := append([]string{}, zkShieldRelays...)
	pyRelay := zkShieldPyRelay
	zkShieldMu.Unlock()
	for _, relay := range relays {
		to := timeoutMs
		if to < 40000 {
			to = 40000
		}
		body = zkShieldRawGet(zkShieldRelayURL(relay, rawurl, referer), headers, "", to)
		if body != "" {
			return []byte(body)
		}
	}
	if pyRelay != "" {
		to := timeoutMs
		if to < 6000 {
			to = 6000
		}
		if body := zkShieldPyFetch(pyRelay, rawurl, referer, to, true); body != "" {
			return []byte(body)
		}
	}
	return nil
}
