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
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"mime"
	"net"
	"net/http"
	"net/url"
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

//go:embed assets/app.js assets/mermaid.min.js assets/katex.min.js assets/katex-auto-render.min.js assets/katex.min.css assets/katex/fonts/*
var staticAssets embed.FS

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
<link rel="stylesheet" href="/__mmark/assets/katex.min.css">
<style>{{.BaseCSS}}</style>
<style id="css-light" media="{{.LightMedia}}">{{.LightCSS}}</style>
<style id="css-dark" media="{{.DarkMedia}}">{{.DarkCSS}}</style>
</head>
<body>
<div id="controls">
{{if .CanPick}}<button id="open-file" type="button" title="파일 열기">📂</button>{{end}}
<button id="toc-toggle" type="button" title="목차" hidden>☰</button>
<button id="search-open" type="button" title="검색">🔎</button>
<button id="print-doc" type="button" title="인쇄/PDF">🖨</button>
<button id="theme-toggle" type="button" title="테마">🌗</button>
</div>
<nav id="toc" aria-label="문서 목차" hidden></nav>
<div id="search-panel" hidden>
  <input id="search-input" type="search" placeholder="검색" autocomplete="off" spellcheck="false">
  <span id="search-count">0/0</span>
  <button id="search-prev" type="button" title="이전">↑</button>
  <button id="search-next" type="button" title="다음">↓</button>
  <button id="search-close" type="button" title="닫기">×</button>
</div>
<article class="markdown-body">{{.Body}}</article>
<script nonce="{{.Nonce}}">
window.__MMARK__ = { stamp: {{.Stamp}}, path: {{.Path}}, theme: {{.Theme}} };
</script>
<script nonce="{{.Nonce}}" src="/__mmark/assets/katex.min.js"></script>
<script nonce="{{.Nonce}}" src="/__mmark/assets/katex-auto-render.min.js"></script>
<script nonce="{{.Nonce}}" src="/__mmark/assets/mermaid.min.js"></script>
<script nonce="{{.Nonce}}" src="/__mmark/assets/app.js"></script>
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
	CanPick    bool
}

type server struct {
	baseDir  string // absolute, cleaned
	mainFile string // absolute; "" in help mode
	baseCSS  template.CSS
	lightCSS template.CSS
	darkCSS  template.CSS
	fs       http.Handler

	mu    sync.RWMutex // guards the mutable fields above and theme
	theme string       // "auto" | "light" | "dark"
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

	fileArg := ""
	if len(os.Args) > 1 {
		fileArg = os.Args[1]
	} else if p, ok := chooseMarkdownFile(); ok {
		fileArg = p
	}
	if fileArg != "" {
		abs, err := filepath.Abs(fileArg)
		if err != nil {
			fatal(err.Error())
		}
		s.openFile(abs)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err.Error())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__mmark/assets/", serveAsset)
	mux.HandleFunc("/__mmark/open", s.openRecent)
	mux.HandleFunc("/__mmark/pick", s.pickFile)
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

func serveAsset(w http.ResponseWriter, r *http.Request) {
	rel := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/__mmark/assets/"))
	if rel == "/" {
		http.NotFound(w, r)
		return
	}
	name := "assets" + rel
	b, err := staticAssets.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(b))
}

func (s *server) openFile(file string) {
	file = filepath.Clean(file)
	s.mu.Lock()
	s.mainFile = file
	s.baseDir = filepath.Dir(file)
	s.fs = http.FileServer(http.Dir(s.baseDir))
	s.mu.Unlock()
	rememberRecentFile(file)
}

func (s *server) hasMainFile() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mainFile != ""
}

func (s *server) fileServer() http.Handler {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.fs
}

