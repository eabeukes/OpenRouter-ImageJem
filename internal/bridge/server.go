package bridge

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
}

type Server struct {
	cfg       Config
	client    *http.Client
	logger    *eventLogger
	semaphore chan struct{}
	build     BuildInfo
}

func NewServer(cfg Config, output io.Writer, build BuildInfo) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          20,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}

	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return &Server{
		cfg:       cfg,
		client:    client,
		logger:    newEventLogger(output),
		semaphore: make(chan struct{}, cfg.MaxConcurrent),
		build:     build,
	}, nil
}

// SetHTTPClient is intended for tests and controlled embedding.
func (s *Server) SetHTTPClient(client *http.Client) {
	s.client = client
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/readyz", s.handleHealth)
	mux.HandleFunc("/version", s.handleVersion)
	mux.HandleFunc("/v1/images/generations", s.withRequest("generation", s.handleGeneration))
	mux.HandleFunc("/images/generations", s.withRequest("generation", s.handleGeneration))
	mux.HandleFunc("/v1/images/edits", s.withRequest("edit", s.handleEdit))
	mux.HandleFunc("/images/edits", s.withRequest("edit", s.handleEdit))
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is allowed")
		return
	}
	writeJSON(w, http.StatusOK, s.build)
}

type requestHandler func(http.ResponseWriter, *http.Request, string) requestResult

type requestResult struct {
	Status         int
	UpstreamStatus int
	Model          string
	References     int
	Code           string
}

func (s *Server) withRequest(operation string, next requestHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Bridge-Request-ID", requestID)
		start := time.Now()

		select {
		case s.semaphore <- struct{}{}:
			defer func() { <-s.semaphore }()
		case <-r.Context().Done():
			writeAPIError(w, http.StatusServiceUnavailable, "request_cancelled", "Request was cancelled before processing")
			return
		}

		result := next(w, r, requestID)
		fields := map[string]any{
			"level":       "info",
			"event":       "image_request",
			"operation":   operation,
			"request_id":  requestID,
			"status":      result.Status,
			"duration_ms": time.Since(start).Milliseconds(),
		}
		if result.UpstreamStatus != 0 {
			fields["upstream_status"] = result.UpstreamStatus
		}
		if result.Model != "" {
			fields["model"] = result.Model
		}
		if result.References != 0 {
			fields["reference_count"] = result.References
		}
		if result.Code != "" {
			fields["code"] = result.Code
		}
		if result.Status >= 500 {
			fields["level"] = "error"
		} else if result.Status >= 400 {
			fields["level"] = "warning"
		}
		s.logger.event(fields)
	}
}

func (s *Server) handleGeneration(w http.ResponseWriter, r *http.Request, requestID string) requestResult {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return requestResult{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed"}
	}
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Expected application/json")
		return requestResult{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type"}
	}
	auth, ok := bearerHeader(r.Header.Get("Authorization"))
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "missing_api_key", "A Bearer API key is required")
		return requestResult{Status: http.StatusUnauthorized, Code: "missing_api_key"}
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxRequestBytes)
	payload, err := decodeJSONObject(r.Body)
	if err != nil {
		status, code := classifyReadError(err)
		writeAPIError(w, status, code, "Invalid JSON request")
		return requestResult{Status: status, Code: code}
	}

	model, prompt, err := validateCoreFields(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return requestResult{Status: http.StatusBadRequest, Model: model, Code: "invalid_request"}
	}
	if err := validateN(payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_n", err.Error())
		return requestResult{Status: http.StatusBadRequest, Model: model, Code: "invalid_n"}
	}
	_ = prompt

	delete(payload, "response_format")
	delete(payload, "stream")
	if size, ok := payload["size"].(string); ok && strings.EqualFold(strings.TrimSpace(size), "auto") {
		delete(payload, "size")
	}

	status, upstreamStatus, code := s.forwardAndWrite(w, r.Context(), auth, payload, requestID)
	return requestResult{Status: status, UpstreamStatus: upstreamStatus, Model: model, Code: code}
}

