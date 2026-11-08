package seku

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type magnetEntry struct {
	Name    string
	URL     string
	SizeMB  int64
	Files   int
	Date    string
	Quality string
	Tags    []string
}

func (m magnetEntry) SizeLabel() string {
	if m.SizeMB <= 0 {
		return ""
	}
	if m.SizeMB >= 1024 {
		return fmt.Sprintf("%.2fGB", float64(m.SizeMB)/1024)
	}
	return fmt.Sprintf("%dMB", m.SizeMB)
}

func magnetQualityOf(name string, tags []string) string {
	upper := strings.ToUpper(name)
	joined := strings.Join(tags, " ")
	switch {
	case strings.Contains(upper, "4K") || strings.Contains(upper, "2160"):
		return "4K"
	case strings.Contains(upper, "1080") || strings.Contains(joined, "HD"):
		return "HD"
	case strings.Contains(upper, "720"):
		return "720P"
	default:
		return ""
	}
}

func magnetHasSubtitle(name string, tags []string) bool {
	if strings.Contains(name, "中字") || strings.Contains(name, "字幕") ||
		strings.HasSuffix(strings.ToUpper(name), "-C") ||
		strings.Contains(strings.ToUpper(name), "-C ") {
		return true
	}
	for _, t := range tags {
		if strings.Contains(t, "字幕") || strings.Contains(t, "中字") {
			return true
		}
	}
	return false
}

var (
	magnetItemRe = regexp.MustCompile(`(?s)<div class="item (?:odd|even)"[^>]*data-size="(\d+)"[^>]*data-files="(\d+)"[^>]*data-date="(\d*)"[^>]*>(.*?)</div>\s*<div class="buttons">`)
	magnetLinkRe = regexp.MustCompile(`href="(magnet:\?xt=urn:btih:[^"]+)"`)
	magnetNameRe = regexp.MustCompile(`(?s)<span class="name">(.*?)</span>`)
	magnetTagRe  = regexp.MustCompile(`(?s)<span class="tag[^"]*">(.*?)</span>`)
)

func parseMagnets(htmlText string) []magnetEntry {
	start := strings.Index(htmlText, `id="magnets"`)
	if start < 0 {
		return nil
	}
	seg := htmlText[start:]
	if end := strings.Index(seg, `magnet-help-modal`); end > 0 {
		seg = seg[:end]
	}
	out := []magnetEntry{}
	for _, match := range magnetItemRe.FindAllStringSubmatch(seg, -1) {
		sizeMB, _ := strconv.ParseInt(match[1], 10, 64)
		files, _ := strconv.Atoi(match[2])
		block := match[4]
		link := magnetLinkRe.FindStringSubmatch(block)
		if link == nil {
			continue
		}
		name := ""
		if nm := magnetNameRe.FindStringSubmatch(block); nm != nil {
			name = strings.TrimSpace(stripTags(nm[1]))
		}
		tags := []string{}
		for _, tm := range magnetTagRe.FindAllStringSubmatch(block, -1) {
			if t := strings.TrimSpace(stripTags(tm[1])); t != "" {
				tags = append(tags, t)
			}
		}
		entry := magnetEntry{
			Name:   name,
			URL:    link[1],
			SizeMB: sizeMB,
			Files:  files,
			Date:   match[3],
			Tags:   tags,
		}
		entry.Quality = magnetQualityOf(name, tags)
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SizeMB > out[j].SizeMB })
	return out
}

func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func magnetMaps(entries []magnetEntry) []interface{} {
	out := []interface{}{}
	for _, e := range entries {
		tags := e.Tags
		if magnetHasSubtitle(e.Name, e.Tags) {
			found := false
			for _, t := range tags {
				if strings.Contains(t, "字幕") {
					found = true
				}
			}
			if !found {
				tags = append(append([]string{}, tags...), "中文字幕")
			}
		}
		row := []interface{}{}
		for _, t := range tags {
			row = append(row, t)
		}
		out = append(out, map[string]interface{}{
			"name":      e.Name,
			"url":       e.URL,
			"size":      e.SizeMB * 1024 * 1024,
			"sizeMB":    e.SizeMB,
			"sizeLabel": e.SizeLabel(),
			"files":     e.Files,
			"date":      e.Date,
			"quality":   e.Quality,
			"tags":      row,
		})
	}
	return out
}