// resolve maps a URL path to an absolute file path, confined to baseDir.
func (s *server) resolve(urlPath string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

func (s *server) openRecent(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("p")
	abs, err := filepath.Abs(p)
	if err != nil || !isMarkdown(abs) || !knownRecentFile(abs) {
		http.Error(w, "invalid recent file", http.StatusBadRequest)
		return
	}
	s.openFile(abs)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) pickFile(w http.ResponseWriter, r *http.Request) {
	p, ok := chooseMarkdownFile()
	if ok {
		abs, err := filepath.Abs(p)
		if err == nil {
			s.openFile(abs)
		}
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *server) root(w http.ResponseWriter, r *http.Request) {
	if !s.hasMainFile() {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s.renderMarkdown(w, "/", "mmark", []byte(helpSource()), "help")
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
	if fs := s.fileServer(); fs != nil {
		fs.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
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
		"default-src 'none'; img-src * data: blob:; media-src * data:; font-src 'self' data:; "+
			"style-src 'self' 'unsafe-inline'; script-src 'nonce-"+scriptNonce+"'; "+
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
		CanPick:    canChooseMarkdownFile(),
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

func recentFile() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "mmark", "recent.json")
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

type recentState struct {
	Files []string `json:"files"`
}

var recentMu sync.Mutex

func cleanRecentPath(p string) string {
	if p == "" {
		return ""
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func loadRecentFiles() []string {
	p := recentFile()
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var st recentState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil
	}
	files := make([]string, 0, len(st.Files))
	for _, f := range st.Files {
		f = cleanRecentPath(f)
		if f == "" || !isMarkdown(f) {
			continue
		}
		dup := false
		for _, existing := range files {
			if samePath(existing, f) {
				dup = true
				break
			}
		}
		if !dup {
			files = append(files, f)
		}
	}
	return files
}

func rememberRecentFile(file string) {
	file = cleanRecentPath(file)
	if file == "" || !isMarkdown(file) {
		return
	}
	recentMu.Lock()
	defer recentMu.Unlock()

	files := []string{file}
	for _, f := range loadRecentFiles() {
		if !samePath(f, file) {
			files = append(files, f)
		}
		if len(files) >= 10 {
			break
		}
	}
	p := recentFile()
	if p == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	b, err := json.MarshalIndent(recentState{Files: files}, "", "  ")
	if err == nil {
		os.WriteFile(p, b, 0o644)
	}
}

func knownRecentFile(file string) bool {
	file = cleanRecentPath(file)
	if file == "" {
		return false
	}
	for _, f := range loadRecentFiles() {
		if samePath(f, file) {
			return true
		}
	}
	return false
}

func helpSource() string {
	files := loadRecentFiles()
	if len(files) == 0 {
		return helpMD
	}
	var b strings.Builder
	b.WriteString(helpMD)
	b.WriteString("\n\n## 최근 파일\n\n")
	for _, f := range files {
		label := template.HTMLEscapeString(filepath.Base(f))
		full := template.HTMLEscapeString(f)
		href := "/__mmark/open?p=" + url.QueryEscape(f)
		fmt.Fprintf(&b, "- <a href=\"%s\">%s</a><br><code>%s</code>\n", href, label, full)
	}
	return b.String()
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

const baseCSS = `:root{color-scheme:light dark}
*{box-sizing:border-box}
.markdown-body{min-width:200px;max-width:980px;margin:0 auto;padding:45px}
#controls{position:fixed;top:12px;right:12px;z-index:30;display:flex;gap:6px}
#controls button,#search-panel button,.mmark-copy{width:36px;height:36px;border:1px solid rgba(128,128,128,.38);border-radius:8px;background:color-mix(in srgb, Canvas 86%, transparent);color:CanvasText;cursor:pointer;font-size:17px;line-height:1;padding:0;box-shadow:0 2px 10px rgba(0,0,0,.08)}
#controls button:hover,#search-panel button:hover,.mmark-copy:hover{background:color-mix(in srgb, CanvasText 10%, Canvas);border-color:rgba(128,128,128,.7)}
#toc{position:fixed;top:62px;left:16px;bottom:16px;z-index:20;width:230px;overflow:auto;padding:10px 8px;border:1px solid rgba(128,128,128,.28);border-radius:8px;background:color-mix(in srgb, Canvas 92%, transparent);backdrop-filter:blur(8px);font-size:13px;line-height:1.35}
#toc ol{list-style:none;margin:0;padding:0}
#toc li{margin:0}
#toc a{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:4px 8px;border-radius:6px;color:inherit;text-decoration:none;opacity:.74}
#toc a:hover,#toc a.is-active{background:color-mix(in srgb, CanvasText 10%, Canvas);opacity:1}
#toc .toc-level-2{padding-left:10px}
#toc .toc-level-3{padding-left:22px}
#toc .toc-level-4{padding-left:34px}
body.toc-collapsed #toc{display:none}
#search-panel{position:fixed;top:58px;right:12px;z-index:40;display:flex;align-items:center;gap:6px;max-width:calc(100vw - 24px);padding:8px;border:1px solid rgba(128,128,128,.34);border-radius:8px;background:color-mix(in srgb, Canvas 94%, transparent);box-shadow:0 8px 28px rgba(0,0,0,.16);backdrop-filter:blur(8px)}
#search-panel[hidden],#toc[hidden],#toc-toggle[hidden]{display:none!important}
#search-input{width:min(260px,calc(100vw - 220px));height:36px;border:1px solid rgba(128,128,128,.42);border-radius:8px;background:Canvas;color:CanvasText;padding:0 10px;font:inherit}
#search-count{min-width:48px;text-align:center;font-size:13px;color:color-mix(in srgb, CanvasText 68%, transparent)}
.mmark-search-hit{background:#ffe066;color:#111;border-radius:3px;padding:0 .08em}
.mmark-search-hit.is-active{background:#ff9f1a;color:#111;outline:2px solid rgba(255,159,26,.35)}
.mmark-code{position:relative}
.mmark-copy{position:absolute;top:8px;right:8px;opacity:0;width:32px;height:32px;font-size:15px}
.mmark-code:hover .mmark-copy,.mmark-copy:focus{opacity:1}
.mmark-mermaid{overflow:auto;margin:16px 0;text-align:center}
.mmark-mermaid svg{max-width:100%;height:auto}
.mmark-mermaid.is-error{text-align:left}
.katex-display{overflow-x:auto;overflow-y:hidden;padding:.2em 0}
@media (min-width:1261px){body.has-toc:not(.toc-collapsed) #toc{display:block}}
@media (max-width:1260px){#toc{display:none;right:12px;left:12px;top:58px;bottom:12px;width:auto}body.toc-open #toc{display:block}.markdown-body{padding-top:58px}}
@media (max-width:767px){.markdown-body{padding:58px 15px 20px}#controls{top:10px;right:10px;gap:4px}#controls button{width:34px;height:34px}#search-panel{left:10px;right:10px;top:54px}#search-input{width:100%;min-width:0}}
@media print{body{background:#fff!important;color:#000!important}#controls,#search-panel,#toc,.mmark-copy{display:none!important}.markdown-body{max-width:none!important;margin:0!important;padding:0!important;color:#000!important}pre,blockquote,table,img,svg{break-inside:avoid}pre{white-space:pre-wrap}a[href^="http"]::after{content:" (" attr(href) ")";font-size:.85em;color:#555}}
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