func (s *Server) handleEdit(w http.ResponseWriter, r *http.Request, requestID string) requestResult {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is allowed")
		return requestResult{Status: http.StatusMethodNotAllowed, Code: "method_not_allowed"}
	}
	auth, ok := bearerHeader(r.Header.Get("Authorization"))
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "missing_api_key", "A Bearer API key is required")
		return requestResult{Status: http.StatusUnauthorized, Code: "missing_api_key"}
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Expected multipart/form-data")
		return requestResult{Status: http.StatusUnsupportedMediaType, Code: "unsupported_media_type"}
	}

	r.Body = http.MaxBytesReader(w, r.Body, s.cfg.MaxRequestBytes)
	reader, err := r.MultipartReader()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_multipart", "Invalid multipart request")
		return requestResult{Status: http.StatusBadRequest, Code: "invalid_multipart"}
	}
	payload, referenceCount, err := s.parseEditMultipart(reader)
	if err != nil {
		status, code, message := classifyMultipartError(err)
		writeAPIError(w, status, code, message)
		return requestResult{Status: status, References: referenceCount, Code: code}
	}

	model, _, err := validateCoreFields(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return requestResult{Status: http.StatusBadRequest, Model: model, References: referenceCount, Code: "invalid_request"}
	}
	if err := validateN(payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_n", err.Error())
		return requestResult{Status: http.StatusBadRequest, Model: model, References: referenceCount, Code: "invalid_n"}
	}

	status, upstreamStatus, code := s.forwardAndWrite(w, r.Context(), auth, payload, requestID)
	return requestResult{Status: status, UpstreamStatus: upstreamStatus, Model: model, References: referenceCount, Code: code}
}

var (
	errTooManyReferences = errors.New("too many reference images")
	errMissingImage      = errors.New("at least one image is required")
	errUnsupportedFile   = errors.New("unsupported file field")
	errUnsupportedImage  = errors.New("file is not a supported image")
	errFieldTooLarge     = errors.New("multipart field is too large")
	errMaskUnsupported   = errors.New("mask editing is not available through the current OpenWebUI request shape")
)

func (s *Server) parseEditMultipart(reader *multipart.Reader) (map[string]any, int, error) {
	payload := make(map[string]any)
	references := make([]any, 0, 1)

	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, len(references), err
		}

		name := part.FormName()
		filename := part.FileName()
		switch name {
		case "image", "image[]":
			if len(references) >= s.cfg.MaxReferenceImages {
				_ = part.Close()
				return nil, len(references), errTooManyReferences
			}
			data, readErr := readLimited(part, s.cfg.MaxRequestBytes)
			_ = part.Close()
			if readErr != nil {
				return nil, len(references), readErr
			}
			if len(data) == 0 {
				return nil, len(references), errUnsupportedImage
			}
			mediaType, ok := canonicalImageMediaType(part.Header.Get("Content-Type"), data)
			if !ok {
				return nil, len(references), errUnsupportedImage
			}
			dataURL := "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
			references = append(references, map[string]any{
				"type": "image_url",
				"image_url": map[string]any{
					"url": dataURL,
				},
			})
		case "mask", "mask[]":
			_ = part.Close()
			return nil, len(references), errMaskUnsupported
		default:
			if filename != "" {
				_ = part.Close()
				return nil, len(references), errUnsupportedFile
			}
			value, readErr := readLimited(part, s.cfg.MaxScalarFieldBytes)
			_ = part.Close()
			if readErr != nil {
				return nil, len(references), errFieldTooLarge
			}
			setMultipartField(payload, name, string(value))
		}
	}

	if len(references) == 0 {
		return nil, 0, errMissingImage
	}
	delete(payload, "response_format")
	delete(payload, "stream")
	if size, ok := payload["size"].(string); ok && strings.EqualFold(strings.TrimSpace(size), "auto") {
		delete(payload, "size")
	}
	payload["input_references"] = references
	return payload, len(references), nil
}

