# PDFCraft for Go

HTML to PDF, and PDF to structured JSON, in one call. The official Go client for
[PDFCraft](https://pdfcraft.dev).

**No dependencies beyond the standard library.**

```bash
go get github.com/igaurav-dev/pdfcraft-go
```

## Render

```go
package main

import (
	"context"
	"os"

	"github.com/igaurav-dev/pdfcraft-go"
)

func main() {
	client, err := pdfcraft.New(os.Getenv("PDFCRAFT_API_KEY"))
	if err != nil {
		panic(err)
	}

	pdf, err := client.Render(context.Background(), pdfcraft.RenderRequest{
		HTML: "<h1>Invoice 1042</h1>",
	})
	if err != nil {
		panic(err)
	}
	os.WriteFile("invoice.pdf", pdf, 0o644)
}
```

A URL with page options. Optional fields are pointers so that *unset* and *set to the zero
value* stay distinguishable — sending `landscape: false` when you said nothing would quietly
override a default:

```go
landscape := true
pdf, err := client.Render(ctx, pdfcraft.RenderRequest{
	URL: "https://example.com/report",
	Options: &pdfcraft.RenderOptions{
		Format:    "A4",
		Landscape: &landscape,
		Margin:    &pdfcraft.Margin{Top: "20mm", Bottom: "20mm"},
		WaitFor:   &pdfcraft.WaitFor{Selector: "#chart-ready"},
	},
	Filename: "report.pdf",
})
```

A signed link instead of the bytes:

```go
result, err := client.RenderToURL(ctx, pdfcraft.RenderRequest{HTML: html})
// result.URL, result.ExpiresAt, result.Pages
```

## Extract

```go
raw, _ := os.ReadFile("statement.pdf")

data, err := client.ExtractPDF(ctx, raw, pdfcraft.ExtractRequest{})
for _, table := range data.Tables {
	fmt.Println(table.Header, len(table.Rows), table.Confidence)
}
```

Ask for named fields:

```go
data, err := client.Extract(ctx, pdfcraft.ExtractRequest{
	File: base64PDF,
	Schema: map[string]any{
		"invoice_total": "currency",
		"due_date":      "date",
	},
})
fmt.Println(data.Fields["invoice_total"].Value)
```

There is **no OCR**. A scan has no text layer, comes back as `extraction_failed`, and is not
billed. Nothing is guessed by a model, so the same document always produces the same answer.

Billed per page read — `Options: &pdfcraft.ExtractOptions{Pages: "1-3"}` narrows the bill as
well as the work.

## Errors

Every failure is a `*pdfcraft.Error`. Switch on `Code`, which is the stable part of the
contract.

```go
pdf, err := client.Render(ctx, req)
var apiErr *pdfcraft.Error
if errors.As(err, &apiErr) {
	switch apiErr.Code {
	case "quota_exceeded":
		// out of renders this period
	case "render_failed":
		// their HTML broke — billable, worth logging
	default:
		log.Printf("pdfcraft %s: %s (%s)", apiErr.Code, apiErr.Message, apiErr.DocsURL)
	}
}
```

A render is billable if Chromium actually ran. Successes and `render_failed` count;
`render_timeout`, `internal_error` and every 4xx that never reached the browser do not.

## Context and retries

Every method takes a `context.Context` and means it. Retries happen on `429`, `5xx` and
network failures — never on a `4xx` other than `429` — and a cancelled context stops them
immediately rather than burning the remaining deadline.

```go
client, _ := pdfcraft.New(key,
	pdfcraft.WithMaxRetries(0),
	pdfcraft.WithBaseURL("https://gateway.internal"),
	pdfcraft.WithHTTPClient(&http.Client{Timeout: 200 * time.Second}),
)
```

`Retry-After` is honoured when present, otherwise the backoff is exponential with jitter —
the same curve as the TypeScript and Python clients.

`Client` is safe for concurrent use.

## Escape hatch

`RenderRaw` and `ExtractRaw` take any JSON-marshalable body and return the raw
`*http.Response`, so a contract addition never blocks you on an SDK release.

## Generated constants

`contract_gen.go` is generated from the same definitions the API validates requests against —
`ErrorCodes`, `PageFormats`, `EmulateMedia`, timeouts — so they cannot drift from the server.
Do not edit it by hand; it is overwritten on the next sync.

## Links

- Docs — https://pdfcraft.dev/docs/
- Every error code, cause and fix — https://pdfcraft.dev/errors/
- TypeScript SDK — `@pdfcraft-dev/pdf`
- Python SDK — `pip install pdfcraft`

MIT licensed.
