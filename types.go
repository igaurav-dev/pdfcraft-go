package pdfcraft

// Request and response shapes.
//
// Concrete structs rather than map[string]any, because the point of a Go SDK
// is that the compiler catches a misspelled field before a customer does.
// Every optional field is a pointer or omitempty so that "unset" and "set to
// the zero value" stay distinguishable — sending `"landscape": false` when the
// caller said nothing is how a default quietly changes meaning.
//
// Anything the structs do not cover goes through RenderRaw or ExtractRaw,
// which take any JSON-marshalable body. That escape hatch exists so a contract
// addition never blocks a caller on an SDK release.

// Margin is a page margin. Values are CSS lengths: "20mm", "0.5in", "18px".
type Margin struct {
	Top    string `json:"top,omitempty"`
	Right  string `json:"right,omitempty"`
	Bottom string `json:"bottom,omitempty"`
	Left   string `json:"left,omitempty"`
}

// WaitFor describes when the page is considered ready to print.
type WaitFor struct {
	Selector    string `json:"selector,omitempty"`
	NetworkIdle *bool  `json:"networkIdle,omitempty"`
	DelayMs     *int   `json:"delayMs,omitempty"`
}

// RenderOptions are the page options. See PageFormats and EmulateMedia in
// contract_gen.go for the accepted enum values.
type RenderOptions struct {
	Format          string   `json:"format,omitempty"`
	Landscape       *bool    `json:"landscape,omitempty"`
	Margin          *Margin  `json:"margin,omitempty"`
	Scale           *float64 `json:"scale,omitempty"`
	PrintBackground *bool    `json:"printBackground,omitempty"`
	PageRanges      string   `json:"pageRanges,omitempty"`
	HeaderHTML      string   `json:"headerHtml,omitempty"`
	FooterHTML      string   `json:"footerHtml,omitempty"`
	WaitFor         *WaitFor `json:"waitFor,omitempty"`
	EmulateMedia    string   `json:"emulateMedia,omitempty"`
	TimeoutMs       *int     `json:"timeoutMs,omitempty"`
}

// Cookie is passed through when rendering an authenticated URL.
type Cookie struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Domain string `json:"domain,omitempty"`
	Path   string `json:"path,omitempty"`
}

// RenderRequest renders HTML or a URL. Set exactly one of HTML or URL.
//
// Output is deliberately absent: the method you call decides it, so a field
// here could only contradict the call.
type RenderRequest struct {
	HTML     string            `json:"html,omitempty"`
	URL      string            `json:"url,omitempty"`
	Options  *RenderOptions    `json:"options,omitempty"`
	Filename string            `json:"filename,omitempty"`
	Headers  map[string]string `json:"headers,omitempty"`
	Cookies  []Cookie          `json:"cookies,omitempty"`
}

// AsyncRenderRequest queues a render and calls WebhookURL when it settles.
type AsyncRenderRequest struct {
	RenderRequest
	WebhookURL string `json:"webhookUrl,omitempty"`
}

