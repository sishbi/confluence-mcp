# Conversion Test Fixture – Comprehensive Confluence Elements

Use this page to verify that each Confluence/Atlassian custom element is correctly converted by the tool. Every element includes a short instruction describing what to check.

* * *

## 1. Table of Contents Macro

<!-- macro:m1 -->

> [!NOTE]
> 
> Instruction: This section tests the Table of Contents macro. The info-panel above is the instruction; the real `toc` macro follows below. Verify the tool recognises the toc macro and renders a heading list.

\[TOC macro placeholder – verify heading list rendering and link targets]

Actual TOC macro:

<!-- macro:m2 -->

\[Table of Contents]

## 2. Headings and Horizontal Rule

Instruction: Verify that all heading levels are preserved with correct hierarchy and that the horizontal rule is rendered as a divider.

### 2.1 Heading Level 3

#### 2.1.1 Heading Level 4

##### 2.1.1.1 Heading Level 5

###### 2.1.1.1.1 Heading Level 6

* * *

## 3. Panels and Message Blocks

Instruction: Verify different panel styles. Note that the prose labels below may not always match the underlying `ac:name` — this is intentional, to test that the converter uses the macro type, not inferred prose.

<!-- macro:m3 -->

> [!NOTE]
> 
> Info panel: Used for general information. Verify background style and icon.

> **Note:**
> 
> Note panel: Used for additional context. Verify that it is distinct from info and warning.

<!-- macro:m4 -->

> [!NOTE]
> 
> Warning panel: Used for potential issues. Verify that styling indicates caution.

<!-- macro:m5 -->

> [!WARNING]
> 
> Error panel: Used for critical problems. Verify that styling indicates an error state.

<!-- macro:m6 -->

> [!TIP]
> 
> Tip panel: this paragraph is inside a `tip` macro. Verify the green tip styling.

## 4. Expand / Collapse Macro (with nested code)

Instruction: Verify expand preserves summary, body paragraphs, AND a nested code macro inside it.

<!-- macro:m7 -->

**▶ Click here to reveal detailed content**

┈┈┈┈┈┈┈┈

Expand summary: Expansion of the ‘click here to reveal detailed content’

Expanded content paragraph 1: Verify that this text is hidden by default (if the tool supports default collapsed state) and is shown after expansion.

Expanded content paragraph 2: Verify that multiple paragraphs inside the expand are preserved.

Nested code macro inside the expand:

```go
func nestedInExpand() string {
    return "code inside expand macro"
}
```

┈┈┈┈┈┈┈┈

<!-- /macro:m7 -->

## 5. Status Lozenge Macro

Instruction: Verify that status lozenges keep their color and text value.

Status examples:

<!-- macro:m8 -->

🔵 In Progress

<!-- macro:m9 -->

🟢 Done

<!-- macro:m10 -->

🔴 Blocked

## 6. Quote Block

Instruction: Verify that block quotes are preserved with indentation and styling distinct from normal paragraphs, and that nested block content inside a quote is preserved.

> This is a quoted block of text with a **nested** list:
> 
> - First nested bullet inside the quote
> - Second nested bullet with `inline code`

## 7. Code Blocks and Noformat

Instruction: Verify that code blocks retain language, formatting, and do not wrap special characters. Also verify that noformat content is preserved literally without syntax highlighting.

7.1 Code block – language: javascript

```js
// Verify JavaScript code block is preserved with language
function sum(a, b) {
  return a + b;
}
const result = sum(2, 3);
```

7.2 Code block – language: python

```py
# Verify Python code block is preserved
from dataclasses import dataclass

@dataclass
class Item:
    name: str
    value: int

item = Item(name="example", value=5)
```

7.3 Code block – language: sql

```sql
-- Verify SQL code block formatting
SELECT id, name
FROM users
WHERE active = TRUE
ORDER BY name;
```

7.4 Noformat-style block (use code block without language)

```
Verify that angle brackets <>, brackets [ ], and other characters are not interpreted or escaped.
Line 1
Line 2
```

