package confluencemcp

import "fmt"

// spliceEnd inserts fragment at the end of body. If body contains at least one
// <ac:layout-cell>, the fragment is inserted immediately before the closing
// tag of the last layout-cell encountered (the innermost trailing cell). For
// bodies without a layout wrapper, the fragment is appended verbatim.
func spliceEnd(body, fragment string) (SpliceResult, error) {
	// Find the byte offset of the last </ac:layout-cell> close event.
	var (
		lastLayoutCellEndStart = -1
		haveLayoutCell         bool
	)

	err := walkStorage(body, func(ev walkEvent) error {
		if ev.kind == eventEnd && ev.name == "layout-cell" {
			lastLayoutCellEndStart = ev.tokStart
			haveLayoutCell = true
		}
		return nil
	})
	if err != nil {
		return SpliceResult{}, fmt.Errorf("walk body: %w", err)
	}

	if !haveLayoutCell {
		merged := body + fragment
		return SpliceResult{
			Merged: merged,
			Boundary: BoundaryInfo{
				InsertAnchor: "end of body (no layout wrapper)",
				Container:    "document root",
			},
		}, nil
	}

	merged := body[:lastLayoutCellEndStart] + fragment + body[lastLayoutCellEndStart:]
	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			InsertAnchor: "before final </ac:layout-cell>",
			Container:    "ac:layout-cell",
		},
	}, nil
}
