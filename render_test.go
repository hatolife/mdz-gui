package main

import (
	"strings"
	"testing"
)

func TestRenderDoesNotExecuteDocumentHTML(t *testing.T) {
	app := &App{}
	for _, input := range []string{`<script>alert(1)</script>`, `<img src=x onerror=alert(1)>`, `[click](javascript:alert%281%29)`} {
		result, err := app.Render(input)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(result, "<script") || strings.Contains(result, "onerror=") || strings.Contains(result, `href="javascript:`) {
			t.Fatalf("unsafe output: %s", result)
		}
	}
}
