package confluencemcp

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSplice_DispatchesByMode(t *testing.T) {
	body := `<h2>Section A</h2><p>old</p>`
	fragment := `<p>new</p>`

	t.Run("ModeEnd", func(t *testing.T) {
		res, err := Splice(body, fragment, SpliceOptions{Mode: ModeEnd})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Boundary.InsertAnchor == "" {
			t.Errorf("InsertAnchor empty — ModeEnd did not run")
		}
		if res.Boundary.StartAnchor != "" {
			t.Errorf("StartAnchor should be empty for ModeEnd")
		}
	})

	t.Run("ModeAfterHeading", func(t *testing.T) {
		res, err := Splice(body, fragment, SpliceOptions{Mode: ModeAfterHeading, Heading: "Section A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Boundary.InsertAnchor == "" {
			t.Errorf("InsertAnchor empty — ModeAfterHeading did not run")
		}
	})

	t.Run("ModeReplaceSection", func(t *testing.T) {
		res, err := Splice(body, fragment, SpliceOptions{Mode: ModeReplaceSection, Heading: "Section A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Boundary.StartAnchor == "" {
			t.Errorf("StartAnchor empty — ModeReplaceSection did not run")
		}
	})

	t.Run("unknown mode returns ErrNotImplemented", func(t *testing.T) {
		_, err := Splice(body, fragment, SpliceOptions{Mode: Mode(99)})
		if !errors.Is(err, ErrNotImplemented) {
			t.Fatalf("got %v, want ErrNotImplemented", err)
		}
	})
}

// headingObservation is a compact view of a heading seen by the walker, used
// in walker tests.
type headingObservation struct {
	level                int
	layoutCellDepth      int
	macroDepth           int
	unsafeContainerDepth int
}

func TestWalker_TracksDepth(t *testing.T) {
	cases := []struct {
		name string
		body string
		// want is the expected list of (level, layoutCellDepth, macroDepth,
		// unsafeContainerDepth) tuples at eventHeadingStart time.
		want []headingObservation
	}{
		{
			name: "flat no-layout body",
			body: `<h2>Hello</h2><p>text</p>`,
			want: []headingObservation{{2, 0, 0, 0}},
		},
		{
			name: "single layout-cell",
			body: `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Hi</h2></ac:layout-cell></ac:layout-section></ac:layout>`,
			want: []headingObservation{{2, 1, 0, 0}},
		},
		{
			name: "three-column layout",
			body: `<ac:layout><ac:layout-section ac:type="three_equal"><ac:layout-cell><h3>A</h3></ac:layout-cell><ac:layout-cell><h3>B</h3></ac:layout-cell><ac:layout-cell><h3>C</h3></ac:layout-cell></ac:layout-section></ac:layout>`,
			want: []headingObservation{{3, 1, 0, 0}, {3, 1, 0, 0}, {3, 1, 0, 0}},
		},
		{
			name: "heading inside structured-macro body",
			body: `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Outer</h2><ac:structured-macro ac:name="expand"><ac:rich-text-body><h3>Inside</h3></ac:rich-text-body></ac:structured-macro></ac:layout-cell></ac:layout-section></ac:layout>`,
			want: []headingObservation{{2, 1, 0, 0}, {3, 1, 1, 0}},
		},
		{
			name: "heading inside td",
			body: `<table><tbody><tr><td><h4>InTd</h4></td></tr></tbody></table>`,
			want: []headingObservation{{4, 0, 0, 1}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []headingObservation
			err := walkStorage(tc.body, func(ev walkEvent) error {
				if ev.kind == eventHeadingStart {
					got = append(got, headingObservation{
						level:                ev.level,
						layoutCellDepth:      ev.layoutCellDepth,
						macroDepth:           ev.macroDepth,
						unsafeContainerDepth: ev.unsafeContainerDepth,
					})
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walkStorage: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d headings, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("heading %d: got %+v, want %+v", i, got[i], w)
				}
			}
		})
	}
}

func TestLocateHeading(t *testing.T) {
	const cellLayout = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>%s</ac:layout-cell></ac:layout-section></ac:layout>`

	cases := []struct {
		name      string
		body      string
		heading   string
		wantErr   error
		wantLevel int
	}{
		{
			name:      "safe match in layout-cell",
			body:      `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Section A</h2><p>body</p></ac:layout-cell></ac:layout-section></ac:layout>`,
			heading:   "Section A",
			wantLevel: 2,
		},
		{
			name:    "not found",
			body:    `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Section A</h2></ac:layout-cell></ac:layout-section></ac:layout>`,
			heading: "Missing",
			wantErr: ErrHeadingNotFound,
		},
		{
			name:    "only match inside macro",
			body:    `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><ac:structured-macro ac:name="expand"><ac:rich-text-body><h3>Target</h3></ac:rich-text-body></ac:structured-macro></ac:layout-cell></ac:layout-section></ac:layout>`,
			heading: "Target",
			wantErr: ErrHeadingInUnsafeContainer,
		},
		{
			name:    "only match inside td",
			body:    `<table><tbody><tr><td><h4>Target</h4></td></tr></tbody></table>`,
			heading: "Target",
			wantErr: ErrHeadingInUnsafeContainer,
		},
		{
			name:    "two safe matches ambiguous",
			body:    `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Dup</h2><p>a</p><h2>Dup</h2></ac:layout-cell></ac:layout-section></ac:layout>`,
			heading: "Dup",
			wantErr: ErrAmbiguousHeading,
		},
		{
			name:      "one safe one unsafe — picks safe",
			body:      `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Target</h2><ac:structured-macro ac:name="expand"><ac:rich-text-body><h2>Target</h2></ac:rich-text-body></ac:structured-macro></ac:layout-cell></ac:layout-section></ac:layout>`,
			heading:   "Target",
			wantLevel: 2,
		},
		{
			name:      "heading with inline formatting",
			body:      `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>27. <em>Final</em> Notes</h2></ac:layout-cell></ac:layout-section></ac:layout>`,
			heading:   "27. Final Notes",
			wantLevel: 2,
		},
		{
			name:      "heading at document root (no layout)",
			body:      `<h1>Top</h1><p>body</p>`,
			heading:   "Top",
			wantLevel: 1,
		},
		{
			name:      "layout-cell, heading with entity",
			body:      fmt.Sprintf(cellLayout, `<h2>A &amp; B</h2>`),
			heading:   "A & B",
			wantLevel: 2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match, err := locateHeading(tc.body, tc.heading)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got err %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if match.level != tc.wantLevel {
				t.Errorf("got level %d, want %d", match.level, tc.wantLevel)
			}
		})
	}
}

func TestSplice_End(t *testing.T) {
	t.Run("layout-wrapped body: inserts before innermost trailing layout-cell", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>existing</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>new</p>`
		res, err := spliceEnd(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>existing</p><p>new</p></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
		if res.Boundary.Container == "" {
			t.Errorf("Container should be populated")
		}
		if res.Boundary.InsertAnchor == "" {
			t.Errorf("InsertAnchor should be populated")
		}
	})

	t.Run("multi-section layout: inserts into last cell of last section", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>A</p></ac:layout-cell></ac:layout-section><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>B</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>C</p>`
		res, err := spliceEnd(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>A</p></ac:layout-cell></ac:layout-section><ac:layout-section ac:type="fixed-width"><ac:layout-cell><p>B</p><p>C</p></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("no-layout body: appends to end", func(t *testing.T) {
		body := `<p>hello</p>`
		fragment := `<p>world</p>`
		res, err := spliceEnd(body, fragment)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<p>hello</p><p>world</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})
}

func TestSplice_AfterHeading(t *testing.T) {
	t.Run("inserts after matched heading close tag", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Section A</h2><p>body</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>new</p>`
		res, err := spliceAfterHeading(body, fragment, "Section A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Section A</h2><p>new</p><p>body</p></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("heading not found returns ErrHeadingNotFound", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>Section A</h2></ac:layout-cell></ac:layout-section></ac:layout>`
		_, err := spliceAfterHeading(body, `<p>x</p>`, "Missing")
		if !errors.Is(err, ErrHeadingNotFound) {
			t.Fatalf("got %v, want ErrHeadingNotFound", err)
		}
	})

	t.Run("heading in macro returns ErrHeadingInUnsafeContainer", func(t *testing.T) {
		body := `<ac:structured-macro ac:name="expand"><ac:rich-text-body><h3>Target</h3></ac:rich-text-body></ac:structured-macro>`
		_, err := spliceAfterHeading(body, `<p>x</p>`, "Target")
		if !errors.Is(err, ErrHeadingInUnsafeContainer) {
			t.Fatalf("got %v, want ErrHeadingInUnsafeContainer", err)
		}
	})

	t.Run("ambiguous heading returns ErrAmbiguousHeading", func(t *testing.T) {
		body := `<h2>Dup</h2><p>a</p><h2>Dup</h2>`
		_, err := spliceAfterHeading(body, `<p>x</p>`, "Dup")
		if !errors.Is(err, ErrAmbiguousHeading) {
			t.Fatalf("got %v, want ErrAmbiguousHeading", err)
		}
	})

	t.Run("heading followed by macro: fragment between them", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>A</h2><ac:structured-macro ac:name="info"><ac:rich-text-body><p>old</p></ac:rich-text-body></ac:structured-macro></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>note</p>`
		res, err := spliceAfterHeading(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>A</h2><p>note</p><ac:structured-macro ac:name="info"><ac:rich-text-body><p>old</p></ac:rich-text-body></ac:structured-macro></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})
}

func TestSplice_ReplaceSection(t *testing.T) {
	const cell = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>%s</ac:layout-cell></ac:layout-section></ac:layout>`

	t.Run("stops at next same-level heading in same cell", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old1</p><p>old2</p><h2>B</h2><p>keep</p>`)
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p><h2>B</h2><p>keep</p>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
		if res.Boundary.ReplacedByteCount <= 0 {
			t.Errorf("ReplacedByteCount should be > 0")
		}
	})

	t.Run("stops at next higher-level heading", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h3>A</h3><p>old</p><h2>Top</h2>`)
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h3>A</h3><p>new</p><h2>Top</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("deeper headings between target and stop are included in replace", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><h3>sub</h3><p>p2</p><h2>B</h2>`)
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p><h2>B</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("stops at end of layout-cell when no later heading", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old</p>`)
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("next same-level heading in sibling column is ignored", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="three_equal"><ac:layout-cell><h2>A</h2><p>old</p></ac:layout-cell><ac:layout-cell><h2>B</h2><p>other</p></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="three_equal"><ac:layout-cell><h2>A</h2><p>new</p></ac:layout-cell><ac:layout-cell><h2>B</h2><p>other</p></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("next same-level heading in later section is ignored", func(t *testing.T) {
		body := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>A</h2><p>old</p></ac:layout-cell></ac:layout-section><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>B</h2></ac:layout-cell></ac:layout-section></ac:layout>`
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>A</h2><p>new</p></ac:layout-cell></ac:layout-section><ac:layout-section ac:type="fixed-width"><ac:layout-cell><h2>B</h2></ac:layout-cell></ac:layout-section></ac:layout>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("no-layout page: extends to end of body when no later heading", func(t *testing.T) {
		body := `<h2>A</h2><p>old</p>`
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<h2>A</h2><p>new</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("heading in macro returns ErrHeadingInUnsafeContainer", func(t *testing.T) {
		body := `<ac:structured-macro ac:name="expand"><ac:rich-text-body><h3>T</h3></ac:rich-text-body></ac:structured-macro>`
		_, err := spliceReplaceSection(body, `<p>new</p>`, "T")
		if !errors.Is(err, ErrHeadingInUnsafeContainer) {
			t.Fatalf("got %v, want ErrHeadingInUnsafeContainer", err)
		}
	})

	t.Run("heading in td returns ErrHeadingInUnsafeContainer", func(t *testing.T) {
		body := `<table><tbody><tr><td><h4>T</h4></td></tr></tbody></table>`
		_, err := spliceReplaceSection(body, `<p>new</p>`, "T")
		if !errors.Is(err, ErrHeadingInUnsafeContainer) {
			t.Fatalf("got %v, want ErrHeadingInUnsafeContainer", err)
		}
	})

	t.Run("ReplacedElementSummary counts top-level replaced elements", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><p>p2</p><ul><li><p>li</p></li></ul><h2>B</h2>`)
		res, err := spliceReplaceSection(body, `<p>new</p>`, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(res.Boundary.ReplacedElementSummary) == 0 {
			t.Errorf("ReplacedElementSummary empty")
		}
		// Expect p×2, ul×1 (order doesn't matter, but we know the implementation
		// iterates in document order).
		joined := strings.Join(res.Boundary.ReplacedElementSummary, " ")
		if !strings.Contains(joined, "<p> x 2") {
			t.Errorf("want '<p> x 2' in summary, got %v", res.Boundary.ReplacedElementSummary)
		}
		if !strings.Contains(joined, "<ul> x 1") {
			t.Errorf("want '<ul> x 1' in summary, got %v", res.Boundary.ReplacedElementSummary)
		}
	})
}
