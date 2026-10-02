// Package pdfcraft is the official Go client for the PDFCraft API: HTML to
// PDF, and PDF to structured JSON, in one call.
//
//	client, err := pdfcraft.New(os.Getenv("PDFCRAFT_API_KEY"))
//	pdf, err := client.Render(ctx, pdfcraft.RenderRequest{HTML: "<h1>hello</h1>"})
//
// Zero dependencies beyond the standard library. Every failure is a *Error;
// switch on its Code, which is the stable part of the contract.
//
// Mirrors the TypeScript and Python SDKs method for method — same retry
// curve, same Retry-After handling, same error codes — so the docs can show
// the same call in three languages and mean it.
package pdfcraft

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Version is this client's version, reported in the User-Agent.
const Version = "1.5.0"

const (
	defaultBaseURL    = "https://api.pdfcraft.dev"
	defaultMaxRetries = 3
	// Just past the API's own 120s ceiling, so a server-side timeout surfaces
	// as the API's render_timeout — which says what happened — rather than as
	// a client-side deadline, which says nothing.
	defaultTimeout = 130 * time.Second
)

// As is errors.As, re-exported so callers can type-assert a *Error without
// importing errors alongside this package for one line.
var As = errors.As

// Client talks to the PDFCraft API. Safe for concurrent use.
type Client struct {
	apiKey     string
	baseURL    string
	maxRetries int
	httpClient *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API host, for testing or a self-hosted gateway.
func WithBaseURL(raw string) Option {
	return func(c *Client) { c.baseURL = trimSlash(raw) }
}

// WithMaxRetries sets how many times a retryable failure is re-sent. Zero
// disables retrying entirely.
func WithMaxRetries(n int) Option {
	return func(c *Client) { c.maxRetries = n }
}

// WithHTTPClient supplies your own http.Client — for a proxy, a custom
// transport, or a shorter deadline.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.httpClient = h }
}

