# CODE — mmark 코드 구조

이 문서는 파일 단위·함수 단위의 역할, 핵심 로직의 상세, 의존 관계를 기술한다.
설계 배경은 [ARCHITECTURE.md](ARCHITECTURE.md), 실행·빌드는 [USAGE.md](USAGE.md)를 참조한다.

행 번호는 문서 작성 시점의 `main` 브랜치 기준이다.

---

## 1. 소스 트리

```
.
├── main.go            755줄  진입점 · HTTP 서버 · 렌더링 · 테마 · 최근 파일 · 워치독
├── math.go            399줄  goldmark 수식 확장 (AST 노드 · 파서 · 렌더러)
├── main_test.go       212줄  단위 테스트 13개
├── os_windows.go       98줄  Windows 전용 Win32 호출 (build tag: windows)
├── os_other.go         11줄  비Windows용 no-op 스텁 (build tag: !windows)
├── go.mod / go.sum            모듈 정의 (Go 1.25.0, 직접 의존 4개)
├── assets/
│   ├── app.js         517줄  브라우저 측 기능 전체
│   ├── help.md         27줄  인자 없이 실행 시 표시할 도움말
│   ├── icon.svg                README 전용 (embed 안 됨)
│   ├── github-markdown-light.css  1,124줄 (반입)
│   ├── github-markdown-dark.css   1,124줄 (반입)
│   ├── katex.min.js / katex.min.css / katex-auto-render.min.js  (반입, 0.17.0)
│   ├── katex/fonts/            60개 (ttf/woff/woff2 각 20종)
│   └── mermaid.min.js          (반입, 11.16.0)
├── .github/workflows/release.yml  31줄  태그 push 시 Windows 바이너리 릴리스
├── README.md / THIRD_PARTY_NOTICES.md / LICENSE / .gitignore
└── docs/                      이 문서 모음
```

패키지는 `main` 하나뿐이다. 내부 패키지 분할이 없으므로 `main.go`, `math.go`,
`os_*.go`의 모든 식별자가 서로 직접 보인다.

---

## 2. `main.go`

### 2.1 파일 상단 구조

| 행 | 내용 |
|---|---|
| 1–7 | 패키지 주석. 동작을 4문장으로 요약 |
| 9–44 | import. 표준 라이브러리 22개 + 외부 9개 |
| 46 | `var version = "dev"` — 링커의 `-X main.version=…`로 덮어씀 |
| 48–58 | `//go:embed` 지시자 4개 |
| 60–69 | 상수 `idleTimeout`, `idleMisses` |
| 71–74 | 전역 `lastPoll`, `errSeq` (둘 다 `atomic.Int64`) |
| 76–82 | `scriptNonce` — 즉시 실행 함수로 1회 생성 |
| 84–95 | `md` — goldmark 인스턴스 |
| 97–127 | `gitHubIDs` 타입과 메서드 |
| 129–167 | `pageTmpl` — HTML 템플릿 |
| 169–182 | `pageData` 구조체 |
| 184–194 | `server` 구조체 |
| 196–755 | 함수 정의 |

### 2.2 embed 지시자

| 행 | 지시자 | 변수 | 타입 |
|---|---|---|---|
| 48–49 | `assets/github-markdown-light.css` | `githubLightCSS` | `string` |
| 51–52 | `assets/github-markdown-dark.css` | `githubDarkCSS` | `string` |
| 54–55 | `assets/help.md` | `helpMD` | `string` |
| 57–58 | `app.js`, `mermaid.min.js`, `katex.min.js`, `katex-auto-render.min.js`, `katex.min.css`, `katex/fonts/*` | `staticAssets` | `embed.FS` |

앞의 세 개는 서버가 내부에서 소비하며 HTTP로 노출되지 않는다. 네 번째만
`/__mmark/assets/`로 서빙된다. `assets/icon.svg`는 어느 지시자에도 포함되지 않는다.

### 2.3 상수와 전역

| 식별자 | 값 / 타입 | 설명 |
|---|---|---|
| `idleTimeout` | `10 * time.Minute` | 마지막 폴링 이후 유휴로 간주하는 경과 시간 |
| `idleMisses` | `5` | 유휴 판정이 연속 몇 회여야 종료하는지 |
| `lastPoll` | `atomic.Int64` | 마지막 폴링 시각(UnixNano) |
| `errSeq` | `atomic.Int64` | 읽기 실패 stamp의 단조 증가 카운터 |
| `scriptNonce` | `string` (hex 32자) | `crypto/rand`로 16바이트 생성 후 hex 인코딩. 프로세스당 1회 |
| `version` | `string`, 기본 `"dev"` | 링커 주입 |
| `baseCSS` | `string` 상수, 686–728행 | 자체 작성 CSS 약 42줄 |
| `md` | `goldmark.Markdown` | 프로세스 전역 공유 |
| `pageTmpl` | `*template.Template` | `template.Must`로 기동 시 파싱 |

### 2.4 goldmark 인스턴스 (`md`, 84–95행)

```go
var md = goldmark.New(
	goldmark.WithExtensions(
		mathExtension{},
		extension.GFM,
		extension.Footnote,
		highlighting.NewHighlighting(
			highlighting.WithFormatOptions(chromahtml.WithClasses(true)),
		),
	),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(ghtml.WithUnsafe()),
)
```

| 요소 | 효과 |
|---|---|
| `mathExtension{}` | `math.go`의 자체 확장. 나머지 확장보다 먼저 등록되지만 실제 적용 순서는 각 파서의 우선순위 수치로 결정된다 |
| `extension.GFM` | Linkify + Table + Strikethrough + TaskList (goldmark 1.8.2의 `gfm.go`에서 확인) |
| `extension.Footnote` | 각주. GFM에 포함되지 않아 별도 등록 |
| `highlighting` | chroma 연동. `WithClasses(true)`로 클래스만 출력 |
| `parser.WithAutoHeadingID()` | heading에 자동 id 부여. 생성기는 요청별 컨텍스트로 교체 |
| `ghtml.WithUnsafe()` | 원시 HTML을 escape 없이 출력 |

`highlighting`에 스타일 이름을 주지 않는데, `WithClasses(true)` 모드에서는 출력이
스타일과 무관한 클래스 이름뿐이므로 영향이 없다. 실제 색상은 `buildThemeCSS()`가
따로 만든다.

### 2.5 `gitHubIDs` (97–127행)

`parser.IDs` 인터페이스를 구현해 goldmark 기본 생성기를 대체한다.

| 함수 | 시그니처 | 역할 |
|---|---|---|
| `newGitHubIDs()` | `() parser.IDs` | `used` 맵을 초기화한 인스턴스 생성 |
| `(*gitHubIDs).Generate` | `([]byte, ast.NodeKind) []byte` | slug 생성 |
| `(*gitHubIDs).Put` | `([]byte)` | 문서에 명시된 id를 사용됨으로 등록 |

`Generate`의 알고리즘:

```go
for _, r := range strings.ToLower(string(value)) {
	switch {
	case unicode.IsLetter(r) || unicode.IsNumber(r) || r == '_' || r == '-':
		sb.WriteRune(r)          // 유니코드 문자·숫자·언더스코어·하이픈 유지
	case r == ' ':
		sb.WriteRune('-')        // 공백 → 하이픈
	}                            // 그 외(문장부호 등)는 버림
}
```

빈 결과는 `"heading"`으로 대체하고, 중복이면 `base-1`, `base-2` … 를 시도한다.
`unicode.IsLetter`가 한글에 대해 참이므로 한글 제목이 보존된다. `사용 방법!` →
`사용-방법`, 같은 제목이 또 나오면 `사용-방법-1`이다(테스트로 고정).

### 2.6 `pageTmpl` (129–167행)

`html/template`로 파싱되는 단일 HTML 문서 템플릿이다.

문서 구조:

| 요소 | 내용 |
|---|---|
| `<html lang="ko">` | 언어 고정 |
| `<title>` | `{{.Title}} · mmark` (파일 이름 또는 `mmark`) |
| `<link rel="icon">` | data URI SVG. 100×100 viewBox, `#0ea5e9`→`#6366f1` 그라디언트 위에 흰색 `M`+아래 화살표 |
| `<link rel="stylesheet">` | `/__mmark/assets/katex.min.css` (유일한 외부 스타일시트) |
| `<style>` | `{{.BaseCSS}}` |
| `<style id="css-light" media="{{.LightMedia}}">` | `{{.LightCSS}}` |
| `<style id="css-dark" media="{{.DarkMedia}}">` | `{{.DarkCSS}}` |
| `#controls` | 버튼 5개 |
| `#toc` | `<nav>`, 초기 `hidden` |
| `#search-panel` | 입력창 + 카운터 + 버튼 3개, 초기 `hidden` |
| `<article class="markdown-body">` | `{{.Body}}` |
| `<script nonce>` ×5 | 설정 객체 + KaTeX + auto-render + Mermaid + app.js |

`#controls`의 버튼:

| id | 글리프 | title | 조건 |
|---|---|---|---|
| `open-file` | 📂 | 파일 열기 | `{{if .CanPick}}` — Windows에서만 |
| `toc-toggle` | ☰ | 목차 | 초기 `hidden`, 목차 생성 후 노출 |
| `search-open` | 🔎 | 검색 | 항상 |
| `print-doc` | 🖨 | 인쇄/PDF | 항상 |
| `theme-toggle` | 🌗 | 테마 | 항상 (글리프는 JS가 갱신) |

`#search-panel`의 요소: `search-input`, `search-count`(`0/0`), `search-prev`(↑),
`search-next`(↓), `search-close`(×).

설정 객체는 다음 한 줄로 주입된다. `html/template`가 JS 문맥을 인식해 값을 인용·이스케이프한다.

```js
window.__MMARK__ = { stamp: {{.Stamp}}, path: {{.Path}}, theme: {{.Theme}} };
```

### 2.7 `pageData` / `server` 타입

