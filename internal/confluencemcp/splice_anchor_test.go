package confluencemcp

import (
	"reflect"
	"strings"
	"testing"
)

func TestFindAnchorReferences(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		heading string
		want    int
		wantIn  string
	}{
		{
			name:    "ac:link anchor naming the heading",
			body:    `<p>See <ac:link ac:anchor="Old Heading"><ac:link-body>jump</ac:link-body></ac:link></p>`,
			heading: "Old Heading",
			want:    1,
			wantIn:  `ac:anchor="Old Heading"`,
		},
		{
			name:    "anchor macro naming the heading",
			body:    `<ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">Old Heading</ac:parameter></ac:structured-macro>`,
			heading: "Old Heading",
			want:    1,
			wantIn:  "anchor macro",
		},
		{
			name:    "entity and whitespace variants still match",
			body:    `<p><ac:link ac:anchor="A &amp;  B"/></p>`,
			heading: "A & B",
			want:    1,
		},
		{
			name:    "anchor naming a different heading",
			body:    `<p><ac:link ac:anchor="Something Else"/></p>`,
			heading: "Old Heading",
			want:    0,
		},
		{
			name:    "body with no anchors",
			body:    `<h2>Old Heading</h2><p>text</p>`,
			heading: "Old Heading",
			want:    0,
		},
		{
			name:    "both forms in one body",
			body:    `<p><ac:link ac:anchor="Old Heading"/></p><ac:structured-macro ac:name="anchor"><ac:parameter ac:name="">Old Heading</ac:parameter></ac:structured-macro>`,
			heading: "Old Heading",
			want:    2,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := findAnchorReferences(tc.body, tc.heading)
			if len(got) != tc.want {
				t.Fatalf("got %d references %v, want %d", len(got), got, tc.want)
			}
			if tc.wantIn != "" && !strings.Contains(strings.Join(got, " "), tc.wantIn) {
				t.Errorf("references %v do not describe %q", got, tc.wantIn)
			}
		})
	}
}

func TestSplice_ReplaceSection_ReportsBrokenAnchors(t *testing.T) {
	body := `<h2>Old</h2><p>old</p><p><ac:link ac:anchor="Old"><ac:link-body>x</ac:link-body></ac:link></p>`

	t.Run("rename reports them", func(t *testing.T) {
		res, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "Old", NewHeading: "New"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if len(res.Boundary.AnchorReferences) != 1 {
			t.Errorf("AnchorReferences = %v, want 1 entry", res.Boundary.AnchorReferences)
		}
	})

	t.Run("no rename reports none", func(t *testing.T) {
		res, err := spliceReplaceSection(body, `<p>new</p>`, SpliceOptions{Heading: "Old"})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got := res.Boundary.AnchorReferences; !reflect.DeepEqual(got, []string(nil)) {
			t.Errorf("AnchorReferences = %v, want nil without a rename", got)
		}
	})
}
