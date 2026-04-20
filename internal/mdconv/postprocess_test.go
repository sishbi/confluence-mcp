package mdconv

import (
	"strings"
	"testing"
)

func TestPostprocessStripsListTerminators(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "single terminator between list items",
			in:   "- item one\n<!--THE END-->\n- item two\n",
			want: "- item one\n- item two\n",
		},
		{
			name: "terminator with surrounding whitespace",
			in:   "- item\n\n<!--THE END-->\n\n- next\n",
			want: "- item\n\n- next\n",
		},
		{
			name: "multiple terminators",
			in:   "a\n<!--THE END-->\nb\n<!--THE END-->\nc\n",
			want: "a\nb\nc\n",
		},
		{
			name: "four blank lines collapsed to two",
			in:   "first\n\n\n\n\nsecond\n",
			want: "first\n\nsecond\n",
		},
		{
			name: "three blank lines collapsed to two",
			in:   "first\n\n\n\nsecond\n",
			want: "first\n\nsecond\n",
		},
		{
			name: "terminator and blank-line collapse together",
			in:   "a\n\n<!--THE END-->\n\n\n\nb\n",
			want: "a\n\nb\n",
		},
		{
			name: "no changes when clean",
			in:   "line one\n\nline two\n",
			want: "line one\n\nline two\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := postprocessMarkdown(tc.in)
			if got != tc.want {
				t.Errorf("postprocessMarkdown(%q)\n got:  %q\n want: %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestToMarkdownFixtureNoTheEndComments(t *testing.T) {
	// Storage fragment modelled on §28 of the fixture page: nested lists
	// where the html-to-markdown library historically emits <!--THE END-->
	// between top-level items.
	storage := `<ul>
  <li><p>Top-level A</p></li>
  <li><p>Top-level B</p>
    <ul>
      <li><p>Nested B.1</p></li>
      <li><p>Nested B.2</p>
        <ol>
          <li><p>Numbered 1</p></li>
          <li><p>Numbered 2</p></li>
        </ol>
      </li>
    </ul>
  </li>
  <li><p>Top-level C</p></li>
</ul>`

	got := ToMarkdown(storage)
	if strings.Contains(got, "<!--THE END-->") {
		t.Fatalf("output still contains <!--THE END--> markers:\n%s", got)
	}
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("output has 3+ consecutive newlines, blank-line collapse failed:\n%s", got)
	}
}
