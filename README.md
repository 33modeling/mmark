<img src="assets/icon.svg" width="96" align="right" alt="mmark 아이콘">

# mmark

윈도우에서 설치 없이 쓰는 초간단 마크다운 뷰어. `mmark.exe` 파일 하나가 전부입니다.

`.md` 파일을 열면 GitHub 스타일로 렌더링된 화면이 기본 브라우저에 뜹니다.

## 다운로드

[Releases](https://github.com/33modeling/mmark/releases/latest) 페이지에서 `mmark.exe`를 내려받으세요. 설치 과정은 없습니다.

> 처음 실행할 때 Windows SmartScreen 경고가 뜰 수 있습니다. *추가 정보 → 실행*을 누르면 됩니다(서명되지 않은 실행 파일이라 표시되는 안내입니다).

## 사용 방법

| 방법 | 하는 법 |
|---|---|
| 드래그 & 드롭 | 탐색기에서 `.md` 파일을 `mmark.exe` 위로 끌어다 놓기 |
| 파일 연결(권장) | `.md` 우클릭 → 연결 프로그램 → 다른 앱 선택 → `mmark.exe` → **항상 이 앱 사용** 체크 |
| 명령줄 | `mmark.exe 문서.md` |

파일 연결을 해두면 이후에는 `.md` 더블클릭만으로 열립니다.

## 기능

- GitHub 스타일 마크다운 렌더링 — 표, 체크박스, 취소선, 각주, 자동 링크
- 코드 블록 문법 하이라이트 (chroma)
- LaTeX 수식 렌더링 (KaTeX, 오프라인 내장) — 인라인 `$...$`·`\(...\)`, 디스플레이 `$$...$$`·`\[...\]`, 수식 블록 안 빈 줄·`aligned` 환경·표 안 수식 지원
- 코드 블록 복사 버튼
- 문서 목차 사이드바와 본문 검색
- 라이트 / 다크 테마 — 기본은 시스템 설정을 따르고, 페이지 우상단 버튼으로 자동 → 라이트 → 다크 전환. 선택은 설정 파일(`%AppData%\mmark\theme`)에 저장되어 다음 실행에도 유지
- 파일 저장 시 자동 새로고침 — 편집기와 나란히 두고 쓰기 좋음
- 인쇄/PDF용 스타일
- 파일을 지정하지 않고 실행하면 Windows 파일 선택창 표시, 최근 파일 목록 제공
- 문서 기준 상대 경로 이미지·링크 지원, `.md` 링크는 클릭 시 이어서 렌더링
- 한글 제목 앵커 지원 — `[목차](#사용-방법)` 같은 문서 내 링크가 GitHub와 동일하게 동작
- Mermaid 다이어그램 렌더링 — <code>```mermaid</code> 코드 블록 지원
- KaTeX 수식 렌더링 — `$$...$$`, `\(...\)`, `\[...\]`, 수식처럼 보이는 `$...$`
- UTF-8 / UTF-16(BOM) / CP949(EUC-KR) 인코딩 자동 감지
- 완전 오프라인 동작 — 외부 네트워크 접근 없음
- 문서에 포함된 `<script>`는 CSP로 실행 차단 — 출처가 불분명한 .md를 열어도 안전

## 동작 방식

`mmark.exe`는 127.0.0.1 전용 로컬 서버를 임의 포트에 띄우고 기본 브라우저를 엽니다.
페이지가 1초마다 파일 변경을 확인해 바뀌면 새로고침하고, 브라우저 탭이 닫혀 폴링이
끊기면 프로세스가 약 10분 후 스스로 종료됩니다(절전 모드에서 깨어난 직후에는 열린
탭이 폴링을 재개할 시간을 준 뒤 판단합니다).

## 직접 빌드

Go 1.25+ 필요.

```sh
# 윈도우용
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H windowsgui" -o mmark.exe .

# 리눅스/맥용
go build -o mmark .
```

`v*` 태그를 푸시하면 GitHub Actions가 윈도우 바이너리를 빌드해 릴리즈에 올립니다.

## 라이선스

[MIT](LICENSE). 내장된 [github-markdown-css](https://github.com/sindresorhus/github-markdown-css), [Mermaid](https://github.com/mermaid-js/mermaid), [KaTeX](https://github.com/KaTeX/KaTeX)도 MIT 라이선스입니다.
