package bridge

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(t *testing.T, upstream string) Config {
	t.Helper()
	parsed, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	return Config{
		ListenAddr:          "127.0.0.1:8787",
		UpstreamURL:         parsed,
		UpstreamTimeout:     5 * time.Second,
		MaxRequestBytes:     8 * 1024 * 1024,
		MaxResponseBytes:    8 * 1024 * 1024,
		MaxReferenceImages:  10,
		MaxConcurrent:       2,
		MaxScalarFieldBytes: 1 * 1024 * 1024,
		AllowHTTPUpstream:   true,
	}
}

func newTestBridge(t *testing.T, upstream *httptest.Server, logs *bytes.Buffer) *Server {
	t.Helper()
	server, err := NewServer(testConfig(t, upstream.URL), logs, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	server.SetHTTPClient(upstream.Client())
	return server
}

func TestGenerationTranslationAndMIME(t *testing.T) {
	var received map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Fatalf("unexpected upstream path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization was not forwarded exactly: %q", got)
		}
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&received); err != nil {
			t.Fatal(err)
		}
		if _, exists := received["response_format"]; exists {
			t.Fatal("response_format was not removed")
		}
		if _, exists := received["stream"]; exists {
			t.Fatal("stream was not removed")
		}
		if _, exists := received["size"]; exists {
			t.Fatal("size=auto should be removed")
		}
		if _, exists := received["provider"]; !exists {
			t.Fatal("additional provider parameter was not preserved")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1,"data":[{"b64_json":"UklGRg==","media_type":"image/webp"}],"usage":{"cost":0.01}}`)
	}))
	defer upstream.Close()

	logs := &bytes.Buffer{}
	bridge := newTestBridge(t, upstream, logs)
	requestBody := `{"model":"openai/gpt-image-2","prompt":"test","n":1,"size":"auto","response_format":"b64_json","stream":true,"provider":{"sort":"price"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(requestBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	data := payload["data"].([]any)[0].(map[string]any)
	if got := data["b64_json"].(string); got != "data:image/webp;base64,UklGRg==" {
		t.Fatalf("unexpected normalized image: %s", got)
	}
	if strings.Contains(logs.String(), "test-key") || strings.Contains(logs.String(), "prompt") {
		t.Fatalf("logs exposed secret or prompt: %s", logs.String())
	}
}

func TestEditMultipartTranslation(t *testing.T) {
	imageBytes := makePNG(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer edit-key" {
			t.Fatalf("authorization was not forwarded exactly: %q", got)
		}
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["model"] != "openai/gpt-image-1" || payload["prompt"] != "change the sky" {
			t.Fatalf("core fields were not preserved: %#v", payload)
		}
		refs, ok := payload["input_references"].([]any)
		if !ok || len(refs) != 1 {
			t.Fatalf("expected one input reference: %#v", payload["input_references"])
		}
		ref := refs[0].(map[string]any)
		imageURL := ref["image_url"].(map[string]any)["url"].(string)
		if !strings.HasPrefix(imageURL, "data:image/png;base64,") {
			t.Fatalf("unexpected reference URL: %s", imageURL[:minInt(len(imageURL), 64)])
		}
		if _, exists := payload["response_format"]; exists {
			t.Fatal("response_format was not removed")
		}
		w.Header().Set("Content-Type", "application/json")
		encoded := base64.StdEncoding.EncodeToString(imageBytes)
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"`+encoded+`","media_type":"image/png"}]}`)
	}))
	defer upstream.Close()

	logs := &bytes.Buffer{}
	bridge := newTestBridge(t, upstream, logs)
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("model", "openai/gpt-image-1")
	_ = writer.WriteField("prompt", "change the sky")
	_ = writer.WriteField("n", "1")
	_ = writer.WriteField("size", "1024x1024")
	_ = writer.WriteField("response_format", "b64_json")
	part, err := writer.CreateFormFile("image", "source.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(imageBytes); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer edit-key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(logs.String(), "edit-key") || strings.Contains(logs.String(), "change the sky") {
		t.Fatalf("logs exposed secret or prompt: %s", logs.String())
	}
}

func TestMissingAuthorization(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("upstream should not be called")
	}))
	defer upstream.Close()
	bridge := newTestBridge(t, upstream, &bytes.Buffer{})
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"x/y","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", response.Code)
	}
}

func TestUpstreamErrorPassThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"rate limited"}}`)
	}))
	defer upstream.Close()
	bridge := newTestBridge(t, upstream, &bytes.Buffer{})
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"x/y","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", response.Code)
	}
	if response.Header().Get("Retry-After") != "30" {
		t.Fatal("Retry-After was not preserved")
	}
	if !strings.Contains(response.Body.String(), "rate limited") {
		t.Fatalf("upstream body was not preserved: %s", response.Body.String())
	}
}

