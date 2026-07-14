package main

import (
	"bytes"
	"fmt"
	stdhtml "html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	extast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var kindMath = ast.NewNodeKind("Math")

type mathNode struct {
	ast.BaseInline
	value   []byte
	display bool
}

func newMathNode(value []byte, display bool) *mathNode {
	copied := make([]byte, len(value))
	copy(copied, value)
	return &mathNode{value: copied, display: display}
}

func (n *mathNode) Kind() ast.NodeKind {
	return kindMath
}

func (n *mathNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, map[string]string{
		"Value":   string(n.value),
		"Display": fmt.Sprintf("%v", n.display),
	}, nil)
}

var kindMathBlock = ast.NewNodeKind("MathBlock")

// mathBlockNode holds a fenced $$ ... $$ display-math block. Treating it as a
// raw block (like a fenced code block) keeps blank lines, list-marker lines
// (`- x &= 1`), and `1.`-style lines inside the math from being re-parsed as
// Markdown structure, which is what used to tear display equations apart.
type mathBlockNode struct {
	ast.BaseBlock
}

func (n *mathBlockNode) Kind() ast.NodeKind { return kindMathBlock }
func (n *mathBlockNode) IsRaw() bool        { return true }

func (n *mathBlockNode) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

type mathExtension struct{}

func (e mathExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(
			util.Prioritized(mathBlockParser{}, 850),
		),
		parser.WithInlineParsers(
			util.Prioritized(mathParser{}, 150),
		),
	)
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(mathHTMLRenderer{}, 500),
	))
}

// mathBlockParser opens on a line starting with `$$` and consumes lines with
// code-fence semantics until a line ending in `$$`.
type mathBlockParser struct{}

func (p mathBlockParser) Trigger() []byte { return []byte{'$'} }

func (p mathBlockParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 || pos+1 >= len(line) || line[pos] != '$' || line[pos+1] != '$' {
		return nil, parser.NoChildren
	}
	rest := line[pos+2:]
	// `$$x$$ ...` on one line stays with the inline parser.
	if bytes.Contains(rest, []byte("$$")) {
		return nil, parser.NoChildren
	}
	node := &mathBlockNode{}
	if len(bytes.TrimSpace(rest)) > 0 {
		node.Lines().Append(text.NewSegment(segment.Start+pos+2, segment.Stop))
	}
	reader.Advance(segment.Len() - 1)
	return node, parser.NoChildren
}

func (p mathBlockParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, segment := reader.PeekLine()
	trimmed := util.TrimRightSpace(line)
	if l := len(trimmed); l >= 2 && trimmed[l-1] == '$' && trimmed[l-2] == '$' &&
		!isEscapedByte(trimmed, l-2) {
		if len(bytes.TrimSpace(trimmed[:l-2])) > 0 {
			node.Lines().Append(text.NewSegment(segment.Start, segment.Start+l-2))
		}
		reader.Advance(segment.Len() - 1)
		return parser.Close
	}
	node.Lines().Append(segment)
	return parser.Continue | parser.NoChildren
}

func (p mathBlockParser) Close(ast.Node, text.Reader, parser.Context) {}

func (p mathBlockParser) CanInterruptParagraph() bool { return true }

func (p mathBlockParser) CanAcceptIndentedLine() bool { return false }

type mathParser struct{}

func (p mathParser) Trigger() []byte {
	return []byte{'$', '\\'}
}

func (p mathParser) Parse(parent ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) == 0 {
		return nil
	}
	src := block.Source()
	start := segment.Start
	var node ast.Node
	switch line[0] {
	case '$':
		node = parseDollarMath(block, src, start)
	case '\\':
		if len(line) < 2 {
			return nil
		}
		switch line[1] {
		case '(':
			node = parseBackslashMath(block, src, start, ')', false)
		case '[':
			node = parseBackslashMath(block, src, start, ']', true)
		}
	}
	return normalizeTableCellMath(parent, node)
}

// normalizeTableCellMath rewrites `\|` to `|` for math inside a table cell.
// GFM requires pipes in cells to be escaped as `\|` to avoid splitting the
// row, but the escape reaches the math source verbatim and KaTeX would render
// it as ‖ (\Vert). GitHub unescapes it the same way.
func normalizeTableCellMath(parent ast.Node, node ast.Node) ast.Node {
	if node == nil {
		return nil
	}
	for n := parent; n != nil; n = n.Parent() {
		if n.Kind() == extast.KindTableCell {
			m := node.(*mathNode)
			m.value = bytes.ReplaceAll(m.value, []byte(`\|`), []byte(`|`))
			break
		}
	}
	return node
}

func parseDollarMath(block text.Reader, src []byte, start int) ast.Node {
	if start+1 < len(src) && src[start+1] == '$' {
		end := findDoubleDollarCloseInBlock(block, src)
		if end < 0 || len(bytes.TrimSpace(src[start+2:end])) == 0 {
			return nil
		}
		block.Advance(end + 2 - start)
		return newMathNode(src[start+2:end], true)
	}
	if start > 0 && src[start-1] == '$' {
		return nil
	}
	end := findSingleDollarClose(src, start+1)
	if end < 0 || !looksLikeInlineMathBytes(src[start+1:end]) {
		return nil
	}
	block.Advance(end + 1 - start)
	return newMathNode(src[start+1:end], false)
}

