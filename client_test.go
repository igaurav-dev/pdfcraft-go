package pdfcraft

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A scripted server, so the interesting cases — which statuses we retry, which
// we do not, and what we put on the wire — are tested against a real socket
// rather than against a mock of our own transport.
type step struct {
	status  int
	body    string
	headers map[string]string
}

type capture struct {
	path    string
	headers http.Header
	body    map[string]any
}

func serve(t *testing.T, steps ...step) (*httptest.Server, *[]capture) {
	t.Helper()
	seen := make([]capture, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var parsed map[string]any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &parsed)
		}
		seen = append(seen, capture{path: r.URL.Path, headers: r.Header.Clone(), body: parsed})

		current := step{status: 200, body: "%PDF-1.7 ok"}
		if len(steps) > 0 {
			index := len(seen) - 1
			if index >= len(steps) {
				index = len(steps) - 1
			}
			current = steps[index]
		}
		for k, v := range current.headers {
			w.Header().Set(k, v)
		}
		w.WriteHeader(current.status)
		_, _ = w.Write([]byte(current.body))
	}))
	t.Cleanup(server.Close)
	return server, &seen
}

func mustClient(t *testing.T, base string, opts ...Option) *Client {
	t.Helper()
	c, err := New("sk_live_test", append([]Option{WithBaseURL(base)}, opts...)...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	_, err := New("")
	var typed *Error
	if !errors.As(err, &typed) || typed.Code != "invalid_api_key" {
		t.Fatalf("want invalid_api_key, got %v", err)
	}
}

func TestRenderSendsTheRightRequest(t *testing.T) {
	server, seen := serve(t)
	pdf, err := mustClient(t, server.URL).Render(
		context.Background(),
		RenderRequest{HTML: "<h1>hi</h1>"},
		"abc-123",
	)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(pdf[:4]) != "%PDF" {
		t.Fatalf("not a PDF: %q", pdf[:8])
	}

	got := (*seen)[0]
	if got.path != "/v1/render" {
		t.Errorf("path = %q", got.path)
	}
	if got.headers.Get("authorization") != "Bearer sk_live_test" {
		t.Errorf("authorization = %q", got.headers.Get("authorization"))
	}
	if got.headers.Get("idempotency-key") != "abc-123" {
		t.Errorf("idempotency-key = %q", got.headers.Get("idempotency-key"))
	}
	if ua := got.headers.Get("user-agent"); ua != "pdfcraft-sdk-go/"+Version {
		t.Errorf("user-agent = %q", ua)
	}
	// The method decides the output mode; the struct has no field for it.
	if got.body["output"] != "binary" {
		t.Errorf("output = %v", got.body["output"])
	}
}

func TestOmittedOptionalsAreNotSent(t *testing.T) {
	// "unset" and "set to false" must stay distinguishable: sending
	// landscape:false when the caller said nothing quietly overrides a default.
	server, seen := serve(t)
	_, _ = mustClient(t, server.URL).Render(
		context.Background(),
		RenderRequest{HTML: "x", Options: &RenderOptions{Format: "A4"}},
	)
	options, _ := (*seen)[0].body["options"].(map[string]any)
	if _, present := options["landscape"]; present {
		t.Error("landscape was sent despite never being set")
	}
	if options["format"] != "A4" {
		t.Errorf("format = %v", options["format"])
	}
}

func TestMapsTheErrorEnvelope(t *testing.T) {
	server, _ := serve(t, step{
		status:  429,
		body:    `{"error":{"code":"quota_exceeded","message":"Monthly limit reached.","docs_url":"https://pdfcraft.dev/errors#quota_exceeded"}}`,
		headers: map[string]string{"content-type": "application/json"},
	})
	_, err := mustClient(t, server.URL, WithMaxRetries(0)).Render(context.Background(), RenderRequest{HTML: "x"})

	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("want *Error, got %T", err)
	}
	if typed.Code != "quota_exceeded" || typed.Status != 429 {
		t.Errorf("got %+v", typed)
	}
	if !typed.Retryable || !IsRetryable(err) {
		t.Error("a 429 should be retryable")
	}
	if typed.DocsURL == "" {
		t.Error("docs_url was dropped")
	}
}

func TestDoesNotRetryAPlain4xx(t *testing.T) {
	server, seen := serve(t, step{status: 400, body: `{"error":{"code":"invalid_request"}}`})
	_, err := mustClient(t, server.URL, WithMaxRetries(3)).Render(context.Background(), RenderRequest{HTML: "x"})
	if err == nil {
		t.Fatal("want an error")
	}
	if len(*seen) != 1 {
		t.Errorf("attempts = %d, want 1", len(*seen))
	}
}

func TestRetriesA500ThenSucceeds(t *testing.T) {
	server, seen := serve(t,
		step{status: 500, body: `{"error":{"code":"internal_error"}}`},
		step{status: 200, body: "%PDF-1.7 recovered"},
	)
	pdf, err := mustClient(t, server.URL, WithMaxRetries(3)).Render(context.Background(), RenderRequest{HTML: "x"})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if string(pdf[:4]) != "%PDF" || len(*seen) != 2 {
		t.Errorf("attempts = %d", len(*seen))
	}
}