func TestURLResponseIsBlocked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"url":"https://example.invalid/image.png"}]}`)
	}))
	defer upstream.Close()
	bridge := newTestBridge(t, upstream, &bytes.Buffer{})
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"x/y","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", response.Code, response.Body.String())
	}
}

func TestHealth(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	bridge := newTestBridge(t, upstream, &bytes.Buffer{})
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
}

func makePNG(t *testing.T) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	if err := png.Encode(buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestEditMultipleReferencesPreserveOrder(t *testing.T) {
	first := makePNG(t)
	second := append([]byte(nil), first...)
	second[len(second)-1] ^= 0x01

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		decoder := json.NewDecoder(r.Body)
		decoder.UseNumber()
		if err := decoder.Decode(&payload); err != nil {
			t.Fatal(err)
		}
		refs, ok := payload["input_references"].([]any)
		if !ok || len(refs) != 2 {
			t.Fatalf("expected two ordered references, got %#v", payload["input_references"])
		}
		gotFirst := decodeReference(t, refs[0])
		gotSecond := decodeReference(t, refs[1])
		if !bytes.Equal(gotFirst, first) || !bytes.Equal(gotSecond, second) {
			t.Fatal("reference image order or content changed")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"b64_json":"UklGRg==","media_type":"image/webp"}]}`)
	}))
	defer upstream.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	_ = writer.WriteField("model", "openai/gpt-image-1")
	_ = writer.WriteField("prompt", "combine the references")
	for index, item := range [][]byte{first, second} {
		field := "image"
		if index == 1 {
			field = "image[]"
		}
		part, err := writer.CreateFormFile(field, "reference.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(item); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	bridge := newTestBridge(t, upstream, &bytes.Buffer{})
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}
}

func TestGenerationRejectsInvalidN(t *testing.T) {
	for _, n := range []string{"0", "11", "1.5", `"2"`} {
		t.Run(n, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("upstream should not be called")
			}))
			defer upstream.Close()
			bridge := newTestBridge(t, upstream, &bytes.Buffer{})
			body := `{"model":"x/y","prompt":"p","n":` + n + `}`
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer key")
			response := httptest.NewRecorder()
			bridge.Handler().ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d: %s", response.Code, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), "invalid_n") {
				t.Fatalf("expected invalid_n error, got %s", response.Body.String())
			}
		})
	}
}

func TestGenerationRejectsOversizedRequest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("upstream should not be called")
	}))
	defer upstream.Close()
	cfg := testConfig(t, upstream.URL)
	cfg.MaxRequestBytes = 1024
	cfg.MaxScalarFieldBytes = 1024
	bridge, err := NewServer(cfg, &bytes.Buffer{}, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	bridge.SetHTTPClient(upstream.Client())
	body := `{"model":"x/y","prompt":"` + strings.Repeat("p", 2048) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d: %s", response.Code, response.Body.String())
	}
}

func TestRedirectIsBlockedWithoutCredentialForwarding(t *testing.T) {
	var redirectTargetCalled atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirectTargetCalled.Store(true)
	}))
	defer target.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, target.URL, http.StatusTemporaryRedirect)
	}))
	defer upstream.Close()

	bridge, err := NewServer(testConfig(t, upstream.URL), &bytes.Buffer{}, BuildInfo{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"x/y","prompt":"p"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer secret-key")
	response := httptest.NewRecorder()
	bridge.Handler().ServeHTTP(response, req)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d: %s", response.Code, response.Body.String())
	}
	if redirectTargetCalled.Load() {
		t.Fatal("redirect target was called")
	}
	if !strings.Contains(response.Body.String(), "upstream_redirect_blocked") {
		t.Fatalf("unexpected response: %s", response.Body.String())
	}
}

func TestNonImageDataURLResponseIsBlocked(t *testing.T) {
	for _, encoded := range []string{
		"data:text/plain;base64,SGVsbG8=",
		"data:image/png,not-base64",
		"data:image/png;base64,",
	} {
		t.Run(encoded, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				payload := map[string]any{"data": []any{map[string]any{"b64_json": encoded}}}
				_ = json.NewEncoder(w).Encode(payload)
			}))
			defer upstream.Close()
			bridge := newTestBridge(t, upstream, &bytes.Buffer{})
			req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(`{"model":"x/y","prompt":"p"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer key")
			response := httptest.NewRecorder()
			bridge.Handler().ServeHTTP(response, req)
			if response.Code != http.StatusBadGateway {
				t.Fatalf("expected 502, got %d: %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestVersionEndpoint(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	server, err := NewServer(testConfig(t, upstream.URL), &bytes.Buffer{}, BuildInfo{
		Version:   "1.2.3",
		Commit:    "abc123",
		BuildDate: "2026-08-28T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/version", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.Code)
	}
	var got BuildInfo
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.2.3" || got.Commit != "abc123" || got.BuildDate == "" {
		t.Fatalf("unexpected build info: %#v", got)
	}
}

func decodeReference(t *testing.T, reference any) []byte {
	t.Helper()
	item, ok := reference.(map[string]any)
	if !ok {
		t.Fatalf("invalid reference shape: %#v", reference)
	}
	imageURL, ok := item["image_url"].(map[string]any)
	if !ok {
		t.Fatalf("invalid image_url shape: %#v", item)
	}
	value, ok := imageURL["url"].(string)
	if !ok {
		t.Fatalf("invalid reference URL: %#v", imageURL)
	}
	_, encoded, found := strings.Cut(value, ",")
	if !found {
		t.Fatalf("invalid data URL: %s", value)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}
