// mmark is a tiny zero-install markdown viewer.
//
// It renders a markdown file to GitHub-flavored HTML, serves it on a
// localhost-only HTTP server, and opens the default browser. The page polls
// the server for file changes (auto-reload) and the process exits on its own
// once no browser tab is polling anymore.
package main

import (
	"bytes"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/korean"
	textunicode "golang.org/x/text/encoding/unicode"
)

var version = "dev"

//go:embed assets/github-markdown-light.css
var githubLightCSS string

//go:embed assets/github-markdown-dark.css
var githubDarkCSS string

//go:embed assets/help.md
var helpMD string

const (
	// Browsers throttle timers in hidden tabs down to about once per
	// minute, so the idle timeout has to be generous or a backgrounded
	// tab would kill the server.
	idleTimeout = 10 * time.Minute
	// The watchdog only exits after this many consecutive over-deadline
	// checks, so a browser that just woke from suspend or heavy tab
	// throttling gets time to re-establish polling.
	idleMisses = 5
)

var (
	lastPoll atomic.Int64
	errSeq   atomic.Int64
)

// scriptNonce lets the page's own script run under a CSP that blocks any
// script embedded in the markdown document itself.
var scriptNonce = func() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}()

var md = goldmark.New(
	goldmark.WithExtensions(
		extension.GFM,
		extension.Footnote,
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
		),
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(ghtml.WithUnsafe()),
)

// gitHubIDs generates heading anchors the way GitHub does — keeping unicode
// letters — instead of goldmark's ASCII-only default, which collapses
// all-Korean headings to "-", "--1", ... and breaks in-document TOC links.
type gitHubIDs struct{ used map[string]bool }

func newGitHubIDs() parser.IDs { return &gitHubIDs{used: map[string]bool{}} }

func (g *gitHubIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	var sb strings.Builder
	for _, r := range strings.ToLower(string(value)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-':
			sb.WriteRune(r)
		case r == ' ':
			sb.WriteRune('-')
		}
	}
	id := sb.String()
	if id == "" {
		id = "heading"
	}
	base, n := id, 1
	for g.used[id] {
		id = fmt.Sprintf("%s-%d", base, n)
		n++
	}
	g.used[id] = true
	return []byte(id)
}

func (g *gitHubIDs) Put(value []byte) { g.used[string(value)] = true }

var pageTmpl = template.Must(template.New("page").Parse(`<!doctype html>
<html lang="ko">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · mmark</title>
<style>{{.BaseCSS}}</style>
<style id="css-light" media="{{.LightMedia}}">{{.LightCSS}}</style>
<style id="css-dark" media="{{.DarkMedia}}">{{.DarkCSS}}</style>
</head>
<body>
<button id="theme-toggle" type="button">🌗</button>
<article class="markdown-body">{{.Body}}</article>
<script nonce="{{.Nonce}}">
(function () {
  var stamp = {{.Stamp}};
  var statusURL = "/__mmark/status?p=" + encodeURIComponent({{.Path}});
  setInterval(function () {
    fetch(statusURL).then(function (r) { return r.json(); }).then(function (j) {
      if (j.stamp !== stamp) location.reload();
    }).catch(function () {});
  }, 1000);
  var THEMES = ["auto", "light", "dark"];
  var ICONS = { auto: "🌗", light: "☀️", dark: "🌙" };
  var LABELS = { auto: "자동", light: "라이트", dark: "다크" };
  var theme = {{.Theme}};
  var btn = document.getElementById("theme-toggle");
  function apply(t) {
    var l = document.getElementById("css-light");
    var d = document.getElementById("css-dark");
    if (t === "light") { l.media = "all"; d.media = "not all"; }
    else if (t === "dark") { l.media = "not all"; d.media = "all"; }
    else { l.media = "(prefers-color-scheme: light)"; d.media = "(prefers-color-scheme: dark)"; }
    btn.textContent = ICONS[t];
    btn.title = "테마: " + LABELS[t] + " (클릭하면 전환)";
  }
  btn.addEventListener("click", function () {
    theme = THEMES[(THEMES.indexOf(theme) + 1) % THEMES.length];
    apply(theme);
    fetch("/__mmark/theme?set=" + theme, { method: "POST" }).catch(function () {});
  });
  apply(theme);
})();
</script>
</body>
</html>
`))

type pageData struct {
	Title      string
	BaseCSS    template.CSS
	LightCSS   template.CSS
	DarkCSS    template.CSS
	LightMedia string
	DarkMedia  string
	Theme      string
	Nonce      string
	Body       template.HTML
	Stamp      string
	Path       string
}

type server struct {
	baseDir  string // absolute, cleaned
	mainFile string // absolute; "" in help mode
	baseCSS  template.CSS
	lightCSS template.CSS
	darkCSS  template.CSS
	fs       http.Handler

	mu    sync.RWMutex
	theme string // "auto" | "light" | "dark"
}

