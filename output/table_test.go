package output

import (
	"strings"
	"testing"
)

func TestRenderTableTruncatesToColumnWidth(t *testing.T) {
	var b strings.Builder
	err := RenderTable(&b, []any{"a very long value"}, []TableColumn{{Header: "NAME", MaxWidth: 5, Value: func(v any) string { return v.(string) }}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "a ve…") {
		t.Fatalf("unexpected table: %q", b.String())
	}
}
