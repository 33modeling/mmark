# USAGE — mmark 사용 방법

설치·실행, 화면 조작, 빌드·배포, 설정 변경 지점, 문제 해결을 다룬다.
설계 배경은 [ARCHITECTURE.md](ARCHITECTURE.md), 코드 상세는 [CODE.md](CODE.md)를 참조한다.

---

## 1. 요구 사항

### 1.1 실행

| 항목 | 내용 |
|---|---|
| OS | Windows(주 대상). Linux·macOS에서도 소스 빌드로 동작 |
| 아키텍처 | 배포 바이너리는 `amd64` |
| 브라우저 | 기본 브라우저 하나. `fetch`, `TreeWalker`, `color-mix()`, `backdrop-filter`를 사용하므로 최신 브라우저 필요 |
| 네트워크 | 불필요. 외부 통신을 하지 않는다 |
| 설치 절차 | 없음. 실행 파일 하나를 원하는 위치에 두면 된다 |
| 관리자 권한 | 불필요 |

### 1.2 빌드

| 항목 | 내용 |
|---|---|
| Go | `go.mod`의 지시자는 `1.25.0`. Go 1.25 이상 |
| 인터넷 | 최초 빌드 시 모듈 다운로드에 필요 (직접 의존 4개, 간접 1개) |
| 크로스 컴파일 | cgo를 쓰지 않으므로 어떤 호스트에서도 `GOOS=windows` 빌드가 가능 |

---

## 2. 설치와 실행

### 2.1 내려받기

저장소의 Releases 페이지에서 `mmark.exe`를 받는다. 설치 과정은 없다.

처음 실행할 때 Windows SmartScreen 경고가 표시될 수 있다. 코드 서명이 없는 실행
파일에 대한 표준 안내이며, *추가 정보 → 실행*으로 진행한다.

### 2.2 문서 열기

| 방법 | 조작 |
|---|---|
| 드래그 앤 드롭 | 탐색기에서 `.md` 파일을 `mmark.exe` 위로 끌어다 놓는다 |
| 파일 연결 | `.md` 우클릭 → *연결 프로그램* → *다른 앱 선택* → `mmark.exe` → *항상 이 앱 사용* 체크. 이후 더블클릭만으로 열린다 |
| 명령줄 | `mmark.exe 문서.md` |

Linux·macOS에서 소스 빌드한 경우도 동일하다.

```sh
./mmark 문서.md
```

### 2.3 버전 확인

```
mmark.exe --version
mmark.exe -v
```

`mmark <버전>` 형식으로 출력한다. 릴리스 바이너리는 태그 이름이, 직접 빌드한
바이너리는 기본값 `dev`가 나온다.

Windows 배포 바이너리는 `-H windowsgui`로 빌드되어 콘솔 창을 띄우지 않는다. 그래도
`cmd`·PowerShell에서 실행하면 `AttachConsole`로 부모 콘솔에 붙어 출력이 보인다.

### 2.4 인식하는 확장자

`.md`, `.markdown`, `.mdown`, `.mkd` (대소문자 무시)

### 2.5 인식하는 인코딩

| 인코딩 | 조건 |
|---|---|
| UTF-8 | BOM 있어도 없어도 됨 |
| UTF-16 LE | BOM(`FF FE`) 필수 |
| UTF-16 BE | BOM(`FE FF`) 필수 |
| CP949 / EUC-KR | UTF-8로 해석되지 않을 때 자동 적용 |

BOM 없는 UTF-16은 지원하지 않는다.

### 2.6 종료

브라우저 탭을 닫으면 폴링이 끊기고, 약 10분 10초 후 프로세스가 스스로 종료된다.
즉시 끝내려면 작업 관리자에서 `mmark.exe`를 종료한다.

절전 모드에서 복귀한 직후에는 열려 있던 탭이 폴링을 재개할 시간을 준 뒤 판정하므로,
절전 시간이 10분을 넘어도 곧바로 종료되지 않는다.

