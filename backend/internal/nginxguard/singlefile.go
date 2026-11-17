package nginxguard

import (
	"fmt"
	"strings"
	"time"
)

const httpBegin = "# GOCMS-SECURITY-HTTP-BEGIN\n"
const httpEnd = "# GOCMS-SECURITY-HTTP-END\n"
const serverBegin = "\n# GOCMS-SECURITY-SERVER-BEGIN\n"
const serverEnd = "# GOCMS-SECURITY-SERVER-END\n"

// SingleFileFragments renders the two managed sections in the same site file.
func SingleFileFragments(s Snapshot, now time.Time, logPath string) (string, string, error) {
	h, server, err := Render(s, now, logPath)
	if err != nil {
		return "", "", err
	}
	h = strings.ReplaceAll(h, "gocms_security", "gocms_site_security")
	server = strings.ReplaceAll(server, "gocms_security", "gocms_site_security")
	return httpBegin + h + httpEnd, serverBegin + server + serverEnd, nil
}

func removeManagedBlock(text, begin, end string) (string, error) {
	starts, ends := strings.Count(text, begin), strings.Count(text, end)
	if starts == 0 && ends == 0 {
		return text, nil
	}
	start, stop := strings.Index(text, begin), strings.Index(text, end)
	if starts != 1 || ends != 1 || stop < start {
		return "", fmt.Errorf("安全配置标记不完整或重复：%s", strings.TrimSpace(begin))
	}
	return text[:start] + text[stop+len(end):], nil
}

// RenderSingleFile keeps all enforcement in the selected site file. That file
// must be included by Nginx in the http context, as BaoTa site files normally are.
func RenderSingleFile(data []byte, s Snapshot, now time.Time, logPath string) ([]byte, error) {
	text, err := removeManagedBlock(string(data), httpBegin, httpEnd)
	if err != nil {
		return nil, err
	}
	text, err = removeManagedBlock(text, serverBegin, serverEnd)
	if err != nil {
		return nil, err
	}
	offset, err := blockOffset([]byte(text), "server")
	if err != nil {
		return nil, err
	}
	h, server, err := SingleFileFragments(s, now, logPath)
	if err != nil {
		return nil, err
	}
	return []byte(h + text[:offset] + server + text[offset:]), nil
}
