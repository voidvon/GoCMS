package nginxguard

import (
	"errors"
	"strings"
)

type configToken struct {
	value  string
	start  int
	end    int
	symbol bool
}

// Keep offsets into the original file so panel-owned directives are preserved
// byte for byte. Comments, quoted strings and variable braces are not blocks.
func configTokens(data []byte) ([]configToken, error) {
	var tokens []configToken
	for i := 0; i < len(data); {
		if strings.ContainsRune(" \t\r\n", rune(data[i])) {
			i++
			continue
		}
		if data[i] == '#' {
			for i < len(data) && data[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		if strings.ContainsRune("{};", rune(data[i])) {
			i++
			tokens = append(tokens, configToken{value: string(data[start:i]), start: start, end: i, symbol: true})
			continue
		}
		var value strings.Builder
		for i < len(data) && !strings.ContainsRune(" \t\r\n{};#", rune(data[i])) {
			if data[i] == '\\' && i+1 < len(data) {
				value.WriteByte(data[i+1])
				i += 2
				continue
			}
			if data[i] == '\'' || data[i] == '"' {
				quote := data[i]
				i++
				for i < len(data) && data[i] != quote {
					if data[i] == '\\' && i+1 < len(data) {
						i++
					}
					value.WriteByte(data[i])
					i++
				}
				if i == len(data) {
					return nil, errors.New("Nginx 配置存在未闭合的引号")
				}
				i++
				continue
			}
			if data[i] == '$' && i+1 < len(data) && data[i+1] == '{' {
				end := i + 2
				for end < len(data) && data[end] != '}' {
					end++
				}
				if end == len(data) {
					return nil, errors.New("Nginx 配置存在未闭合的变量")
				}
				value.Write(data[i : end+1])
				i = end + 1
				continue
			}
			value.WriteByte(data[i])
			i++
		}
		tokens = append(tokens, configToken{value: value.String(), start: start, end: i})
	}
	return tokens, nil
}

type httpServer struct {
	start int
	end   int
	names []string
}

func httpServers(data []byte) ([]httpServer, error) {
	tokens, err := configTokens(data)
	if err != nil {
		return nil, err
	}
	var servers []httpServer
	var stack []string
	var statement []configToken
	active, serverDepth := -1, 0
	for _, token := range tokens {
		if !token.symbol {
			statement = append(statement, token)
			continue
		}
		switch token.value {
		case "{":
			name := ""
			if len(statement) > 0 {
				name = statement[0].value
			}
			if name == "server" && (len(stack) == 0 || len(stack) == 1 && stack[0] == "http") {
				servers = append(servers, httpServer{start: token.end})
				active, serverDepth = len(servers)-1, len(stack)+1
			}
			stack = append(stack, name)
			statement = nil
		case "}":
			if len(stack) == 0 {
				return nil, errors.New("Nginx 配置块未配对")
			}
			if active >= 0 && len(stack) == serverDepth {
				servers[active].end = token.start
				active = -1
			}
			stack = stack[:len(stack)-1]
			statement = nil
		case ";":
			if active >= 0 && len(stack) == serverDepth && len(statement) > 0 && statement[0].value == "server_name" {
				for _, name := range statement[1:] {
					servers[active].names = append(servers[active].names, strings.ToLower(name.value))
				}
			}
			statement = nil
		}
	}
	if len(stack) > 0 || len(statement) > 0 {
		return nil, errors.New("Nginx 配置块或指令未结束")
	}
	return servers, nil
}