```go
type pageData struct {
	Title      string          // 문서 파일명 또는 "mmark"
	BaseCSS    template.CSS    // baseCSS 상수
	LightCSS   template.CSS    // buildThemeCSS("github", …)
	DarkCSS    template.CSS    // buildThemeCSS("github-dark", …)
	LightMedia string          // media 속성 값
	DarkMedia  string
	Theme      string          // "auto" | "light" | "dark"
	Nonce      string          // scriptNonce
	Body       template.HTML   // 렌더링된 본문
	Stamp      string          // 변경 감지용
	Path       string          // 현재 문서의 URL 경로
	CanPick    bool            // 파일 선택창 지원 여부
}
```

`template.CSS`/`template.HTML` 타입은 해당 값을 이스케이프하지 말라는 표시다.
`Title`, `Theme`, `Nonce`, `Stamp`, `Path`는 일반 `string`이므로 템플릿이 문맥에 맞게
이스케이프한다.

```go
type server struct {
	baseDir  string        // 절대 경로, Clean 적용
	mainFile string        // 절대 경로. 도움말 모드에서는 ""
	baseCSS  template.CSS
	lightCSS template.CSS
	darkCSS  template.CSS
	fs       http.Handler  // http.FileServer(http.Dir(baseDir))

	mu    sync.RWMutex
	theme string
}
```

`mu`가 실제로 보호하는 필드는 `baseDir`, `mainFile`, `fs`, `theme` 네 개다.
`baseCSS`/`lightCSS`/`darkCSS`는 기동 시 1회 쓰고 이후 읽기만 한다. 필드 위 주석은
"위쪽 가변 필드"라고 표현하고 있어 실제 범위보다 넓게 읽힌다.

### 2.8 함수 목록

| 행 | 함수 | 시그니처 | 역할 |
|---:|---|---|---|
| 196 | `main` | `()` | 진입점 |
| 252 | `watchdog` | `()` | 유휴 감지 고루틴 |
| 275 | `fatal` | `(string)` | stderr + 메시지 박스 후 `os.Exit(1)` |
| 281 | `serveAsset` | `(http.ResponseWriter, *http.Request)` | embed 자산 서빙 |
| 301 | `(*server).openFile` | `(string)` | 표시 문서 교체 |
| 311 | `(*server).hasMainFile` | `() bool` | 도움말 모드 판정 |
| 317 | `(*server).fileServer` | `() http.Handler` | 파일 서버 핸들 획득 |
| 324 | `(*server).resolve` | `(string) (string, bool)` | URL 경로 → 파일 경로, 범위 제한 |
| 347 | `isMarkdown` | `(string) bool` | 확장자 판정 |
| 355 | `(*server).openRecent` | 핸들러 | 최근 파일 열기 |
| 366 | `(*server).pickFile` | 핸들러 | 대화상자로 열기 |
| 377 | `(*server).root` | 핸들러 | 루트 라우팅 |
| 402 | `(*server).renderFile` | `(http.ResponseWriter, string, string)` | 파일 읽기 + 렌더링 |
| 421 | `(*server).renderMarkdown` | `(http.ResponseWriter, string, string, []byte, string)` | 변환 + 헤더 + 템플릿 실행 |
| 460 | `themeMedia` | `(string) (string, string)` | 테마 → media 문자열 쌍 |
| 470 | `validTheme` | `(string) bool` | 테마 값 검증 |
| 474 | `themeFile` | `() string` | 테마 저장 경로 |
| 482 | `recentFile` | `() string` | 최근 파일 저장 경로 |
| 490 | `loadTheme` | `() string` | 저장된 테마 읽기 |
| 507 | `cleanRecentPath` | `(string) string` | 절대화 + Clean |
| 518 | `samePath` | `(string, string) bool` | 경로 동일성 (Windows는 대소문자 무시) |
| 526 | `loadRecentFiles` | `() []string` | 최근 파일 읽기 + 정제 |
| 559 | `rememberRecentFile` | `(string)` | 최근 파일 추가 + 저장 |
| 589 | `knownRecentFile` | `(string) bool` | 최근 목록 포함 여부 |
| 602 | `helpSource` | `() string` | 도움말 Markdown 생성 |
| 621 | `(*server).setTheme` | 핸들러 | 테마 변경 + 영속화 |
| 640 | `(*server).status` | 핸들러 | stamp 반환 + heartbeat |
| 650 | `fileStamp` | `(string) string` | 파일 stamp 생성 |
| 661 | `decodeText` | `([]byte) string` | 인코딩 판별 후 디코드 |
| 732 | `buildThemeCSS` | `(string, string) string` | 테마 CSS 조립 |
| 746 | `openBrowser` | `(string)` | 기본 브라우저 실행 |

### 2.9 `main()` 상세 (196–244행)

```
attachConsole()                      // Windows에서만 실질 동작
if os.Args[1] in {"--version","-v"}  // 출력 후 반환
server 생성
  baseCSS  = baseCSS 상수
  lightCSS = buildThemeCSS("github", githubLightCSS)
  darkCSS  = buildThemeCSS("github-dark", githubDarkCSS)
  theme    = loadTheme()
fileArg 결정
  os.Args[1]  또는  chooseMarkdownFile()
  결정되면 filepath.Abs → s.openFile(abs)
net.Listen("tcp", "127.0.0.1:0")
mux 등록 6개
lastPoll = now;  go watchdog()
openBrowser("http://" + ln.Addr().String() + "/")
http.Serve(ln, mux)
```

주목할 점:

- 테마 CSS 조립이 기동 시 **1회**뿐이다. 요청마다 chroma CSS를 재생성하지 않는다.
- `openBrowser()`가 `http.Serve()`보다 먼저 호출되지만, 리스너는 이미 존재하므로
  브라우저의 첫 연결은 커널 backlog에 쌓였다가 처리된다.