func parseBackslashMath(block text.Reader, src []byte, start int, closer byte, display bool) ast.Node {
	end := findBackslashClose(src, start+2, closer)
	if display {
		end = findBackslashCloseInBlock(block, src, closer)
	}
	if end < 0 || len(bytes.TrimSpace(src[start+2:end])) == 0 {
		return nil
	}
	block.Advance(end + 2 - start)
	return newMathNode(src[start+2:end], display)
}

func findSingleDollarClose(src []byte, start int) int {
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '\n', '\r':
			return -1
		case '$':
			if !isEscapedByte(src, i) &&
				(i == 0 || src[i-1] != '$') &&
				(i+1 >= len(src) || src[i+1] != '$') {
				return i
			}
		}
	}
	return -1
}

func findDoubleDollarCloseInBlock(block text.Reader, src []byte) int {
	savedLine, savedSegment := block.Position()
	defer block.SetPosition(savedLine, savedSegment)

	block.Advance(2)
	for {
		line, segment := block.PeekLine()
		if line == nil {
			return -1
		}
		for i := 0; i+1 < len(line); i++ {
			index := segment.Start + i
			if src[index] == '$' && src[index+1] == '$' && !isEscapedByte(src, index) {
				return index
			}
		}
		block.AdvanceLine()
	}
}

func findBackslashClose(src []byte, start int, closer byte) int {
	for i := start; i+1 < len(src); i++ {
		if src[i] == '\n' || src[i] == '\r' {
			return -1
		}
		if src[i] == '\\' && src[i+1] == closer && !isEscapedByte(src, i) {
			return i
		}
	}
	return -1
}

func findBackslashCloseInBlock(block text.Reader, src []byte, closer byte) int {
	savedLine, savedSegment := block.Position()
	defer block.SetPosition(savedLine, savedSegment)

	block.Advance(2)
	for {
		line, segment := block.PeekLine()
		if line == nil {
			return -1
		}
		for i := 0; i+1 < len(line); i++ {
			index := segment.Start + i
			if src[index] == '\\' && src[index+1] == closer && !isEscapedByte(src, index) {
				return index
			}
		}
		block.AdvanceLine()
	}
}

func isEscapedByte(src []byte, index int) bool {
	backslashes := 0
	for i := index - 1; i >= 0 && src[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func looksLikeInlineMathBytes(source []byte) bool {
	text := strings.TrimSpace(string(source))
	if text == "" || isPlainNumber(text) {
		return false
	}
	for _, r := range text {
		switch r {
		case '\\', '_', '^', '{', '}', '=', '+', '-', '*', '/', '<', '>',
			'\u2211', '\u222b', '\u221a', '\u221e', '\u2248', '\u2260', '\u2264', '\u2265':
			return true
		}
	}
	if isSingleASCIIIdentifier(text) {
		return true
	}
	return hasInlineMathOperator(text)
}

func isPlainNumber(text string) bool {
	seenDigit := false
	seenSeparator := false
	for _, r := range text {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
		case (r == '.' || r == ',') && !seenSeparator:
			seenSeparator = true
		default:
			return false
		}
	}
	return seenDigit
}

func isSingleASCIIIdentifier(text string) bool {
	if len(text) == 0 || text[0] < 'A' ||
		(text[0] > 'Z' && text[0] < 'a') || text[0] > 'z' {
		return false
	}
	for i := 1; i < len(text); i++ {
		if text[i] < '0' || text[i] > '9' {
			return false
		}
	}
	return true
}

func hasInlineMathOperator(text string) bool {
	runes := []rune(text)
	for i, r := range runes {
		if !strings.ContainsRune("=+-*/^_", r) {
			continue
		}
		if hasASCIIAlphaBefore(runes, i) || hasASCIIAlphaNumAfter(runes, i) {
			return true
		}
	}
	return false
}

func hasASCIIAlphaBefore(runes []rune, index int) bool {
	for i := index - 1; i >= 0; i-- {
		if runes[i] == ' ' || runes[i] == '\t' {
			continue
		}
		return (runes[i] >= 'A' && runes[i] <= 'Z') || (runes[i] >= 'a' && runes[i] <= 'z')
	}
	return false
}

func hasASCIIAlphaNumAfter(runes []rune, index int) bool {
	for i := index + 1; i < len(runes); i++ {
		if runes[i] == ' ' || runes[i] == '\t' {
			continue
		}
		return (runes[i] >= 'A' && runes[i] <= 'Z') ||
			(runes[i] >= 'a' && runes[i] <= 'z') ||
			(runes[i] >= '0' && runes[i] <= '9')
	}
	return false
}

type mathHTMLRenderer struct{}

func (r mathHTMLRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMath, r.renderMath)
	reg.Register(kindMathBlock, r.renderMathBlock)
}

func (r mathHTMLRenderer) renderMathBlock(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	var buf bytes.Buffer
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		s := lines.At(i)
		buf.Write(s.Value(source))
	}
	_, _ = w.WriteString(`<span class="mmark-math mmark-math-display" data-display="true">`)
	_, _ = w.WriteString(stdhtml.EscapeString(buf.String()))
	_, _ = w.WriteString(`</span>` + "\n")
	return ast.WalkSkipChildren, nil
}

func (r mathHTMLRenderer) renderMath(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	node := n.(*mathNode)
	display := "false"
	class := "mmark-math mmark-math-inline"
	if node.display {
		display = "true"
		class = "mmark-math mmark-math-display"
	}
	_, _ = w.WriteString(`<span class="` + class + `" data-display="` + display + `">`)
	_, _ = w.WriteString(stdhtml.EscapeString(string(node.value)))
	_, _ = w.WriteString(`</span>`)
	return ast.WalkSkipChildren, nil
}
