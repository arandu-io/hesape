package rpc

import (
	"net/http"
	"slices"
	"strings"
)

var exposedHeaders = []string{
	"Grpc-Message",
	"Grpc-Status",
	"Grpc-Status-Details-Bin",
}

func corsHandler(path string, next http.Handler, origins, headers []string) http.Handler {
	originSet := normalizedSet(origins)
	headerSet := normalizedSet(headers)
	canonicalHeaders := make([]string, 0, len(headers))
	for _, header := range headers {
		canonicalHeaders = append(canonicalHeaders, http.CanonicalHeaderKey(strings.TrimSpace(header)))
	}
	allowedHeaders := strings.Join(canonicalHeaders, ", ")
	exposed := strings.Join(exposedHeaders, ", ")

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.URL.Path, path) {
			next.ServeHTTP(response, request)
			return
		}
		origin := request.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(response, request)
			return
		}
		if !slices.Contains(originSet, strings.ToLower(origin)) {
			http.Error(response, "origin is not allowed", http.StatusForbidden)
			return
		}

		response.Header().Add("Vary", "Origin")
		response.Header().Set("Access-Control-Allow-Origin", origin)
		response.Header().Set("Access-Control-Expose-Headers", exposed)

		if request.Method != http.MethodOptions {
			next.ServeHTTP(response, request)
			return
		}
		response.Header().Add("Vary", "Access-Control-Request-Method")
		response.Header().Add("Vary", "Access-Control-Request-Headers")
		if !strings.EqualFold(request.Header.Get("Access-Control-Request-Method"), http.MethodPost) || !headersAllowed(request.Header.Values("Access-Control-Request-Headers"), headerSet) {
			http.Error(response, "CORS preflight is not allowed", http.StatusForbidden)
			return
		}
		response.Header().Set("Access-Control-Allow-Methods", http.MethodPost)
		response.Header().Set("Access-Control-Allow-Headers", allowedHeaders)
		response.WriteHeader(http.StatusNoContent)
	})
}

func normalizedSet(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, strings.ToLower(strings.TrimSpace(value)))
	}
	return result
}

func headersAllowed(lines []string, allowed []string) bool {
	for _, line := range lines {
		for header := range strings.SplitSeq(line, ",") {
			header = strings.ToLower(strings.TrimSpace(header))
			if header != "" && !slices.Contains(allowed, header) {
				return false
			}
		}
	}
	return true
}
