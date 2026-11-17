package nginxguard

import (
	"errors"
	"fmt"
	"strings"
)

func blockOffset(data []byte, block string) (int, error) {
	start, _, err := blockBounds(data, block)
	return start, err
}

func blockBounds(data []byte, block string) (int, int, error) {
	depth, count, offset, end := 0, 0, -1, -1
	previous := ""
	for i := 0; i < len(data); {
		c := data[i]
		if c == '#' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote := c
			i++
			for i < len(data) {
				if data[i] == '\\' {
					i += 2
					continue
				}
				if data[i] == quote {
					i++
					break
				}
				i++
			}
			previous = ""
			continue
		}
		if c == '{' {
			if depth == 0 && previous == block {
				count++
				offset = i + 1
			}
			depth++
			previous = ""
			i++
			continue
		}
		if c == '}' {
			depth--
			if depth < 0 {
				return 0, 0, errors.New("unbalanced Nginx blocks")
			}
			if depth == 0 && offset >= 0 && end < 0 {
				end = i
			}
			previous = ""
			i++
			continue
		}
		if c == ';' {
			previous = ""
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			i++
			continue
		}
		start := i
		for i < len(data) && !strings.ContainsRune(" \t\r\n{};#\"'", rune(data[i])) {
			if data[i] == '\\' && i+1 < len(data) {
				i++
			}
			i++
		}
		if start == i {
			i++
		}
		previous = string(data[start:i])
	}
	if depth != 0 || count != 1 {
		return 0, 0, fmt.Errorf("expected exactly one top-level %s block, found %d", block, count)
	}
	return offset, end, nil
}