func main() {
	attachConsole() // no-op outside Windows; makes stdout/stderr reach the shell despite -H windowsgui
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("mmark", version)
		return
	}

	s := &server{
		baseCSS:  template.CSS(baseCSS),
		lightCSS: template.CSS(buildThemeCSS("github", githubLightCSS)),
		darkCSS:  template.CSS(buildThemeCSS("github-dark", githubDarkCSS)),
		theme:    loadTheme(),
	}
	if len(os.Args) > 1 {
		abs, err := filepath.Abs(os.Args[1])
		if err != nil {
			fatal(err.Error())
		}
		s.mainFile = abs
		s.baseDir = filepath.Dir(abs)
		s.fs = http.FileServer(http.Dir(s.baseDir))
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err.Error())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__mmark/status", s.status)
	mux.HandleFunc("/__mmark/theme", s.setTheme)
	mux.HandleFunc("/", s.root)

	lastPoll.Store(time.Now().UnixNano())
	go watchdog()

	openBrowser("http://" + ln.Addr().String() + "/")
	if err := http.Serve(ln, mux); err != nil {
		fatal(err.Error())
	}
}

// watchdog exits the process once no browser tab has polled for a while.
// It never trusts a single measurement: lastPoll is wall-clock, so waking
// from a >idleTimeout system suspend looks like idleness even though a tab
// is still open. A wall-clock jump between ticks therefore resets the
// window, and shutdown additionally requires idleMisses consecutive
// over-deadline checks.
func watchdog() {
	const tick = 2 * time.Second
	misses := 0
	prev := time.Now()
	for {
		time.Sleep(tick)
		now := time.Now()
		if now.Round(0).Sub(prev.Round(0)) > 5*tick { // Round strips the monotonic reading
			lastPoll.Store(now.UnixNano())
			misses = 0
		}
		prev = now
		if time.Since(time.Unix(0, lastPoll.Load())) > idleTimeout {
			misses++
			if misses >= idleMisses {
				os.Exit(0)
			}
		} else {
			misses = 0
		}
	}
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "mmark:", msg)
	fatalUI("mmark: " + msg)
	os.Exit(1)
}

// resolve maps a URL path to an absolute file path, confined to baseDir.
func (s *server) resolve(urlPath string) (string, bool) {
	if urlPath == "/" {
		return s.mainFile, s.mainFile != ""
	}
	if s.baseDir == "" {
		return "", false
	}
	rel := strings.TrimPrefix(path.Clean(urlPath), "/")
	p := filepath.Join(s.baseDir, filepath.FromSlash(rel))
	// baseDir may already end in a separator (drive roots like `E:\`, UNC
	// share roots, or `/`) — don't blindly append another one.
	prefix := s.baseDir
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	if p == s.baseDir || strings.HasPrefix(p, prefix) {
		return p, true
	}
	return "", false
}

func isMarkdown(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

func (s *server) root(w http.ResponseWriter, r *http.Request) {
	if s.mainFile == "" {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.renderMarkdown(w, "/", "mmark", []byte(helpMD), "help")
		return
	}
	p, ok := s.resolve(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/" || isMarkdown(p) {
		s.renderFile(w, r.URL.Path, p)
		return
	}
	s.fs.ServeHTTP(w, r)
}

func (s *server) renderFile(w http.ResponseWriter, urlPath, file string) {
	raw, err := os.ReadFile(file)
	if err != nil {
		// If the file exists but the read failed (e.g. a Windows sharing
		// violation while an editor saves), stamp the error page with a
		// one-off value so the next poll mismatches and retries. A truly
		// missing file keeps its real stamp ("gone") and stays stable
		// until the file appears.
		stamp := fileStamp(file)
		if _, serr := os.Stat(file); serr == nil {
			stamp = fmt.Sprintf("retry-%d", errSeq.Add(1))
		}
		src := fmt.Sprintf("# 파일을 열 수 없습니다\n\n```\n%s\n```\n", err)
		s.renderMarkdown(w, urlPath, filepath.Base(file), []byte(src), stamp)
		return
	}
	s.renderMarkdown(w, urlPath, filepath.Base(file), []byte(decodeText(raw)), fileStamp(file))
}

func (s *server) renderMarkdown(w http.ResponseWriter, urlPath, title string, src []byte, stamp string) {
	var buf bytes.Buffer
	ctx := parser.NewContext(parser.WithIDs(newGitHubIDs()))
	if err := md.Convert(src, &buf, parser.WithContext(ctx)); err != nil {
		buf.Reset()
		buf.WriteString("<h1>렌더링 오류</h1><pre>")
		template.HTMLEscape(&buf, []byte(err.Error()))
		buf.WriteString("</pre>")
	}
	s.mu.RLock()
	theme := s.theme
	s.mu.RUnlock()
	lightMedia, darkMedia := themeMedia(theme)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Raw HTML in the document is rendered (WithUnsafe), so a CSP keeps
	// scripts inside untrusted .md files from running on this origin and
	// reading the served directory; only our nonce'd page script may run.
	w.Header().Set("Content-Security-Policy",
		"default-src 'none'; img-src * data: blob:; media-src * data:; "+
			"style-src 'unsafe-inline'; script-src 'nonce-"+scriptNonce+"'; "+
			"connect-src 'self'; base-uri 'none'; form-action 'none'")
	pageTmpl.Execute(w, pageData{
		Title:      title,
		BaseCSS:    s.baseCSS,
		LightCSS:   s.lightCSS,
		DarkCSS:    s.darkCSS,
		LightMedia: lightMedia,
		DarkMedia:  darkMedia,
		Theme:      theme,
		Nonce:      scriptNonce,
		Body:       template.HTML(buf.String()),
		Stamp:      stamp,
		Path:       urlPath,
	})
}

func themeMedia(theme string) (light, dark string) {
	switch theme {
	case "light":
		return "all", "not all"
	case "dark":
		return "not all", "all"
	}
	return "(prefers-color-scheme: light)", "(prefers-color-scheme: dark)"
}

func validTheme(t string) bool {
	return t == "auto" || t == "light" || t == "dark"
}

func themeFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mmark", "theme")
}