## 8. Inline Code

Instruction: Verify that inline code is rendered with monospace styling inside normal text.

Run the command `npm install` and then start the application with `npm start`. Confirm that inline code formatting is preserved.

## 9. Tables with Merged Cells (colspan + rowspan)

Instruction: Verify tables, header row, row header, merged cells (colspan AND rowspan), rich inline content in cells, and a nested date node.

| Section | Element | Instruction |  |
| --- | --- | --- | --- |
| (spans 2 cols) Merged across two columns (spans 2 cols) | (spans 2 cols) Merged across two columns | Verify that this cell appears to the right of a two-column merge. |  |
| Layout | Columns | Confirm that the layout section below is rendered with multiple columns and not as a table. |  |
| Spans two rows⬆ | Row 1, col 2 | Row 1, col 3 with **bold** and `code` |  |
| ⬆ | Row 2, col 2 with a [link](https://example.com) | Row 2, col 3 with a list:<br>• item A<br>• item B |  |
| Row header | Tests | in the first column, not only the first row. | Date in cell: 2026-04-17 |

## 10. Layouts and Columns

Instruction: Verify that multi-column page layouts are preserved and that each column’s content remains in the correct position.

┈┈ Column 1 ┈┈

**\[Column 1]**

Left column: Verify that this text remains in the left column.

Add more text here to confirm wrapping behavior in the left column.

┈┈ Column 2 ┈┈

**\[Column 2]**

Middle column: Verify separation from left and right columns.

- Bullet item 1 – middle column
- Bullet item 2 – middle column

┈┈ Column 3 ┈┈

**\[Column 3]**

Right column: Verify that this column aligns on the right side.

Confirm that conversion does not flatten the layout into a single column.

┈┈┈┈┈┈┈┈

## 11. Task Lists

Instruction: Verify that task lists remain interactive checkboxes, with checked/unchecked states preserved.

- [ ] Uncompleted task – verify checkbox is empty.
- [x] Completed task – verify checkbox is checked.
- [ ] Nested details are preserved: `inline code` and **bold**/*italic* formatting.

## 12. Placeholders for Mentions

Instruction: Verify that mention placeholders are preserved as plain text prompts and not converted into real mentions. The real mention node below tests the actual mention-conversion path.

Example placeholders: \[Add user mention], \[Add team mention], \[Add admin role mention]. Confirm that the conversion tool does not attempt to resolve these into real mentions.

Actual user mention: @user(712020:00000000-1111-2222-3333-444444444444)

## 13. Date Nodes

Instruction: Verify that standalone date nodes with ISO formats are preserved and not converted into plain text dates. Also verify that literal date-like text (e.g. \[CurrentDate]) is left untouched.

Placeholder: \[CurrentDate]

Created on: 2026-04-05 (verify formatting).

Reviewed on: 2026-03-06 (verify that this is rendered as a date element).

Dates inside a list:

- Kickoff: 2026-04-17
- Midpoint: 2026-04-18

## 14. Emoji

Instruction: Verify that real emoticon nodes are preserved, and that literal text emoji placeholders (e.g. :smile:) are left unchanged.

Literal text placeholders (should remain unchanged): :smile: :warning: :rocket:

Actual emojis: 🙂 ⚠️ ✅ ❌

## 15. Smart Links (Inline Cards)

Instruction: Verify that smart links are represented as inline cards and that their URLs and titles are preserved without becoming plain text URLs. Both url-as-anchor and custom-anchor-text forms are tested.

URL-as-anchor form: [https://example.com/documentation/conversion-spec](https://example.com/documentation/conversion-spec).

Custom-anchor form: [Text to display for inline link](https://example.com/projects/test-fixture).

## 16. Attachments

Instruction: Verify that both image attachments and non-image attachment links are preserved, and that placeholder text is not mistakenly converted into an attachment reference.

Placeholder list (text only, should not be converted):

- \[Add design-spec.pdf attachment here]
- \[Add data-sample.csv attachment here]
- \[Add architecture-diagram.docx attachment here]

Actual image attachment (real `ac:image` with `ri:attachment`):

Image attachment title

Actual attachment link (same filename, different code path – `ac:link` with `ri:attachment`):

<!-- macro:m11 -->

[Screenshot 2026-04-17 at 12.37.31.png](attachment:Screenshot%202026-04-17%20at%2012.37.31.png)

## 17. Jira Macros

Instruction: Verify that Jira-related macro placeholders AND real Jira macros are both handled. Placeholders should stay as text; real macros should be recognised.

Jira issue macro placeholder: \[Insert Jira issue macro – key: PROJ-123 – verify single-issue embed behavior].

Jira filter/board macro placeholder: \[Insert Jira filter/board macro – filter: All Open Bugs – verify table or board-style rendering].

Jira roadmap macro placeholder: \[Insert Jira roadmap macro – verify timeline view and issue grouping].

Actual Jira issue macro:

<!-- macro:m12 -->

\[Jira: SUA-1]

## 18. Excerpt and Excerpt-Include

Instruction: Verify that excerpt regions and excerpt-include placeholders are preserved, and that included content boundaries are clear. Both the info-panel placeholder and a real `excerpt` macro are present.

<!-- macro:m13 -->

> [!NOTE]
> 
> Excerpt macro region start – verify that tools can treat this specific portion as an excerpt for reuse.
> 
> Excerpt content: This paragraph simulates content inside an excerpt macro. Only this region should be considered part of the excerpt.
> 
> Excerpt macro region end.

Excerpt-include placeholder: \[Insert excerpt-include macro referencing the excerpt on this page – verify that content is reused and synchronized].

Actual excerpt macro:

<!-- macro:m14 -->

> **Excerpt:**
> 
> Real excerpt content: this paragraph is inside a genuine `excerpt` macro. Only this region should be treated as an excerpt.
> 
> - bullets within an excerpt

## 19. Anchors and In-Page Navigation

Instruction: Verify that anchor markers and references to them are preserved and remain functional after conversion. Both text placeholders and real anchor macros are present.

Anchor placeholder: \[Anchor: top-of-fixture]. This marks a logical target near the top of the page.

Anchor placeholder: \[Anchor: jira-section]. This marks the Jira macros section above.

Linking instruction: After conversion, create links that jump to \[Anchor: top-of-fixture] and \[Anchor: jira-section] if anchor support exists.

Actual anchor macro:

<!-- macro:m15 -->

\[Anchor: top-of-fixture]

Actual in-page link to that anchor: [link](https://example.atlassian.net/wiki/spaces/~712020000000001111222233334444444444/pages/1234567890#top-of-fixture).

## 20. Button Macro Placeholder

Instruction: Verify that the idea of a styled button with an action target is preserved, even if represented as plain text in this fixture.

Button placeholder: \[Insert button macro – label: "Open Conversion Report" – target: smart link to reporting page].

## 21. Tabs Macro Placeholder

Instruction: Verify that tabbed content groupings can be represented logically and remain distinguishable after conversion.

Tabs placeholder structure:

- Tab 1 – Summary: \[Insert content describing overview of the conversion].
- Tab 2 – Technical Details: \[Insert content with detailed technical notes].
- Tab 3 – Known Issues: \[Insert content listing known limitations].

## 22. Charts Placeholder

Instruction: Verify that chart macro placeholders are preserved for later replacement with real charts.

Charts placeholder list:

- \[Insert pie chart macro – data: issue distribution by status].
- \[Insert line chart macro – data: conversion errors over time].

## 23. Confluence Database and Whiteboard Embeds Placeholders

Instruction: Verify that advanced Confluence objects such as databases and whiteboards can be referenced symbolically for testing.

Confluence database placeholder: \[Insert database view – name: Conversion Test Records – verify table-like rendering].

Confluence whiteboard placeholder: \[Insert whiteboard – name: Conversion Flow Diagram – verify drawing surface embedding].

## 24. Page Properties and Page Properties Report

Instruction: Verify that page metadata defined via a real `details` macro is preserved in a dedicated table and that Page Properties Report references can still aggregate that data.

<!-- macro:m16 -->

**▶ Details:**

┈┈┈┈┈┈┈┈

| Property | Value |
| --- | --- |
| Fixture Name | Comprehensive Conversion Test Fixture |
| Owner | @user(712020:00000000-1111-2222-3333-444444444444) |
| Review Date | 2024-09-01 |

┈┈┈┈┈┈┈┈

<!-- /macro:m16 -->

Page Properties Report placeholder: \[Insert page properties report macro – filter by label: conversion-fixture – verify aggregated table appearance].

## 25. Children Display Macro

Instruction: Verify that automatic child page listings can be represented and remain clearly associated with this page.

Children Display placeholder: \[Insert children display macro – depth: 2 – verify that subpages of this fixture are listed].

Actual children macro:

<!-- macro:m17 -->

\[Child pages]

## 26. Include Page Macro Placeholder

Instruction: Verify that the include-page concept is preserved and can pull content from another page in real scenarios.

Include placeholder: \[Insert include page macro – source: "Conversion Tool User Guide" – verify that content from that page appears inline].

## 27. Final Verification Notes

Instruction: Use this section to record observations about how well the conversion tool handled each element in this fixture.

- Confirm that all headings, panels (info/note/warning/tip), expands, statuses, quotes, and code blocks are present and correctly formatted.
- Verify that tables (colspan AND rowspan, row header, nested date), layouts, task lists, real mentions, real emoticons, dates, smart links, attachments (image AND link), and real Page Properties macro are converted accurately.
- Check that real Confluence macros (TOC, Jira, excerpt, anchor, children, details) remain identifiable after round-trip, and that text placeholders are NOT resolved into macros.

## 28. Additional Headings, Lists, and Quotes

Instruction: Add substantial additional content to extend this fixture. Each new section should include an Instruction line and varied elements for conversion testing.

### 28.1 Nested bullet and numbered lists

- Top-level bullet A
- Top-level bullet B
  
  - Nested bullet B.1
  - Nested bullet B.2
    
    1. Numbered 1 under B.2
    2. Numbered 2 under B.2
  - Top-level bullet C

> Quote: Deep nesting lists should remain structurally intact across conversions.

- Checklist – verify nesting levels:
- Level 1
  
  - Level 2
    
    - Level 3

1. Step 1
2. Step 2
   
   - Sub-step 2.a
   - Sub-step 2.b
3. Step 3

## 29. Additional Code Blocks (Multi-language)

Instruction: Verify that additional language variants and long code/noformat blocks are preserved, including indentation and long-line handling.

29.1 Shell (bash)

```bash
#!/usr/bin/env bash
# Instruction: Run and verify variables, pipes, and heredocs render correctly
set -euo pipefail
name="World"
echo "Hello, $name!" | sed 's/World/Bash/'
cat <<'EOF'
line with backticks `code` and dollars $FOO and braces ${BAR}
EOF
# very long line to test wrapping: AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
```

29.2 PowerShell

```powershell
# Instruction: Verify variables, pipelines, and here-strings
$Name = 'World'
Write-Output "Hello, $Name!" | ForEach-Object { $_.ToUpper() }
$here = @"
Line with `backticks`, quotes 'single' "double", and ${Env:PATH}
@{ nested = 'hash' }
"@
$here
```

## 30. Rich Inline Formatting

Instruction: Verify that combined inline formatting (bold, italic, code, strikethrough, underline, sub, sup, and combinations) round-trips correctly.

Single: **bold**, *italic*, `code`, ~~strikethrough~~, <u>underline</u>.

Combined: `bold italic code`, ***bold italic***, `italic code`.

Sub and sup: water is H<sub>2</sub>O, and area scales as X<sup>2</sup>.

Mixed in one paragraph: start with **bold**, then a link to [example.com](https://example.com), then `some.function()`, then a mention @user(712020:00000000-1111-2222-3333-444444444444) , then a date 2026-04-17 .

* * *

*Smoke test marker — safe to delete.*