func setMultipartField(payload map[string]any, name, raw string) {
	value := strings.TrimSpace(raw)
	if name == "" || name == "response_format" || name == "stream" {
		return
	}
	if value == "" && name != "prompt" {
		return
	}

	switch name {
	case "n", "seed", "output_compression":
		if parsed, err := strconv.ParseInt(value, 10, 64); err == nil {
			payload[name] = parsed
			return
		}
	}
	if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		var parsed any
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.UseNumber()
		if err := decoder.Decode(&parsed); err == nil {
			payload[name] = parsed
			return
		}
	}
	payload[name] = raw
}

func (s *Server) forwardAndWrite(w http.ResponseWriter, parent context.Context, auth string, payload map[string]any, requestID string) (int, int, string) {
	body, err := json.Marshal(payload)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "Request could not be encoded")
		return http.StatusBadRequest, 0, "invalid_request"
	}

	ctx, cancel := context.WithTimeout(parent, s.cfg.UpstreamTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.UpstreamURL.String(), bytes.NewReader(body))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_request_failed", "Could not create the OpenRouter request")
		return http.StatusBadGateway, 0, "upstream_request_failed"
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "owui-openrouter-image-bridge/"+safeVersion(s.build.Version))
	req.Header.Set("X-Bridge-Request-ID", requestID)

	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			writeAPIError(w, http.StatusGatewayTimeout, "upstream_timeout", "OpenRouter did not respond before the timeout")
			return http.StatusGatewayTimeout, 0, "upstream_timeout"
		}
		writeAPIError(w, http.StatusBadGateway, "upstream_unavailable", "Could not reach OpenRouter")
		return http.StatusBadGateway, 0, "upstream_unavailable"
	}
	defer resp.Body.Close()

	responseBody, err := readLimited(resp.Body, s.cfg.MaxResponseBytes)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "upstream_response_too_large", "OpenRouter response exceeded the configured limit")
		return http.StatusBadGateway, resp.StatusCode, "upstream_response_too_large"
	}

	copySafeUpstreamHeaders(w.Header(), resp.Header)
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		writeAPIError(w, http.StatusBadGateway, "upstream_redirect_blocked", "OpenRouter returned an unexpected redirect")
		return http.StatusBadGateway, resp.StatusCode, "upstream_redirect_blocked"
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "application/json"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(responseBody)
		return resp.StatusCode, resp.StatusCode, "upstream_error"
	}

	normalized, err := normalizeImageResponse(responseBody)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "invalid_upstream_response", err.Error())
		return http.StatusBadGateway, resp.StatusCode, "invalid_upstream_response"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(normalized)
	return http.StatusOK, resp.StatusCode, ""
}

func normalizeImageResponse(body []byte) ([]byte, error) {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var response map[string]any
	if err := decoder.Decode(&response); err != nil {
		return nil, fmt.Errorf("OpenRouter returned invalid JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("OpenRouter returned more than one JSON value")
	}
	data, ok := response["data"].([]any)
	if !ok || len(data) == 0 {
		return nil, fmt.Errorf("OpenRouter response did not contain image data")
	}
	for index, entry := range data {
		item, ok := entry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("OpenRouter image %d had an invalid shape", index)
		}
		encoded, ok := item["b64_json"].(string)
		if !ok || strings.TrimSpace(encoded) == "" {
			if _, hasURL := item["url"]; hasURL {
				return nil, fmt.Errorf("OpenRouter returned a URL response, which the bridge blocks to protect the API key")
			}
			return nil, fmt.Errorf("OpenRouter image %d did not contain b64_json", index)
		}
		if strings.HasPrefix(encoded, "data:") {
			header, data, found := strings.Cut(encoded, ",")
			if !found {
				return nil, fmt.Errorf("OpenRouter image %d contained an invalid data URL", index)
			}
			parts := strings.Split(header, ";")
			mediaType := strings.TrimPrefix(parts[0], "data:")
			if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
				return nil, fmt.Errorf("OpenRouter image %d contained a non-image data URL", index)
			}
			base64Encoded := false
			for _, parameter := range parts[1:] {
				if strings.EqualFold(strings.TrimSpace(parameter), "base64") {
					base64Encoded = true
					break
				}
			}
			if !base64Encoded || strings.TrimSpace(data) == "" {
				return nil, fmt.Errorf("OpenRouter image %d contained a non-base64 data URL", index)
			}
		} else {
			mediaType, _ := item["media_type"].(string)
			if !strings.HasPrefix(strings.ToLower(mediaType), "image/") {
				mediaType = inferBase64ImageMediaType(encoded)
			}
			item["b64_json"] = "data:" + mediaType + ";base64," + encoded
		}
		delete(item, "url")
	}
	return json.Marshal(response)
}

