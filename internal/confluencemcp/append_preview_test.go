package confluencemcp

import (
	"strings"
	"testing"
)

func TestBuildPreview(t *testing.T) {
	t.Run("ModeEnd populates insert anchor and tail context", func(t *testing.T) {
		base := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>existing</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>new</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeEnd})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "new"}, ModeEnd, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.PageID != "42" {
			t.Errorf("PageID = %q", p.PageID)
		}
		if p.Position != "end" {
			t.Errorf("Position = %q, want end", p.Position)
		}
		if p.Boundary.InsertAnchor == "" {
			t.Errorf("InsertAnchor empty")
		}
		if p.Boundary.StartAnchor != "" {
			t.Errorf("StartAnchor should be empty for ModeEnd")
		}
		if p.Fragment.InputBody != "new" {
			t.Errorf("InputBody = %q", p.Fragment.InputBody)
		}
		if p.Fragment.StorageOutput != fragment {
			t.Errorf("StorageOutput mismatch")
		}
		if p.Fragment.StorageByteCount != len(fragment) {
			t.Errorf("StorageByteCount = %d", p.Fragment.StorageByteCount)
		}
		if p.Sizes.DeltaBytes != len(res.Merged)-len(base) {
			t.Errorf("DeltaBytes mismatch: %d", p.Sizes.DeltaBytes)
		}
		if p.Context.Before == "" {
			t.Errorf("Context.Before empty for ModeEnd")
		}
	})

	t.Run("ModeAfterHeading populates anchor and context on both sides", func(t *testing.T) {
		base := `<h2>A</h2><p>before</p><h2>B</h2><p>after</p>`
		fragment := `<p>new</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeAfterHeading, Heading: "A"})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "new", Heading: "A"}, ModeAfterHeading, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.Position != "after_heading" {
			t.Errorf("Position = %q", p.Position)
		}
		if !strings.Contains(p.ActionSummary, "A") {
			t.Errorf("ActionSummary missing heading: %q", p.ActionSummary)
		}
		if p.Context.Before == "" || p.Context.After == "" {
			t.Errorf("Context.Before/After should both be populated")
		}
	})

	t.Run("ModeReplaceSection populates replaced-* fields", func(t *testing.T) {
		cell := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>%s</ac:layout-cell></ac:layout-section></ac:layout>`
		base := replaceStr(cell, `<h2>A</h2><p>old</p><h2>B</h2>`)
		fragment := `<p>new</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeReplaceSection, Heading: "A"})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "new", Heading: "A"}, ModeReplaceSection, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.Position != "replace_section" {
			t.Errorf("Position = %q", p.Position)
		}
		if p.Boundary.StartAnchor == "" || p.Boundary.EndAnchor == "" {
			t.Errorf("StartAnchor/EndAnchor should be populated")
		}
		if p.Boundary.ReplacedByteCount <= 0 {
			t.Errorf("ReplacedByteCount should be > 0")
		}
		if len(p.Boundary.ReplacedElementSummary) == 0 {
			t.Errorf("ReplacedElementSummary should be populated")
		}
		if p.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
	})

	t.Run("ModeEndOfSection populates insert anchor and end-of-section context", func(t *testing.T) {
		base := `<h2>A</h2><p>existing</p><h2>B</h2><p>other</p>`
		fragment := `<p>new</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeEndOfSection, Heading: "A"})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "new", Heading: "A"}, ModeEndOfSection, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.Position != "end_of_section" {
			t.Errorf("Position = %q, want end_of_section", p.Position)
		}
		if p.ActionSummary == "" {
			t.Errorf("ActionSummary empty")
		}
		// Must show the section's existing content in Before (proves the
		// splice point is below it, not at the top of the section — the
		// after_heading defect this mode exists to avoid) and the next
		// heading in After.
		if !strings.Contains(p.Context.Before, "existing") {
			t.Errorf("Context.Before should contain the section's existing content: %q", p.Context.Before)
		}
		if strings.Contains(p.Context.Before, "<h2>B</h2>") {
			t.Errorf("Context.Before should stop at the splice point, not run past the next heading: %q", p.Context.Before)
		}
		if !strings.HasPrefix(p.Context.After, "<h2>B</h2>") {
			t.Errorf("Context.After should begin at the next heading: %q", p.Context.After)
		}
		const wantAnchor = "before next heading at same or higher level"
		if p.Boundary.InsertAnchor != wantAnchor {
			t.Errorf("InsertAnchor = %q, want %q", p.Boundary.InsertAnchor, wantAnchor)
		}
		if p.Boundary.ReplacedByteCount != 0 {
			t.Errorf("ReplacedByteCount should be 0 for end_of_section, got %d", p.Boundary.ReplacedByteCount)
		}
		if p.Boundary.ReplacedElementSummary != nil {
			t.Errorf("ReplacedElementSummary should be nil for end_of_section, got %v", p.Boundary.ReplacedElementSummary)
		}
	})

	t.Run("DeltaBytes negative for shrinking replace", func(t *testing.T) {
		base := `<h2>A</h2><p>aaaaaaaaaaaaaaaaaa</p>`
		fragment := `<p>x</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeReplaceSection, Heading: "A"})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "x", Heading: "A"}, ModeReplaceSection, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.Sizes.DeltaBytes >= 0 {
			t.Errorf("DeltaBytes should be negative, got %d", p.Sizes.DeltaBytes)
		}
	})

	// The two preamble-editing positions reuse the same parity check every
	// other mode above pins: buildPreview's Sizes fields must match the sizes
	// of the SAME res.Merged a real write would send — buildPreview never
	// recomputes the merged body independently. This is the whole safety
	// point of the preview: the caller sees the size of the exact bytes that
	// will be written, not an approximation.
	t.Run("ModeStart preview sizes match the merged body a real write would send", func(t *testing.T) {
		base := appendTestLayoutBody
		fragment := `<p>new</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeStart})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "new"}, ModeStart, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.Position != "start" {
			t.Errorf("Position = %q, want start", p.Position)
		}
		if p.Sizes.BaseBodyBytes != len(base) {
			t.Errorf("BaseBodyBytes = %d, want %d", p.Sizes.BaseBodyBytes, len(base))
		}
		if p.Sizes.MergedBodyBytes != len(res.Merged) {
			t.Errorf("MergedBodyBytes = %d, want %d (the real write's merged body length)", p.Sizes.MergedBodyBytes, len(res.Merged))
		}
		if p.Sizes.DeltaBytes != len(res.Merged)-len(base) {
			t.Errorf("DeltaBytes mismatch: %d", p.Sizes.DeltaBytes)
		}
	})

	t.Run("ModeReplacePreamble preview sizes match the merged body a real write would send", func(t *testing.T) {
		base := `<p>preamble</p><h2>Section A</h2><p>existing</p>`
		fragment := `<p>new preamble</p>`
		res, err := Splice(base, fragment, SpliceOptions{Mode: ModeReplacePreamble})
		if err != nil {
			t.Fatalf("splice: %v", err)
		}
		p := buildPreview(WriteItem{PageID: "42", Body: "new preamble"}, ModeReplacePreamble, base, res.Merged, fragment, "markdown", res.Boundary)
		if p.Position != "replace_preamble" {
			t.Errorf("Position = %q, want replace_preamble", p.Position)
		}
		if p.Sizes.BaseBodyBytes != len(base) {
			t.Errorf("BaseBodyBytes = %d, want %d", p.Sizes.BaseBodyBytes, len(base))
		}
		if p.Sizes.MergedBodyBytes != len(res.Merged) {
			t.Errorf("MergedBodyBytes = %d, want %d (the real write's merged body length)", p.Sizes.MergedBodyBytes, len(res.Merged))
		}
		if p.Sizes.DeltaBytes != len(res.Merged)-len(base) {
			t.Errorf("DeltaBytes mismatch: %d", p.Sizes.DeltaBytes)
		}
	})
}

