package confluencemcp

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// chunkCursor identifies the resume point for a chunked page read.
//
// Mode is "section" when SectionIdx points at a heading in the cached page's
// sections slice, or "offset" when Offset is a byte index into the cached
// Markdown. Offset is only used when a single section exceeds maxPageSize and
// therefore needs to be sliced internally.
type chunkCursor struct {
	PageID     string `json:"page_id"`
	Mode       string `json:"mode"`
	SectionIdx int    `json:"section_idx,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

func encodeChunkToken(c chunkCursor) (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeChunkToken(tok string) (chunkCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		return chunkCursor{}, err
	}
	var c chunkCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return chunkCursor{}, err
	}
	return c, nil
}

// chunkPage returns the next chunk body plus a continuation token. The token
// is empty when no more content remains.
//
// When c is nil, chunking starts at the beginning of md. Otherwise c.Mode
// selects between section-based resumption (preferred) and byte-offset
// resumption (used when a single section exceeds maxPageSize).
//
// Chunks end at the second H2 >= start when possible, so the first chunk
// always contains the document prologue plus the first H2 section. On
// continuation calls start is itself at an H2, so "second H2 >= start" is the
// next H2 after the current one.
func chunkPage(md string, sections []section, pageID string, c *chunkCursor) (string, string) {
	start := 0
	if c != nil {
		switch c.Mode {
		case "offset":
			start = c.Offset
		case "section":
			if c.SectionIdx >= 0 && c.SectionIdx < len(sections) {
				start = sections[c.SectionIdx].Start
			}
		}
	}
	if start < 0 {
		start = 0
	}
	if start >= len(md) {
		return "", ""
	}

	end := len(md)
	next := ""

	// Locate H2 cut points at or after start.
	for i, s := range sections {
		if s.Level != 2 || s.Start < start {
			continue
		}
		// Skip the first H2 encountered — it is (or starts) the section we
		// are about to emit. The second one is where the next chunk begins.
		nextI, nextStart, ok := findSecondH2(sections, i)
		if !ok {
			break
		}
		if nextStart-start <= maxPageSize {
			end = nextStart
			tok, _ := encodeChunkToken(chunkCursor{
				PageID: pageID, Mode: "section", SectionIdx: nextI,
			})
			next = tok
		}
		break
	}

	// Byte-offset fallback when the chosen slice is still too large — e.g.
	// no H2 boundary fits in maxPageSize, or no H2s remain at all.
	if end-start > maxPageSize {
		cut := start + maxPageSize
		if nl := strings.LastIndex(md[start:cut], "\n"); nl > 0 {
			cut = start + nl
		}
		end = cut
		tok, _ := encodeChunkToken(chunkCursor{
			PageID: pageID, Mode: "offset", Offset: cut,
		})
		next = tok
	}

	return strings.TrimRight(md[start:end], "\n"), next
}

// findSecondH2 returns the index and byte offset of the H2 that follows the
// H2 at fromIdx. Used to decide where a chunk should stop.
func findSecondH2(sections []section, fromIdx int) (int, int, bool) {
	for j := fromIdx + 1; j < len(sections); j++ {
		if sections[j].Level == 2 {
			return j, sections[j].Start, true
		}
	}
	return 0, 0, false
}
