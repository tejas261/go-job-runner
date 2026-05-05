package utils

import (
	"io"
	"log"
	"net/http"
	"strings"
)

func CheckNilError(err error) {
	if err != nil {
		log.Println(err)
	}
}

func EnsureURLScheme(rawURL string) string {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}

	return "https://" + trimmed
}

func MakeResponseReadable(response *http.Response) string {
	bodyBytes, _ := io.ReadAll(response.Body)
	return string(bodyBytes)
}