func validateCoreFields(payload map[string]any) (string, string, error) {
	model, _ := payload["model"].(string)
	prompt, _ := payload["prompt"].(string)
	model = strings.TrimSpace(model)
	prompt = strings.TrimSpace(prompt)
	if model == "" {
		return model, prompt, fmt.Errorf("model is required")
	}
	if prompt == "" {
		return model, prompt, fmt.Errorf("prompt is required")
	}
	payload["model"] = model
	payload["prompt"] = prompt
	return model, prompt, nil
}

func validateN(payload map[string]any) error {
	value, ok := payload["n"]
	if !ok {
		return nil
	}
	var n int64
	switch typed := value.(type) {
	case json.Number:
		parsed, err := typed.Int64()
		if err != nil {
			return fmt.Errorf("n must be an integer between 1 and 10")
		}
		n = parsed
	case float64:
		n = int64(typed)
		if float64(n) != typed {
			return fmt.Errorf("n must be an integer between 1 and 10")
		}
	case int:
		n = int64(typed)
	case int64:
		n = typed
	default:
		return fmt.Errorf("n must be an integer between 1 and 10")
	}
	if n < 1 || n > 10 {
		return fmt.Errorf("n must be an integer between 1 and 10")
	}
	payload["n"] = n
	return nil
}

func bearerHeader(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) <= len("Bearer ") || !strings.EqualFold(trimmed[:len("Bearer ")], "Bearer ") {
		return "", false
	}
	if strings.TrimSpace(trimmed[len("Bearer "):]) == "" {
		return "", false
	}
	return trimmed, true
}

func decodeJSONObject(reader io.Reader) (map[string]any, error) {
	decoder := json.NewDecoder(reader)
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("JSON object is required")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("multiple JSON values are not allowed")
		}
		return nil, err
	}
	return payload, nil
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, &http.MaxBytesError{Limit: limit}
	}
	return data, nil
}

func classifyReadError(err error) (int, string) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return http.StatusRequestEntityTooLarge, "request_too_large"
	}
	return http.StatusBadRequest, "invalid_request"
}

func classifyMultipartError(err error) (int, string, string) {
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		return http.StatusRequestEntityTooLarge, "request_too_large", "Multipart request exceeded the configured limit"
	case errors.Is(err, errTooManyReferences):
		return http.StatusBadRequest, "too_many_references", "Too many reference images"
	case errors.Is(err, errMissingImage):
		return http.StatusBadRequest, "missing_image", "At least one image is required"
	case errors.Is(err, errUnsupportedFile):
		return http.StatusBadRequest, "unsupported_file_field", "Only image and image[] file fields are supported"
	case errors.Is(err, errUnsupportedImage):
		return http.StatusUnsupportedMediaType, "unsupported_image", "Uploaded content was not recognised as an image"
	case errors.Is(err, errFieldTooLarge):
		return http.StatusRequestEntityTooLarge, "field_too_large", "A multipart field exceeded the configured limit"
	case errors.Is(err, errMaskUnsupported):
		return http.StatusBadRequest, "mask_not_supported", errMaskUnsupported.Error()
	default:
		return http.StatusBadRequest, "invalid_multipart", "Invalid multipart request"
	}
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

func copySafeUpstreamHeaders(destination, source http.Header) {
	for _, name := range []string{"Retry-After", "X-Request-ID", "X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"} {
		if value := source.Get(name); value != "" {
			destination.Set(name, value)
		}
	}
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    "bridge_error",
			"code":    code,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func newRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buffer)
}

func safeVersion(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "dev"
	}
	return value
}
