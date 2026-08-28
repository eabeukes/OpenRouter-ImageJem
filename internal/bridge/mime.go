package bridge

import (
	"bytes"
	"encoding/base64"
	"mime"
	"net/http"
	"strings"
)

func canonicalImageMediaType(declared string, data []byte) (string, bool) {
	if declared != "" {
		if parsed, _, err := mime.ParseMediaType(declared); err == nil {
			declared = strings.ToLower(parsed)
		}
	}
	if declared == "image/jpg" {
		declared = "image/jpeg"
	}
	if strings.HasPrefix(declared, "image/") {
		return declared, true
	}

	detected := strings.ToLower(http.DetectContentType(firstBytes(data, 512)))
	if detected == "image/jpg" {
		detected = "image/jpeg"
	}
	if strings.HasPrefix(detected, "image/") {
		return detected, true
	}
	if looksLikeSVG(data) {
		return "image/svg+xml", true
	}
	return "", false
}

func inferBase64ImageMediaType(encoded string) string {
	if strings.HasPrefix(encoded, "data:") {
		header := strings.SplitN(encoded, ",", 2)[0]
		mediaType := strings.TrimPrefix(strings.SplitN(header, ";", 2)[0], "data:")
		if strings.HasPrefix(mediaType, "image/") {
			return mediaType
		}
	}

	prefixLen := len(encoded)
	if prefixLen > 4096 {
		prefixLen = 4096
	}
	prefixLen -= prefixLen % 4
	if prefixLen <= 0 {
		return "image/png"
	}
	decoded := make([]byte, base64.StdEncoding.DecodedLen(prefixLen))
	n, err := base64.StdEncoding.Decode(decoded, []byte(encoded[:prefixLen]))
	if err != nil {
		return "image/png"
	}
	decoded = decoded[:n]
	if mediaType, ok := canonicalImageMediaType("", decoded); ok {
		return mediaType
	}
	return "image/png"
}

func looksLikeSVG(data []byte) bool {
	trimmed := bytes.TrimSpace(firstBytes(data, 4096))
	lower := bytes.ToLower(trimmed)
	return bytes.HasPrefix(lower, []byte("<svg")) ||
		(bytes.HasPrefix(lower, []byte("<?xml")) && bytes.Contains(lower, []byte("<svg")))
}

func firstBytes(data []byte, n int) []byte {
	if len(data) <= n {
		return data
	}
	return data[:n]
}
