package mdconv

// ConversionLog records structural metrics about a storage-to-Markdown conversion.
type ConversionLog struct {
	InputBytes  int
	OutputBytes int
	Elements    map[string]int // e.g. "code_block":3, "adf_panel":1, "task_list":1
	Macros      map[string]int // e.g. "info":2, "toc":1, "status":1
	Skipped     []string       // descriptions of unhandled elements (malformed XML etc.)
	Errors      int            // count of elements that matched a pattern but failed to process
}

// NewConversionLog returns a ConversionLog with initialised maps.
func NewConversionLog() *ConversionLog {
	return &ConversionLog{
		Elements: map[string]int{},
		Macros:   map[string]int{},
	}
}

// Element increments the counter for the given element kind.
func (l *ConversionLog) Element(kind string) {
	if l == nil {
		return
	}
	l.Elements[kind]++
}

// Macro increments the counter for the given macro name.
func (l *ConversionLog) Macro(name string) {
	if l == nil {
		return
	}
	l.Macros[name]++
}

// Skip records an unhandled element description and increments the error count.
func (l *ConversionLog) Skip(desc string) {
	if l == nil {
		return
	}
	l.Skipped = append(l.Skipped, desc)
	l.Errors++
}