// New returns a Client. The API key is required.
func New(apiKey string, opts ...Option) (*Client, error) {
	if apiKey == "" {
		return nil, newError("invalid_api_key", "An API key is required.", 401, "")
	}
	c := &Client{
		apiKey:     apiKey,
		baseURL:    defaultBaseURL,
		maxRetries: defaultMaxRetries,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// ── rendering ──────────────────────────────────────────────────────────────

// Render returns the PDF bytes.
func (c *Client) Render(ctx context.Context, req RenderRequest, idempotencyKey ...string) ([]byte, error) {
	body, err := withOutput(req, "binary")
	if err != nil {
		return nil, err
	}
	response, err := c.do(ctx, http.MethodPost, "/v1/render", body, key(idempotencyKey))
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	return io.ReadAll(response.Body)
}

// RenderToURL stores the PDF and returns a signed link instead of the bytes.
func (c *Client) RenderToURL(ctx context.Context, req RenderRequest, idempotencyKey ...string) (*RenderURLResponse, error) {
	body, err := withOutput(req, "url")
	if err != nil {
		return nil, err
	}
	return decode[RenderURLResponse](c.do(ctx, http.MethodPost, "/v1/render", body, key(idempotencyKey)))
}

// RenderAsync queues a render; the webhook fires when it settles.
func (c *Client) RenderAsync(ctx context.Context, req AsyncRenderRequest, idempotencyKey ...string) (*Accepted, error) {
	return decode[Accepted](c.do(ctx, http.MethodPost, "/v1/render/async", req, key(idempotencyKey)))
}

// GetRender polls one render.
func (c *Client) GetRender(ctx context.Context, id string) (*StatusResponse, error) {
	return decode[StatusResponse](c.do(ctx, http.MethodGet, "/v1/renders/"+url.PathEscape(id), nil, ""))
}

// Usage reports the current period's usage and quota.
func (c *Client) Usage(ctx context.Context) (*UsageResponse, error) {
	return decode[UsageResponse](c.do(ctx, http.MethodGet, "/v1/usage", nil, ""))
}

// ── extraction ─────────────────────────────────────────────────────────────

// Extract reads a PDF into tables and labelled fields, each with a bounding box.
//
// Billed per page read. There is no OCR: a scan has no text layer and comes
// back as extraction_failed, unbilled. Nothing is guessed by a model, so the
// same document always produces the same answer.
func (c *Client) Extract(ctx context.Context, req ExtractRequest, idempotencyKey ...string) (*ExtractResponse, error) {
	body, err := withOutput(req, "inline")
	if err != nil {
		return nil, err
	}
	return decode[ExtractResponse](c.do(ctx, http.MethodPost, "/v1/extract", body, key(idempotencyKey)))
}

// ExtractPDF is the common case: hand it PDF bytes and it does the base64.
func (c *Client) ExtractPDF(ctx context.Context, pdf []byte, req ExtractRequest, idempotencyKey ...string) (*ExtractResponse, error) {
	req.File = base64.StdEncoding.EncodeToString(pdf)
	return c.Extract(ctx, req, idempotencyKey...)
}

// ExtractToURL stores the JSON and returns a signed link.
func (c *Client) ExtractToURL(ctx context.Context, req ExtractRequest, idempotencyKey ...string) (*RenderURLResponse, error) {
	body, err := withOutput(req, "url")
	if err != nil {
		return nil, err
	}
	return decode[RenderURLResponse](c.do(ctx, http.MethodPost, "/v1/extract", body, key(idempotencyKey)))
}

// ExtractAsync queues an extraction.
func (c *Client) ExtractAsync(ctx context.Context, req AsyncExtractRequest, idempotencyKey ...string) (*Accepted, error) {
	return decode[Accepted](c.do(ctx, http.MethodPost, "/v1/extract/async", req, key(idempotencyKey)))
}

// GetExtraction polls one extraction. An extraction id is not a render id:
// GetRender will 404 on one and this will 404 on a render id, deliberately.
func (c *Client) GetExtraction(ctx context.Context, id string) (*StatusResponse, error) {
	return decode[StatusResponse](c.do(ctx, http.MethodGet, "/v1/extractions/"+url.PathEscape(id), nil, ""))
}

// ── accessibility ──────────────────────────────────────────────────────────

// Scan starts an accessibility scan and returns at once with an id to poll.
//
// A scan of a thousand documents at one request per second per host has a
// floor measured in minutes, so there is nothing to return but an id and
// somewhere to look:
//
//	scan, err := client.Scan(ctx, pdfcraft.ScanRequest{
//		Source:  pdfcraft.ScanSource{Domain: "example.gov"},
//		Options: &pdfcraft.ScanOptions{MaxDocuments: 500},
//	})
//	for scan.Status != "succeeded" && scan.Status != "failed" {
//		time.Sleep(10 * time.Second)
//		scan, err = client.GetScan(ctx, scan.ID)
//	}
//
// MaxDocuments is clamped to your plan rather than refused; the gap shows up
// as Discovered minus Checked.
func (c *Client) Scan(ctx context.Context, req ScanRequest) (*Accepted, error) {
	return decode[Accepted](c.do(ctx, http.MethodPost, "/v1/a11y/scan", req, ""))
}

// GetScan polls one scan: progress while it runs, every document ranked with
// findings and cost once it succeeds.
func (c *Client) GetScan(ctx context.Context, id string) (*ScanResponse, error) {
	return decode[ScanResponse](c.do(ctx, http.MethodGet, "/v1/a11y/scans/"+url.PathEscape(id), nil, ""))
}

// RenderRaw posts an arbitrary body to /v1/render and returns the raw
// response. The escape hatch for a contract addition the structs do not carry
// yet, so an API change never blocks a caller on an SDK release.
func (c *Client) RenderRaw(ctx context.Context, body any) (*http.Response, error) {
	return c.do(ctx, http.MethodPost, "/v1/render", body, "")
}

// ExtractRaw is RenderRaw for /v1/extract.
func (c *Client) ExtractRaw(ctx context.Context, body any) (*http.Response, error) {
	return c.do(ctx, http.MethodPost, "/v1/extract", body, "")
}

// ── transport ──────────────────────────────────────────────────────────────

func (c *Client) do(ctx context.Context, method, path string, body any, idempotencyKey string) (*http.Response, error) {
	var payload []byte
	if body != nil {
		var err error
		payload, err = json.Marshal(body)
		if err != nil {
			return nil, newError("invalid_request", fmt.Sprintf("could not encode request: %v", err), 400, "")
		}
	}

	var last *Error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		var reader io.Reader
		if payload != nil {
			reader = bytes.NewReader(payload)
		}
		request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
		if err != nil {
			return nil, newError("invalid_request", err.Error(), 400, "")
		}
		request.Header.Set("authorization", "Bearer "+c.apiKey)
		request.Header.Set("user-agent", "pdfcraft-sdk-go/"+Version)
		if payload != nil {
			request.Header.Set("content-type", "application/json")
		}
		if idempotencyKey != "" {
			request.Header.Set("idempotency-key", idempotencyKey)
		}

		response, err := c.httpClient.Do(request)
		if err != nil {
			// A cancelled context is the caller's decision, not a transient
			// failure. Retrying it would ignore them and burn the deadline.
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			last = newError("network_error", fmt.Sprintf("could not reach PDFCraft: %v", err), 0, "")
			if attempt == c.maxRetries {
				return nil, last
			}
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}

		if response.StatusCode < 300 {
			return response, nil
		}

		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		last = toError(response.StatusCode, raw)
		if !last.Retryable || attempt == c.maxRetries {
			return nil, last
		}
		wait := retryAfter(response)
		if wait == 0 {
			wait = backoff(attempt)
		}
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
	}

	if last != nil {
		return nil, last
	}
	return nil, newError("internal_error", "request failed", 500, "")
}

func decode[T any](response *http.Response, err error) (*T, error) {
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	var out T
	if err := json.NewDecoder(response.Body).Decode(&out); err != nil {
		return nil, newError("internal_error", fmt.Sprintf("could not decode response: %v", err), 0, "")
	}
	return &out, nil
}

// withOutput re-encodes a typed request into a map so the output mode can be
// added. The method decides the mode, and the structs deliberately have no
// Output field for it to contradict.
func withOutput(req any, mode string) (map[string]any, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, newError("invalid_request", err.Error(), 400, "")
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, newError("invalid_request", err.Error(), 400, "")
	}
	out["output"] = mode
	return out, nil
}

func key(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

// backoff is exponential with jitter, matching the other two SDKs' curve.
func backoff(attempt int) time.Duration {
	base := 500 * math.Pow(2, float64(attempt))
	return time.Duration(base*(0.75+rand.Float64()*0.5)) * time.Millisecond
}

func retryAfter(response *http.Response) time.Duration {
	header := response.Header.Get("retry-after")
	if header == "" {
		return 0
	}
	seconds, err := strconv.ParseFloat(header, 64)
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds * float64(time.Second))
}

// sleep waits, but gives up the moment the caller's context does.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
