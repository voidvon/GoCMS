package routing

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	DefaultRootListPath  = "valve"
	DefaultChildListPath = "Products"
	DefaultListPattern   = "{id}.html"
	DefaultDetailPath    = "Product"
	DefaultDetailPattern = "{id}.html"
)

// NormalizeDirectory validates a route directory stored in the database. It
// remains relative to the published site root and can contain nested folders.
func NormalizeDirectory(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		return "", fmt.Errorf("目录不能为空")
	}
	clean := path.Clean(value)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("目录不能包含上级路径")
	}
	for _, segment := range strings.Split(clean, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("目录层级无效")
		}
		for _, char := range segment {
			if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '-' || char == '.' {
				continue
			}
			return "", fmt.Errorf("目录只能包含字母、数字、中文、下划线、短横线和点号")
		}
	}
	return clean, nil
}

func NormalizeFilePattern(value string, allowPage bool) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, `\`, "/"))
	if value == "" {
		return "", fmt.Errorf("文件名规则不能为空")
	}
	if strings.Contains(value, "/") || strings.Contains(value, "\\") || strings.ContainsAny(value, "?#%") {
		return "", fmt.Errorf("文件名规则不能包含目录或 URL 特殊字符")
	}
	if !strings.Contains(value, "{id}") {
		return "", fmt.Errorf("文件名规则必须包含 {id}")
	}
	if strings.Contains(value, "{page}") && !allowPage {
		return "", fmt.Errorf("详情页文件名规则不支持 {page}")
	}
	for index := 0; index < len(value); {
		if value[index] == '{' {
			end := strings.IndexByte(value[index:], '}')
			if end < 0 {
				return "", fmt.Errorf("文件名规则占位符不完整")
			}
			end += index
			placeholder := value[index : end+1]
			if placeholder != "{id}" && (!allowPage || placeholder != "{page}") {
				return "", fmt.Errorf("不支持的文件名占位符 %s", placeholder)
			}
			index = end + 1
			continue
		}
		char, size := utf8.DecodeRuneInString(value[index:])
		if char == utf8.RuneError && size == 1 {
			return "", fmt.Errorf("文件名规则包含无效字符")
		}
		if !(unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '-' || char == '.') {
			return "", fmt.Errorf("文件名规则包含无效字符")
		}
		index += size
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return "", fmt.Errorf("文件名规则不能以点号开头或结尾")
	}
	return value, nil
}

func DefaultListPath(parentID int64) string {
	if parentID == 0 {
		return DefaultRootListPath
	}
	return DefaultChildListPath
}

func RenderDetailFilename(pattern string, productID int64) (string, error) {
	pattern, err := NormalizeFilePattern(pattern, false)
	if err != nil {
		return "", err
	}
	return strings.ReplaceAll(pattern, "{id}", strconv.FormatInt(productID, 10)), nil
}

func RenderListFilename(pattern string, categoryID int64, page int) (string, error) {
	pattern, err := NormalizeFilePattern(pattern, false)
	if err != nil {
		return "", err
	}
	filename := strings.ReplaceAll(pattern, "{id}", strconv.FormatInt(categoryID, 10))
	if page == 1 {
		return filename, nil
	}
	dot := strings.LastIndexByte(filename, '.')
	if dot < 0 {
		return filename + "-" + strconv.Itoa(page), nil
	}
	return filename[:dot] + "-" + strconv.Itoa(page) + filename[dot:], nil
}

func RenderListPageFilename(pattern string, categoryID int64, page int) (string, error) {
	pattern, err := NormalizeFilePattern(pattern, false)
	if err != nil {
		return "", err
	}
	filename := strings.ReplaceAll(pattern, "{id}", strconv.FormatInt(categoryID, 10))
	dot := strings.LastIndexByte(filename, '.')
	if dot < 0 {
		return filename + "-" + strconv.Itoa(page), nil
	}
	return filename[:dot] + "-" + strconv.Itoa(page) + filename[dot:], nil
}
