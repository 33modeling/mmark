package main

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuin/goldmark/parser"
)

func TestGitHubIDsKeepKoreanAndDeduplicate(t *testing.T) {
	ids := newGitHubIDs()

	if got := string(ids.Generate([]byte("사용 방법!"), 0)); got != "사용-방법" {
		t.Fatalf("first heading id = %q, want %q", got, "사용-방법")
	}
	if got := string(ids.Generate([]byte("사용 방법!"), 0)); got != "사용-방법-1" {
		t.Fatalf("duplicate heading id = %q, want %q", got, "사용-방법-1")
	}
	if got := string(ids.Generate([]byte("!!!"), 0)); got != "heading" {
		t.Fatalf("empty heading fallback = %q, want %q", got, "heading")
	}
}

func TestResolveKeepsPathsInsideBaseDir(t *testing.T) {
	baseDir := t.TempDir()
	mainFile := filepath.Join(baseDir, "doc.md")
	s := &server{baseDir: baseDir, mainFile: mainFile}

	if got, ok := s.resolve("/"); !ok || got != mainFile {
		t.Fatalf("root resolve = %q, %v; want %q, true", got, ok, mainFile)
	}

	got, ok := s.resolve("/../outside.md")
	if !ok {
		t.Fatal("path traversal-like URL should resolve as a confined relative path")
	}
	want := filepath.Join(baseDir, "outside.md")
	if got != want {
		t.Fatalf("confined path = %q, want %q", got, want)
	}

	got, ok = s.resolve("/nested/../doc.md")
	if !ok || got != mainFile {
		t.Fatalf("cleaned in-dir path = %q, %v; want %q, true", got, ok, mainFile)
	}
}

func TestRenderMarkdownCSPBlocksRemoteMedia(t *testing.T) {
	s := &server{theme: "auto"}
	w := httptest.NewRecorder()

	s.renderMarkdown(w, "/", "test.md", []byte("# Test"), "stamp")

	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{
		"default-src 'none'",
		"img-src 'self' data: blob:",
		"media-src 'self' data: blob:",
		"connect-src 'self'",
	} {
		if !strings.Contains(csp, want) {
			t.Fatalf("CSP %q does not contain %q", csp, want)
		}
	}
	if strings.Contains(csp, "img-src *") || strings.Contains(csp, "media-src *") {
		t.Fatalf("CSP still allows remote media wildcard: %q", csp)
	}
}

func TestDecodeTextHandlesCommonWindowsMarkdownEncodings(t *testing.T) {
	if got := decodeText(append([]byte{0xEF, 0xBB, 0xBF}, []byte("hello")...)); got != "hello" {
		t.Fatalf("UTF-8 BOM decode = %q, want hello", got)
	}
	if got := decodeText([]byte{0xFF, 0xFE, 0x5C, 0xD5, 0x00, 0xAE}); got != "한글" {
		t.Fatalf("UTF-16LE decode = %q, want 한글", got)
	}
	if got := decodeText([]byte{0xFE, 0xFF, 0xD5, 0x5C, 0xAE, 0x00}); got != "한글" {
		t.Fatalf("UTF-16BE decode = %q, want 한글", got)
	}
	if got := decodeText([]byte{0xC7, 0xD1, 0xB1, 0xDB}); got != "한글" {
		t.Fatalf("CP949/EUC-KR decode = %q, want 한글", got)
	}
}

func renderMarkdownBodyForTest(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(newGitHubIDs()))
	if err := md.Convert([]byte(src), &buf, parser.WithContext(ctx)); err != nil {
		t.Fatalf("Convert() error = %v", err)
	}
	return buf.String()
}

