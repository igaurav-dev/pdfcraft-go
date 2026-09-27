// GENERATED FILE — do not edit.
// Written by packages/contract/scripts/sync-polyglot.mjs from the same
// definitions the PDFCraft API validates requests against.
// Editing it by hand will be silently overwritten on the next sync.

package pdfcraft

// ContractHash fingerprints the API contract these constants came from.
const ContractHash = "f5af303a2509e0d11d0c6efdc069e9acb80fddcd43a337a4789fa8f538f2588a"

// ErrorCodes are the stable machine-readable codes. Switch on these, never on
// the message, which is written for humans and may be reworded.
var ErrorCodes = []string{
	"invalid_request",
	"invalid_api_key",
	"payment_required",
	"not_found",
	"render_timeout",
	"render_failed",
	"rate_limited",
	"quota_exceeded",
	"demo_busy",
	"unsupported_file",
	"extraction_failed",
	"internal_error",
}

var PageFormats = []string{
	"A4",
	"A3",
	"A5",
	"Letter",
	"Legal",
	"Tabloid",
}

var EmulateMedia = []string{
	"print",
	"screen",
}

var OutputModes = []string{
	"binary",
	"url",
}

var ExtractOutputModes = []string{
	"inline",
	"url",
}

const (
	MaxTimeoutMS     = 120000
	DefaultTimeoutMS = 30000
)
