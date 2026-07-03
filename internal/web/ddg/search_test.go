package ddg

import (
	"testing"
)

func TestCleanDDGURL(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{
			raw:  "https://duckduckgo.com/l/?uddg=https%3A%2F%2Fgolang.org%2F&rutt=1",
			want: "https://golang.org/",
		},
		{
			raw:  "/l/?uddg=https%3A%2F%2Fexample.com%2Fpath%3Fq%3D1&rutt=2",
			want: "https://example.com/path?q=1",
		},
		{
			raw:  "https://google.com",
			want: "https://google.com",
		},
	}

	for _, tt := range tests {
		got := cleanDDGURL(tt.raw)
		if got != tt.want {
			t.Errorf("cleanDDGURL(%q) = %q, want %q", tt.raw, got, tt.want)
		}
	}
}

func TestStripTags(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"<b>Hello</b> World", "Hello World"},
		{"<div class=\"test\">Snippet &amp; more</div>", "Snippet & more"},
		{"Hello &lt;World&gt;", "Hello <World>"},
	}

	for _, tt := range tests {
		got := stripTags(tt.input)
		if got != tt.want {
			t.Errorf("stripTags(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseDDGResults(t *testing.T) {
	htmlContent := `
<html>
<body>
<div class="web-result">
  <div class="result__body">
    <h2 class="result__title">
      <a class="result__a" href="/l/?uddg=https%3A%2F%2Fgolang.org%2F&rutt=123">Go Programming Language</a>
    </h2>
    <a class="result__url" href="https://golang.org/">golang.org</a>
    <div class="result__snippet">Go is an open source programming language that makes it easy to build simple, reliable, and efficient software.</div>
  </div>
</div>
<div class="web-result">
  <div class="result__body">
    <h2 class="result__title">
      <a class="result__a" href="https://github.com/golang/go">GitHub - golang/go</a>
    </h2>
    <a class="result__url" href="https://github.com/golang/go">github.com/golang/go</a>
    <div class="result__snippet">The Go programming language repository.</div>
  </div>
</div>
</body>
</html>
`

	results := parseDDGResults(htmlContent)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results[0].Title != "Go Programming Language" {
		t.Errorf("expected Title %q, got %q", "Go Programming Language", results[0].Title)
	}
	if results[0].URL != "https://golang.org/" {
		t.Errorf("expected URL %q, got %q", "https://golang.org/", results[0].URL)
	}
	if results[0].Snippet != "Go is an open source programming language that makes it easy to build simple, reliable, and efficient software." {
		t.Errorf("expected Snippet %q, got %q", "Go is an open source...", results[0].Snippet)
	}

	if results[1].Title != "GitHub - golang/go" {
		t.Errorf("expected Title %q, got %q", "GitHub - golang/go", results[1].Title)
	}
	if results[1].URL != "https://github.com/golang/go" {
		t.Errorf("expected URL %q, got %q", "https://github.com/golang/go", results[1].URL)
	}
	if results[1].Snippet != "The Go programming language repository." {
		t.Errorf("expected Snippet %q, got %q", "The Go...", results[1].Snippet)
	}
}
