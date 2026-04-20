package confluencemcp

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sishbi/confluence-mcp/internal/confluence"
	"github.com/sishbi/confluence-mcp/internal/mdconv"
)

// TestAppend_RoundTripFixture drives an append on the all-elements fixture and
// verifies (a) the appended text survives round-trip, (b) no macro/element
// counts regressed, (c) CDATA regions are byte-identical in the merged body.
func TestAppend_RoundTripFixture(t *testing.T) {
	raw, err := os.ReadFile("../mdconv/testdata/fixture-all-elements.xml")
	require.NoError(t, err)
	base := string(raw)

	// Baseline conversion — count macros and elements before append.
	_, baseMacros, baseLog := mdconv.ToMarkdownWithMacros(base)

	var captured map[string]any
	h := &handlers{client: &mockClient{
		GetPageFn: func(_ context.Context, id string) (*confluence.Page, error) {
			return &confluence.Page{
				ID: id, Title: "Fixture", Version: confluence.PageVersion{Number: 1},
				Body: confluence.PageBody{Storage: confluence.StorageBody{Value: base}},
			}, nil
		},
		UpdatePageFn: func(_ context.Context, id string, payload map[string]any) (*confluence.Page, error) {
			captured = payload
			return &confluence.Page{ID: id, Title: "Fixture"}, nil
		},
	}}

	const sentinel = "Appended sentinel paragraph — 2026-04-20."
	_, err = h.writeAppend(context.Background(), WriteItem{
		PageID:   "p1",
		Body:     sentinel,
		Position: "end",
	}, false)
	require.NoError(t, err)

	body := captured["body"].(map[string]any)
	storage := body["storage"].(map[string]any)
	merged := storage["value"].(string)

	// Sentinel text present.
	assert.Contains(t, merged, sentinel)
	// Original fixture content present.
	assert.Contains(t, merged, `ac:name="toc"`)
	assert.Contains(t, merged, `ac:name="jira"`)
	assert.Contains(t, merged, `ac:name="details"`)

	// CDATA blocks byte-identical.
	baseCDATAs := extractCDATAs(base)
	mergedCDATAs := extractCDATAs(merged)
	require.Equal(t, len(baseCDATAs), len(mergedCDATAs), "CDATA count changed")
	for i := range baseCDATAs {
		if baseCDATAs[i] != mergedCDATAs[i] {
			t.Errorf("CDATA block %d corrupted\nbase:   %q\nmerged: %q", i, baseCDATAs[i], mergedCDATAs[i])
		}
	}

	// Round-trip: the merged body should convert without more macro errors than baseline.
	_, mergedMacros, mergedLog := mdconv.ToMarkdownWithMacros(merged)
	if mergedLog.Errors > baseLog.Errors {
		t.Errorf("merged body introduced %d conversion errors beyond baseline", mergedLog.Errors-baseLog.Errors)
	}
	// Macro entries: the merged registry should have at least every macro the
	// base registry had (we only added a paragraph — no macros added or removed).
	if len(mergedMacros.Entries) < len(baseMacros.Entries) {
		t.Errorf("macros lost: base=%d merged=%d", len(baseMacros.Entries), len(mergedMacros.Entries))
	}
}

// extractCDATAs returns each CDATA section's contents in document order.
func extractCDATAs(s string) []string {
	var out []string
	for {
		i := strings.Index(s, "<![CDATA[")
		if i < 0 {
			return out
		}
		j := strings.Index(s[i:], "]]>")
		if j < 0 {
			return out
		}
		out = append(out, s[i+len("<![CDATA["):i+j])
		s = s[i+j+len("]]>"):]
	}
}
