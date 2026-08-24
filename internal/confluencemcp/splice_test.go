package confluencemcp

import (
	"errors"
	"fmt"
	"reflect"
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

	t.Run("ModeEndOfSection", func(t *testing.T) {
		res, err := Splice(body, fragment, SpliceOptions{Mode: ModeEndOfSection, Heading: "Section A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if res.Boundary.InsertAnchor == "" {
			t.Errorf("InsertAnchor empty — ModeEndOfSection did not run")
		}
		if res.Boundary.StartAnchor != "" {
			t.Errorf("StartAnchor should be empty for ModeEndOfSection")
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
		{
			name: "heading inside ac:adf-content panel",
			body: `<ac:adf-extension><ac:adf-node type="panel"><ac:adf-content><h2>Inside</h2></ac:adf-content></ac:adf-node></ac:adf-extension>`,
			want: []headingObservation{{2, 0, 0, 1}},
		},
		{
			name: "heading inside ac:adf-fallback",
			body: `<ac:adf-extension><ac:adf-node type="panel"><ac:adf-fallback><div><h2>Inside</h2></div></ac:adf-fallback></ac:adf-node></ac:adf-extension>`,
			want: []headingObservation{{2, 0, 0, 1}},
		},
		{
			name: "heading inside ac:task-body",
			body: `<ac:task-list><ac:task><ac:task-body><h3>Inside</h3></ac:task-body></ac:task></ac:task-list>`,
			want: []headingObservation{{3, 0, 0, 1}},
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
			name:    "only match inside ac:adf-content panel",
			body:    `<ac:adf-extension><ac:adf-node type="panel"><ac:adf-content><h2>Target</h2></ac:adf-content></ac:adf-node></ac:adf-extension>`,
			heading: "Target",
			wantErr: ErrHeadingInUnsafeContainer,
		},
		{
			name:    "only match inside ac:task-body",
			body:    `<ac:task-list><ac:task><ac:task-body><h3>Target</h3></ac:task-body></ac:task></ac:task-list>`,
			heading: "Target",
			wantErr: ErrHeadingInUnsafeContainer,
		},
		{
			name:    "ADF panel with both ac:adf-content and ac:adf-fallback — fallback-nested heading is also unsafe",
			body:    `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-attribute key="panel-type">info</ac:adf-attribute><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node><ac:adf-fallback><div class="panel"><div class="panelContent"><h2>Nested</h2><p>c</p></div></div></ac:adf-fallback></ac:adf-extension><h2>B</h2>`,
			heading: "Nested",
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

func TestLocateHeading_ReportsInnerRange(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		heading   string
		wantInner string
	}{
		{
			name:      "heading with attributes",
			body:      `<p>before</p><h2 ac:local-id="x">Section A</h2><p>after</p>`,
			heading:   "Section A",
			wantInner: `Section A`,
		},
		{
			name:      "heading with inline markup",
			body:      `<h3>27. <em>Final</em> Notes</h3><p>x</p>`,
			heading:   "27. Final Notes",
			wantInner: `27. <em>Final</em> Notes`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			match, err := locateHeading(tc.body, tc.heading)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got := tc.body[match.headingOpenEndOff:match.headingCloseStartOff]; got != tc.wantInner {
				t.Errorf("inner range: got %q, want %q", got, tc.wantInner)
			}
			// The pre-existing offsets must still span the whole element, since
			// the replace splice measures its region from headingEndOff.
			outer := tc.body[match.headingStartOff:match.headingEndOff]
			if !strings.HasPrefix(outer, fmt.Sprintf("<h%d", match.level)) ||
				!strings.HasSuffix(outer, fmt.Sprintf("</h%d>", match.level)) {
				t.Errorf("outer range %q does not span the whole heading element", outer)
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

func TestSplice_ReplaceSection_RenamesHeading(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		newHeading string
		opts       SpliceOptions
		want       string
		wantRename *HeadingRename
	}{
		{
			name:       "preserves level and attributes",
			body:       `<h2 ac:local-id="z">Old</h2><p>old</p><h2>B</h2><p>keep</p>`,
			newHeading: "New",
			want:       `<h2 ac:local-id="z">New</h2><p>new</p><h2>B</h2><p>keep</p>`,
			wantRename: &HeadingRename{From: "Old", To: "New"},
		},
		{
			name:       "escapes markup characters in the new text",
			body:       `<h2>Old</h2><p>old</p>`,
			newHeading: `A & B <x>`,
			want:       `<h2>A &amp; B &lt;x&gt;</h2><p>new</p>`,
			wantRename: &HeadingRename{From: "Old", To: `A & B <x>`},
		},
		{
			name:       "renames while replacing subsections",
			body:       `<h2>Old</h2><p>old</p><h3>sub</h3><p>p</p><h2>B</h2>`,
			newHeading: "New",
			opts:       SpliceOptions{IncludeSubsections: true},
			want:       `<h2>New</h2><p>new</p><h2>B</h2>`,
			wantRename: &HeadingRename{From: "Old", To: "New"},
		},
		{
			name: "no rename leaves the heading byte-identical",
			body: `<h2 ac:local-id="z">Old</h2><p>old</p>`,
			want: `<h2 ac:local-id="z">Old</h2><p>new</p>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			opts.Heading = "Old"
			opts.NewHeading = tc.newHeading

			res, err := spliceReplaceSection(tc.body, `<p>new</p>`, opts)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if res.Merged != tc.want {
				t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, tc.want)
			}
			if !reflect.DeepEqual(res.Boundary.HeadingRenamed, tc.wantRename) {
				t.Errorf("HeadingRenamed = %+v, want %+v", res.Boundary.HeadingRenamed, tc.wantRename)
			}
		})
	}
}

func TestSplice_ReplaceSection(t *testing.T) {
	const cell = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>%s</ac:layout-cell></ac:layout-section></ac:layout>`

	t.Run("stops at next same-level heading in same cell", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old1</p><p>old2</p><h2>B</h2><p>keep</p>`)
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
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
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h3>A</h3><p>new</p><h2>Top</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	// Regression for the real page edit that lost "7.1 Delivery sequence" when
	// its parent h2 was replaced: by default a replace stops at the first
	// subsection, and the subsections are named back to the caller either way.
	t.Run("subsections", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><h3>sub one</h3><p>p2</p><h3>sub two</h3><p>p3</p><h2>B</h2>`)
		fragment := `<p>new</p>`

		t.Run("default keeps every subsection", func(t *testing.T) {
			res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p><h3>sub one</h3><p>p2</p><h3>sub two</h3><p>p3</p><h2>B</h2>`)
			if res.Merged != want {
				t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
			}
			if got := res.Boundary.PreservedSections; !reflect.DeepEqual(got, []string{"sub one", "sub two"}) {
				t.Errorf("PreservedSections = %v", got)
			}
			if got := res.Boundary.ReplacedSections; got != nil {
				t.Errorf("ReplacedSections should be nil for a narrow replace, got %v", got)
			}
			if got := res.Boundary.EndAnchor; got != "before next heading of any level" {
				t.Errorf("EndAnchor = %q", got)
			}
			// Only the section's own <p> is replaced — the subsections' content
			// must not be counted.
			if got := res.Boundary.ReplacedElementSummary; !reflect.DeepEqual(got, []string{"<p> x 1"}) {
				t.Errorf("ReplacedElementSummary = %v", got)
			}
		})

		t.Run("includeSubsections removes them", func(t *testing.T) {
			res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A", IncludeSubsections: true})
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p><h2>B</h2>`)
			if res.Merged != want {
				t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
			}
			if got := res.Boundary.ReplacedSections; !reflect.DeepEqual(got, []string{"sub one", "sub two"}) {
				t.Errorf("ReplacedSections = %v", got)
			}
			if got := res.Boundary.PreservedSections; got != nil {
				t.Errorf("PreservedSections should be nil for a wide replace, got %v", got)
			}
			if got := res.Boundary.EndAnchor; got != "before next heading at same or higher level" {
				t.Errorf("EndAnchor = %q", got)
			}
		})

		t.Run("no subsections: both extents behave identically", func(t *testing.T) {
			plain := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><h2>B</h2>`)
			want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p><h2>B</h2>`)
			for _, includeSubsections := range []bool{false, true} {
				res, err := spliceReplaceSection(plain, fragment, SpliceOptions{Heading: "A", IncludeSubsections: includeSubsections})
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if res.Merged != want {
					t.Errorf("includeSubsections=%v merged mismatch\n got: %s\nwant: %s", includeSubsections, res.Merged, want)
				}
				if res.Boundary.ReplacedSections != nil || res.Boundary.PreservedSections != nil {
					t.Errorf("includeSubsections=%v should report no nested sections", includeSubsections)
				}
			}
		})
	})

	t.Run("stops at end of layout-cell when no later heading", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old</p>`)
		fragment := `<p>new</p>`
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
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
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
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
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
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
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
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
		_, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "T"})
		if !errors.Is(err, ErrHeadingInUnsafeContainer) {
			t.Fatalf("got %v, want ErrHeadingInUnsafeContainer", err)
		}
	})

	t.Run("heading in td returns ErrHeadingInUnsafeContainer", func(t *testing.T) {
		body := `<table><tbody><tr><td><h4>T</h4></td></tr></tbody></table>`
		_, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "T"})
		if !errors.Is(err, ErrHeadingInUnsafeContainer) {
			t.Fatalf("got %v, want ErrHeadingInUnsafeContainer", err)
		}
	})

	t.Run("regression: same-or-higher heading nested inside an unsafe container does not become the stop, and the enclosing container is fully replaced", func(t *testing.T) {
		// Well-formed in every case: the whole enclosing element is removed as
		// one unit (its own open and close tags travel together), never
		// leaving an orphaned open tag or a dangling close tag behind.
		cases := []struct {
			name string
			body string
			want string
		}{
			{
				name: "td",
				body: `<h2>A</h2><p>x</p><table><tbody><tr><td><h2>Nested</h2><p>c</p></td></tr></tbody></table><h2>B</h2>`,
				want: `<h2>A</h2><p>NEW</p><h2>B</h2>`,
			},
			{
				name: "ADF panel (ac:adf-content)",
				body: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node></ac:adf-extension><h2>B</h2>`,
				want: `<h2>A</h2><p>NEW</p><h2>B</h2>`,
			},
			{
				name: "ADF panel with both ac:adf-content and ac:adf-fallback",
				body: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-attribute key="panel-type">info</ac:adf-attribute><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node><ac:adf-fallback><div class="panel"><div class="panelContent"><h2>Nested</h2><p>c</p></div></div></ac:adf-fallback></ac:adf-extension><h2>B</h2>`,
				want: `<h2>A</h2><p>NEW</p><h2>B</h2>`,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := spliceReplaceSection(tc.body, `<p>NEW</p>`, SpliceOptions{Heading: "A"})
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if res.Merged != tc.want {
					t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, tc.want)
				}
			})
		}
	})

	t.Run("ReplacedElementSummary does not double-count when a nested container shares the top-level element's tag name", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><ul><li><ul><li><p>x</p></li></ul><p>y</p></li></ul><h2>B</h2>`)
		res, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		// The inner </ul> (nested two <li> deep) must not be mistaken for the
		// close of the top-level <ul>, which would otherwise let <p>y</p> be
		// counted as a second top-level sibling.
		want := []string{"<ul> x 1"}
		if len(res.Boundary.ReplacedElementSummary) != len(want) || res.Boundary.ReplacedElementSummary[0] != want[0] {
			t.Errorf("got %v, want %v", res.Boundary.ReplacedElementSummary, want)
		}
	})

	t.Run("ReplacedElementSummary includes a top-level sibling that is itself an unsafe-container tag", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want []string
		}{
			{
				name: "blockquote sibling",
				body: `<h2>A</h2><p>x</p><blockquote><p>q</p></blockquote><p>y</p><h2>B</h2>`,
				want: []string{"<p> x 2", "<blockquote> x 1"},
			},
			{
				name: "adf-extension sibling",
				body: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-content><p>q</p></ac:adf-content></ac:adf-node></ac:adf-extension><p>y</p><h2>B</h2>`,
				want: []string{"<p> x 2", "<adf-extension> x 1"},
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := spliceReplaceSection(tc.body, `<p>new</p>`, SpliceOptions{Heading: "A"})
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				want := `<h2>A</h2><p>new</p><h2>B</h2>`
				if res.Merged != want {
					t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
				}
				if len(res.Boundary.ReplacedElementSummary) != len(tc.want) {
					t.Fatalf("got %v, want %v", res.Boundary.ReplacedElementSummary, tc.want)
				}
				for i, w := range tc.want {
					if res.Boundary.ReplacedElementSummary[i] != w {
						t.Errorf("got %v, want %v", res.Boundary.ReplacedElementSummary, tc.want)
					}
				}
			})
		}
	})

	t.Run("ReplacedElementSummary counts top-level replaced elements", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><p>p2</p><ul><li><p>li</p></li></ul><h2>B</h2>`)
		res, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "A"})
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

	t.Run("fragment that redundantly starts with the target heading is de-duplicated", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>Data scrubbing</h2><p>old</p><h2>B</h2>`)
		fragment := `<h2>Data scrubbing</h2><p>new</p>`
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "Data scrubbing"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>Data scrubbing</h2><p>new</p><h2>B</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("leading heading with different level but same text is stripped", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old</p><h2>B</h2>`)
		fragment := `<h3>A</h3><p>new</p>`
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>new</p><h2>B</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("leading heading with different text is preserved", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old</p><h2>B</h2>`)
		fragment := `<h3>Details</h3><p>new</p>`
		res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "A"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><h3>Details</h3><p>new</p><h2>B</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	// A rename accepts either name at the head of the fragment: the agent may
	// send the section as it was, or as it is about to become.
	t.Run("rename strips a leading heading carrying either name", func(t *testing.T) {
		body := `<h2>Old</h2><p>old</p><h2>B</h2>`
		want := `<h2>New</h2><p>new</p><h2>B</h2>`
		for _, fragment := range []string{
			`<h2>Old</h2><p>new</p>`,
			`<h2>New</h2><p>new</p>`,
			`<p>new</p>`,
		} {
			res, err := spliceReplaceSection(body, fragment, SpliceOptions{Heading: "Old", NewHeading: "New"})
			if err != nil {
				t.Fatalf("fragment %q: unexpected err: %v", fragment, err)
			}
			if res.Merged != want {
				t.Errorf("fragment %q\n got: %s\nwant: %s", fragment, res.Merged, want)
			}
		}
	})

	t.Run("rename keeps a leading heading naming a third section", func(t *testing.T) {
		body := `<h2>Old</h2><p>old</p><h2>B</h2>`
		res, err := spliceReplaceSection(body, `<h3>Details</h3><p>new</p>`, SpliceOptions{Heading: "Old", NewHeading: "New"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<h2>New</h2><h3>Details</h3><p>new</p><h2>B</h2>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})
}

func TestSplice_ReplaceSection_RejectsBadRename(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		heading    string
		newHeading string
		wantErr    error
		wantInMsg  string
	}{
		{
			name:       "new text normalises equal to the old",
			body:       `<h2>A &amp; B</h2><p>old</p>`,
			heading:    "A & B",
			newHeading: "A &  B",
			wantErr:    ErrRenameNoOp,
		},
		{
			name:       "new text names another heading on the page",
			body:       `<h2>Old</h2><p>old</p><h2>Taken</h2><p>x</p>`,
			heading:    "Old",
			newHeading: "Taken",
			wantErr:    ErrRenameAmbiguous,
			wantInMsg:  "Taken",
		},
		{
			name:       "heading holding a mention",
			body:       `<h2>Owned by <ac:link><ri:user ri:account-id="u1"/></ac:link></h2><p>old</p>`,
			heading:    "Owned by",
			newHeading: "Owned by the team",
			wantErr:    ErrHeadingHasChildren,
			wantInMsg:  "ac:link",
		},
		{
			name:       "heading holding a status lozenge macro",
			body:       `<h2>Rollout <ac:structured-macro ac:name="status"><ac:parameter ac:name="title">Done</ac:parameter></ac:structured-macro></h2><p>old</p>`,
			heading:    "Rollout Done",
			newHeading: "Rollout complete",
			wantErr:    ErrHeadingHasChildren,
			wantInMsg:  "ac:structured-macro",
		},
		{
			name:       "heading holding an emoticon",
			body:       `<h2>Ship it <ac:emoticon ac:name="smile"/></h2><p>old</p>`,
			heading:    "Ship it",
			newHeading: "Shipped",
			wantErr:    ErrHeadingHasChildren,
			wantInMsg:  "ac:emoticon",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := spliceReplaceSection(tc.body, `<p>new</p>`, SpliceOptions{
				Heading: tc.heading, NewHeading: tc.newHeading,
			})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got err %v, want %v", err, tc.wantErr)
			}
			if tc.wantInMsg != "" && !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Errorf("error %q does not name %q", err, tc.wantInMsg)
			}
			if res.Merged != "" {
				t.Errorf("no merged body should be produced on a rejected rename, got %q", res.Merged)
			}
		})
	}

	// A heading that is only a candidate inside a macro or unsafe container can
	// never be located, so renaming onto its text cannot create an ambiguity.
	t.Run("heading inside a macro does not make the new name ambiguous", func(t *testing.T) {
		body := `<h2>Old</h2><p>old</p><ac:structured-macro ac:name="expand"><ac:rich-text-body><h2>Taken</h2></ac:rich-text-body></ac:structured-macro>`
		if _, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "Old", NewHeading: "Taken"}); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	})

	t.Run("entities and whitespace alone are not element children", func(t *testing.T) {
		body := `<h2> A &amp; B </h2><p>old</p>`
		if _, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "A & B", NewHeading: "C"}); err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	})

	// Inline formatting is markup the caller DID see in the Markdown they read,
	// and it carries nothing but presentation — renaming replaces it along with
	// the words, which is what the caller asked for.
	t.Run("inline formatting is renamed, not refused", func(t *testing.T) {
		for _, body := range []string{
			`<h2>27. <em>Final</em> Notes</h2><p>old</p>`,
			`<h2>27. <strong>Final</strong> <code>Notes</code></h2><p>old</p>`,
			`<h2>27. <span class="x">Final</span> Notes</h2><p>old</p>`,
		} {
			res, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{
				Heading: "27. Final Notes", NewHeading: "28. Notes",
			})
			if err != nil {
				t.Fatalf("body %q: unexpected err: %v", body, err)
			}
			if want := `<h2>28. Notes</h2><p>new</p>`; res.Merged != want {
				t.Errorf("body %q\n got: %s\nwant: %s", body, res.Merged, want)
			}
		}
	})
}

func TestSplice_EndOfSection(t *testing.T) {
	const cell = `<ac:layout><ac:layout-section ac:type="fixed-width"><ac:layout-cell>%s</ac:layout-cell></ac:layout-section></ac:layout>`

	t.Run("regression: new sibling section lands between, first section keeps its paragraph", func(t *testing.T) {
		body := `<h2>Section 6.6</h2><p>paragraph for 6.6</p><h2>Section 6.8</h2><p>other</p>`
		fragment := `<h2>Section 6.7</h2><p>new content</p>`
		res, err := spliceEndOfSection(body, fragment, "Section 6.6")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<h2>Section 6.6</h2><p>paragraph for 6.6</p><h2>Section 6.7</h2><p>new content</p><h2>Section 6.8</h2><p>other</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.InsertAnchor != "before next heading at same or higher level" {
			t.Errorf("got InsertAnchor %q, want %q", res.Boundary.InsertAnchor, "before next heading at same or higher level")
		}
	})

	t.Run("appends to section that already has content", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>existing</p><h2>B</h2>`)
		fragment := `<p>new</p>`
		res, err := spliceEndOfSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>existing</p><p>new</p><h2>B</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		// ModeEndOfSection must not populate the replace-only BoundaryInfo fields.
		if res.Boundary.EndAnchor != "" {
			t.Errorf("EndAnchor should be empty, got %q", res.Boundary.EndAnchor)
		}
		if res.Boundary.StartAnchor != "" {
			t.Errorf("StartAnchor should be empty, got %q", res.Boundary.StartAnchor)
		}
		if res.Boundary.ReplacedByteCount != 0 {
			t.Errorf("ReplacedByteCount should be 0, got %d", res.Boundary.ReplacedByteCount)
		}
		if res.Boundary.ReplacedElementSummary != nil {
			t.Errorf("ReplacedElementSummary should be nil, got %v", res.Boundary.ReplacedElementSummary)
		}
	})

	t.Run("last section on the page with no layout: inserts at end of body", func(t *testing.T) {
		body := `<h2>A</h2><p>only</p>`
		fragment := `<p>new</p>`
		res, err := spliceEndOfSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<h2>A</h2><p>only</p><p>new</p>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.InsertAnchor != "end of document root" {
			t.Errorf("got InsertAnchor %q, want %q", res.Boundary.InsertAnchor, "end of document root")
		}
	})

	t.Run("section ending at ac:layout-cell close: inserts before the close, never crosses it", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>old</p>`)
		fragment := `<p>new</p>`
		res, err := spliceEndOfSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>old</p><p>new</p>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.CrossesLayout {
			t.Errorf("CrossesLayout should be false")
		}
		if res.Boundary.InsertAnchor != "end of ac:layout-cell" {
			t.Errorf("got InsertAnchor %q, want %q", res.Boundary.InsertAnchor, "end of ac:layout-cell")
		}
	})

	t.Run("subsection present: stop is the next h2, fragment lands after the h3's content", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><h3>sub</h3><p>p2</p><h2>B</h2>`)
		fragment := `<p>new</p>`
		res, err := spliceEndOfSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>p1</p><h3>sub</h3><p>p2</p><p>new</p><h2>B</h2>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("fragment beginning with a heading matching the target text is preserved, not stripped", func(t *testing.T) {
		body := `<h2>Data scrubbing</h2><p>old</p><h2>B</h2>`
		fragment := `<h2>Data scrubbing</h2><p>new</p>`
		res, err := spliceEndOfSection(body, fragment, "Data scrubbing")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := `<h2>Data scrubbing</h2><p>old</p><h2>Data scrubbing</h2><p>new</p><h2>B</h2>`
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
	})

	t.Run("passthrough errors from locateHeading", func(t *testing.T) {
		cases := []struct {
			name    string
			body    string
			heading string
			wantErr error
		}{
			{
				name:    "heading not found",
				body:    `<h2>A</h2>`,
				heading: "Missing",
				wantErr: ErrHeadingNotFound,
			},
			{
				name:    "ambiguous heading",
				body:    `<h2>Dup</h2><p>a</p><h2>Dup</h2>`,
				heading: "Dup",
				wantErr: ErrAmbiguousHeading,
			},
			{
				name:    "heading in macro",
				body:    `<ac:structured-macro ac:name="expand"><ac:rich-text-body><h3>T</h3></ac:rich-text-body></ac:structured-macro>`,
				heading: "T",
				wantErr: ErrHeadingInUnsafeContainer,
			},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				_, err := spliceEndOfSection(tc.body, `<p>x</p>`, tc.heading)
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v, want %v", err, tc.wantErr)
				}
			})
		}
	})

	t.Run("regression: fragment lands after the nested same-or-higher heading's unsafe container, never inside it", func(t *testing.T) {
		cases := []struct {
			name string
			body string
			want string
		}{
			{
				name: "td",
				body: `<h2>A</h2><p>x</p><table><tbody><tr><td><h2>Nested</h2><p>c</p></td></tr></tbody></table><h2>B</h2>`,
				want: `<h2>A</h2><p>x</p><table><tbody><tr><td><h2>Nested</h2><p>c</p></td></tr></tbody></table><p>NEW</p><h2>B</h2>`,
			},
			{
				name: "li (list)",
				body: `<h3>A</h3><p>x</p><ul><li><h3>Nested</h3><p>c</p></li></ul><h3>B</h3>`,
				want: `<h3>A</h3><p>x</p><ul><li><h3>Nested</h3><p>c</p></li></ul><p>NEW</p><h3>B</h3>`,
			},
			{
				name: "ADF panel (ac:adf-content)",
				body: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node></ac:adf-extension><h2>B</h2>`,
				want: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node></ac:adf-extension><p>NEW</p><h2>B</h2>`,
			},
			{
				name: "ADF panel with both ac:adf-content and ac:adf-fallback",
				body: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-attribute key="panel-type">info</ac:adf-attribute><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node><ac:adf-fallback><div class="panel"><div class="panelContent"><h2>Nested</h2><p>c</p></div></div></ac:adf-fallback></ac:adf-extension><h2>B</h2>`,
				want: `<h2>A</h2><p>x</p><ac:adf-extension><ac:adf-node type="panel"><ac:adf-attribute key="panel-type">info</ac:adf-attribute><ac:adf-content><h2>Nested</h2><p>c</p></ac:adf-content></ac:adf-node><ac:adf-fallback><div class="panel"><div class="panelContent"><h2>Nested</h2><p>c</p></div></div></ac:adf-fallback></ac:adf-extension><p>NEW</p><h2>B</h2>`,
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				res, err := spliceEndOfSection(tc.body, `<p>NEW</p>`, "A")
				if err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				if res.Merged != tc.want {
					t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, tc.want)
				}
			})
		}
	})

	t.Run("regression: only same-or-higher heading is inside an unsafe container — stop falls through to layout-cell close", func(t *testing.T) {
		body := fmt.Sprintf(cell, `<h2>A</h2><p>x</p><table><tbody><tr><td><h2>Nested</h2><p>c</p></td></tr></tbody></table>`)
		fragment := `<p>NEW</p>`
		res, err := spliceEndOfSection(body, fragment, "A")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		want := fmt.Sprintf(cell, `<h2>A</h2><p>x</p><table><tbody><tr><td><h2>Nested</h2><p>c</p></td></tr></tbody></table><p>NEW</p>`)
		if res.Merged != want {
			t.Errorf("merged mismatch\n got: %s\nwant: %s", res.Merged, want)
		}
		if res.Boundary.InsertAnchor != "end of ac:layout-cell" {
			t.Errorf("got InsertAnchor %q, want %q", res.Boundary.InsertAnchor, "end of ac:layout-cell")
		}
	})
}
