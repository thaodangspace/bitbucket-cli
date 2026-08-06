package output

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// TableColumn describes one deterministic table column. MaxWidth is a display
// width cap; long values are truncated with an ellipsis.
type TableColumn struct {
	Header   string
	MaxWidth int
	Value    func(any) string
}

// RenderTable writes a simple whitespace-separated table with explicit columns
// and width-aware truncation. It is intentionally writer-based so callers can
// send the result through the shared pager/color pipeline.
func RenderTable(w io.Writer, rows []any, columns []TableColumn) error {
	widths := make([]int, len(columns))
	for i, column := range columns {
		widths[i] = displayWidth(column.Header)
	}
	values := make([][]string, len(rows))
	for r, row := range rows {
		values[r] = make([]string, len(columns))
		for c, column := range columns {
			values[r][c] = truncate(column.Value(row), column.MaxWidth)
			if n := displayWidth(values[r][c]); n > widths[c] {
				widths[c] = n
			}
		}
	}
	for i, column := range columns {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, padRight(column.Header, widths[i]))
	}
	if len(columns) > 0 {
		fmt.Fprintln(w)
	}
	for _, row := range values {
		for i, value := range row {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			fmt.Fprint(w, padRight(value, widths[i]))
		}
		if len(columns) > 0 {
			fmt.Fprintln(w)
		}
	}
	return nil
}

func displayWidth(value string) int { return utf8.RuneCountInString(value) }
func padRight(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-displayWidth(value)))
}
func truncate(value string, maxWidth int) string {
	if maxWidth <= 0 || displayWidth(value) <= maxWidth {
		return value
	}
	if maxWidth == 1 {
		return "…"
	}
	runes := []rune(value)
	return string(runes[:maxWidth-1]) + "…"
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