// ExtractOptions narrows what is read. Pages narrows the bill as well as the
// work, because extraction is billed per page read.
type ExtractOptions struct {
	Pages     string `json:"pages,omitempty"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

// ExtractRequest reads a PDF into structured JSON. Set exactly one of File,
// HTML or URL. File takes base64 PDF bytes or an https URL to one; pass HTML
// or URL instead and the page is rendered first, then extracted.
//
// Schema is left as a free-form map because the contract accepts both a string
// shorthand and a full field object per key, and a Go type that modelled both
// would be worse to use than the map.
type ExtractRequest struct {
	File    string          `json:"file,omitempty"`
	HTML    string          `json:"html,omitempty"`
	URL     string          `json:"url,omitempty"`
	Options *ExtractOptions `json:"options,omitempty"`
	Schema  map[string]any  `json:"schema,omitempty"`
}

// AsyncExtractRequest queues an extraction.
type AsyncExtractRequest struct {
	ExtractRequest
	WebhookURL string `json:"webhookUrl,omitempty"`
}

// RenderURLResponse is returned by RenderToURL.
type RenderURLResponse struct {
	ID         string `json:"id"`
	URL        string `json:"url"`
	ExpiresAt  string `json:"expires_at"`
	Pages      int    `json:"pages"`
	Bytes      int64  `json:"bytes"`
	DurationMs int    `json:"duration_ms"`
}

// Accepted is returned by the async endpoints.
type Accepted struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// StatusResponse is one render or extraction, polled.
type StatusResponse struct {
	ID         string `json:"id"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	Pages      int    `json:"pages"`
	Bytes      int64  `json:"bytes"`
	DurationMs int    `json:"duration_ms"`
	ErrorCode  string `json:"error_code,omitempty"`
	URL        string `json:"url,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// UsageResponse is the current period's usage and quota.
type UsageResponse struct {
	Plan        string `json:"plan"`
	Used        int    `json:"used"`
	Limit       int    `json:"limit"`
	Overage     int    `json:"overage"`
	ResetsAt    string `json:"resets_at"`
	PeriodStart string `json:"period_start"`
}

// Bbox is a bounding box in PDF points, origin top-left.
type Bbox struct {
	Page int     `json:"page"`
	X0   float64 `json:"x0"`
	Y0   float64 `json:"y0"`
	X1   float64 `json:"x1"`
	Y1   float64 `json:"y1"`
}

// Table is one reconstructed table.
type Table struct {
	Page             int                 `json:"page"`
	Header           []string            `json:"header,omitempty"`
	HeaderConfidence float64             `json:"header_confidence"`
	Rows             [][]string          `json:"rows"`
	RowsAsObjects    []map[string]string `json:"rows_as_objects,omitempty"`
	Confidence       float64             `json:"confidence"`
	Bbox             *Bbox               `json:"bbox,omitempty"`
}

// Field is one labelled key/value found on the page.
type Field struct {
	Value      any     `json:"value"`
	Raw        string  `json:"raw,omitempty"`
	Confidence float64 `json:"confidence"`
	Bbox       *Bbox   `json:"bbox,omitempty"`
}

// ExtractResponse is a PDF read into structure.
type ExtractResponse struct {
	ID               string           `json:"id"`
	Pages            int              `json:"pages"`
	Tables           []Table          `json:"tables"`
	Fields           map[string]Field `json:"fields,omitempty"`
	Text             string           `json:"text,omitempty"`
	TablesSuppressed int              `json:"tables_suppressed,omitempty"`
	DurationMs       int              `json:"duration_ms"`
}

// ── accessibility ──────────────────────────────────────────────────────────

// ScanSource says what to scan. Exactly one field, the same "one input" rule
// RenderRequest enforces: a body naming two sources has no correct reading,
// and picking one silently is how a customer scans the wrong thing.
type ScanSource struct {
	// Domain is a bare hostname or an origin. Sitemap first, bounded crawl as fallback.
	Domain string `json:"domain,omitempty"`
	// Sitemap is an explicit sitemap URL, for when it is not where we would look.
	Sitemap string `json:"sitemap,omitempty"`
	// URLs is an explicit list, for when you already know the documents.
	URLs []string `json:"urls,omitempty"`
}

// ScanOptions tunes a scan within what the plan allows.
type ScanOptions struct {
	// MaxDocuments is clamped to the plan, never refused. The remainder is
	// reported as Discovered minus Checked.
	MaxDocuments int `json:"max_documents,omitempty"`
	CrawlDepth   int `json:"crawl_depth,omitempty"`
	// RespectRobots may only be true. robots.txt is obeyed without exception;
	// sending false is an error rather than a setting that is quietly ignored,
	// because believing you had turned it off would be worse than not being
	// offered the field at all.
	RespectRobots *bool `json:"respect_robots,omitempty"`
}

// ScanRequest starts an accessibility scan.
type ScanRequest struct {
	Source  ScanSource   `json:"source"`
	Options *ScanOptions `json:"options,omitempty"`
}

// Finding is one thing wrong with one document.
type Finding struct {
	CheckID string `json:"check_id"`
	// Severity is one of A11ySeverities, ordered worst first.
	Severity string `json:"severity"`
	// Pages is empty for a document-level finding: "not tagged" is not on a page.
	Pages   []int  `json:"pages"`
	Message string `json:"message"`
	// TechnicalDetail carries the spec citation and the concrete fix.
	TechnicalDetail string `json:"technical_detail"`
	// Occurrences is volume, not severity. One check failing 1,535 times is one
	// thing wrong, fixed once — never rank on this.
	Occurrences int      `json:"occurrences"`
	WCAG        []string `json:"wcag"`
	// Layer is "machine" or "geometric". The second is the half a conformance
	// validator cannot do.
	Layer string `json:"layer"`
}

// ScanDocument is one checked PDF.
type ScanDocument struct {
	URL      string `json:"url"`
	Pages    *int   `json:"pages"`
	Severity string `json:"severity"`
	// Score is 0-100, lower is worse, and moves with consequence rather than
	// with the count of failed checks.
	Score       int     `json:"score"`
	CostLowUSD  float64 `json:"cost_low_usd"`
	CostHighUSD float64 `json:"cost_high_usd"`
	// Priority is severity weighted by reach. Sort on this to answer "what do
	// I fix first" — it is the one number a single-document checker cannot give.
	Priority float64   `json:"priority"`
	Findings []Finding `json:"findings"`
}

// ScanResponse is a scan's progress, then its full result.
type ScanResponse struct {
	ID string `json:"id"`
	// Status is one of ScanStatuses. Poll until it is terminal.
	Status string `json:"status"`
	// Discovered is every PDF the crawl found, which may exceed Checked when
	// the plan capped the scan. The gap is the upgrade prompt.
	Discovered *int `json:"discovered"`
	Checked    int  `json:"checked"`
	Failing    int  `json:"failing"`
	// ReportURL is a share token. Anyone holding it can read the report with
	// no account, so treat it as a credential.
	ReportURL        string         `json:"report_url,omitempty"`
	TotalCostLowUSD  float64        `json:"total_cost_low_usd,omitempty"`
	TotalCostHighUSD float64        `json:"total_cost_high_usd,omitempty"`
	Documents        []ScanDocument `json:"documents,omitempty"`
}
