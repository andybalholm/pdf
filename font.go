package pdf

import (
	"fmt"
	"io/ioutil"
	"log"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/encoding/charmap"
)

type Font struct {
	sfnt *sfnt.Font

	encode    map[rune]byte
	toUnicode [256]rune
}

// LoadFont loads a TrueType or OpenType font from the file specified. If it
// has already been loaded into this Document, the previous instance is
// returned instead of loading it again.
func (d *Document) LoadFont(filename string) (*Font, error) {
	if f, ok := d.fontCache[filename]; ok {
		return f, nil
	}

	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	f := &Font{
		encode: make(map[rune]byte),
	}

	f.sfnt, err = sfnt.Parse(b)
	if err != nil {
		return nil, err
	}

	if d.fontCache == nil {
		d.fontCache = make(map[string]*Font)
	}
	d.fontCache[filename] = f
	return f, nil
}

func (f *Font) writeTo(e *encoder) {
	var firstChar, lastChar int
	for i := 0; i < 256; i++ {
		if f.toUnicode[i] != 0 {
			firstChar = i
			break
		}
	}
	for i := 255; i >= firstChar; i-- {
		if f.toUnicode[i] != 0 {
			lastChar = i
			break
		}
	}
	widths := make([]int, lastChar-firstChar+1)
	cp := &charProcs{procs: make(map[string]*type3Glyph)}
	var differences []string
	prevDifference := -1

	for i := firstChar; i <= lastChar; i++ {
		var buffer sfnt.Buffer
		r := f.toUnicode[i]
		if r == 0 {
			continue
		}
		name := glyphName(r)
		if r != charmap.Windows1252.DecodeByte(byte(i)) {
			if prevDifference != i-1 {
				differences = append(differences, fmt.Sprint(i))
			}
			differences = append(differences, "/"+name)
			prevDifference = i
		}

		g, err := f.sfnt.GlyphIndex(&buffer, r)
		if err != nil {
			continue
		}
		w, err := f.sfnt.GlyphAdvance(&buffer, g, fixed.I(1000), font.HintingNone)
		if err != nil {
			continue
		}
		widths[i-firstChar] = w.Round()
		outlines, err := f.sfnt.LoadGlyph(&buffer, g, fixed.I(1000), nil)
		if err != nil {
			log.Println(err)
			continue
		}
		cp.procs[name] = &type3Glyph{
			outline: outlines,
			width:   w.Round(),
		}
	}

	var buffer sfnt.Buffer

	var bounds []int
	rawBounds, err := f.sfnt.Bounds(&buffer, fixed.I(1000), font.HintingNone)
	if err == nil {
		bounds = []int{rawBounds.Min.X.Floor(), -rawBounds.Max.Y.Ceil(), rawBounds.Max.X.Ceil(), -rawBounds.Min.Y.Floor()}
	}

	fmt.Fprintf(e, "<< /Type /Font /Subtype /Type3\n")
	if len(differences) == 0 {
		fmt.Fprintln(e, "/Encoding /WinAnsiEncoding")
	} else {
		fmt.Fprintf(e, "/Encoding << /BaseEncoding /WinAnsiEncoding /Differences %v >>\n", differences)
	}
	fmt.Fprintf(e, "/FontBBox %d\n", bounds)
	fmt.Fprintf(e, "/FontMatrix [0.001 0 0 0.001 0 0]\n")
	fmt.Fprintf(e, "/FirstChar %d /LastChar %d\n", firstChar, lastChar)
	fmt.Fprintf(e, "/Widths %d\n", widths)
	fmt.Fprintf(e, "/CharProcs %d 0 R\n", e.getRef(cp))
	fmt.Fprint(e, ">>")
}

type type3Glyph struct {
	outline []sfnt.Segment
	width   int
}