- 인자가 없을 때의 `chooseMarkdownFile()` 호출이 [5.2 결함 A](#52-결함-a--windows-파일-선택-대화상자-panic)의
  진입 경로다.

### 2.10 `watchdog()` 상세 (252–273행)

```go
const tick = 2 * time.Second
misses := 0
prev := time.Now()
for {
	time.Sleep(tick)
	now := time.Now()
	if now.Round(0).Sub(prev.Round(0)) > 5*tick {   // 10초 초과 점프
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
```

| 수치 | 값 | 의미 |
|---|---|---|
| `tick` | 2초 | 검사 주기 |
| 점프 임계 | `5*tick` = 10초 | 이보다 큰 벽시계 전진은 절전 복귀로 간주 |
| `idleTimeout` | 10분 | 유휴 판정 기준 |
| `idleMisses` | 5 | 연속 판정 횟수 |
| 실제 종료 지연 | 약 10분 10초 | `idleTimeout` + `idleMisses × tick` |

`Round(0)`은 `time.Time`에서 단조 시계 성분을 제거한다. 이를 생략하면 `Sub`가 단조
시계 기준으로 계산되어 절전 구간이 반영되지 않으므로 점프 감지가 무력화된다.

### 2.11 `serveAsset()` 상세 (281–299행)

```go
rel := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/__mmark/assets/"))
if rel == "/" { 404 }
name := "assets" + rel
b, err := staticAssets.ReadFile(name)
if err != nil { 404 }
if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
	w.Header().Set("Content-Type", ct)
}
w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
w.Header().Set("X-Content-Type-Options", "nosniff")
http.ServeContent(w, r, path.Base(name), time.Time{}, bytes.NewReader(b))
```

- `path.Clean("/" + …)`이 `..`를 흡수하므로 embed FS 밖으로 나갈 수 없다.
- `http.ServeContent`에 zero `time.Time`을 넘겨 `Last-Modified`를 생략한다. Range
  요청과 조건부 요청 처리는 `ServeContent`가 담당한다.
- `mime.TypeByExtension`은 Go 내장 표에 없는 확장자에 대해 OS 설정을 참조한다. 내장
  표에는 `.woff`/`.woff2`/`.ttf`가 없다. Linux에서는 `/etc/mime.types`로
  `font/woff2`, `font/ttf`가 해석됨을 실측했다. Windows는 레지스트리를 참조하므로
  값이 없을 수 있으며, 그 경우 Content-Type 헤더가 붙지 않고 `ServeContent`가
  내용 스니핑 결과를 사용한다. 브라우저는 폰트 요청에 `nosniff` 차단을 적용하지
  않으므로 렌더링에 영향이 없다.

실측한 응답:

| 요청 | 상태 | Content-Type | 크기 |
|---|---:|---|---:|
| `/__mmark/assets/app.js` | 200 | `text/javascript; charset=utf-8` | 17,323 |
| `/__mmark/assets/katex.min.css` | 200 | `text/css; charset=utf-8` | 25,146 |
| `/__mmark/assets/katex/fonts/KaTeX_Main-Regular.woff2` | 200 | `font/woff2` | 26,272 |
| `/__mmark/assets/mermaid.min.js` | 200 | `text/javascript; charset=utf-8` | 3,565,102 |
| `/__mmark/assets/../main.go` | 404 | — | — |
| `/__mmark/assets/help.md` | 404 | — | — |
| `/__mmark/assets/github-markdown-light.css` | 404 | — | — |

뒤의 두 404는 의도된 결과다. 해당 파일은 `embed.FS`가 아니라 문자열로 embed되어
HTTP에 노출되지 않는다.

### 2.12 `resolve()` 상세 (324–345행)

```go
if urlPath == "/" { return s.mainFile, s.mainFile != "" }
if s.baseDir == "" { return "", false }
rel := strings.TrimPrefix(path.Clean(urlPath), "/")
p := filepath.Join(s.baseDir, filepath.FromSlash(rel))
prefix := s.baseDir
if !strings.HasSuffix(prefix, string(filepath.Separator)) {
	prefix += string(filepath.Separator)
}
if p == s.baseDir || strings.HasPrefix(p, prefix) { return p, true }
return "", false
```

접두사 조립에서 구분자 중복을 피하는 분기가 있는 이유는 `baseDir`가 이미 구분자로 끝날
수 있기 때문이다. Windows 드라이브 루트(`E:\`), UNC 공유 루트, POSIX 루트(`/`)가 그런
경우다. 조건 없이 구분자를 붙이면 `E:\\`가 되어 접두사 비교가 항상 실패한다.

동작 확인:

| 입력 | 출력 | 비고 |
|---|---|---|
| `/` | `mainFile`, true | |
| `/../outside.md` | `baseDir/outside.md`, true | `path.Clean`이 루트 위로 못 올라감. 테스트가 이 동작을 명시적으로 고정 |
| `/nested/../doc.md` | `baseDir/doc.md`, true | |
| `""` | `baseDir`, true | `path.Clean("")`이 `"."`이 되어 `Join`이 `baseDir`을 반환. 사용 경로에서는 도달하지 않음 |

### 2.13 `root()` 라우팅 (377–400행)

```
if !hasMainFile():                       # 도움말 모드
    if path != "/": 404
    renderMarkdown(w, "/", "mmark", helpSource(), "help")
    return
p, ok := resolve(path)
if !ok: 404
if path == "/" or isMarkdown(p):
    renderFile(w, path, p)
    return
if fs := fileServer(); fs != nil:
    fs.ServeHTTP(w, r)
    return
404
```

`isMarkdown()`이 인식하는 확장자는 `.md`, `.markdown`, `.mdown`, `.mkd` 네 개이며
대소문자를 무시한다(`strings.ToLower(filepath.Ext(p))`).

### 2.14 `renderFile()`의 stamp 분기 (402–419행)

```go
raw, err := os.ReadFile(file)
if err != nil {
	stamp := fileStamp(file)                       // 통상 "gone"
	if _, serr := os.Stat(file); serr == nil {
		stamp = fmt.Sprintf("retry-%d", errSeq.Add(1))
	}
	src := fmt.Sprintf("# 파일을 열 수 없습니다\n\n```\n%s\n```\n", err)
	s.renderMarkdown(w, urlPath, filepath.Base(file), []byte(src), stamp)
	return
}
s.renderMarkdown(w, urlPath, filepath.Base(file), []byte(decodeText(raw)), fileStamp(file))
```

| 상황 | `os.Stat` | stamp | 폴링 결과 |
|---|---|---|---|
| 파일 없음 | 실패 | `"gone"` (안정) | 파일이 생길 때까지 재요청 없음, 생기면 stamp가 바뀌어 자동 복구 |
| 파일 있으나 읽기 실패 | 성공 | `"retry-N"` (매번 증가) | 다음 폴링에서 반드시 불일치 → 즉시 재시도 |

두 번째 경우는 Windows에서 편집기가 저장하는 순간의 공유 위반을 상정한 것이다.

오류 화면도 HTTP 200으로 반환한다. 실측으로 존재하지 않는 문서 요청은 200과 stamp
`"gone"`을 준다.

### 2.15 `renderMarkdown()` 상세 (421–458행)

```
buf 준비
ctx = parser.NewContext(parser.WithIDs(newGitHubIDs()))   ← 요청마다 새 ID 집합
md.Convert(src, &buf, parser.WithContext(ctx))
  실패 시 buf를 "<h1>렌더링 오류</h1><pre>…</pre>"로 대체 (에러 메시지는 HTMLEscape)
theme 읽기 (RLock)
lightMedia, darkMedia = themeMedia(theme)
헤더 설정
  Content-Type: text/html; charset=utf-8
  Cache-Control: no-store
  X-Content-Type-Options: nosniff
  Content-Security-Policy: (9개 지시자)
pageTmpl.Execute(w, pageData{…})
```

`Cache-Control: no-store`는 자동 새로고침이 항상 최신 본문을 받도록 하기 위한 것이다.
정적 자산의 `immutable`과는 정반대의 정책이며, 이 대비는 의도된 것이다.

`pageTmpl.Execute`의 반환 오류는 확인하지 않는다. 이 시점에는 이미 헤더가 전송되어
있어 상태 코드를 바꿀 수 없다.

### 2.16 테마 관련 함수 (460–499, 621–636행)

| 함수 | 동작 |
|---|---|
| `themeMedia("light")` | `("all", "not all")` |
| `themeMedia("dark")` | `("not all", "all")` |
| `themeMedia(그 외)` | `("(prefers-color-scheme: light)", "(prefers-color-scheme: dark)")` |
| `validTheme(t)` | `t`가 `auto`/`light`/`dark` 중 하나인지 |
| `themeFile()` | `os.UserConfigDir()` + `mmark/theme`. 실패 시 `""` |
| `loadTheme()` | 파일을 읽어 `TrimSpace` 후 `validTheme`이면 반환, 아니면 `"auto"` |
| `(*server).setTheme` | 쿼리 `set` 검증 → 메모리 반영 → 디렉터리 생성(`0o755`) → 파일 기록(`0o644`) → `204 No Content` |

검증 실패 시 `400 Bad Request`를 반환한다(실측: `?set=bogus` → 400).
파일 쓰기 실패는 무시한다. 설정 저장은 보조 기능이며, 실패해도 현재 세션의 테마는
메모리에 반영되어 동작한다.

### 2.17 최근 파일 (501–600행)

```go
type recentState struct {
	Files []string `json:"files"`
}
var recentMu sync.Mutex
```

| 함수 | 동작 |
|---|---|
| `cleanRecentPath(p)` | 빈 문자열이면 `""`. `filepath.Abs` 실패 시 `""`. 성공 시 `filepath.Clean` 결과 |
| `samePath(a,b)` | 양쪽 `Clean` 후, Windows면 `strings.EqualFold`, 그 외는 `==` |
| `loadRecentFiles()` | 파일 읽기 → JSON 파싱 → 각 항목에 `cleanRecentPath` + `isMarkdown` 필터 + 중복 제거 → 슬라이스 반환. 어떤 단계든 실패하면 `nil` |
| `rememberRecentFile(f)` | `recentMu` 아래에서 `f`를 맨 앞에 두고 기존 목록을 이어 붙임. **최대 10개**. 디렉터리 생성 후 `MarshalIndent`(들여쓰기 2칸)로 기록 |
| `knownRecentFile(f)` | 목록에 `samePath`로 일치하는 항목이 있는지 |

`rememberRecentFile`의 반복문은 다음과 같다.

```go
files := []string{file}
for _, f := range loadRecentFiles() {
	if !samePath(f, file) {
		files = append(files, f)
	}
	if len(files) >= 10 {
		break
	}
}
```

길이 검사가 append 여부와 무관하게 매 반복 수행되므로, 목록에 이미 같은 파일이 있으면
결과 길이가 10보다 하나 적어질 수 있다. 상한 자체는 항상 10 이하로 유지된다.

저장 파일 실측 내용:

```json
{
  "files": [
    "<절대 경로>"
  ]
}
```

### 2.18 `helpSource()` (602–617행)

`helpMD`(embed된 `assets/help.md`)를 기본으로 하고, 최근 파일이 있으면 뒤에
`## 최근 파일` 절을 덧붙인다. 항목은 원시 HTML로 생성한다.

```go
label := template.HTMLEscapeString(filepath.Base(f))
full  := template.HTMLEscapeString(f)
href  := "/__mmark/open?p=" + url.QueryEscape(f)
fmt.Fprintf(&b, "- <a href=\"%s\">%s</a><br><code>%s</code>\n", href, label, full)
```

표시 문자열은 HTML 이스케이프, URL은 쿼리 이스케이프로 각각 처리한다.
`url.QueryEscape`는 `"`와 `&`를 각각 `%22`, `%26`으로 바꾸므로 속성값 문맥에서도
안전하다. 원시 HTML이 그대로 출력되는 것은 `ghtml.WithUnsafe()` 덕분이다.

### 2.19 `fileStamp()` / `status()` (640–656행)

```go
func fileStamp(p string) string {
	st, err := os.Stat(p)
	if err != nil { return "gone" }
	return fmt.Sprintf("%d-%d", st.ModTime().UnixNano(), st.Size())
}
```

stamp 형식은 `<수정시각 나노초>-<바이트 크기>`다. 실측 예: `1787552826344562519-461`.

`status()`는 두 가지 일을 한다.

1. `lastPoll.Store(time.Now().UnixNano())` — heartbeat 갱신 (워치독의 유일한 입력)
2. 쿼리 `p`를 `resolve`한 뒤 stamp를 JSON으로 반환. 해석 실패 시 `"help"`

응답은 `{"stamp":"…"}` 한 개 키의 객체다.

### 2.20 `decodeText()` (661–684행)

| 순서 | 조건 | 처리 |
|---:|---|---|
| 1 | `len(b) >= 2` 이고 `b[0:2] == FF FE` | `unicode.UTF16(LittleEndian, ExpectBOM)` 디코드, 성공 시 반환 |
| 2 | `len(b) >= 2` 이고 `b[0:2] == FE FF` | `unicode.UTF16(BigEndian, ExpectBOM)` 디코드, 성공 시 반환 |
| 3 | 접두사 `EF BB BF` | 제거 |
| 4 | `utf8.Valid(b)` | 그대로 문자열화 |
| 5 | — | `korean.EUCKR` 디코드, 성공 시 반환 |
| 6 | — | 원본 바이트를 문자열화 |

테스트가 고정하는 값:

| 입력 바이트 | 기대 결과 |
|---|---|
| `EF BB BF` + `hello` | `hello` |
| `FF FE 5C D5 00 AE` | `한글` |
| `FE FF D5 5C AE 00` | `한글` |
| `C7 D1 B1 DB` | `한글` |

`korean.EUCKR`은 `golang.org/x/text`에서 CP949(EUC-KR 확장)를 구현한다.

### 2.21 `baseCSS` (686–728행)

`main.go` 안에 raw string 상수로 들어 있는 자체 CSS다. 주요 수치를 정리한다.

| 항목 | 값 |
|---|---|
| `--mm-accent` | `#6366f1` |
| `--mm-ring` | `rgba(99,102,241,.3)` |
| `.markdown-body` | `min-width:200px; max-width:980px; margin:0 auto; padding:45px` |
| 본문 등장 애니메이션 | `mm-fade .3s ease` (opacity 0→1, translateY 6px→0) |
| 패널 등장 애니메이션 | `mm-drop .18s ease` (translateY −8px→0) |
| `#controls` | `position:fixed; top:12px; right:12px; z-index:30; gap:6px` |
| 컨트롤 버튼 | `38×38px`, `border-radius:11px`, `backdrop-filter:blur(10px)` |
| `#toc` | `position:fixed; top:62px; left:16px; bottom:16px; z-index:20; width:236px` |
| 목차 들여쓰기 | level-2 `10px`, level-3 `22px`, level-4 `34px` |
| 목차 활성 항목 | 배경 `color-mix(… var(--mm-accent) 14% …)`, `box-shadow: inset 3px 0 0` |
| `#search-panel` | `position:fixed; top:60px; right:12px; z-index:40` |
| `#search-input` | `width:min(260px, calc(100vw - 220px)); height:38px` |
| 검색 하이라이트 | `#ffe066` (배경), 활성 항목 `#ff9f1a` |
| 코드 복사 버튼 | `32×32px`, 기본 `opacity:0`, hover/focus 시 `1` |
| 수식 오류 색 | `#d1242f` |
| 데스크톱 breakpoint | `min-width:1261px` — 목차 상시 표시 |
| 태블릿 breakpoint | `max-width:1260px` — 목차를 전체 폭 오버레이로 |
| 모바일 breakpoint | `max-width:767px` — 패딩 `58px 15px 20px`, 버튼 `34×34px` |
| 인쇄 | `#controls`, `#search-panel`, `#toc`, `.mmark-copy` 숨김, `pre` 줄바꿈, 외부 링크 뒤 URL 표기 |
| 모션 감소 | `prefers-reduced-motion: reduce`에서 모든 애니메이션·트랜지션 제거 |

`color-mix(in srgb, Canvas 82%, transparent)`처럼 시스템 색상 키워드(`Canvas`,
`CanvasText`)를 쓰므로, 컨트롤 UI는 별도 테마 CSS 없이 라이트·다크 양쪽에 자동으로
맞춰진다.

### 2.22 `buildThemeCSS()` (732–744행)

```go
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
```

다크 여부를 스타일 이름의 부분 문자열 `"dark"` 포함 여부로 판정한다. 현재 인자는
`"github"`과 `"github-dark"` 두 개뿐이라 문제가 없지만, `"solarized-dark"`가 아닌
다른 명명 규칙의 어두운 스타일을 넣으면 배경색이 어긋난다.

측정한 출력 크기:

| 호출 | 총 길이 | 구성 |
|---|---:|---|
| `buildThemeCSS("github", "")` | 4,357 | `body{…}` 30바이트 + chroma CSS |
| `buildThemeCSS("github-dark", "")` | 5,016 | `body{…}` 33바이트 + chroma CSS |

실제 사용 시에는 여기에 github-markdown CSS 22,219바이트가 더해진다.

### 2.23 `openBrowser()` (746–755행)

| GOOS | 명령 |
|---|---|
| `windows` | `rundll32 url.dll,FileProtocolHandler <url>` |
| `darwin` | `open <url>` |
| 그 외 | `xdg-open <url>` |

모두 `.Start()`이므로 완료를 기다리지 않고 오류도 확인하지 않는다. 브라우저가 열리지
않아도 서버는 동작하며, 사용자가 직접 URL을 입력해 접속할 수 있다.

Windows에서 `rundll32 url.dll,FileProtocolHandler`를 쓰는 것은 `cmd /c start`가
콘솔 창을 잠깐 띄우는 문제를 피하기 위한 통상적 방법이다.

---

## 3. `math.go`

### 3.1 구성

| 행 | 요소 |
|---|---|
| 18 | `kindMath` — 인라인 수식 노드 종류 |
| 20–41 | `mathNode` 타입 (`ast.BaseInline` 임베드) |
| 43 | `kindMathBlock` — 블록 수식 노드 종류 |
| 45–58 | `mathBlockNode` 타입 (`ast.BaseBlock` 임베드) |
| 60–74 | `mathExtension` — 확장 등록 |
| 76–120 | `mathBlockParser` |
| 122–201 | `mathParser` (인라인) 및 하위 파싱 함수 |
| 203–277 | 구분자 탐색 함수 4개 + `isEscapedByte` |
| 279–359 | 수식 판정 휴리스틱 5개 함수 |
| 361–399 | `mathHTMLRenderer` |

### 3.2 AST 노드

```go
type mathNode struct {
	ast.BaseInline
	value   []byte   // 구분자를 제외한 수식 원문 (복사본)
	display bool     // true면 디스플레이 수식
}
```

`newMathNode()`는 입력 슬라이스를 그대로 참조하지 않고 **복사**한다. 원본은 goldmark의
소스 버퍼이며 이후 단계에서 재사용될 수 있기 때문이다.

```go
type mathBlockNode struct{ ast.BaseBlock }
func (n *mathBlockNode) IsRaw() bool { return true }
```

`IsRaw() == true`가 이 타입의 핵심이다. goldmark는 raw 블록의 내용을 인라인 파싱하지
않으므로, 블록 안의 `-`, `1.`, `*`, 빈 줄이 Markdown 구조로 해석되지 않는다.
fenced code block과 같은 취급이다.

### 3.3 확장 등록 (62–74행)

```go
m.Parser().AddOptions(
	parser.WithBlockParsers(util.Prioritized(mathBlockParser{}, 850)),
	parser.WithInlineParsers(util.Prioritized(mathParser{}, 150)),
)
m.Renderer().AddOptions(renderer.WithNodeRenderers(
	util.Prioritized(mathHTMLRenderer{}, 500),
))
```

goldmark 1.8.2의 기본 우선순위와 비교하면 위치가 명확해진다.

| 종류 | 파서 | 우선순위 |
|---|---|---:|
| 블록 | setext heading | 100 |
| 블록 | thematic break | 200 |
| 블록 | list | 300 |
| 블록 | list item | 400 |
| 블록 | code block | 500 |
| 블록 | ATX heading | 600 |
| 블록 | fenced code block | 700 |
| 블록 | blockquote | 800 |
| 블록 | **mathBlockParser** | **850** |
| 블록 | HTML block | 900 |
| 블록 | paragraph | 1000 |
| 인라인 | code span | 100 |
| 인라인 | **mathParser** | **150** |
| 인라인 | link | 200 |
| 인라인 | auto link | 300 |
| 인라인 | raw HTML | 400 |
| 인라인 | emphasis | 500 |

인라인 150의 의미:

- code span(100)보다 뒤 → `` `$x$` ``는 코드로 유지된다.
- emphasis(500)보다 앞 → `$a*b*c$`의 `*`가 강조로 소비되기 전에 수식으로 확보된다.

렌더러 우선순위 500은 기본 HTML 렌더러(1000)와 다른 슬롯일 뿐이다.
`kindMath`/`kindMathBlock`은 기본 렌더러가 다루지 않는 종류이므로 충돌하지 않는다.

### 3.4 `mathBlockParser` (78–120행)

fenced code block과 유사한 열기/계속/닫기 의미를 가진다.

| 메서드 | 동작 |
|---|---|
| `Trigger()` | `[]byte{'$'}` |
| `Open()` | 블록 오프셋 위치에서 `$$`로 시작하는지 확인. 나머지에 `$$`가 또 있으면(단일 행 형태) 열지 않고 인라인 파서에 넘긴다. 여는 줄 뒤에 내용이 있으면 그 구간을 라인으로 추가 |
| `Continue()` | 오른쪽 공백을 제거한 줄이 `$$`로 끝나고 그 `$$`가 이스케이프되지 않았으면 앞부분을 추가하고 `parser.Close`. 아니면 줄 전체를 추가하고 `parser.Continue \| parser.NoChildren` |
| `Close()` | 없음 |
| `CanInterruptParagraph()` | `true` — 문단 중간에서도 `$$` 줄이 블록을 시작할 수 있다 |
| `CanAcceptIndentedLine()` | `false` — 4칸 들여쓴 줄은 코드 블록이 우선 |

`Open()`의 단일 행 회피 분기가 중요하다.

```go
rest := line[pos+2:]
if bytes.Contains(rest, []byte("$$")) {
	return nil, parser.NoChildren   // `$$x$$ …` 는 인라인 파서 담당
}
```

이 분기 덕분에 `앞 $$E=mc^2$$ 뒤.` 같은 문장이 블록으로 오인되지 않는다
(테스트 `TestSingleLineDoubleDollarStaysInline`).

여는 줄과 닫는 줄에 내용이 있어도 보존한다. `$$\begin{aligned}` 로 시작하고
`\end{aligned} $$` 로 끝나는 형태가 그대로 동작한다
(테스트 `TestDisplayMathBlockOpenerAndCloserMayCarryContent`).

### 3.5 `mathParser` (인라인, 122–201행)

`Trigger()`는 `[]byte{'$', '\\'}`를 반환한다.

```
line[0] == '$'  → parseDollarMath()
line[0] == '\\' 이고 line[1] == '(' → parseBackslashMath(…, ')', display=false)
line[0] == '\\' 이고 line[1] == '[' → parseBackslashMath(…, ']', display=true)
그 외 → nil
```

결과는 `normalizeTableCellMath()`를 거쳐 반환된다.

#### `parseDollarMath()` (171–189행)

```
src[start+1] == '$' (즉 `$$`)
    end = findDoubleDollarCloseInBlock()      # 여러 줄에 걸쳐 탐색
    내용이 공백뿐이면 nil
    display=true 노드
그 외 (`$` 하나)
    src[start-1] == '$' 이면 nil               # `$$`의 두 번째 `$`를 다시 처리하지 않음
    end = findSingleDollarClose(src, start+1) # 같은 줄 안에서만
    looksLikeInlineMathBytes() 실패 시 nil
    display=false 노드
```

#### `parseBackslashMath()` (191–201행)

```go
end := findBackslashClose(src, start+2, closer)
if display {
	end = findBackslashCloseInBlock(block, src, closer)
}
```

`display == true`인 경로에서 첫 줄의 `findBackslashClose` 결과는 즉시 덮어써진다.
결과에는 영향이 없고 불필요한 스캔이 1회 발생한다.

#### `normalizeTableCellMath()` (157–169행)

부모 체인을 거슬러 올라가 `extast.KindTableCell`을 만나면 값의 `\|`를 `|`로 바꾼다.
GFM 표에서 셀 안의 파이프는 `\|`로 이스케이프해야 하는데, 그 이스케이프가 수식 원문에
남으면 KaTeX가 `‖`(`\Vert`)로 렌더링해 버린다. GitHub도 같은 되돌림을 한다.

표 밖에서는 되돌리지 않으므로 `$\|x\|$`(노름)는 그대로 유지된다. 두 동작 모두 테스트로
고정되어 있다. 브라우저 실측에서 표 안의 `$P(a\|b)$`는 `P(a∣b)`로 렌더링되고 셀 구조
(`<td>` 2개)도 유지되었다.

### 3.6 구분자 탐색 함수 (203–277행)

| 함수 | 범위 | 종료 조건 |
|---|---|---|
| `findSingleDollarClose(src, start)` | 같은 줄 | 개행을 만나면 −1. `$`가 이스케이프되지 않고 앞뒤가 `$`가 아니면 그 위치 반환 |
| `findDoubleDollarCloseInBlock(block, src)` | 여러 줄 | 리더 위치를 저장/복원하며 줄 단위로 전진. 이스케이프되지 않은 `$$`를 찾으면 위치 반환. 줄이 없으면 −1 |
| `findBackslashClose(src, start, closer)` | 같은 줄 | 개행을 만나면 −1. 이스케이프되지 않은 `\<closer>`를 찾으면 그 위치 반환 |
| `findBackslashCloseInBlock(block, src, closer)` | 여러 줄 | 위와 같되 줄 단위 전진 |
| `isEscapedByte(src, index)` | — | `index` 앞의 연속 백슬래시 개수가 홀수면 `true` |

`…InBlock` 계열은 `block.Position()`으로 위치를 저장하고 `defer block.SetPosition(...)`로
복원한다. 탐색만 하고 리더 상태는 바꾸지 않기 위함이다. 실제 전진은 호출부에서
`block.Advance()`로 한다.

### 3.7 인라인 수식 판정 휴리스틱 (279–359행)

`looksLikeInlineMathBytes(source []byte) bool`의 판정 순서:

```
text = TrimSpace(source)
1. text == "" → false
2. isPlainNumber(text) → false
3. text에 다음 문자가 하나라도 있으면 → true
     \ _ ^ { } = + - * / < >
     ∑(U+2211) ∫(U+222B) √(U+221A) ∞(U+221E) ≈(U+2248) ≠(U+2260) ≤(U+2264) ≥(U+2265)
4. isSingleASCIIIdentifier(text) → true
5. hasInlineMathOperator(text) 결과 반환
```

보조 함수:

| 함수 | 판정 |
|---|---|
| `isPlainNumber(text)` | 숫자와 구분자(`.` 또는 `,`) **하나**로만 구성되고 숫자가 하나 이상 있으면 `true`. `5`, `3.14`, `1,000`이 해당. `1.2.3`은 구분자가 둘이라 `false` |
| `isSingleASCIIIdentifier(text)` | 첫 글자가 ASCII 영문이고 이후가 전부 숫자면 `true`. `x`, `a1`, `T2` 해당 |
| `hasInlineMathOperator(text)` | `=+-*/^_` 중 하나를 찾아, 그 왼쪽(공백 건너뜀)에 ASCII 영문이 있거나 오른쪽(공백 건너뜀)에 ASCII 영숫자가 있으면 `true` |
| `hasASCIIAlphaBefore(runes, i)` | 왼쪽으로 공백·탭을 건너뛰고 처음 만난 문자가 ASCII 영문인지 |
| `hasASCIIAlphaNumAfter(runes, i)` | 오른쪽으로 공백·탭을 건너뛰고 처음 만난 문자가 ASCII 영숫자인지 |

3번 목록에 `-`가 포함되어 있어 `$-5$`처럼 음수도 수식으로 판정된다. 통화 표기에서
`$-5`가 나올 가능성은 낮다고 본 판단이다.

`assets/app.js`의 `looksLikeInlineMath()`가 같은 규칙을 정규식으로 구현한다.

```js
if (!text || /^\d+(?:[.,]\d+)?$/.test(text)) return false;
if (/[\\_^{}=+\-*/<>]|[∑∫√∞≈≠≤≥]/.test(text)) return true;
if (/^[A-Za-z](?:\d+)?$/.test(text)) return true;
return /[A-Za-z]\s*[=+\-*/^_]|[=+\-*/^_]\s*[A-Za-z0-9]/.test(text);
```

두 구현은 완전히 동일하지는 않다. 예를 들어 Go 쪽 `isPlainNumber`는 `1,000` 같은
천 단위 구분을 수용하지만 JS 쪽 정규식 `^\d+(?:[.,]\d+)?$`는 `1,000`도 매칭한다
(소수부로 해석). 실질적 차이는 거의 없으나 두 곳을 함께 고쳐야 한다는 점은 유지보수
비용이다.

### 3.8 `mathHTMLRenderer` (361–399행)

두 노드 종류를 등록한다.

| 노드 | 출력 |
|---|---|
| `kindMathBlock` | `<span class="mmark-math mmark-math-display" data-display="true">` + escape된 원문 + `</span>\n` |
| `kindMath` (display=true) | `<span class="mmark-math mmark-math-display" data-display="true">…</span>` |
| `kindMath` (display=false) | `<span class="mmark-math mmark-math-inline" data-display="false">…</span>` |

두 경우 모두 `stdhtml.EscapeString`으로 이스케이프한다. 즉 **서버는 수식을 렌더링하지
않는다.** 원문을 안전하게 담아 두고 실제 조판은 브라우저의 KaTeX가 한다. 이 구조 덕분에
서버에 수식 엔진을 넣지 않아도 되고, 오류 수식도 원문을 잃지 않는다.

`renderMathBlock`은 노드의 모든 라인을 순서대로 이어 붙인다. `ast.WalkSkipChildren`을
반환해 자식 순회를 막는다.

디스플레이 수식을 `<div>`가 아니라 `<span>`으로 내보내면서 CSS
`.mmark-math-display{display:block}`으로 블록화한다. 인라인 문맥(문단 내부)에서
`<div>`를 내보내면 HTML 유효성이 깨지기 때문이다.

---

## 4. 플랫폼 의존 코드

### 4.1 `os_other.go` (11줄)

```go
//go:build !windows

func attachConsole()                     {}
func fatalUI(msg string)                 {}
func canChooseMarkdownFile() bool        { return false }
func chooseMarkdownFile() (string, bool) { return "", false }
```

`canChooseMarkdownFile()`이 `false`이므로 `pageData.CanPick`이 `false`가 되고,
템플릿의 `{{if .CanPick}}` 분기에 따라 📂 버튼이 아예 렌더링되지 않는다.
`/__mmark/pick`은 등록되어 있으나 아무 일도 하지 않고 `/`로 리다이렉트한다.

### 4.2 `os_windows.go` (98줄)

`syscall.NewLazyDLL`로 세 DLL을 지연 로드한다.

| DLL | 프로시저 | 용도 |
|---|---|---|
| `kernel32.dll` | `AttachConsole` | 부모 콘솔 연결 |
| `user32.dll` | `MessageBoxW` | 오류 대화상자 |
| `comdlg32.dll` | `GetOpenFileNameW` | 파일 선택 대화상자 |

#### `openFileName` 구조체 (20–44행)

Win32 `OPENFILENAMEW`의 Go 표현이다. 필드 순서와 타입이 정확히 대응한다.
`lStructSize`에 `unsafe.Sizeof(openFileName{})`를 넣는데, amd64에서 Go의 구조체
레이아웃이 C의 `OPENFILENAMEW`와 동일한 152바이트가 되도록 필드 순서가 맞춰져 있다
(`nFileOffset`/`nFileExtension` 두 `uint16` 뒤에 포인터 정렬용 4바이트 패딩이
자동 삽입되는 것까지 C와 일치).

#### `attachConsole()` (50–60행)

```go
const attachParentProcess = ^uintptr(0) & 0xFFFFFFFF   // 0xFFFFFFFF = ATTACH_PARENT_PROCESS
r, _, _ := procAttachConsole.Call(attachParentProcess)
if r == 0 { return }
if f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0); err == nil {
	os.Stdout = f
	os.Stderr = f
}
```

`-H windowsgui`로 빌드하면 콘솔이 붙지 않아 `--version` 출력과 오류 메시지가 사라진다.
`AttachConsole(ATTACH_PARENT_PROCESS)`로 호출한 쉘의 콘솔에 붙고, `CONOUT$`를 열어
`os.Stdout`/`os.Stderr`를 교체한다. 콘솔이 없으면(더블클릭 실행) 조용히 반환한다.

#### `fatalUI()` (64–69행)

`MessageBoxW(0, text, "mmark", MB_ICONERROR)`를 호출한다. `MB_ICONERROR`는 `0x10`이다.
콘솔이 없는 실행 경로에서 기동 오류를 사용자에게 보여 주는 유일한 수단이다.

#### `chooseMarkdownFile()` (73–98행)

```go
file := make([]uint16, 32768)          // 경로 버퍼 64 KB
filter := syscall.StringToUTF16(
	"Markdown files (*.md;*.markdown;*.mdown;*.mkd)\x00*.md;*.markdown;*.mdown;*.mkd\x00All files (*.*)\x00*.*\x00")
title  := syscall.StringToUTF16("마크다운 파일 열기")
defExt := syscall.StringToUTF16("md")
ofn := openFileName{
	lStructSize: uint32(unsafe.Sizeof(openFileName{})),
	lpstrFilter: &filter[0],
	lpstrFile:   &file[0],
	nMaxFile:    uint32(len(file)),
	lpstrTitle:  &title[0],
	flags:       ofnExplorer | ofnFileMustExist | ofnHideReadOnly | ofnPathMustExist,
	lpstrDefExt: &defExt[0],
}
```

플래그 값: `OFN_EXPLORER`=`0x00080000`, `OFN_FILEMUSTEXIST`=`0x00001000`,
`OFN_HIDEREADONLY`=`0x00000004`, `OFN_PATHMUSTEXIST`=`0x00000800`.

이 함수에는 결함이 있다. [5.2](#52-결함-a--windows-파일-선택-대화상자-panic) 참조.

---

## 5. 확인된 결함

실행·측정으로 확인한 것만 적는다.

### 5.1 요약

| # | 위치 | 심각도 | 영향 |
|---:|---|---|---|
| A | `os_windows.go:81` | 높음 (Windows 전용) | 파일 선택 대화상자 경로에서 프로세스 panic |
| B | `main.go:691` + github-markdown CSS 2행 | 중간 (전 플랫폼) | 본문 좌측 정렬 + 목차가 본문을 가림 |
| C | `main.go:296` | 낮음 | 자산 캐시 헤더가 실행 간에 무효 |

### 5.2 결함 A — Windows 파일 선택 대화상자 panic

**원인.** `syscall.StringToUTF16`은 인자에 NUL 바이트가 있으면 panic한다. Go 표준
라이브러리 구현이 명시적으로 그렇게 되어 있다.

```go
// syscall/syscall_windows.go
func StringToUTF16(s string) []uint16 {
	a, err := UTF16FromString(s)
	if err != nil {
		panic("syscall: string with NUL passed to StringToUTF16")
	}
	return a
}

func UTF16FromString(s string) ([]uint16, error) {
	if bytealg.IndexByteString(s, 0) != -1 {
		return nil, EINVAL
	}
	...
}
```

그런데 Win32 `OPENFILENAMEW`의 `lpstrFilter`는 정의상 "NUL로 구분되고 이중 NUL로
끝나는" 문자열이어야 한다. `os_windows.go:81`의 필터 문자열에는 NUL이 4개 들어 있다
(측정으로 확인).

**영향 경로.**

| 진입 | 결과 |
|---|---|
| 인자 없이 실행 | `main()`의 `chooseMarkdownFile()`에서 panic. 리스너 생성 전이므로 프로세스가 그대로 종료된다. README 35행과 `help.md`가 설명하는 "파일 선택창 → 도움말" 경로에 도달할 수 없다. |
| 📂 버튼 | `/__mmark/pick` 처리기에서 panic. `net/http`가 핸들러 panic을 복구하고 연결을 끊으므로 서버는 계속 살아 있고, 브라우저에서만 요청이 실패한다. |

파일 인자를 주는 경로(드래그 앤 드롭, 파일 연결, 명령줄)는 이 함수를 호출하지 않으므로
영향이 없다. 이 때문에 결함이 드러나기 어렵다.

**확인 방법.** Windows 대상 크로스 컴파일과 `go vet`은 통과한다(둘 다 실측). panic은
런타임 조건이므로 정적 검사로 잡히지 않는다. 필터 문자열의 NUL 개수와 표준 라이브러리
구현을 대조해 확정했다.

**수정 방향.** `syscall.StringToUTF16` 대신 NUL을 허용하는 인코딩을 직접 수행한다.

```go
import "unicode/utf16"

filterRunes := []rune("Markdown files (*.md;…)\x00*.md;…\x00All files (*.*)\x00*.*\x00")
filter := append(utf16.Encode(filterRunes), 0)   // 이중 NUL 종료
```

`title`과 `defExt`는 NUL을 포함하지 않으므로 현재 코드 그대로 두어도 된다.
`fatalUI()`가 쓰는 `syscall.UTF16PtrFromString`은 오류를 반환하는 안전한 형태다.

### 5.3 결함 B — 본문 정렬과 목차 겹침

**원인.** CSS 캐스케이드 순서 문제다.

| 순서 | 위치 | 규칙 |
|---:|---|---|
| 1 | `main.go:691` (첫 번째 `<style>`) | `.markdown-body{min-width:200px;max-width:980px;margin:0 auto;padding:45px;…}` |
| 2 | `github-markdown-*.css` 2행 (두 번째 `<style>`) | `.markdown-body { … margin: 0; … }` |

두 선택자의 특이도는 모두 `(0,1,0)`으로 같다. CSS 규칙상 특이도가 같으면 소스 순서상
뒤에 오는 것이 이긴다. 따라서 `margin: 0`이 적용되고 가운데 정렬이 무효화된다.

생성된 HTML에서 두 선언의 바이트 오프셋을 확인했다: `margin:0 auto`가 1,237,
`margin: 0;`이 6,609. 후자가 뒤다.

**측정.** Chrome에서 세 가지 뷰포트 폭으로 계산값을 읽었다.

| 뷰포트 폭 | 계산된 margin | article 사각형 | 첫 문단 | `#toc` 사각형 |
|---:|---|---|---|---|
| 1300 px | `0px / 0px` | left 0, width 980 | 45 → 935 | 16 → 252 |
| 1400 px | `0px / 0px` | left 0, width 980 | 45 → 935 | 16 → 252 |
| 1600 px | `0px / 0px` | left 0, width 980 | 45 → 935 | 16 → 252 |
| 1000 px | `0px / 0px` | left 0, width 980 | 45 → 935 | 목차 숨김 |

`#toc`는 `position:fixed; left:16px; width:236px`이므로 오른쪽 끝이 252 px이고,
`@media (min-width:1261px)`에서 기본 표시된다. 본문 텍스트는 45 px에서 시작하므로
약 207 px 폭만큼 항상 가려진다.

두 가지 독립적 방법으로 확인했다.

- `document.elementFromPoint(h1.left + 5, h1.top + h1.height/2)` → `toc`
- 본문 링크 클릭 시도 시 자동화 도구가 `<nav id="toc"> intercepts pointer events`로 거부

즉 텍스트가 가려질 뿐 아니라 그 영역의 링크를 클릭할 수 없다.

**부수 사실.** 가운데 정렬이 정상 동작하더라도 겹침이 완전히 사라지지는 않는다.
본문 텍스트의 왼쪽 좌표는 `(W-980)/2 + 45`이고, 이것이 252 px를 넘으려면 `W ≥ 1394`가
필요하다. breakpoint가 1261 px이므로 1261–1393 px 구간은 원래 설계로도 겹친다.

**수정 방향.** 두 가지를 함께 처리해야 한다.

1. 정렬 복구 — `baseCSS`의 규칙 특이도를 높이거나(`body .markdown-body{margin:0 auto}`)
   레이아웃 규칙을 테마 CSS 뒤로 옮긴다.
2. 겹침 제거 — 목차 상시 표시 breakpoint를 1394 px 이상으로 올리거나, 목차가 열려
   있을 때 본문에 왼쪽 여백을 주어 공간을 확보한다.

### 5.4 결함 C — 자산 캐시 헤더의 실효성

`serveAsset`이 붙이는 `Cache-Control: public, max-age=31536000, immutable`은 origin
단위로 적용된다. origin에는 포트가 포함되고, mmark는 `127.0.0.1:0`으로 매번 다른
포트를 받는다. 따라서 실행할 때마다 캐시가 미스되어 3.5 MB Mermaid와 271 KB KaTeX가
다시 전송된다. loopback이라 체감 비용은 작지만 헤더가 실행 간에는 이득을 주지 않는다.
같은 실행 안에서 문서 사이를 이동할 때는 정상 동작한다.

### 5.5 미사용·중복 요소

| 항목 | 위치 | 내용 |
|---|---|---|
| `assets/icon.svg` | — | `//go:embed` 대상이 아니다. README 이미지 전용. favicon은 `pageTmpl`에 data URI로 별도 작성(512×512 대 100×100 viewBox). 같은 도안의 두 사본이므로 한쪽만 고치면 어긋난다 |
| `.gitignore`의 `dist/` | `.gitignore:3` | 저장소·워크플로 어디에도 `dist/`를 만드는 곳이 없다 |
| `data-mmark-theme` | `app.js:47` | 설정만 하고 읽는 곳이 없다. 저장소 전체 검색으로 확인 |
| `findBackslashClose` 첫 호출 | `math.go:192` | `display == true`에서 결과가 즉시 버려진다 |
| `server.mu` 주석 | `main.go:192` | "위쪽 가변 필드"라 표현하지만 실제 보호 대상은 4개 필드뿐 |
| `resolve("")` | `main.go:324` | `baseDir` 자체를 반환한다. 도달 경로 없음 |

---

## 6. `main_test.go`

테스트 13개, 전부 통과한다(Go 1.27.0 실측).

| # | 테스트 | 고정하는 동작 |
|---:|---|---|
| 1 | `TestGitHubIDsKeepKoreanAndDeduplicate` | `사용 방법!` → `사용-방법`, 중복 시 `사용-방법-1`, `!!!` → `heading` |
| 2 | `TestResolveKeepsPathsInsideBaseDir` | `/` → mainFile, `/../outside.md` → `baseDir/outside.md`(true), `/nested/../doc.md` → mainFile |
| 3 | `TestRenderMarkdownCSPBlocksRemoteMedia` | CSP에 `default-src 'none'`, `img-src 'self' data: blob:`, `media-src 'self' data: blob:`, `connect-src 'self'` 포함. `img-src *`·`media-src *` 없음 |
| 4 | `TestDecodeTextHandlesCommonWindowsMarkdownEncodings` | UTF-8 BOM, UTF-16 LE/BE, CP949 디코드 |
| 5 | `TestMathDelimitersSurviveMarkdownParsing` | `\(a*b*c\)`·`\[x_i = y^2\]`가 `<em>` 없이 span으로 |
| 6 | `TestDollarMathIsProtectedBeforeEmphasis` | `$a*b*c$`·`$$\sum_{i=1}^n i$$` 동일 |
| 7 | `TestDollarMathAvoidsCommonFalsePositives` | `Cost is $5` 는 수식 아님, `` `$x$` ``는 `<code>$x$</code>` 유지 |
| 8 | `TestDisplayMathBlockAllowsBlankLines` | 빈 줄이 있어도 `mmark-math-display`가 정확히 1개 |
| 9 | `TestDisplayMathBlockSurvivesListLikeLines` | `- x &= 1`, `1. + y &= 2`가 `<ul>`/`<ol>`이 되지 않고 `\begin{aligned}` 보존 |
| 10 | `TestDisplayMathBlockOpenerAndCloserMayCarryContent` | `$$\begin{aligned}` … `\end{aligned} $$` 형태 지원 |
| 11 | `TestSingleLineDoubleDollarStaysInline` | `앞 $$E=mc^2$$ 뒤.`가 디스플레이 수식 + 주변 텍스트 유지 |
| 12 | `TestTableCellMathUnescapesPipes` | 표 셀 안 `$P(a\|b)$` → `P(a|b)`, `<td>` 2개 유지 |
| 13 | `TestPipeOutsideTableCellIsKeptInMath` | 표 밖 `$\|x\|$`는 `\|` 유지 |

헬퍼는 하나다.

```go
func renderMarkdownBodyForTest(t *testing.T, src string) string
```

전역 `md`로 변환하고 요청별 `parser.Context`를 만들어 실제 렌더링 경로와 같은 조건을
재현한다.

**테스트되지 않는 영역** — 워치독의 시간 로직, 최근 파일 영속화, 테마 영속화,
`serveAsset`, `openFile`/`root`의 라우팅 분기, 플랫폼 의존 코드, `app.js` 전체.

---

## 7. `assets/app.js`

IIFE 하나로 감싼 ES5 코드다. 트랜스파일하지 않고 그대로 embed한다.

### 7.1 상단 상태

```js
var config = window.__MMARK__ || {};          // 서버가 주입
var article = document.querySelector(".markdown-body");
if (!article) return;                         // 본문이 없으면 전체 중단
var theme = config.theme || "auto";
var darkQuery = window.matchMedia ? window.matchMedia("(prefers-color-scheme: dark)") : null;
var mermaidRun = 0;                           // Mermaid 렌더 세대 카운터
```

### 7.2 함수 목록

| 행 | 함수 | 역할 |
|---:|---|---|
| 12 | `setupPolling` | 1,000 ms 주기로 `/__mmark/status` 폴링, stamp 불일치 시 `location.reload()` |
| 22 | `resolvedTheme` | `auto`일 때 미디어 쿼리로 실제 테마 결정 |
| 27 | `applyTheme` | 두 `<style>`의 `media` 교체, 버튼 글리프·title 갱신, `data-mmark-theme` 설정 |
| 50 | `setupTheme` | 버튼 클릭 시 `auto→light→dark→auto` 순환, 서버 저장, Mermaid 재렌더. 시스템 테마 변경 구독 |
| 74 | `setupPrint` | 🖨 → `window.print()` |
| 79 | `setupOpenFile` | 📂 → `location.href = "/__mmark/pick"` |
| 84 | `setupToc` | `h1`~`h4`에서 목차 생성, 토글, 스크롤 연동 활성 항목 표시 |
| 138 | `copyText` | 클립보드 API, 실패 시 임시 `<textarea>` + `execCommand("copy")` 폴백 |
| 160 | `setupCodeCopy` | 각 `<pre>`를 `div.mmark-code`로 감싸고 복사 버튼 추가 |
| 185 | `isEscaped` | 앞의 연속 백슬래시 개수가 홀수인지 |
| 191 | `isSingleDollar` | 위치가 단일 `$`인지(앞뒤가 `$`가 아니고 이스케이프 아님) |
| 198 | `looksLikeInlineMath` | Go의 `looksLikeInlineMathBytes`와 같은 규칙 |
| 206 | `skipMathTextNode` | 수식 스캔 제외 대상 판정 |
| 216 | `protectInlineDollarMath` | 텍스트 노드의 `$…$`를 `\(…\)`로 치환 |
| 260 | `renderProtectedMath` | `.mmark-math` span을 KaTeX로 렌더 |
| 279 | `setupMath` | 위 둘 실행 후 KaTeX auto-render 호출 |
| 295 | `prepareMermaid` | `pre > code.language-mermaid` 등을 `div.mmark-mermaid`로 교체 |
| 307 | `renderMermaidDiagrams` | Mermaid 초기화 후 전체 다이어그램 렌더 |
| 341 | `setupSearch` | 검색 패널 전체(하이라이트, 이동, 카운터, 단축키) |

### 7.3 폴링 (12–20행)

```js
var statusURL = "/__mmark/status?p=" + encodeURIComponent(config.path || "/");
setInterval(function () {
	fetch(statusURL).then(r => r.json()).then(j => {
		if (j.stamp !== stamp) location.reload();
	}).catch(function () {});
}, 1000);
```

- 간격 1,000 ms 고정
- `catch`가 비어 있다. 서버가 종료된 뒤에도 콘솔에 오류가 쌓이지 않는다
- `config.path`가 자기 문서의 URL 경로이므로 하위 문서를 보고 있어도 정확히 그 문서를
  감시한다(실측: `/sub/child.md` 페이지의 `window.__MMARK__.path`가 `/sub/child.md`)

### 7.4 테마 (22–72행)

`applyTheme(t)`가 하는 일:

| 대상 | `light` | `dark` | `auto` |
|---|---|---|---|
| `#css-light`의 `media` | `all` | `not all` | `(prefers-color-scheme: light)` |
| `#css-dark`의 `media` | `not all` | `all` | `(prefers-color-scheme: dark)` |
| 버튼 글리프 | ☀️ | 🌙 | 🌗 |
| 버튼 title | `테마: 라이트` | `테마: 다크` | `테마: 자동` |

`document.documentElement.dataset.mmarkTheme`에 `resolvedTheme()` 값을 넣지만 이를
읽는 코드는 없다.

버튼 클릭 시 순환하고, `fetch("/__mmark/theme?set=" + theme, {method:"POST"})`로 서버에
알린 뒤 `renderMermaidDiagrams()`를 호출한다. 시스템 테마 변경 이벤트는 `theme`이
`auto`일 때만 반응한다. `addEventListener`가 없는 구형 브라우저를 위해 `addListener`
폴백이 있다.

### 7.5 목차 (84–136행)

- 대상: `article` 안의 `h1, h2, h3, h4` 중 `id`가 있고 텍스트가 비지 않은 것
- 하나도 없으면 목차와 ☰ 버튼을 모두 만들지 않고 반환
- 생성 후 `toc.hidden = false`, `btn.hidden = false`, `body.classList.add("has-toc")`
- 링크 `href`는 `"#" + encodeURIComponent(heading.id)` — 한글 id를 위해 필요
- 항목 클래스는 `toc-level-1` … `toc-level-4`

토글 동작이 폭에 따라 다르다.

```js
if (window.matchMedia("(max-width: 1260px)").matches) {
	document.body.classList.toggle("toc-open");     // 오버레이 열기/닫기
} else {
	document.body.classList.toggle("toc-collapsed"); // 상시 표시 접기/펴기
}
```

활성 항목 추적은 스크롤 이벤트를 `requestAnimationFrame`으로 스로틀하고,
`getBoundingClientRect().top <= 90`인 마지막 heading을 활성으로 표시한다.
`90`은 상단 컨트롤 영역 높이를 고려한 값이다. 스크롤 리스너는 `{passive: true}`다.

### 7.6 코드 복사 (138–183행)

```js
if (pre.closest(".mmark-mermaid") || pre.parentElement.classList.contains("mmark-code")) return;
```

Mermaid 컨테이너 안의 `<pre>`(오류 표시용)와 이미 감싼 것은 건너뛴다.
`prepareMermaid()`가 먼저 실행되므로 정상 렌더된 다이어그램에는 복사 버튼이 붙지
않는다(실측: 코드 블록 1개 + Mermaid 1개 문서에서 복사 버튼 1개).

복사 성공 시 버튼이 `✓`로, 실패 시 `!`로 바뀌고 **900 ms** 후 `⧉`로 돌아온다.
클립보드 API가 없으면 화면 밖(`left:-9999px`)에 `<textarea>`를 만들어
`document.execCommand("copy")`로 폴백한다.

### 7.7 수식 (185–293행)

`setupMath()`의 순서:

1. `renderProtectedMath()` — 서버가 만든 `.mmark-math` span을 KaTeX로 렌더.
   `dataset.rendered === "true"`면 건너뛴다. `katex.render(source, node, {displayMode, throwOnError:false})`.
   예외 발생 시 `is-error` 클래스와 `title` 속성(오류 메시지)을 붙인다.
2. `window.renderMathInElement`가 없으면 종료.
3. `protectInlineDollarMath(article)` — 원시 HTML 안에 남은 `$…$`를 `\(…\)`로 치환.
4. `renderMathInElement(article, {...})` 호출.

4단계의 옵션:

| 옵션 | 값 |
|---|---|
| `delimiters` | `$$…$$`(display), `\[…\]`(display), `\(…\)`(inline) |
| `throwOnError` | `false` |
| `ignoredTags` | `script`, `noscript`, `style`, `textarea`, `pre`, `code`, `option` |
| `ignoredClasses` | `katex`, `mmark-math`, `mmark-mermaid` |

`delimiters`에 단일 `$…$`가 없다는 점이 중요하다. 단일 `$`는 오탐이 잦아
`protectInlineDollarMath`가 휴리스틱으로 걸러 `\(…\)`로 바꾼 것만 넘어간다.

`protectInlineDollarMath`는 `TreeWalker`로 텍스트 노드를 모은 뒤(순회 중 DOM을 바꾸면
walker가 무효화되므로 배열에 먼저 담는다) `DocumentFragment`를 만들어 교체한다.
`$` 없는 노드와 `skipMathTextNode` 대상은 `FILTER_REJECT`로 제외한다.

### 7.8 Mermaid (295–339행)

`prepareMermaid()`가 찾는 선택자:

```
pre > code.language-mermaid,
pre > code.lang-mermaid,
pre > code[data-lang='mermaid']
```

실제로 생성되는 것은 첫 번째다. 확인 근거: chroma 2.27.0의 embed된 lexer 279개 중
`mermaid`가 없으므로 `lexers.Get("mermaid")`가 `nil`을 반환하고,
goldmark-highlighting은 이 경우 `<pre><code class="language-<언어>">` 폴백을 출력한다.
실제 렌더 결과를 확인했다.

```html
<pre><code class="language-mermaid">graph TD;
A--&gt;B;
</code></pre>
```

비교를 위해 `go` 블록은 chroma가 처리한다.

```html
<pre class="chroma"><code><span class="line"><span class="cl"><span class="kd">func</span>…
```

`renderMermaidDiagrams()`의 세대 관리:

```js
var run = ++mermaidRun;
…
Promise.resolve(window.mermaid.render(id, source)).then(function (result) {
	if (run !== mermaidRun) return;      // 이전 세대 결과는 폐기
	block.innerHTML = result.svg;
	…
});
```

테마 전환으로 재렌더가 겹칠 때 늦게 도착한 이전 결과가 새 결과를 덮어쓰지 않게 한다.
렌더 중에는 `is-rendering` 클래스로 `opacity:.35`를 적용한다. 실패 시 `is-error`와
함께 오류 메시지를 `<pre><code>`로 표시한다.

`mermaid.initialize`는 `startOnLoad:false`, `securityLevel:"strict"`,
`theme: resolvedTheme() === "dark" ? "dark" : "default"`로 매 렌더마다 호출된다.
원본 소스는 `block.dataset.source`에 보관되므로 재렌더가 가능하다.

### 7.9 검색 (341–506행)

내부 상태: `marks`(하이라이트 `<mark>` 배열), `active`(현재 인덱스), `searchTimer`.

| 동작 | 구현 |
|---|---|
| 입력 디바운스 | **120 ms**. 입력 중에는 카운터에 `...` 표시 |
| 빈 입력 | 디바운스 없이 즉시 `runSearch()` (마크 제거) |
| 검색 범위 | `article` 안의 텍스트 노드. `SCRIPT`, `STYLE`, `TEXTAREA`, `INPUT`, `BUTTON` 태그와 `.katex`, `.mmark-mermaid` 하위는 제외 |
| 대소문자 | 무시 (`toLowerCase()` 비교) |
| 하이라이트 | `<mark class="mmark-search-hit">`, 활성 항목은 `is-active` 추가 |
| 이동 | `activate(index)`가 `(index + n) % n`으로 순환. `scrollIntoView({block:"center"})` |
| 카운터 | `<현재>/<전체>` 형식. 결과 없으면 `0/0` |
| 마크 제거 | 원래 텍스트 노드로 되돌린 뒤 `parent.normalize()`로 인접 노드 병합 |

단축키:

| 키 | 동작 |
|---|---|
| `Ctrl`+`F` / `Cmd`+`F` | 검색 열기 (브라우저 기본 동작 차단) |
| `/` (입력 중이 아닐 때) | 검색 열기 |
| `Esc` (패널 열림) | 검색 닫기 + 마크 제거 + 입력 비우기 |
| `Enter` (입력창 포커스) | 다음 결과 |
| `Shift`+`Enter` (입력창 포커스) | 이전 결과 |

`flushSearch()`는 디바운스 대기 중에 이동 버튼이나 Enter가 눌리면 즉시 검색을 실행해
결과 없는 상태로 이동하는 것을 막는다.

실측: 3개 일치하는 질의에 대해 `.mmark-search-hit` 3개 생성, 카운터 `1/3`.

---

## 8. 의존 관계

### 8.1 외부 모듈

`go.mod` 기준. Go 지시자는 `1.25.0`이다.

| 모듈 | 버전 | 종류 | 용도 |
|---|---|---|---|
| `github.com/yuin/goldmark` | v1.8.2 | 직접 | Markdown 파서·렌더러 |
| `github.com/yuin/goldmark-highlighting/v2` | v2.0.0-20230729083705-37449abec8cc | 직접 | goldmark ↔ chroma 연결 |
| `github.com/alecthomas/chroma/v2` | v2.27.0 | 직접 | 문법 강조 (embed lexer 279개) |
| `golang.org/x/text` | v0.38.0 | 직접 | UTF-16·EUC-KR 디코더 |
| `github.com/dlclark/regexp2/v2` | v2.2.1 | 간접 | chroma의 정규식 엔진 |

표준 라이브러리 외에 다른 의존은 없다. GUI 툴킷·웹뷰·파일 감시 라이브러리를 쓰지
않는다.

### 8.2 파일 간 의존

```
main.go ──uses──> math.go            (mathExtension)
main.go ──uses──> os_windows.go / os_other.go
                    attachConsole()
                    fatalUI()
                    canChooseMarkdownFile()
                    chooseMarkdownFile()
main.go ──embeds─> assets/*
main_test.go ──tests──> main.go, math.go
```

`math.go`는 `main.go`의 식별자를 참조하지 않는다. 단방향 의존이다.
`os_windows.go`/`os_other.go`는 서로 배타적인 빌드 태그를 가지며, 네 개의 동일한
시그니처를 제공한다.

### 8.3 서버 내부 호출 그래프

```
main()
 ├─ attachConsole()
 ├─ buildThemeCSS() ×2 ──> chromahtml.New().WriteCSS(), styles.Get()
 ├─ loadTheme() ──> themeFile()
 ├─ chooseMarkdownFile()                 [플랫폼별]
 ├─ (*server).openFile() ──> rememberRecentFile() ──> loadRecentFiles(), cleanRecentPath(), samePath()
 ├─ watchdog()
 └─ openBrowser()

(*server).root()
 ├─ hasMainFile()
 ├─ helpSource() ──> loadRecentFiles()
 ├─ resolve()
 ├─ isMarkdown()
 ├─ renderFile() ──> decodeText(), fileStamp(), renderMarkdown()
 ├─ renderMarkdown() ──> md.Convert(), newGitHubIDs(), themeMedia(), pageTmpl.Execute(), canChooseMarkdownFile()
 └─ fileServer() ──> http.FileServer

(*server).status()  ──> lastPoll.Store(), resolve(), fileStamp()
(*server).setTheme() ──> validTheme(), themeFile()
(*server).openRecent() ──> isMarkdown(), knownRecentFile(), openFile()
(*server).pickFile() ──> chooseMarkdownFile(), openFile()
serveAsset() ──> staticAssets.ReadFile(), mime.TypeByExtension(), http.ServeContent()
```

### 8.4 클라이언트 → 서버 호출

| 위치 | 요청 | 주기 / 계기 |
|---|---|---|
| `setupPolling` | `GET /__mmark/status?p=<경로>` | 1,000 ms |
| `setupTheme` | `POST /__mmark/theme?set=<값>` | 테마 버튼 클릭 |
| `setupOpenFile` | `GET /__mmark/pick` (네비게이션) | 📂 클릭 |
| 도움말 화면의 링크 | `GET /__mmark/open?p=<경로>` (네비게이션) | 최근 파일 클릭 |
| `<script src>` ×4, `<link href>` ×1 | `GET /__mmark/assets/…` | 페이지 로드 |
| KaTeX CSS의 `@font-face` | `GET /__mmark/assets/katex/fonts/…` | 사용된 글꼴만 |

---

## 9. 수치·상수 일람

| 항목 | 값 | 위치 |
|---|---|---|
| 폴링 간격 | 1,000 ms | `app.js:19` |
| 워치독 검사 주기 | 2초 | `main.go:253` |
| 유휴 판정 시간 | 10분 | `main.go:64` |
| 유휴 연속 판정 횟수 | 5 | `main.go:68` |
| 벽시계 점프 임계 | 10초 (`5*tick`) | `main.go:259` |
| 실제 종료 지연 | 약 10분 10초 | 계산값 |
| 최근 파일 최대 | 10 | `main.go:572` |
| nonce 길이 | 16바이트 → hex 32자 | `main.go:79` |
| 검색 디바운스 | 120 ms | `app.js:435` |
| 복사 버튼 복귀 | 900 ms | `app.js:176, 179` |
| 목차 활성 판정 기준선 | 뷰포트 상단 90 px | `app.js:124` |
| 본문 최대 폭 | 980 px | `main.go:691` |
| 본문 패딩 | 45 px (모바일 `58px 15px 20px`) | `main.go:691, 725` |
| 목차 폭 / 좌측 여백 | 236 px / 16 px | `main.go:696` |
| 목차 상시 표시 breakpoint | 1,261 px 이상 | `main.go:723` |
| 모바일 breakpoint | 767 px 이하 | `main.go:725` |
| 컨트롤 버튼 크기 | 38×38 px (모바일 34×34) | `main.go:693, 725` |
| 자산 캐시 수명 | 31,536,000초 (1년) | `main.go:296` |
| 설정 디렉터리 권한 | `0o755` | `main.go:580, 631` |
| 설정 파일 권한 | `0o644` | `main.go:585, 632` |
| Windows 경로 버퍼 | 32,768 × `uint16` (64 KB) | `os_windows.go:80` |
| `OPENFILENAMEW` 크기 (amd64) | 152 바이트 | 구조체 레이아웃 |
| 수식 블록 파서 우선순위 | 850 | `math.go:65` |
| 수식 인라인 파서 우선순위 | 150 | `math.go:68` |
| 수식 렌더러 우선순위 | 500 | `math.go:72` |
| 지원 확장자 | `.md`, `.markdown`, `.mdown`, `.mkd` | `main.go:349` |
| 리스너 주소 | `127.0.0.1:0` | `main.go:224` |

---

## 10. 검증 기록

Go 1.27.0으로 확인했다.

| 명령 | 결과 |
|---|---|
| `go build ./...` | 성공 |
| `go vet ./...` (linux/amd64) | 지적 사항 없음 |
| `GOOS=windows GOARCH=amd64 go vet ./...` | 지적 사항 없음 |
| `go test ./...` | `ok github.com/33modeling/mmark 0.038s`, 13/13 통과 |
| `GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=…"` | 성공, 19,709,952 바이트 |

브라우저 실행 검증 항목은 [ARCHITECTURE.md 11.3](ARCHITECTURE.md#113-실행-확인)에 정리했다.