func TestHonoursRetryAfter(t *testing.T) {
	server, seen := serve(t,
		step{status: 429, body: `{"error":{"code":"rate_limited"}}`, headers: map[string]string{"retry-after": "0"}},
		step{status: 200, body: "%PDF-1.7 ok"},
	)
	if _, err := mustClient(t, server.URL, WithMaxRetries(2)).Render(context.Background(), RenderRequest{HTML: "x"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if len(*seen) != 2 {
		t.Errorf("attempts = %d", len(*seen))
	}
}

func TestSurvivesANonJSONErrorBody(t *testing.T) {
	server, _ := serve(t, step{status: 502, body: "<html>Bad Gateway</html>"})
	_, err := mustClient(t, server.URL, WithMaxRetries(0)).Render(context.Background(), RenderRequest{HTML: "x"})
	var typed *Error
	if !errors.As(err, &typed) || typed.Status != 502 || typed.Code != "internal_error" {
		t.Fatalf("got %v", err)
	}
}

func TestACancelledContextIsNotRetried(t *testing.T) {
	server, seen := serve(t, step{status: 500, body: `{"error":{"code":"internal_error"}}`})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := mustClient(t, server.URL, WithMaxRetries(5)).Render(ctx, RenderRequest{HTML: "x"})
	if err == nil {
		t.Fatal("want an error")
	}
	// The caller's deadline is their decision, not a transient failure. Five
	// retries here would ignore them and burn the remaining time.
	if len(*seen) > 2 {
		t.Errorf("attempts = %d — the context deadline was ignored", len(*seen))
	}
}

func TestExtractPDFBase64sTheBytes(t *testing.T) {
	server, seen := serve(t, step{status: 200, body: `{"id":"ext_1","pages":1}`})
	result, err := mustClient(t, server.URL).ExtractPDF(context.Background(), []byte("%PDF-1.7 tiny"), ExtractRequest{})
	if err != nil {
		t.Fatalf("ExtractPDF: %v", err)
	}
	if result.ID != "ext_1" {
		t.Errorf("id = %q", result.ID)
	}
	body := (*seen)[0].body
	if body["output"] != "inline" {
		t.Errorf("output = %v", body["output"])
	}
	decoded, err := base64.StdEncoding.DecodeString(body["file"].(string))
	if err != nil || string(decoded) != "%PDF-1.7 tiny" {
		t.Errorf("file round-trip failed: %v %q", err, decoded)
	}
}

func TestExtractionIDsGoToTheExtractionEndpoint(t *testing.T) {
	server, seen := serve(t, step{status: 200, body: `{"id":"ext_1","status":"succeeded"}`})
	if _, err := mustClient(t, server.URL).GetExtraction(context.Background(), "ext_01ABC"); err != nil {
		t.Fatalf("GetExtraction: %v", err)
	}
	if (*seen)[0].path != "/v1/extractions/ext_01ABC" {
		t.Errorf("path = %q", (*seen)[0].path)
	}
}

func TestGeneratedContractIsPresent(t *testing.T) {
	// The generated file is how this module learns about a contract change. If
	// it is stale everything above still passes and a customer finds out.
	if len(ContractHash) != 64 {
		t.Fatalf("ContractHash = %q", ContractHash)
	}
	var found bool
	for _, code := range ErrorCodes {
		if code == "quota_exceeded" {
			found = true
		}
	}
	if !found {
		t.Error("quota_exceeded missing from the generated ErrorCodes")
	}
}

func TestScanPostsTheContractShape(t *testing.T) {
	server, seen := serve(t, step{status: 202, body: `{"id":"scn_1","status":"queued","discovered":null}`})
	accepted, err := mustClient(t, server.URL).Scan(context.Background(), ScanRequest{
		Source:  ScanSource{Domain: "example.gov"},
		Options: &ScanOptions{MaxDocuments: 500},
	})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if accepted.Status != "queued" {
		t.Errorf("status = %q", accepted.Status)
	}
	if (*seen)[0].path != "/v1/a11y/scan" {
		t.Errorf("path = %q", (*seen)[0].path)
	}
	// omitempty on every ScanSource field is what keeps an unset source out of
	// the body. Without it we would send {"domain":"","sitemap":"","urls":null}
	// and the API would reject it for naming three sources at once.
	source, _ := (*seen)[0].body["source"].(map[string]any)
	if len(source) != 1 || source["domain"] != "example.gov" {
		t.Errorf("source = %v", source)
	}
}

func TestScanIDsGoToTheScanEndpoint(t *testing.T) {
	server, seen := serve(t, step{status: 200, body: `{"id":"scn_1","status":"succeeded","checked":2}`})
	if _, err := mustClient(t, server.URL).GetScan(context.Background(), "scn_01ABC"); err != nil {
		t.Fatalf("GetScan: %v", err)
	}
	if (*seen)[0].path != "/v1/a11y/scans/scn_01ABC" {
		t.Errorf("path = %q", (*seen)[0].path)
	}
}

func TestAccessibilityEnumsAreGenerated(t *testing.T) {
	// A client comparing a severity against a hand-typed string silently stops
	// matching the day a value is added.
	if len(A11ySeverities) == 0 || A11ySeverities[0] != "blocker" {
		t.Errorf("A11ySeverities = %v", A11ySeverities)
	}
	if len(ScanStatuses) == 0 || len(FindingLayers) != 2 {
		t.Errorf("ScanStatuses = %v, FindingLayers = %v", ScanStatuses, FindingLayers)
	}
}