func (g *type3Glyph) writeTo(e *encoder) {
	var min, max fixed.Point26_6
	if len(g.outline) > 0 {
		min = g.outline[0].Args[0]
		max = min
	}
	for _, segment := range g.outline {
		pointCount := int(segment.Op)
		if pointCount == 0 {
			pointCount = 1
		}
		for i := 0; i < pointCount; i++ {
			p := segment.Args[i]
			if p.X < min.X {
				min.X = p.X
			}
			if p.Y < min.Y {
				min.Y = p.Y
			}
			if p.X > max.X {
				max.X = p.X
			}
			if p.Y > max.Y {
				max.Y = p.Y
			}
		}
	}
	min.Y, max.Y = -max.Y, -min.Y

	s := new(stream)
	fmt.Fprintf(s, "%d 0 %d %d %d %d d1\n", g.width, min.X.Floor(), min.Y.Floor(), max.X.Ceil(), max.Y.Ceil())
	var current fixed.Point26_6
	for _, segment := range g.outline {
		switch segment.Op {
		case sfnt.SegmentOpMoveTo:
			fmt.Fprintf(s, "%d %d m\n", segment.Args[0].X.Round(), -segment.Args[0].Y.Round())
			current = segment.Args[0]
		case sfnt.SegmentOpLineTo:
			fmt.Fprintf(s, "%d %d l\n", segment.Args[0].X.Round(), -segment.Args[0].Y.Round())
			current = segment.Args[0]
		case sfnt.SegmentOpQuadTo:
			args := segment.Args
			control1 := fixed.Point26_6{
				X: current.X + (args[0].X-current.X)*2/3,
				Y: current.Y + (args[0].Y-current.Y)*2/3,
			}
			control2 := fixed.Point26_6{
				X: args[1].X + (args[0].X-args[1].X)*2/3,
				Y: args[1].Y + (args[0].Y-args[1].Y)*2/3,
			}
			fmt.Fprintf(s, "%d %d %d %d %d %d c\n", control1.X.Round(), -control1.Y.Round(), control2.X.Round(), -control2.Y.Round(), args[1].X.Round(), -args[1].Y.Round())
			current = args[1]
		case sfnt.SegmentOpCubeTo:
			for _, p := range segment.Args {
				fmt.Fprintf(s, "%d %d ", p.X.Round(), -p.Y.Round())
			}
			fmt.Fprintln(s, "c")
			current = segment.Args[2]
		}
	}
	if len(g.outline) > 0 {
		fmt.Fprint(s, "f")
	}

	s.writeTo(e)
}

type charProcs struct {
	procs map[string]*type3Glyph
}

func (c *charProcs) writeTo(e *encoder) {
	names := make([]string, 0, len(c.procs))
	for name := range c.procs {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Fprintln(e, "<<")
	for _, name := range names {
		fmt.Fprintf(e, "/%s %d 0 R\n", name, e.getRef(c.procs[name]))
	}
	fmt.Fprint(e, ">>")
}

func (p *Page) SetFont(f *Font, size float64) {
	fontID, ok := p.fonts[f]
	if !ok {
		if p.fonts == nil {
			p.fonts = make(map[*Font]int)
		}
		fontID = len(p.fonts)
		p.fonts[f] = fontID
	}

	fmt.Fprintf(p.contents, "/F%d %g Tf ", fontID, size)
	p.currentFont = f
	p.currentSize = size
}

// SetLeading sets the line spacing to be used by Multiline.
func (p *Page) SetLeading(leading float64) {
	fmt.Fprintf(p.contents, "%g TL ", leading)
}

func (f *Font) encodeRune(r rune) (b byte, ok bool) {
	if b, ok := f.encode[r]; ok {
		return b, true
	}
	if b, ok := charmap.Windows1252.EncodeRune(r); ok && f.toUnicode[b] == 0 {
		f.encode[r] = b
		f.toUnicode[b] = r
		return b, true
	}

	for i := 31; i > 0 && b == 0; i-- {
		if f.toUnicode[i] == 0 {
			b = byte(i)
		}
	}
	for i := 127; i < 256 && b == 0; i++ {
		if f.toUnicode[i] == 0 {
			b = byte(i)
		}
	}
	for i := 126; i > 32 && b == 1; i-- {
		if f.toUnicode[i] == 0 {
			b = byte(i)
		}
	}

	if b != 0 {
		f.encode[r] = b
		f.toUnicode[b] = r
		return b, true
	}

	return 0, false
}

func (f *Font) encodeString(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		c, ok := f.encodeRune(r)
		if ok {
			b = append(b, c)
		}
	}
	return string(b)
}

func (f *Font) runeWidth(r rune) int {
	var buffer sfnt.Buffer
	g, err := f.sfnt.GlyphIndex(&buffer, r)
	if err != nil {
		return 0
	}
	advance, err := f.sfnt.GlyphAdvance(&buffer, g, fixed.I(1000), font.HintingNone)
	if err != nil {
		return 0
	}
	return advance.Round()
}