func TestMathDelimitersSurviveMarkdownParsing(t *testing.T) {
	html := renderMarkdownBodyForTest(t, `Inline \(a*b*c\) and \[x_i = y^2\].`)

	for _, want := range []string{
		`<span class="mmark-math mmark-math-inline" data-display="false">a*b*c</span>`,
		`<span class="mmark-math mmark-math-display" data-display="true">x_i = y^2</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
	if strings.Contains(html, "<em>") {
		t.Fatalf("math contents were parsed as Markdown emphasis: %q", html)
	}
}

func TestDollarMathIsProtectedBeforeEmphasis(t *testing.T) {
	html := renderMarkdownBodyForTest(t, `Inline $a*b*c$ and $$\sum_{i=1}^n i$$.`)

	for _, want := range []string{
		`<span class="mmark-math mmark-math-inline" data-display="false">a*b*c</span>`,
		`<span class="mmark-math mmark-math-display" data-display="true">\sum_{i=1}^n i</span>`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q does not contain %q", html, want)
		}
	}
	if strings.Contains(html, "<em>") {
		t.Fatalf("math contents were parsed as Markdown emphasis: %q", html)
	}
}

func TestDollarMathAvoidsCommonFalsePositives(t *testing.T) {
	html := renderMarkdownBodyForTest(t, "Cost is $5 and `$x$` stays code.")

	if strings.Contains(html, "mmark-math") {
		t.Fatalf("non-math dollar text or code span was parsed as math: %q", html)
	}
	if !strings.Contains(html, "<code>$x$</code>") {
		t.Fatalf("code span was not preserved: %q", html)
	}
}

func TestDisplayMathBlockAllowsBlankLines(t *testing.T) {
	html := renderMarkdownBodyForTest(t, "$$\nJ(\\theta_T) = J(\\theta_0)\n\n+ \\sum_i I(z_i)\n$$\n")

	if strings.Count(html, "mmark-math-display") != 1 {
		t.Fatalf("blank line split the display math block: %q", html)
	}
	if strings.Contains(html, "<ul>") || strings.Contains(html, "<em>") {
		t.Fatalf("display math contents were re-parsed as Markdown: %q", html)
	}
}

func TestDisplayMathBlockSurvivesListLikeLines(t *testing.T) {
	html := renderMarkdownBodyForTest(t,
		"$$\n\\begin{aligned}\n- x &= 1 \\\\\n1. + y &= 2\n\\end{aligned}\n$$\n")

	if strings.Count(html, "mmark-math-display") != 1 {
		t.Fatalf("list-marker lines split the display math block: %q", html)
	}
	if strings.Contains(html, "<ul>") || strings.Contains(html, "<ol>") {
		t.Fatalf("display math lines were parsed as lists: %q", html)
	}
	if !strings.Contains(html, `\begin{aligned}`) {
		t.Fatalf("math source was mangled: %q", html)
	}
}

func TestDisplayMathBlockOpenerAndCloserMayCarryContent(t *testing.T) {
	html := renderMarkdownBodyForTest(t, "$$\\begin{aligned}\na &= b\n\\end{aligned} $$\n")

	if strings.Count(html, "mmark-math-display") != 1 {
		t.Fatalf("content on the $$ opener/closer lines broke the block: %q", html)
	}
	for _, want := range []string{`\begin{aligned}`, `\end{aligned}`} {
		if !strings.Contains(html, want) {
			t.Fatalf("rendered HTML %q lost math content %q", html, want)
		}
	}
}

func TestSingleLineDoubleDollarStaysInline(t *testing.T) {
	html := renderMarkdownBodyForTest(t, "앞 $$E=mc^2$$ 뒤.")

	if !strings.Contains(html, `data-display="true">E=mc^2</span>`) {
		t.Fatalf("single-line $$...$$ was not parsed as display math: %q", html)
	}
	if !strings.Contains(html, "앞") || !strings.Contains(html, "뒤") {
		t.Fatalf("surrounding text was lost: %q", html)
	}
}

func TestTableCellMathUnescapesPipes(t *testing.T) {
	html := renderMarkdownBodyForTest(t,
		"| 항목 | 수식 |\n|---|---|\n| 조건부 | $P(a\\|b)$ |\n")

	if !strings.Contains(html, "P(a|b)") {
		t.Fatalf("escaped pipe was not normalized inside table-cell math: %q", html)
	}
	if strings.Contains(html, `P(a\|b)`) {
		t.Fatalf("math still contains the raw \\| escape: %q", html)
	}
	if c := strings.Count(html, "<td>"); c != 2 {
		t.Fatalf("table lost its cell structure (%d cells): %q", c, html)
	}
}

func TestPipeOutsideTableCellIsKeptInMath(t *testing.T) {
	html := renderMarkdownBodyForTest(t, `본문 $\|x\|$ 노름.`)

	if !strings.Contains(html, `\|x\|`) {
		t.Fatalf("\\| outside a table was rewritten: %q", html)
	}
}
