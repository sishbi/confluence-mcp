package confluence

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInlineComment_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name                string
		body                string
		wantID              string
		wantResolutionState string
		wantMarkerRef       string
		wantOriginalSel     string
		wantBodyValue       string
		wantPageID          string
		wantVersionNumber   int
		wantCreatedZero     bool
	}{
		{
			name: "page-level inline comment payload decodes all fields",
			body: `{
				"id": "98765",
				"status": "current",
				"title": "",
				"pageId": "12345",
				"version": {"number": 3, "createdAt": "2026-01-01T00:00:00.000Z"},
				"body": {"storage": {"representation": "storage", "value": "<p>Looks good</p>"}},
				"resolutionStatus": "open",
				"properties": {
					"inlineMarkerRef": "marker-ref-1",
					"inlineOriginalSelection": "the highlighted text"
				},
				"_links": {"webui": "/wiki/spaces/DEV/pages/12345"}
			}`,
			wantID:              "98765",
			wantResolutionState: "open",
			wantMarkerRef:       "marker-ref-1",
			wantOriginalSel:     "the highlighted text",
			wantBodyValue:       "<p>Looks good</p>",
			wantPageID:          "12345",
			wantVersionNumber:   3,
			wantCreatedZero:     false,
		},
		{
			name: "children model payload has no pageId or createdAt",
			body: `{
				"id": "111",
				"status": "current",
				"version": {"number": 1},
				"body": {"storage": {"representation": "storage", "value": "<p>Reply</p>"}},
				"resolutionStatus": "resolved",
				"properties": {
					"inlineMarkerRef": "marker-ref-2",
					"inlineOriginalSelection": ""
				}
			}`,
			wantID:              "111",
			wantResolutionState: "resolved",
			wantMarkerRef:       "marker-ref-2",
			wantOriginalSel:     "",
			wantBodyValue:       "<p>Reply</p>",
			wantPageID:          "",
			wantVersionNumber:   1,
			wantCreatedZero:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got InlineComment
			err := json.Unmarshal([]byte(tt.body), &got)
			require.NoError(t, err)

			assert.Equal(t, tt.wantID, got.ID)
			assert.Equal(t, tt.wantResolutionState, got.ResolutionStatus)
			assert.Equal(t, tt.wantMarkerRef, got.Properties.InlineMarkerRef)
			assert.Equal(t, tt.wantOriginalSel, got.Properties.InlineOriginalSelection)
			assert.Equal(t, tt.wantBodyValue, got.Body.Storage.Value)
			assert.Equal(t, tt.wantPageID, got.PageID)
			assert.Equal(t, tt.wantVersionNumber, got.Version.Number)
			assert.Equal(t, tt.wantCreatedZero, got.Version.Created.IsZero())
		})
	}
}