---

## 3. 화면 사용법

### 3.1 컨트롤 버튼 (우상단)

| 버튼 | 기능 | 비고 |
|---|---|---|
| 📂 | 파일 선택 대화상자 | Windows에서만 표시. [7.1 결함](#71-windows-파일-선택-대화상자가-동작하지-않는다) 참조 |
| ☰ | 목차 열기/닫기 | 문서에 `h1`~`h4`가 하나도 없으면 표시되지 않는다 |
| 🔎 | 검색 패널 열기 | |
| 🖨 | 인쇄 대화상자 | 브라우저의 "PDF로 저장"으로 PDF 생성 |
| 🌗 ☀️ 🌙 | 테마 순환 | 현재 테마에 따라 글리프가 바뀐다 |

### 3.2 키보드 단축키

| 키 | 동작 |
|---|---|
| `Ctrl`+`F` (macOS `Cmd`+`F`) | 검색 열기. 브라우저 기본 검색 대신 문서 검색이 열린다 |
| `/` | 검색 열기 (입력 중이 아닐 때) |
| `Esc` | 검색 닫기 |
| `Enter` | 다음 검색 결과 |
| `Shift`+`Enter` | 이전 검색 결과 |

### 3.3 목차

- `h1`~`h4`를 수집해 만든다. 들여쓰기로 단계를 표시한다.
- 화면 폭 1,261 px 이상에서는 왼쪽에 상시 표시되고, ☰로 접을 수 있다.
- 1,260 px 이하에서는 숨겨져 있고 ☰로 전체 폭 오버레이로 연다.
- 스크롤하면 현재 위치의 항목이 강조된다.

### 3.4 검색

- 입력 후 120 ms 뒤에 실행된다. 입력 중에는 카운터에 `...`가 표시된다.
- 대소문자를 구분하지 않는다.
- 코드 블록·표·본문 텍스트를 모두 검색한다. 렌더링된 수식(`.katex`)과
  Mermaid 다이어그램 내부는 제외한다.
- 결과 개수는 `현재/전체` 형식으로 표시된다.

### 3.5 코드 복사

코드 블록에 마우스를 올리면 우상단에 `⧉` 버튼이 나타난다. 클릭하면 클립보드에
복사되고 버튼이 900 ms 동안 `✓`(성공) 또는 `!`(실패)로 바뀐다.
Mermaid 다이어그램에는 복사 버튼이 붙지 않는다.

### 3.6 테마

`자동` → `라이트` → `다크` 순으로 순환한다. `자동`은 운영체제 설정을 따르며,
설정이 바뀌면 페이지가 즉시 반응한다.

선택값은 설정 파일에 저장되어 다음 실행에도 유지된다. 저장 위치는
[4.1](#41-저장-위치)에 있다.

Mermaid 다이어그램은 테마 전환 시 다시 그려진다.

### 3.7 자동 새로고침

페이지가 1초마다 서버에 파일의 수정 시각·크기를 묻고, 달라지면 자동으로 새로고침한다.
편집기를 옆에 두고 저장할 때마다 결과를 확인하는 용도다.

저장하는 순간 일시적으로 파일을 읽지 못하면 오류 화면이 잠깐 보였다가 다음 폴링에서
자동 복구된다.

### 3.8 문서 안의 링크와 이미지

| 대상 | 동작 |
|---|---|
| 같은 디렉터리·하위 디렉터리의 이미지 | 표시된다 |
| 하위 디렉터리의 `.md` | 클릭하면 이어서 렌더링된다 |
| 상위 디렉터리(`../`)의 파일 | 접근할 수 없다. 문서 디렉터리로 경로가 제한된다 |
| `http://`·`https://` 이미지 | CSP가 차단한다. 외부 요청을 하지 않는다 |
| 문서 내 앵커(`#제목`) | 한글 제목도 GitHub와 같은 규칙으로 동작한다 |

### 3.9 지원하는 Markdown 문법

| 분류 | 내용 |
|---|---|
| 기본 | CommonMark 전체 |
| GFM | 표, 취소선, 작업 목록(체크박스), 자동 링크 |
| 각주 | `[^1]` 형식 |
| 코드 강조 | chroma가 지원하는 언어 (embed lexer 279종) |
| 수식 (인라인) | `$…$`(수식처럼 보일 때만), `\(…\)` |
| 수식 (디스플레이) | `$$…$$`, `\[…\]` |
| 다이어그램 | ```` ```mermaid ```` 코드 블록 |
| 원시 HTML | 그대로 렌더링. 단 스크립트는 실행되지 않는다 |

`$…$`는 통화 표기와 구분하기 위해 휴리스틱으로 판정한다. `$5`, `$3.14` 같은 순수
숫자는 수식으로 보지 않고, 연산자(`= + - * / ^ _` 등)나 수학 기호(`∑ ∫ √ ∞ ≈ ≠ ≤ ≥`)가
있거나 `x`·`a1` 같은 단일 식별자면 수식으로 본다. 확실히 수식으로 표시하려면
`\(…\)`를 쓴다.

디스플레이 수식 블록 안에서는 빈 줄, `-`로 시작하는 줄, `1.`로 시작하는 줄이 모두
Markdown 구조로 해석되지 않으므로 `\begin{aligned}` 환경을 그대로 쓸 수 있다.

표 셀 안의 수식에서 `|`는 GFM 규칙대로 `\|`로 이스케이프한다. mmark가 이를 자동으로
되돌리므로 `‖`로 잘못 렌더링되지 않는다.

---

## 4. 설정과 저장 데이터

### 4.1 저장 위치

`os.UserConfigDir()` 아래 `mmark/` 디렉터리다.

| OS | 경로 |
|---|---|
| Windows | `%AppData%\mmark\` |
| Linux | `$XDG_CONFIG_HOME/mmark/` (기본 `~/.config/mmark/`) |
| macOS | `~/Library/Application Support/mmark/` |

### 4.2 파일

| 파일 | 형식 | 내용 |
|---|---|---|
| `theme` | 평문 | `auto`, `light`, `dark` 중 하나 |
| `recent.json` | JSON | 최근에 연 Markdown 파일 최대 10개 |

`recent.json`의 구조:

```json
{
  "files": [
    "C:\\docs\\a.md",
    "C:\\docs\\b.md"
  ]
}
```

가장 최근에 연 파일이 맨 앞이다. 읽을 때 확장자·중복을 다시 검사하므로 손으로 편집해도
잘못된 항목은 무시된다. 두 파일 모두 지워도 문제없다. 다음 실행에서 기본값(`auto`,
빈 목록)으로 시작한다.

### 4.3 최근 파일 목록 보기

인자 없이 실행하면 도움말 화면이 뜨고 하단에 `## 최근 파일` 절이 표시된다. 항목을
클릭하면 그 문서로 전환된다.

Windows에서는 인자 없는 실행이 현재 동작하지 않는다.
[7.1](#71-windows-파일-선택-대화상자가-동작하지-않는다)을 참조한다.

---

## 5. 빌드

### 5.1 저장소 받기

```sh
git clone <저장소 URL>
cd mmark
```

### 5.2 Windows 배포용 빌드

```sh
GOOS=windows GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w -H windowsgui -X main.version=v1.0.0" \
  -o mmark.exe .
```

플래그의 의미:

| 플래그 | 효과 |
|---|---|
| `-trimpath` | 바이너리에서 빌드 머신의 절대 경로를 제거한다. 재현 가능한 빌드에 필요 |
| `-ldflags "-s"` | 심볼 테이블 제거 |
| `-ldflags "-w"` | DWARF 디버그 정보 제거 |
| `-ldflags "-H windowsgui"` | 콘솔 창 없이 실행되는 GUI 서브시스템 바이너리. 이것이 없으면 더블클릭 시 검은 콘솔 창이 함께 뜬다 |
| `-ldflags "-X main.version=…"` | `version` 변수에 값 주입 |

Go 1.27.0으로 측정한 결과물 크기는 19,709,952 바이트(약 18.8 MiB)다. 대부분은 embed된
5.0 MB 자산과 chroma의 lexer 정의가 차지한다.

### 5.3 Linux·macOS 빌드

```sh
go build -o mmark .
```

`-H windowsgui`는 Windows 전용 플래그이므로 붙이지 않는다. 이 플랫폼에서는 파일 선택
대화상자와 오류 메시지 박스가 동작하지 않고(no-op 스텁), 📂 버튼도 표시되지 않는다.

### 5.4 테스트

```sh
go test ./...
go test ./... -v      # 개별 테스트 이름까지 확인
go vet ./...
```

테스트는 13개다. Go 1.27.0에서 전부 통과함을 확인했다.

Windows 전용 코드도 정적 검사할 수 있다.

```sh
GOOS=windows GOARCH=amd64 go vet ./...
GOOS=windows GOARCH=amd64 go build -o /dev/null .
```

### 5.5 릴리스

`.github/workflows/release.yml`이 `v*` 형태의 태그 push에 반응한다.

```sh
git tag v1.0.0
git push origin v1.0.0
```

워크플로가 하는 일:

1. `ubuntu-latest`에서 체크아웃
2. `actions/setup-go@v5`로 `stable` 버전 Go 설치
3. `GOOS=windows GOARCH=amd64`로 `mmark.exe` 빌드.
   버전은 `-X main.version=${GITHUB_REF_NAME}`로 태그 이름이 주입된다
4. `gh release create "$GITHUB_REF_NAME" --generate-notes || true`
5. `gh release upload "$GITHUB_REF_NAME" mmark.exe --clobber`

4번의 `|| true`는 릴리스가 이미 있을 때 실패하지 않게 하고, 5번의 `--clobber`는 같은
이름의 자산을 덮어쓴다. 따라서 같은 태그로 다시 실행해도 안전하다.

필요한 권한은 워크플로에 선언된 `contents: write`뿐이다. 별도 시크릿을 설정할 필요가
없다(`github.token` 사용).

Go 버전이 `stable`로 지정되어 있으므로 릴리스 시점에 따라 산출물 크기가 조금 달라질 수
있다.

---

## 6. 설정 변경 지점

값을 바꾸려면 아래 위치를 수정하고 다시 빌드한다.

### 6.1 동작 파라미터

| 바꾸려는 것 | 위치 | 현재 값 |
|---|---|---|
| 자동 종료까지의 유휴 시간 | `main.go`의 `idleTimeout` | `10 * time.Minute` |
| 유휴 연속 판정 횟수 | `main.go`의 `idleMisses` | `5` |
| 워치독 검사 주기 | `watchdog()`의 `tick` | `2 * time.Second` |
| 파일 변경 폴링 간격 | `app.js`의 `setInterval(…, 1000)` | 1,000 ms |
| 최근 파일 보관 개수 | `rememberRecentFile()`의 `len(files) >= 10` | 10 |
| 검색 입력 디바운스 | `app.js`의 `setTimeout(…, 120)` | 120 ms |
| 복사 버튼 복귀 시간 | `app.js`의 `setTimeout(…, 900)` ×2 | 900 ms |
| 인식할 확장자 | `isMarkdown()`의 switch, `os_windows.go`의 필터 문자열 | `.md .markdown .mdown .mkd` |

자동 종료를 아예 끄려면 `main()`에서 `go watchdog()` 호출을 제거한다. 이 경우 브라우저
탭을 닫아도 프로세스가 남으므로 직접 종료해야 한다.

### 6.2 외형

| 바꾸려는 것 | 위치 |
|---|---|
| 본문 최대 폭 (980 px) | `baseCSS`의 `.markdown-body{max-width:980px}` |
| 본문 여백 (45 px) | `baseCSS`의 `.markdown-body{padding:45px}` |
| 강조 색 (`#6366f1`) | `baseCSS`의 `--mm-accent`, `--mm-ring` |
| 목차 폭 (236 px) | `baseCSS`의 `#toc{width:236px}` |
| 목차 상시 표시 기준 (1,261 px) | `baseCSS`의 `@media (min-width:1261px)`와 `@media (max-width:1260px)` 양쪽 |
| 모바일 기준 (767 px) | `baseCSS`의 `@media (max-width:767px)` |
| 검색 하이라이트 색 | `baseCSS`의 `.mmark-search-hit`, `.mmark-search-hit.is-active` |
| 코드 강조 팔레트 | `main()`의 `buildThemeCSS("github", …)` / `buildThemeCSS("github-dark", …)` 인자 |
| 페이지 배경색 | `buildThemeCSS()`의 `body{…background:#fff}` / `#0d1117` |
| favicon | `pageTmpl`의 `<link rel="icon">` data URI |
| 버튼 글리프 | `pageTmpl`의 버튼 텍스트와 `app.js`의 `icons` 객체 |

코드 강조 팔레트를 바꿀 때 주의할 점이 있다. `buildThemeCSS()`는 스타일 이름에
`"dark"`라는 부분 문자열이 있는지로 배경색을 정한다. 이름에 `dark`가 들어가지 않는
어두운 스타일을 쓰면 배경색이 어긋나므로 해당 분기도 함께 수정해야 한다.

### 6.3 문서 내용

| 바꾸려는 것 | 위치 |
|---|---|
| 인자 없이 실행했을 때 화면 | `assets/help.md` |
| 페이지 제목 형식 | `pageTmpl`의 `<title>{{.Title}} · mmark</title>` |
| 문서 언어 | `pageTmpl`의 `<html lang="ko">` |

### 6.4 보안 정책

CSP는 `renderMarkdown()`에서 문자열로 조립된다. 완화가 필요한 경우의 예를 든다.

| 하고 싶은 것 | 필요한 변경 | 부작용 |
|---|---|---|
| 원격 이미지 표시 | `img-src`에 호스트 또는 `https:` 추가 | 문서를 열었다는 사실이 외부 서버에 전달될 수 있다 |
| 문서 내 스크립트 실행 허용 | `script-src`에 `'unsafe-inline'` 추가 | 출처가 불분명한 문서가 로컬 서버를 통해 문서 디렉터리를 읽을 수 있게 된다. 권장하지 않는다 |
| 폼 전송 허용 | `form-action`을 `'self'` 등으로 | |

---

## 7. 문제 해결

### 7.1 Windows 파일 선택 대화상자가 동작하지 않는다

**증상.**

- 인자 없이 `mmark.exe`를 실행하면 아무 창도 뜨지 않고 프로세스가 곧바로 끝난다.
- 페이지의 📂 버튼을 눌러도 대화상자가 뜨지 않고 브라우저에 연결 오류가 표시된다.

**원인.** 코드 결함이다. `chooseMarkdownFile()`이 파일 필터 문자열을
`syscall.StringToUTF16`에 넘기는데, 이 함수는 인자에 NUL 바이트가 있으면 panic한다.
Win32 파일 대화상자의 필터는 정의상 NUL로 구분되므로 항상 panic 조건에 해당한다.
자세한 분석은 [CODE.md 5.2](CODE.md#52-결함-a--windows-파일-선택-대화상자-panic)에 있다.

**회피.** 파일을 인자로 지정하는 경로는 정상 동작한다.

- 드래그 앤 드롭
- `.md` 파일 연결 후 더블클릭
- `mmark.exe 문서.md`

📂 버튼을 쓰지 않고 문서를 바꾸려면 다른 파일을 드래그하거나 새로 실행한다.

**수정.** [CODE.md 5.2](CODE.md#52-결함-a--windows-파일-선택-대화상자-panic)의 수정
방향을 참조한다. `unicode/utf16`의 `Encode`로 직접 인코딩하고 종료 NUL을 붙이면 된다.

### 7.2 목차가 본문 글자를 가린다

**증상.** 화면 폭이 넓을 때 왼쪽 목차 패널이 본문 텍스트의 앞부분을 덮는다. 가려진
영역의 링크는 클릭되지 않는다.

**원인.** CSS 캐스케이드 결함이다. `baseCSS`의 `margin:0 auto`(가운데 정렬)가
github-markdown CSS의 `margin: 0`에 의해 무효화되어 본문이 왼쪽 끝에 붙는다.
목차는 왼쪽 16–252 px에 고정되고 본문 텍스트는 45 px에서 시작하므로 항상 겹친다.
자세한 측정값은 [CODE.md 5.3](CODE.md#53-결함-b--본문-정렬과-목차-겹침)에 있다.

**회피.** ☰ 버튼으로 목차를 접는다. 또는 브라우저 창 폭을 1,260 px 이하로 줄이면
목차가 자동으로 숨겨진다(☰로 오버레이 표시).

**수정.** 두 가지를 함께 처리한다.

1. `baseCSS`의 정렬 규칙 특이도를 올린다.

   ```css
   body .markdown-body{margin:0 auto}
   ```

2. 목차 상시 표시 breakpoint를 1,394 px 이상으로 올리거나, 목차가 열려 있을 때 본문에
   왼쪽 여백을 준다. 1,261–1,393 px 구간은 정렬을 고쳐도 겹치기 때문이다.

### 7.3 브라우저가 열리지 않는다

프로세스는 실행되었는데 브라우저 창이 뜨지 않는 경우다. mmark는 브라우저 실행 결과를
확인하지 않으므로 서버는 정상 동작 중일 수 있다.

포트를 찾아 직접 접속한다.

- Windows: `netstat -ano | findstr mmark` 또는 리소스 모니터의 네트워크 탭
- Linux: `ss -lptn | grep mmark`

`http://127.0.0.1:<포트>/`를 주소창에 입력한다.

### 7.4 SmartScreen 경고

코드 서명이 없는 실행 파일에 대한 표준 안내다. *추가 정보 → 실행*으로 진행한다.

### 7.5 한글이 깨진다

지원 인코딩은 UTF-8, BOM 있는 UTF-16 LE/BE, CP949/EUC-KR이다. BOM 없는 UTF-16은
지원하지 않는다. 편집기에서 UTF-8로 다시 저장하면 해결된다.

### 7.6 수식이 렌더링되지 않는다

| 증상 | 확인할 것 |
|---|---|
| `$…$`가 그대로 보인다 | 휴리스틱이 수식으로 판정하지 않은 경우다. `\(…\)`로 바꾼다 |
| 수식이 붉은색으로 표시된다 | KaTeX가 지원하지 않는 명령이 포함된 경우다. KaTeX 지원 목록을 확인한다 |
| 수식 자리에 원문이 보이고 조판이 안 된다 | KaTeX 스크립트가 로드되지 않은 경우다. 브라우저 콘솔에서 `/__mmark/assets/katex.min.js` 요청 상태를 확인한다 |
| 글꼴 모양이 이상하다 | KaTeX 폰트 로드 실패다. `/__mmark/assets/katex/fonts/…` 요청이 404인지 확인한다. KaTeX를 교체했다면 [8.1](#81-katex-교체) 참조 |

### 7.7 Mermaid 다이어그램이 코드 블록으로만 보인다

- 코드 펜스의 언어가 정확히 `mermaid`인지 확인한다.
- 다이어그램 문법 오류는 그 자리에 오류 메시지 상자로 표시된다.
- 브라우저 콘솔에서 `/__mmark/assets/mermaid.min.js` 로드를 확인한다.

### 7.8 자동 새로고침이 동작하지 않는다

| 원인 | 확인 |
|---|---|
| 편집기가 파일을 교체(replace) 방식으로 저장 | 수정 시각·크기가 바뀌면 감지된다. 둘 다 그대로면 감지되지 않는다 |
| 탭이 백그라운드에 있음 | 브라우저가 타이머를 억제한다. 탭을 활성화하면 즉시 반영된다 |
| 서버가 이미 종료됨 | 페이지를 새로 고쳤을 때 연결 오류가 나면 프로세스가 종료된 것이다. 다시 실행한다 |

### 7.9 프로세스가 남아 있다

브라우저 탭을 모두 닫았는데도 `mmark.exe`가 보이는 경우다. 자동 종료는 마지막 폴링
후 약 10분 10초에 일어나므로, 그 전이라면 정상이다. 즉시 끝내려면 작업 관리자에서
종료한다.

### 7.10 이미지가 표시되지 않는다

| 경로 형태 | 결과 |
|---|---|
| `![](img/a.png)` (하위 디렉터리) | 표시됨 |
| `![](a.png)` (같은 디렉터리) | 표시됨 |
| `![](../img/a.png)` (상위 디렉터리) | 표시되지 않음. 경로 제한 때문 |
| `![](https://…)` | CSP가 차단 |
| `![](data:image/png;base64,…)` | 표시됨 |

상위 디렉터리의 이미지를 써야 한다면 문서를 상위 디렉터리로 옮기거나, 이미지를 문서
디렉터리 아래로 복사한다.

### 7.11 문서 링크를 눌러도 이동하지 않는다

`../`로 상위 디렉터리를 가리키는 링크는 동작하지 않는다. 브라우저가 `../`를 URL
단계에서 정규화하고, 서버는 문서 디렉터리 안에서만 경로를 해석하기 때문이다.

또한 목차가 가리는 영역의 링크는 클릭되지 않는다([7.2](#72-목차가-본문-글자를-가린다)).

### 7.12 포트가 매번 바뀐다

의도된 동작이다. `127.0.0.1:0`으로 요청해 OS가 빈 포트를 할당한다. 포트 충돌이 나지
않는 대신 브라우저 캐시가 실행 간에 재사용되지 않아 자산이 매번 다시 전송된다
(로컬 전송이라 체감 영향은 작다).

---

## 8. 반입 자산 교체

`assets/` 아래의 KaTeX·Mermaid·github-markdown-css는 upstream에서 가져온 파일이다.
교체 시 주의할 점이 있다.

### 8.1 KaTeX 교체

현재 버전은 0.17.0이다. 다음 파일을 모두 교체한다.

```
assets/katex.min.js
assets/katex.min.css
assets/katex-auto-render.min.js
assets/katex/fonts/          (60개: ttf/woff/woff2 각 20종)
```

**반드시 해야 할 후처리.** 원본 `katex.min.css`는 폰트를 `url(fonts/…)`처럼 상대
경로로 참조한다. mmark는 CSS를 `/__mmark/assets/katex.min.css`에서 서빙하므로 상대
경로가 `/__mmark/assets/fonts/…`로 해석되는데, 실제 폰트는
`/__mmark/assets/katex/fonts/…`에 있다. 현재 저장소의 CSS는 모든 `url()`이 절대 경로
`/__mmark/assets/katex/fonts/…`로 재작성되어 있다.

교체 후 같은 재작성을 다시 적용하지 않으면 폰트 요청이 전부 404가 되고 수식이 시스템
글꼴로 대체되어 조판이 무너진다.

확인 방법:

```sh
grep -o "url([^)]*)" assets/katex.min.css | sed 's#.*url(##; s#).*##' | sed 's#/[^/]*$##' | sort -u
```

출력이 `/__mmark/assets/katex/fonts` 한 줄이면 정상이다.

폰트 파일 개수도 확인한다.

```sh
ls assets/katex/fonts | wc -l    # 60
```

### 8.2 Mermaid 교체

현재 버전은 11.16.0이다. `assets/mermaid.min.js` 하나를 교체한다.
`window.mermaid.initialize()`와 `window.mermaid.render(id, source)`가 Promise를
반환하는 API 형태를 유지해야 한다. `app.js`의 `renderMermaidDiagrams()`가 이를
전제로 한다.

Mermaid가 ESM만 제공하는 형태로 바뀌면 `<script src>` 로드 방식이 동작하지 않으므로
UMD/IIFE 번들을 사용해야 한다.

### 8.3 github-markdown-css 교체

`assets/github-markdown-light.css`와 `assets/github-markdown-dark.css` 두 개다.
파일 첫머리의 `/*light */`, `/*dark */` 주석과 `.markdown-body{color-scheme: …}`으로
구분한다.

교체 시 [7.2](#72-목차가-본문-글자를-가린다)의 `margin: 0` 문제가 여전히 존재하는지
확인한다.

```sh
grep -n -A20 "^\.markdown-body {" assets/github-markdown-light.css | grep margin
```

### 8.4 교체 후 확인

```sh
go build ./... && go test ./...
go build -o mmark . && ./mmark 샘플문서.md
```

샘플 문서에는 인라인 수식, 디스플레이 수식, 표 안 수식, 코드 블록, Mermaid 블록,
각주, 한글 제목을 모두 넣어 한 번에 확인하는 것이 좋다.

### 8.5 라이선스 고지 갱신

`THIRD_PARTY_NOTICES.md`의 버전 표를 함께 갱신한다. 현재 표에 기재된 값은
Mermaid 11.16.0, KaTeX 0.17.0이다.

---

## 9. 개발 시 참고

### 9.1 자주 쓰는 명령

```sh
go build -o mmark . && ./mmark 문서.md     # 빌드 후 즉시 확인
go test ./... -run TestMath -v             # 수식 관련 테스트만
go vet ./...                                # 정적 검사
GOOS=windows GOARCH=amd64 go vet ./...      # Windows 코드 정적 검사
```

### 9.2 새 기능 추가 위치

| 추가하려는 것 | 손댈 곳 |
|---|---|
| UI 버튼 | `pageTmpl`의 `#controls` → `baseCSS` 스타일 → `app.js`의 `setupXxx()` 작성 후 마지막 초기화 목록에 등록 |
| 서버 endpoint | `main()`의 `mux.HandleFunc` + `server` 메서드. `/__mmark/` 접두사 유지 |
| Markdown 확장 | `md` 선언의 `goldmark.WithExtensions(...)` |
| 텍스트 인코딩 | `decodeText()`의 판별 순서 |
| 플랫폼 기능 | `os_windows.go`와 `os_other.go` **양쪽**에 같은 시그니처로 |

`app.js`의 초기화 순서에는 의존 관계가 있다. `setupMath()`는 `prepareMermaid()`보다
앞이어야 하고(Mermaid 소스가 아직 `<pre><code>` 안에 있어야 수식 스캔에서 제외됨),
`setupCodeCopy()`는 `prepareMermaid()`보다 뒤여야 한다(다이어그램에 복사 버튼이 붙지
않게). 순서를 바꿀 때 확인이 필요하다.

### 9.3 디버깅

- 브라우저 개발자 도구의 콘솔에서 CSP 위반과 자산 로드 실패를 확인할 수 있다.
- `window.__MMARK__`에 현재 stamp·경로·테마가 들어 있다.
- `document.documentElement.dataset.mmarkTheme`에 실제 적용 중인 테마(`light`/`dark`)가
  들어 있다.
- `/__mmark/status?p=/`를 직접 호출하면 현재 stamp를 볼 수 있다.
- Windows 배포 바이너리는 콘솔 없이 실행되므로 로그를 보려면 `-H windowsgui` 없이
  빌드한다.
