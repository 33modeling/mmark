// mmark is a tiny zero-install markdown viewer.
//
// It renders a markdown file to GitHub-flavored HTML, serves it on a
// localhost-only HTTP server, and opens the default browser. The page polls
// the server for file changes (auto-reload) and the process exits on its own
// once no browser tab is polling anymore.
package main

import (
	"bytes"
	_ "embed"
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
	"unicode/utf8"

	chromahtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/text/encoding/korean"
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
	// After a pagehide beacon, wait this long for a follow-up poll (page
	// reload / navigation) before shutting down.
	byeGrace = 10 * time.Second
)

var lastPoll atomic.Int64

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
<script>
(function () {
  var stamp = {{.Stamp}};
  var statusURL = "/__mmark/status?p=" + encodeURIComponent({{.Path}});
  setInterval(function () {
    fetch(statusURL).then(function (r) { return r.json(); }).then(function (j) {
      if (j.stamp !== stamp) location.reload();
    }).catch(function () {});
  }, 1000);
  addEventListener("pagehide", function () {
    navigator.sendBeacon("/__mmark/bye");
  });

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
			fmt.Fprintln(os.Stderr, "mmark:", err)
			os.Exit(1)
		}
		s.mainFile = abs
		s.baseDir = filepath.Dir(abs)
		s.fs = http.FileServer(http.Dir(s.baseDir))
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "mmark:", err)
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/__mmark/status", s.status)
	mux.HandleFunc("/__mmark/bye", s.bye)
	mux.HandleFunc("/__mmark/theme", s.setTheme)
	mux.HandleFunc("/", s.root)

	lastPoll.Store(time.Now().UnixNano())
	go func() {
		for {
			time.Sleep(2 * time.Second)
			if time.Since(time.Unix(0, lastPoll.Load())) > idleTimeout {
				os.Exit(0)
			}
		}
	}()

	openBrowser("http://" + ln.Addr().String() + "/")
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "mmark:", err)
		os.Exit(1)
	}
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
	if p == s.baseDir || strings.HasPrefix(p, s.baseDir+string(filepath.Separator)) {
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
		src := fmt.Sprintf("# 파일을 열 수 없습니다\n\n```\n%s\n```\n", err)
		s.renderMarkdown(w, urlPath, filepath.Base(file), []byte(src), fileStamp(file))
		return
	}
	s.renderMarkdown(w, urlPath, filepath.Base(file), []byte(decodeText(raw)), fileStamp(file))
}

func (s *server) renderMarkdown(w http.ResponseWriter, urlPath, title string, src []byte, stamp string) {
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
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
	pageTmpl.Execute(w, pageData{
		Title:      title,
		BaseCSS:    s.baseCSS,
		LightCSS:   s.lightCSS,
		DarkCSS:    s.darkCSS,
		LightMedia: lightMedia,
		DarkMedia:  darkMedia,
		Theme:      theme,
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

// bye rewinds the heartbeat so the process exits shortly after the last tab
// closes; an immediate poll from a reload/navigation cancels it.
func (s *server) bye(w http.ResponseWriter, r *http.Request) {
	lastPoll.Store(time.Now().Add(byeGrace - idleTimeout).UnixNano())
	w.WriteHeader(http.StatusNoContent)
}

func fileStamp(p string) string {
	st, err := os.Stat(p)
	if err != nil {
		return "gone"
	}
	return fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
}

// decodeText assumes UTF-8 and falls back to CP949/EUC-KR, the legacy
// encoding of Korean Windows text files.
func decodeText(b []byte) string {
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
