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
		p := buildPreview("42", base, res.Merged, fragment, ModeEnd, "", res.Boundary, "new", "markdown")
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
		p := buildPreview("42", base, res.Merged, fragment, ModeAfterHeading, "A", res.Boundary, "new", "markdown")
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
		p := buildPreview("42", base, res.Merged, fragment, ModeReplaceSection, "A", res.Boundary, "new", "markdown")
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
		p := buildPreview("42", base, res.Merged, fragment, ModeEndOfSection, "A", res.Boundary, "new", "markdown")
		if p.Position != "end_of_section" {
			t.Errorf("Position = %q, want end_of_section", p.Position)
		}
		if p.ActionSummary == "" {
			t.Errorf("ActionSummary empty")
		}
		if p.Context.Before == "" || p.Context.After == "" {
			t.Errorf("Context.Before/After should both be populated")
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
		p := buildPreview("42", base, res.Merged, fragment, ModeReplaceSection, "A", res.Boundary, "x", "markdown")
		if p.Sizes.DeltaBytes >= 0 {
			t.Errorf("DeltaBytes should be negative, got %d", p.Sizes.DeltaBytes)
		}
	})
}

func replaceStr(pattern, v string) string {
	return strings.Replace(pattern, "%s", v, 1)
}

func TestModeString(t *testing.T) {
	tests := []struct {
		name string
		mode Mode
		want string
	}{
		{"end", ModeEnd, "end"},
		{"after_heading", ModeAfterHeading, "after_heading"},
		{"replace_section", ModeReplaceSection, "replace_section"},
		{"end_of_section", ModeEndOfSection, "end_of_section"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modeString(tt.mode); got != tt.want {
				t.Errorf("modeString(%v) = %q, want %q", tt.mode, got, tt.want)
			}
		})
	}
}

func TestSummariseAction_EndOfSectionDistinctFromAfterHeading(t *testing.T) {
	afterHeading := summariseAction(ModeAfterHeading, "A", BoundaryInfo{})
	endOfSection := summariseAction(ModeEndOfSection, "A", BoundaryInfo{})
	if afterHeading == endOfSection {
		t.Errorf("summariseAction should differ between after_heading and end_of_section, got %q for both", afterHeading)
	}
	const want = `Append to end of section "A".`
	if endOfSection != want {
		t.Errorf("summariseAction(ModeEndOfSection, ...) = %q, want %q", endOfSection, want)
	}
}

func TestContextAround_EndOfSection(t *testing.T) {
	base := `<h2>A</h2><p>existing</p><h2>B</h2><p>other</p>`
	before, after := contextAround(base, ModeEndOfSection, "A")
	if !strings.Contains(before, "existing") {
		t.Errorf("before should contain section A's existing content: %q", before)
	}
	if strings.Contains(before, "<h2>B</h2>") {
		t.Errorf("before should not include the next heading: %q", before)
	}
	if !strings.HasPrefix(after, "<h2>B</h2>") {
		t.Errorf("after should begin at the next heading (the splice/stop point): %q", after)
	}
}
