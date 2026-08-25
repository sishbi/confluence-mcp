package confluencemcp

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestLocatePreambleExtent(t *testing.T) {
	t.Run("no layout wrapper: preamble before h2", func(t *testing.T) {
		body := `<p>intro</p><h2>Section A</h2><p>body</p>`
		ext, err := locatePreambleExtent(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ext.containerStart != 0 {
			t.Errorf("containerStart = %d, want 0", ext.containerStart)
		}
		wantHeadingStart := strings.Index(body, "<h2>")
		if ext.firstHeadingStart != wantHeadingStart {
			t.Errorf("firstHeadingStart = %d, want %d", ext.firstHeadingStart, wantHeadingStart)
		}
		if ext.container != "document root" {
			t.Errorf("container = %q, want %q", ext.container, "document root")
		}
		if ext.firstHeadingText != "Section A" {
			t.Errorf("firstHeadingText = %q, want %q", ext.firstHeadingText, "Section A")
		}
	})

	t.Run("layout wrapper: containerStart lands just past the opening layout-cell tag", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>intro</p><h2>Section A</h2></ac:layout-cell></ac:layout-section></ac:layout>`
		ext, err := locatePreambleExtent(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		wantStart := strings.Index(body, "<ac:layout-cell>") + len("<ac:layout-cell>")
		if ext.containerStart != wantStart {
			t.Errorf("containerStart = %d, want %d (just past the opening tag, not before it)", ext.containerStart, wantStart)
		}
		if ext.container != "ac:layout-cell" {
			t.Errorf("container = %q, want %q", ext.container, "ac:layout-cell")
		}
		wantHeadingStart := strings.Index(body, "<h2>")
		if ext.firstHeadingStart != wantHeadingStart {
			t.Errorf("firstHeadingStart = %d, want %d", ext.firstHeadingStart, wantHeadingStart)
		}
	})

	t.Run("heading inside a macro body does not end the preamble", func(t *testing.T) {
		body := `<p>intro</p><ac:structured-macro ac:name="expand"><ac:rich-text-body><h2>Hidden</h2></ac:rich-text-body></ac:structured-macro><h2>Real</h2><p>body</p>`
		ext, err := locatePreambleExtent(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		wantHeadingStart := strings.LastIndex(body, "<h2>Real</h2>")
		if ext.firstHeadingStart != wantHeadingStart {
			t.Errorf("firstHeadingStart = %d, want %d (the macro-nested heading must not count as the boundary)", ext.firstHeadingStart, wantHeadingStart)
		}
		if ext.firstHeadingText != "Real" {
			t.Errorf("firstHeadingText = %q, want %q", ext.firstHeadingText, "Real")
		}
	})

	t.Run("heading inside a td does not end the preamble", func(t *testing.T) {
		body := `<p>intro</p><table><tbody><tr><td><h2>Hidden</h2></td></tr></tbody></table><h2>Real</h2><p>body</p>`
		ext, err := locatePreambleExtent(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		wantHeadingStart := strings.LastIndex(body, "<h2>Real</h2>")
		if ext.firstHeadingStart != wantHeadingStart {
			t.Errorf("firstHeadingStart = %d, want %d (the td-nested heading must not count as the boundary)", ext.firstHeadingStart, wantHeadingStart)
		}
	})

	t.Run("body opening immediately with a heading: zero-length preamble, no error", func(t *testing.T) {
		body := `<h2>First</h2><p>body</p>`
		ext, err := locatePreambleExtent(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ext.firstHeadingStart != ext.containerStart {
			t.Errorf("firstHeadingStart = %d, want == containerStart %d", ext.firstHeadingStart, ext.containerStart)
		}
	})

	t.Run("no heading anywhere on the page returns ErrNoHeadingOnPage", func(t *testing.T) {
		body := `<p>only</p><p>text</p>`
		_, err := locatePreambleExtent(body)
		if !errors.Is(err, ErrNoHeadingOnPage) {
			t.Fatalf("got %v, want ErrNoHeadingOnPage", err)
		}
	})

	t.Run("two-cell layout: first cell has no heading, second does — ErrNoHeadingOnPage, never a range crossing into cell two", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="two_equal"><ac:layout-cell><p>no heading here</p></ac:layout-cell><ac:layout-cell><h2>Second cell heading</h2></ac:layout-cell></ac:layout-section></ac:layout>`
		_, err := locatePreambleExtent(body)
		if !errors.Is(err, ErrNoHeadingOnPage) {
			t.Fatalf("got %v, want ErrNoHeadingOnPage — the second cell's heading must never be treated as the preamble boundary", err)
		}
	})

	t.Run("firstHeadingText is the normalised heading text", func(t *testing.T) {
		body := `<p>x</p><h2>A  &amp;   B</h2><p>y</p>`
		ext, err := locatePreambleExtent(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ext.firstHeadingText != "A & B" {
			t.Errorf("firstHeadingText = %q, want %q", ext.firstHeadingText, "A & B")
		}
	})
}

// TestLocateContainer_GuardsSelfClosingEmptyCell covers the corrupt-page
// defect where an empty first layout-cell is written self-closing
// (<ac:layout-cell/>): the XML decoder reports that element's start and end
// at the exact same byte offset, so containerInfo.start == containerInfo.end
// there. Without a guard, spliceStart would insert the fragment at that
// shared offset — which sits between the empty cell's own closing tag and
// the NEXT <ac:layout-cell> (or </ac:layout-section> if there is no next
// cell), i.e. as a direct child of <ac:layout-section>. That is not valid
// Confluence storage: page content belongs inside a layout-cell.
//
// Confluence itself always renders an empty cell as
// <ac:layout-cell><p /></ac:layout-cell> rather than self-closing, so this
// shape is unlikely to come from a real page, but a hand-authored or
// corrupted body could still carry it — worth a guard given how quietly it
// would corrupt the write.
func TestLocateContainer_GuardsSelfClosingEmptyCell(t *testing.T) {
	t.Run("self-closing first cell with a real second cell: falls back to the second cell", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell/><ac:layout-cell><p>real</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`
		res, err := spliceStart(body, `<p>NEW</p>`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell/><ac:layout-cell><p>NEW</p><p>real</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch — must fall back to the next (real) cell, never split the empty cell's own tag\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("self-closing first cell with no other cell: falls back to the document root", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell/></ac:layout-section></ac:layout>`
		res, err := spliceStart(body, `<p>NEW</p>`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<p>NEW</p>` + body
		if res.Merged != want {
			t.Errorf("merged mismatch — must fall back to the document root, never split the empty cell's own tag\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.Container != "document root" {
			t.Errorf("Container = %q, want %q", res.Boundary.Container, "document root")
		}
	})

	// An empty tag PAIR (<ac:layout-cell></ac:layout-cell>) is not the same
	// shape as a self-closing tag: its close tag is a real token with
	// tokStart < tokEnd, and it is a perfectly usable container —
	// <ac:layout-cell><p>NEW</p></ac:layout-cell> is valid storage. Testing
	// ev.tokStart == info.start (the container's recorded start offset) is
	// also true for this shape, since an empty pair's start-of-content offset
	// trivially equals its own close tag's start offset — so that test
	// wrongly treated an empty pair as self-closing too, and fell through to
	// the second cell.
	t.Run("empty tag pair (real close tag, not self-closing): first cell is still used, not skipped", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell></ac:layout-cell><ac:layout-cell><p>real</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`
		res, err := spliceStart(body, `<p>NEW</p>`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell><p>NEW</p></ac:layout-cell><ac:layout-cell><p>real</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch — must use the first (empty) cell, not fall through to the second\n got: %s\nwant: %s", res.Merged, want)
		}
	})
}

func TestSplice_Start(t *testing.T) {
	t.Run("no layout wrapper: fragment lands at byte 0, merged is exactly fragment+body", func(t *testing.T) {
		body := `<p>hello</p>`
		fragment := `<p>NEW</p>`
		res, err := spliceStart(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fragment + body
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.InsertAnchor != "start of body (no layout wrapper)" {
			t.Errorf("InsertAnchor = %q, want %q", res.Boundary.InsertAnchor, "start of body (no layout wrapper)")
		}
		if res.Boundary.Container != "document root" {
			t.Errorf("Container = %q, want %q", res.Boundary.Container, "document root")
		}
		if res.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
	})

	t.Run("layout wrapper: fragment lands just past the opening <ac:layout-cell> tag, not before it", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>existing</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>NEW</p>`
		res, err := spliceStart(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>NEW</p><p>existing</p></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.InsertAnchor != "after opening <ac:layout-cell>" {
			t.Errorf("InsertAnchor = %q, want %q", res.Boundary.InsertAnchor, "after opening <ac:layout-cell>")
		}
		if res.Boundary.Container != "ac:layout-cell" {
			t.Errorf("Container = %q, want %q", res.Boundary.Container, "ac:layout-cell")
		}
		if res.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
	})

	t.Run("body with no headings still succeeds — start has a well-defined meaning there", func(t *testing.T) {
		body := `<p>only text, no headings anywhere</p>`
		fragment := `<p>NEW</p>`
		res, err := spliceStart(body, fragment)
		if errors.Is(err, ErrNoHeadingOnPage) {
			t.Fatalf("spliceStart must not return ErrNoHeadingOnPage for a headless body")
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fragment + body
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
	})

	t.Run("two-cell layout: start lands in the LEFT cell, end lands in the RIGHT cell", func(t *testing.T) {
		// TestSplice_Start's other layout case above uses a single fixed-width
		// cell, which cannot distinguish "the first cell" from "the only cell".
		// This is the case that actually pins the documented start/end
		// asymmetry (see server.go and README.md): on a genuinely multi-column
		// page, start and end must land in different cells.
		body := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell><p>left existing</p></ac:layout-cell>` +
			`<ac:layout-cell><p>right existing</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`

		startRes, err := spliceStart(body, `<p>NEW-START</p>`)
		if err != nil {
			t.Fatalf("spliceStart: unexpected err: %v", err)
		}
		wantStart := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell><p>NEW-START</p><p>left existing</p></ac:layout-cell>` +
			`<ac:layout-cell><p>right existing</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`
		if startRes.Merged != wantStart {
			t.Errorf("spliceStart merged mismatch — must land in the LEFT cell\n got: %s\nwant: %s", startRes.Merged, wantStart)
		}

		endRes, err := spliceEnd(body, `<p>NEW-END</p>`)
		if err != nil {
			t.Fatalf("spliceEnd: unexpected err: %v", err)
		}
		wantEnd := `<ac:layout><ac:layout-section ac:type="two_equal">` +
			`<ac:layout-cell><p>left existing</p></ac:layout-cell>` +
			`<ac:layout-cell><p>right existing</p><p>NEW-END</p></ac:layout-cell>` +
			`</ac:layout-section></ac:layout>`
		if endRes.Merged != wantEnd {
			t.Errorf("spliceEnd merged mismatch — must land in the RIGHT cell\n got: %s\nwant: %s", endRes.Merged, wantEnd)
		}
	})

	t.Run("dispatched via Splice with ModeStart", func(t *testing.T) {
		body := `<p>hello</p>`
		fragment := `<p>NEW</p>`
		res, err := Splice(body, fragment, SpliceOptions{Mode: ModeStart})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fragment + body
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})
}

func TestSplice_ReplacePreamble(t *testing.T) {
	t.Run("replaces [containerStart, firstHeadingStart) and leaves the heading and everything after byte-identical", func(t *testing.T) {
		body := `<p>intro</p><p>more intro</p><h2>Section A</h2><p>section content</p>`
		fragment := `<p>NEW PREAMBLE</p>`
		res, err := spliceReplacePreamble(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		wantSuffix := `<h2>Section A</h2><p>section content</p>`
		want := fragment + wantSuffix
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if !strings.HasSuffix(res.Merged, wantSuffix) {
			t.Errorf("merged %q does not end with the untouched heading and body %q", res.Merged, wantSuffix)
		}
	})

	t.Run("a macro in the preamble is replaced and named in ReplacedElementSummary", func(t *testing.T) {
		body := `<p>intro</p><ac:structured-macro ac:name="toc"/><h2>Section A</h2><p>content</p>`
		fragment := `<p>NEW</p>`
		res, err := spliceReplacePreamble(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fragment + `<h2>Section A</h2><p>content</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		wantSummary := []string{"<p> x 1", `macro "toc" x 1`}
		if !reflect.DeepEqual(res.Boundary.ReplacedElementSummary, wantSummary) {
			t.Errorf("ReplacedElementSummary = %v, want %v", res.Boundary.ReplacedElementSummary, wantSummary)
		}
	})

	t.Run("zero-length preamble behaves as an insert", func(t *testing.T) {
		body := `<h2>First</h2><p>body</p>`
		fragment := `<p>NEW</p>`
		res, err := spliceReplacePreamble(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fragment + body
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.ReplacedByteCount != 0 {
			t.Errorf("ReplacedByteCount = %d, want 0", res.Boundary.ReplacedByteCount)
		}
	})

	t.Run("no heading on the page returns ErrNoHeadingOnPage and produces no merged body", func(t *testing.T) {
		body := `<p>only</p>`
		res, err := spliceReplacePreamble(body, `<p>x</p>`)
		if !errors.Is(err, ErrNoHeadingOnPage) {
			t.Fatalf("got %v, want ErrNoHeadingOnPage", err)
		}
		if res.Merged != "" {
			t.Errorf("no merged body should be produced when there is no heading, got %q", res.Merged)
		}
	})

	t.Run("a fragment starting with a heading is inserted whole, not stripped", func(t *testing.T) {
		body := `<p>intro</p><h2>Section A</h2><p>content</p>`
		fragment := `<h1>My Title</h1><p>scope statement</p>`
		res, err := spliceReplacePreamble(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fragment + `<h2>Section A</h2><p>content</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch — fragment's leading heading must survive\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("StartAnchor and EndAnchor name the container start and the boundary heading", func(t *testing.T) {
		body := `<p>intro</p><h2>Section A</h2><p>content</p>`
		res, err := spliceReplacePreamble(body, `<p>new</p>`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Boundary.StartAnchor != "start of container" {
			t.Errorf("StartAnchor = %q, want %q", res.Boundary.StartAnchor, "start of container")
		}
		wantEnd := `before first heading "Section A"`
		if res.Boundary.EndAnchor != wantEnd {
			t.Errorf("EndAnchor = %q, want %q", res.Boundary.EndAnchor, wantEnd)
		}
	})

	t.Run("layout wrapper: the replaced range starts inside the first cell, so the layout opening tags survive", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>intro</p><h2>Section A</h2><p>content</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>NEW</p>`
		res, err := spliceReplacePreamble(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>NEW</p><h2>Section A</h2><p>content</p></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		wantPrefix := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>`
		if !strings.HasPrefix(res.Merged, wantPrefix) {
			t.Errorf("merged %q does not open with the layout wrapper tags %q — page layout destroyed", res.Merged, wantPrefix)
		}
	})

	t.Run("dispatched via Splice with ModeReplacePreamble", func(t *testing.T) {
		body := `<p>intro</p><h2>Section A</h2><p>content</p>`
		res, err := Splice(body, `<p>new</p>`, SpliceOptions{Mode: ModeReplacePreamble})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<p>new</p><h2>Section A</h2><p>content</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})
}

// TestSpliceReplacePreamble_BoundaryBalance covers the defect where the
// preamble range could cut through an unclosed wrapper element: when the
// first heading sits inside a plain container (e.g. <div>) that itself opens
// inside the preamble but closes after it, replacing [containerStart,
// firstHeadingStart) deletes the wrapper's own opening tag while its closing
// tag survives untouched further down the page — an orphaned close tag with
// no matching open. The guard must refuse exactly that shape and nothing
// else: every ordinary preamble shape (including ones that already have their
// own dedicated tests above) must still succeed.
func TestSpliceReplacePreamble_BoundaryBalance(t *testing.T) {
	t.Run("heading inside a div that opens in the preamble is refused, body untouched", func(t *testing.T) {
		body := `<div><p>intro</p><h2>A</h2><p>c</p></div>`
		res, err := spliceReplacePreamble(body, `<p>NEW</p>`)
		if !errors.Is(err, ErrPreambleBoundaryUnbalanced) {
			t.Fatalf("got err %v, want ErrPreambleBoundaryUnbalanced", err)
		}
		if res.Merged != "" {
			t.Errorf("no merged body should be produced when the boundary is unbalanced, got %q", res.Merged)
		}
	})

	t.Run("heading inside a same-named nested div is still detected as unbalanced", func(t *testing.T) {
		// The outer <div> never closes before the heading — <h1>T</h1> sits
		// between the inner </div> and the outer one. A tracker that clears on
		// the FIRST end event named "div", regardless of nesting, would see the
		// inner </div> and wrongly conclude the outer one had closed too.
		body := `<div><div>x</div><h1>T</h1><p>a</p></div>`
		res, err := spliceReplacePreamble(body, `<p>NEW</p>`)
		if !errors.Is(err, ErrPreambleBoundaryUnbalanced) {
			t.Fatalf("got err %v, want ErrPreambleBoundaryUnbalanced", err)
		}
		if res.Merged != "" {
			t.Errorf("no merged body should be produced when the boundary is unbalanced, got %q", res.Merged)
		}
	})

	t.Run("ReplacedElementSummary does not include a nested same-name child", func(t *testing.T) {
		// The outer <div> fully closes before the heading here, so the range is
		// balanced — but the same first-end-event bug would also close the
		// outer div's tracking on the INNER </div>, letting the following
		// <p>inner</p> (still nested inside the outer div) be counted as a
		// second top-level sibling.
		body := `<div><div>x</div><p>inner</p></div><h1>T</h1>`
		res, err := spliceReplacePreamble(body, `<p>NEW</p>`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := []string{"<div> x 1"}
		if !reflect.DeepEqual(res.Boundary.ReplacedElementSummary, want) {
			t.Errorf("ReplacedElementSummary = %v, want %v (must not name the nested <p>)", res.Boundary.ReplacedElementSummary, want)
		}
	})

	t.Run("ordinary shapes are unaffected by the guard", func(t *testing.T) {
		cases := []struct {
			name string
			body string
		}{
			{
				name: "flat body",
				body: `<p>intro</p><h2>Section A</h2><p>content</p>`,
			},
			{
				name: "macro in the preamble",
				body: `<p>intro</p><ac:structured-macro ac:name="toc"/><h2>Section A</h2><p>content</p>`,
			},
			{
				name: "blockquote in the preamble",
				body: `<p>intro</p><blockquote><p>quoted</p></blockquote><h2>Section A</h2><p>content</p>`,
			},
			{
				name: "table in the preamble",
				body: `<p>intro</p><table><tbody><tr><td>cell</td></tr></tbody></table><h2>Section A</h2><p>content</p>`,
			},
			{
				name: "bare-text preamble",
				body: `Some intro text.<h2>Section A</h2><p>content</p>`,
			},
			{
				name: "zero-length preamble",
				body: `<h2>Section A</h2><p>content</p>`,
			},
			{
				name: "layout-wrapped body",
				body: `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>intro</p><h2>Section A</h2><p>content</p></ac:layout-cell></ac:layout-section></ac:layout>`,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := spliceReplacePreamble(tc.body, `<p>NEW</p>`)
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
			})
		}
	})
}