// encodeAndKern converts s from UTF-8 to a format suitable for displaying with
// the TJ operator, with kerning applied. It also returns the string's width,
// in units of 1/1000 of an em. If maxWidth is nonzero and s is too long to fit,
// the result will be truncated.
func (f *Font) encodeAndKern(s string, maxWidth int) (tj []string, width int) {
	s = f.encodeString(s)
	var buffer sfnt.Buffer

	var prevGlyph sfnt.GlyphIndex
	chunkStart := 0
	for i := 0; i < len(s); i++ {
		g, err := f.sfnt.GlyphIndex(&buffer, f.toUnicode[s[i]])
		if err != nil {
			continue
		}
		advance, err := f.sfnt.GlyphAdvance(&buffer, g, fixed.I(1000), font.HintingNone)
		if err == nil {
			oldWidth := width
			width += advance.Round()
			if maxWidth != 0 && width > maxWidth {
				tj = append(tj, quoteString(s[chunkStart:i]))
				return tj, oldWidth
			}
		}
		if i != 0 {
			kern, err := f.sfnt.Kern(&buffer, prevGlyph, g, fixed.I(1000), font.HintingNone)
			if err == nil && kern != 0 {
				width += kern.Round()
				tj = append(tj,
					quoteString(s[chunkStart:i]),
					strconv.Itoa(-kern.Round()),
				)
				chunkStart = i
			}
		}
		prevGlyph = g
	}
	tj = append(tj, quoteString(s[chunkStart:]))

	return tj, width
}

var stringEscaper = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`, "(", `\(`, ")", `\)`, `\`, `\\`)

func quoteString(s string) string {
	return "(" + stringEscaper.Replace(s) + ")"
}

// beginText begins a text object. All text output and positioning must happen
// between calls to BeginText and EndText.
func (p *Page) beginText() {
	fmt.Fprint(p.contents, "BT ")
}

func (p *Page) endText() {
	fmt.Fprint(p.contents, "ET ")
}

// show puts s on the page.
func (p *Page) show(s string) {
	tj, _ := p.currentFont.encodeAndKern(s, 0)
	fmt.Fprintf(p.contents, "%v TJ ", tj)
}

// Left puts s on the page, left-aligned at (x, y).
func (p *Page) Left(x, y float64, s string) {
	p.beginText()
	fmt.Fprintf(p.contents, "%g %g Td ", x, y)
	p.show(s)
	p.endText()
}

// Right puts s on the page, right-aligned at (x, y).
func (p *Page) Right(x, y float64, s string) {
	p.beginText()
	tj, w := p.currentFont.encodeAndKern(s, 0)
	fmt.Fprintf(p.contents, "%g %g Td %v TJ ", x-float64(w)*0.001*p.currentSize, y, tj)
	p.endText()
}

// Center puts s on the page, centered at (x, y).
func (p *Page) Center(x, y float64, s string) {
	p.beginText()
	tj, w := p.currentFont.encodeAndKern(s, 0)
	fmt.Fprintf(p.contents, "%g %g Td %v TJ ", x-float64(w)*0.001*p.currentSize*0.5, y, tj)
	p.endText()
}

// Multiline puts multiple lines of text on the page (splitting s at '\n'). It
// uses the line spacing set with Leading.
func (p *Page) Multiline(x, y float64, s string) {
	p.beginText()
	fmt.Fprintf(p.contents, "%g %g Td ", x, y)
	for i, line := range strings.Split(s, "\n") {
		if i > 0 {
			fmt.Fprint(p.contents, "T* ")
		}
		p.show(line)
	}
	p.endText()
}

// Truncate displays s at (x, y), truncating it with an ellipsis if it is
// longer than width.
func (p *Page) Truncate(x, y, width float64, s string) {
	scaledWidth := int(width / p.currentSize * 1000)
	p.beginText()
	fmt.Fprintf(p.contents, "%g %g Td ", x, y)
	if full, w := p.currentFont.encodeAndKern(s, 0); w <= scaledWidth {
		fmt.Fprintf(p.contents, "%v TJ ", full)
		p.endText()
		return
	}
	tj, _ := p.currentFont.encodeAndKern(s, scaledWidth-p.currentFont.runeWidth('…'))
	ellipsis, ok := p.currentFont.encodeRune('…')
	if ok {
		tj = append(tj, quoteString(string([]byte{ellipsis})))
	}
	fmt.Fprintf(p.contents, "%v TJ ", tj)
	p.endText()
}

// WordWrap displays s on multiple lines, wrapping at word boundaries to keep
// the width less than margin.
func (p *Page) WordWrap(x, y, margin float64, s string) {
	scaledMargin := int(margin / p.currentSize * 1000)
	p.beginText()
	fmt.Fprintf(p.contents, "%g %g Td ", x, y)
	words := strings.Fields(s)
	i := 0
	for i < len(words) {
		line, lineWidth := p.currentFont.encodeAndKern(words[i], 0)
		i++
		for i < len(words) {
			word, wordWidth := p.currentFont.encodeAndKern(" "+words[i], 0)
			if lineWidth+wordWidth > scaledMargin {
				break
			}
			line = append(line, word...)
			lineWidth += wordWidth
			i++
		}
		fmt.Fprintf(p.contents, "%v TJ ", line)
		if i < len(words) {
			fmt.Fprint(p.contents, "T* ")
		}
	}
	p.endText()
}
