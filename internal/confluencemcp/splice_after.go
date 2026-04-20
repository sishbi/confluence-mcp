package confluencemcp

import "fmt"

// spliceAfterHeading inserts fragment immediately after the closing tag of the
// heading matching heading. Errors from locateHeading (not found, unsafe
// container, ambiguous) pass through unchanged.
func spliceAfterHeading(body, fragment, heading string) (SpliceResult, error) {
	match, err := locateHeading(body, heading)
	if err != nil {
		return SpliceResult{}, err
	}

	merged := body[:match.headingEndOff] + fragment + body[match.headingEndOff:]
	container := "document root"
	if match.layoutCellDepth > 0 {
		container = "ac:layout-cell"
	}
	return SpliceResult{
		Merged: merged,
		Boundary: BoundaryInfo{
			InsertAnchor: fmt.Sprintf("after </h%d> %q", match.level, heading),
			Container:    container,
		},
	}, nil
}
