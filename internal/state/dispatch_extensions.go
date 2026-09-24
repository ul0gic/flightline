package state

import (
	"fmt"
	"strings"
)

func isIAPCommercePath(path string) bool {
	return strings.HasPrefix(path, "/spec/iap/products/") && strings.Contains(path, "/commerce/")
}

func isBetaGroupBuildsPath(path string) bool {
	return strings.HasPrefix(path, "/spec/testflight/groups/") && strings.HasSuffix(path, "/builds")
}

func isPreviewPath(path string) bool {
	if strings.HasPrefix(path, "/spec/previews/locales/") {
		return true
	}
	return strings.HasPrefix(path, "/spec/customProductPages/") && strings.Contains(path, "/previews/")
}

func isScreenshotOrderPath(path string) bool {
	if strings.HasPrefix(path, "/spec/screenshots/locales/") {
		return strings.HasSuffix(path, "/order")
	}
	return strings.HasPrefix(path, "/spec/customProductPages/") &&
		strings.Contains(path, "/screenshots/") && strings.HasSuffix(path, "/order")
}

func unescapeJSONPointerToken(token string) (string, error) {
	var out strings.Builder
	for index := 0; index < len(token); index++ {
		if token[index] != '~' {
			out.WriteByte(token[index])
			continue
		}
		if index+1 >= len(token) {
			return "", fmt.Errorf("invalid JSON pointer escape in %q", token)
		}
		index++
		switch token[index] {
		case '0':
			out.WriteByte('~')
		case '1':
			out.WriteByte('/')
		default:
			return "", fmt.Errorf("invalid JSON pointer escape in %q", token)
		}
	}
	return out.String(), nil
}