// TestBuildPreview_PreamblePositions covers modeString, summariseAction and
// contextAround for the two preamble-editing positions.
func TestBuildPreview_PreamblePositions(t *testing.T) {
	t.Run("modeString", func(t *testing.T) {
		if got := modeString(ModeStart); got != "start" {
			t.Errorf("modeString(ModeStart) = %q, want start", got)
		}
		if got := modeString(ModeReplacePreamble); got != "replace_preamble" {
			t.Errorf("modeString(ModeReplacePreamble) = %q, want replace_preamble", got)
		}
	})

	t.Run("summariseAction ModeStart", func(t *testing.T) {
		got := summariseAction(ModeStart, "", "", BoundaryInfo{})
		want := "Insert at start of page."
		if got != want {
			t.Errorf("summariseAction = %q, want %q", got, want)
		}
	})

	t.Run("summariseAction ModeReplacePreamble with no replaced elements", func(t *testing.T) {
		got := summariseAction(ModeReplacePreamble, "", "", BoundaryInfo{})
		want := "Replace page preamble."
		if got != want {
			t.Errorf("summariseAction = %q, want %q", got, want)
		}
	})

	t.Run("summariseAction ModeReplacePreamble names destroyed elements — the safety point of this mode", func(t *testing.T) {
		b := BoundaryInfo{ReplacedElementSummary: []string{"<p> x 2", `macro "toc" x 1`}}
		got := summariseAction(ModeReplacePreamble, "", "", b)
		want := `Replace page preamble (replaces <p> x 2, macro "toc" x 1).`
		if got != want {
			t.Errorf("summariseAction = %q, want %q", got, want)
		}
	})

	// A bare-text preamble (no element children at all) produces no walker
	// events, so ReplacedElementSummary is empty even though bytes are about
	// to be destroyed. Without this fallback the preview reads as if nothing
	// were being replaced, while appendSuccessMsg's own fallback reports the
	// byte count after the write — the pre-write preview must say the same.
	t.Run("summariseAction ModeReplacePreamble falls back to a byte count for a bare-text range", func(t *testing.T) {
		body := `Some intro text.<h2>Section A</h2><p>content</p>`
		res, err := spliceReplacePreamble(body, `<p>new</p>`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		got := summariseAction(ModeReplacePreamble, "", "", res.Boundary)
		want := `Replace page preamble (replaces 16 bytes with no locatable elements).`
		if got != want {
			t.Errorf("summariseAction = %q, want %q", got, want)
		}
	})

	t.Run("contextAround ModeStart on a layout body", func(t *testing.T) {
		base := appendTestLayoutBody
		before, after := contextAround(base, ModeStart, WriteItem{})
		if before != "" {
			t.Errorf("before should be empty for ModeStart, got %q", before)
		}
		if strings.HasPrefix(after, "<ac:layout>") {
			t.Errorf("after should start inside the first cell, not with the <ac:layout> tags: %q", after)
		}
		if !strings.HasPrefix(after, "<h2>Section A</h2>") {
			t.Errorf("after should start with the content inside the first cell: %q", after)
		}
	})

	t.Run("contextAround ModeStart with no layout wrapper", func(t *testing.T) {
		base := `<h2>A</h2><p>existing</p>`
		before, after := contextAround(base, ModeStart, WriteItem{})
		if before != "" {
			t.Errorf("before should be empty for ModeStart, got %q", before)
		}
		if !strings.HasPrefix(after, "<h2>A</h2>") {
			t.Errorf("after should start at byte 0 of the body: %q", after)
		}
	})

	t.Run("contextAround ModeReplacePreamble on a layout body", func(t *testing.T) {
		base := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>preamble</p><h2>Section A</h2><p>existing</p></ac:layout-cell></ac:layout-section></ac:layout>`
		before, after := contextAround(base, ModeReplacePreamble, WriteItem{})
		if !strings.HasSuffix(before, "<ac:layout-cell>") {
			t.Errorf("before should run up to and include the layout opening tags: %q", before)
		}
		if !strings.HasPrefix(after, "<h2>Section A</h2>") {
			t.Errorf("after should start at the first heading: %q", after)
		}
	})

	t.Run("contextAround ModeReplacePreamble with no layout wrapper", func(t *testing.T) {
		base := `<p>preamble</p><h2>Section A</h2><p>existing</p>`
		before, after := contextAround(base, ModeReplacePreamble, WriteItem{})
		if before != "" {
			t.Errorf("before should be empty with no layout wrapper, got %q", before)
		}
		if !strings.HasPrefix(after, "<h2>Section A</h2>") {
			t.Errorf("after should start at the first heading: %q", after)
		}
	})

	t.Run("contextAround ModeReplacePreamble on a headless page degrades to empty, never panics", func(t *testing.T) {
		base := `<p>no heading anywhere</p>`
		before, after := contextAround(base, ModeReplacePreamble, WriteItem{})
		if before != "" || after != "" {
			t.Errorf("before/after should both be empty when there is no heading to bound the preamble, got (%q, %q)", before, after)
		}
	})
}

func replaceStr(pattern, v string) string {
	return strings.Replace(pattern, "%s", v, 1)
}

// TestContextAround_ReplaceSection pins replace_section's before/after
// asymmetry: unlike end_of_section, its "before" excludes the section body,
// because that body is what is being replaced.
func TestContextAround_ReplaceSection(t *testing.T) {
	base := `<h2>A</h2><p>old</p><h2>B</h2><p>other</p>`
	before, after := contextAround(base, ModeReplaceSection, WriteItem{Heading: "A"})
	if !strings.HasSuffix(before, "<h2>A</h2>") {
		t.Errorf("before should end at the target heading's closing tag: %q", before)
	}
	if strings.Contains(before, "old") {
		t.Errorf("before should not contain the section's existing body text (it is being replaced): %q", before)
	}
	if !strings.HasPrefix(after, "<h2>B</h2>") {
		t.Errorf("after should begin at the next heading: %q", after)
	}
}

// TestContextAround_ReplaceSection_Rename pins the rule that a preview shows
// the page the write will produce: with a rename, the "before" snippet ends at
// the NEW heading. Showing the old one would hide the change being previewed.
func TestContextAround_ReplaceSection_Rename(t *testing.T) {
	base := `<h2 ac:local-id="z">A</h2><p>old</p><h2>B</h2>`
	before, after := contextAround(base, ModeReplaceSection, WriteItem{Heading: "A", NewHeading: "A & C"})
	if !strings.HasSuffix(before, `<h2 ac:local-id="z">A &amp; C</h2>`) {
		t.Errorf("before should end at the escaped new heading, attributes intact: %q", before)
	}
	if !strings.HasPrefix(after, "<h2>B</h2>") {
		t.Errorf("after should begin at the next heading: %q", after)
	}
}

func TestSummariseAction_Rename(t *testing.T) {
	b := BoundaryInfo{AnchorReferences: []string{`ac:link ac:anchor="A"`}}

	t.Run("names the rename and the broken anchors", func(t *testing.T) {
		got := summariseAction(ModeReplaceSection, "A", "B", b)
		if !strings.Contains(got, `rename it to "B"`) {
			t.Errorf("summary does not name the rename: %q", got)
		}
		if !strings.Contains(got, "breaks 1 on-page anchor reference(s)") {
			t.Errorf("summary does not name the broken anchors: %q", got)
		}
	})

	t.Run("says nothing about a rename that was not requested", func(t *testing.T) {
		got := summariseAction(ModeReplaceSection, "A", "", BoundaryInfo{})
		if strings.Contains(got, "rename") || strings.Contains(got, "anchor") {
			t.Errorf("summary invented a rename: %q", got)
		}
	})
}