func loadTheme() string {
	if p := themeFile(); p != "" {
		if b, err := os.ReadFile(p); err == nil {
			if t := strings.TrimSpace(string(b)); validTheme(t) {
				return t
			}
		}
	}
	return "auto"
}

// setTheme stores the user's theme choice; persisting it under the OS config
// dir keeps it across runs even though the port (= web origin) changes.
func (s *server) setTheme(w http.ResponseWriter, r *http.Request) {
	t := r.URL.Query().Get("set")
	if !validTheme(t) {
		http.Error(w, "invalid theme", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.theme = t
	s.mu.Unlock()
	if p := themeFile(); p != "" {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err == nil {
			os.WriteFile(p, []byte(t), 0o644)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// status reports the current stamp of the polled file and doubles as the
// browser heartbeat that keeps the process alive.
func (s *server) status(w http.ResponseWriter, r *http.Request) {
	lastPoll.Store(time.Now().UnixNano())
	stamp := "help"
	if p, ok := s.resolve(r.URL.Query().Get("p")); ok {
		stamp = fileStamp(p)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"stamp": stamp})
}

func fileStamp(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return "gone"
	}
	return fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
}

// decodeText assumes UTF-8 and falls back to the legacy encodings of Korean
// Windows text files: UTF-16 with BOM (old Notepad "유니코드", PowerShell 5.1
// redirection) and CP949/EUC-KR.
func decodeText(b []byte) string {
	if len(b) >= 2 {
		var enc encoding.Encoding
		switch {
		case b[0] == 0xFF && b[1] == 0xFE:
			enc = textunicode.UTF16(textunicode.LittleEndian, textunicode.ExpectBOM)
		case b[0] == 0xFE && b[1] == 0xFF:
			enc = textunicode.UTF16(textunicode.BigEndian, textunicode.ExpectBOM)
		}
		if enc != nil {
			if d, err := enc.NewDecoder().Bytes(b); err == nil {
				return string(d)
			}
		}
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
	if d, err := korean.EUCKR.NewDecoder().Bytes(b); err == nil {
		return string(d)
	}
	return string(b)
}

const baseCSS = `.markdown-body{box-sizing:border-box;min-width:200px;max-width:980px;margin:0 auto;padding:45px}
@media (max-width: 767px){.markdown-body{padding:15px}}
#theme-toggle{position:fixed;top:12px;right:12px;z-index:10;width:38px;height:38px;border-radius:50%;border:1px solid rgba(128,128,128,.4);background:rgba(128,128,128,.12);cursor:pointer;font-size:18px;line-height:1;padding:0;opacity:.55}
#theme-toggle:hover{opacity:1}
@media print{#theme-toggle{display:none}}
`

// buildThemeCSS combines one github-markdown-css variant with the matching
// chroma style; the two results are toggled via <style media=...> switching.
func buildThemeCSS(chromaStyle, markdownCSS string) string {
	var b strings.Builder
	if strings.Contains(chromaStyle, "dark") {
		b.WriteString("body{margin:0;background:#0d1117}\n")
	} else {
		b.WriteString("body{margin:0;background:#fff}\n")
	}
	b.WriteString(markdownCSS)
	b.WriteString("\n")
	f := chromahtml.New(chromahtml.WithClasses(true))
	f.WriteCSS(&b, styles.Get(chromaStyle))
	return b.String()
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}
