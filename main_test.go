package main

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
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
