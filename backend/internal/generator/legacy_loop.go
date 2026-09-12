package generator

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var legacyLoopPattern = regexp.MustCompile(`(?is)\[e:loop=\{([^{}]*)\}\](.*?)\[/e:loop\]`)

// expandLegacyLoopSyntax converts the data-only subset of EmpireCMS e:loop
// into Go template syntax. It deliberately rejects PHP and arbitrary SQL.
func expandLegacyLoopSyntax(source string) (string, error) {
	if !strings.Contains(strings.ToLower(source), "[e:loop=") {
		return source, nil
	}
	var firstErr error
	result := legacyLoopPattern.ReplaceAllStringFunc(source, func(block string) string {
		if firstErr != nil {
			return block
		}
		matches := legacyLoopPattern.FindStringSubmatch(block)
		if len(matches) != 3 {
			firstErr = fmt.Errorf("invalid [e:loop] block")
			return block
		}
		args, err := legacyLoopArguments(matches[1])
		if err != nil {
			firstErr = err
			return block
		}
		body := matches[2]
		lower := strings.ToLower(body)
		if strings.Contains(lower, "<?") || strings.Contains(lower, "?>") || strings.Contains(lower, "$bqr") || strings.Contains(lower, "$bqsr") {
			firstErr = fmt.Errorf("[e:loop] only accepts Go template bodies; PHP variables and code are not supported")
			return block
		}
		if args.imageOnly {
			return fmt.Sprintf(`{{range contentItemsWithImage %d %d %t %t true %q}}%s{{end}}`, args.categoryID, args.limit, args.descendants, args.featured, args.order, body)
		}
		return fmt.Sprintf(`{{range contentItems %d %d %t %t %q}}%s{{end}}`, args.categoryID, args.limit, args.descendants, args.featured, args.order, body)
	})
	if firstErr != nil {
		return "", firstErr
	}
	if strings.Contains(strings.ToLower(result), "[e:loop=") {
		return "", fmt.Errorf("nested or incomplete [e:loop] blocks are not supported")
	}
	return result, nil
}

type legacyLoopArgs struct {
	categoryID, limit     int
	descendants, featured bool
	imageOnly             bool
	order                 string
}

func legacyLoopArguments(raw string) (legacyLoopArgs, error) {
	parts := strings.Split(raw, ",")
	if len(parts) < 2 || len(parts) > 6 {
		return legacyLoopArgs{}, fmt.Errorf("[e:loop] expects category, limit, operation, image flag, condition and order")
	}
	categoryID, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || categoryID < 0 {
		return legacyLoopArgs{}, fmt.Errorf("[e:loop] category ID must be a non-negative integer")
	}
	limit, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || limit < 1 || limit > 500 {
		return legacyLoopArgs{}, fmt.Errorf("[e:loop] limit must be between 1 and 500")
	}
	operation := 2
	if len(parts) > 2 && strings.TrimSpace(parts[2]) != "" {
		operation, err = strconv.Atoi(strings.TrimSpace(parts[2]))
		if err != nil {
			return legacyLoopArgs{}, fmt.Errorf("[e:loop] operation must be numeric")
		}
	}
	if operation != 2 {
		return legacyLoopArgs{}, fmt.Errorf("[e:loop] only operation type 2 (栏目) is supported")
	}
	featured := false
	imageOnly := false
	if len(parts) > 3 && strings.TrimSpace(parts[3]) != "" {
		imageFlag := strings.TrimSpace(parts[3])
		if imageFlag != "0" && imageFlag != "1" {
			return legacyLoopArgs{}, fmt.Errorf("[e:loop] image flag must be 0 or 1")
		}
		imageOnly = imageFlag == "1"
	}
	if len(parts) > 4 && strings.TrimSpace(parts[4]) != "" {
		return legacyLoopArgs{}, fmt.Errorf("[e:loop] arbitrary SQL conditions are not supported")
	}
	order := "sort"
	if len(parts) > 5 && strings.TrimSpace(parts[5]) != "" {
		value := strings.ToLower(strings.TrimSpace(parts[5]))
		switch value {
		case "newstime desc", "id desc", "newest":
			order = "newest"
		case "newstime asc", "id asc", "oldest":
			order = "oldest"
		default:
			return legacyLoopArgs{}, fmt.Errorf("[e:loop] unsupported order %q", parts[5])
		}
	}
	return legacyLoopArgs{categoryID: categoryID, limit: limit, descendants: true, featured: featured, imageOnly: imageOnly, order: order}, nil
}
