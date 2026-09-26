package agent

import (
	"context"

	"github.com/mobiumdev/mobium/internal/mobiumdriver"
	"github.com/mobiumdev/mobium/internal/uitree"
	"github.com/mobiumdev/mobium/internal/webview"
)

// SourceView is the result of app_source.
type SourceView struct {
	// Source is the hierarchy as the device-side server sent it, or in a
	// WebView the page's markup as it stands now.
	Source string `json:"source"`
	// Format is "xml" for a native screen and "html" for a page.
	Format string `json:"format"`
	// Units is what the source's geometry is in: "px" on Android, "pt" on
	// iOS — where map, taps and screenshots are in pixels, Scale times as
	// many. Absent for a page, whose geometry is CSS pixels.
	Units string  `json:"units,omitempty"`
	Scale float64 `json:"scale,omitempty"`
	// Redacted is how many password fields had their contents hidden.
	Redacted int    `json:"redacted"`
	Context  string `json:"context,omitempty"`
	Device   string `json:"device"`
}

func (h *Handlers) source(ctx context.Context, args map[string]interface{}) (*ToolsCallResult, error) {
	s, err := h.sessionFor(ctx, args)
	if err != nil {
		return nil, err
	}
	return h.sourceOn(ctx, s)
}

// sourceOn is app_source once the device is resolved.
//
// The source is the one answer that is not parsed on the way out, so it is
// the one that needs its passwords hidden separately — uitree.RedactSource,
// on the same terms the parsers use, and in a page by the script that
// serializes it.
func (h *Handlers) sourceOn(ctx context.Context, s *session) (*ToolsCallResult, error) {
	if s.web != nil {
		html, n, err := webview.Source(ctx, s.web)
		if err != nil {
			return nil, err
		}
		return Result(html, SourceView{
			Source: html, Format: "html", Redacted: n, Context: s.webCtx, Device: s.dev.Serial,
		}), nil
	}
	r, ok := mobiumdriver.AsSourceReader(s.driver)
	if !ok {
		return nil, cannot(s, mobiumdriver.CapSource, "hand over the raw hierarchy — use app_map, or app_find")
	}
	src, err := r.Source(ctx)
	if err != nil {
		return nil, err
	}
	xml, n, err := uitree.RedactSource(src.XML)
	if err != nil {
		return nil, err
	}
	return Result(string(xml), SourceView{
		Source: string(xml), Format: "xml", Units: src.Units, Scale: src.Scale,
		Redacted: n, Device: s.dev.Serial,
	}), nil
}
