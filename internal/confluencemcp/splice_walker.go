package confluencemcp

import (
	"encoding/xml"
	"io"
	"strings"
)

// walkEvent is one decoded structural event during a walk of a storage body.
type walkEvent struct {
	kind eventKind
	// name is the element local name (for start/end events); empty otherwise.
	name string
	// level is the heading level (1-6) when kind == eventHeadingStart; 0 otherwise.
	level int
	// tokStart, tokEnd are byte offsets into the original body for the token that
	// produced this event — inclusive/exclusive.
	tokStart int
	tokEnd   int
	// Depth counters after applying this token (for start events), or before
	// applying (for end events, so the closing tag reports the depth it belongs to).
	layoutCellDepth      int
	macroDepth           int
	unsafeContainerDepth int
}

type eventKind int

const (
	eventStart eventKind = iota
	eventEnd
	eventHeadingStart
	eventHeadingEnd
)

// unsafeContainerTags is the set of element local-names that make a splice
// target inside them unsafe (we refuse to splice there).
var unsafeContainerTags = map[string]bool{
	"td":         true,
	"th":         true,
	"blockquote": true,
	"li":         true,
}

// walkStorage walks the storage-format body and invokes fn for each structural
// event. Returning an error from fn stops the walk and propagates the error.
// The walker tracks layout-cell, macro, and unsafe-container depth.
//
// Heading events (h1..h6) are emitted as a distinct kind so callers don't need
// to re-parse element names.
func walkStorage(body string, fn func(walkEvent) error) error {
	// Wrap in a root element so the decoder is happy with multiple top-level
	// elements and bare text. The wrapper's offset is stripped from reported
	// positions.
	const wrapper = "<root>"
	const wrapperEnd = "</root>"
	wrapped := wrapper + body + wrapperEnd
	offset := int64(len(wrapper))

	dec := xml.NewDecoder(strings.NewReader(wrapped))
	dec.Strict = false
	dec.AutoClose = nil
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(_ string, r io.Reader) (io.Reader, error) { return r, nil }

	var (
		layoutCellDepth      int
		macroDepth           int
		unsafeContainerDepth int
	)

	for {
		tokStart := dec.InputOffset()
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		tokEnd := dec.InputOffset()
		// Adjust for wrapper offset.
		adjStart := int(tokStart - offset)
		adjEnd := int(tokEnd - offset)
		// Skip the synthetic wrapper's own start/end elements.
		if adjStart < 0 {
			continue
		}
		if adjStart >= len(body) {
			// We've passed the content into the wrapper's closing tag.
			continue
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local
			switch {
			case name == "layout-cell":
				layoutCellDepth++
			case name == "structured-macro":
				macroDepth++
			case unsafeContainerTags[name]:
				unsafeContainerDepth++
			}
			ev := walkEvent{
				kind:                 eventStart,
				name:                 name,
				tokStart:             adjStart,
				tokEnd:               adjEnd,
				layoutCellDepth:      layoutCellDepth,
				macroDepth:           macroDepth,
				unsafeContainerDepth: unsafeContainerDepth,
			}
			if isHeadingName(name) {
				ev.kind = eventHeadingStart
				ev.level = int(name[1] - '0')
			}
			if err := fn(ev); err != nil {
				return err
			}
		case xml.EndElement:
			name := t.Name.Local
			ev := walkEvent{
				kind:     eventEnd,
				name:     name,
				tokStart: adjStart,
				tokEnd:   adjEnd,
			}
			if isHeadingName(name) {
				ev.kind = eventHeadingEnd
				ev.level = int(name[1] - '0')
			}
			// Report with the depth *before* decrementing, so end-tag consumers
			// know which container they're closing.
			ev.layoutCellDepth = layoutCellDepth
			ev.macroDepth = macroDepth
			ev.unsafeContainerDepth = unsafeContainerDepth
			switch {
			case name == "layout-cell":
				if layoutCellDepth > 0 {
					layoutCellDepth--
				}
			case name == "structured-macro":
				if macroDepth > 0 {
					macroDepth--
				}
			case unsafeContainerTags[name]:
				if unsafeContainerDepth > 0 {
					unsafeContainerDepth--
				}
			}
			if err := fn(ev); err != nil {
				return err
			}
		}
	}
}

func isHeadingName(name string) bool {
	if len(name) != 2 || name[0] != 'h' {
		return false
	}
	c := name[1]
	return c >= '1' && c <= '6'
}
