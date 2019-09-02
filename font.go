package pdf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type fontType int

const (
	trueType fontType = iota
	openType
)

type Font struct {
	typ        fontType
	name       string
	data       []byte
	sfnt       *sfnt.Font
	usedGlyphs map[rune]sfnt.GlyphIndex
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
		data:       b,
		usedGlyphs: make(map[rune]sfnt.GlyphIndex),
	}

	if len(b) < 4 {
		return nil, errors.New("font file too small")
	}
	switch string(b[:4]) {
	case "true", "\x00\x01\x00\x00":
		f.typ = trueType
	case "OTTO":
		f.typ = openType
	default:
		return nil, errors.New("unrecognized font format")
	}

	f.sfnt, err = sfnt.Parse(b)
	if err != nil {
		return nil, err
	}

	f.name, err = f.sfnt.Name(nil, sfnt.NameIDPostScript)
	if err != nil {
		return nil, errors.New("missing PostScript font name")
	}

	if d.fontCache == nil {
		d.fontCache = make(map[string]*Font)
	}
	d.fontCache[filename] = f
	return f, nil
}

type sortedRunes []rune

func (s sortedRunes) Len() int           { return len(s) }
func (s sortedRunes) Less(i, j int) bool { return s[i] < s[j] }
func (s sortedRunes) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (f *Font) writeTo(e *encoder) {
	var CIDType int
	switch f.typ {
	case trueType:
		CIDType = 2
	case openType:
		CIDType = 0
	}

	var buffer sfnt.Buffer
	numGlyphs := f.sfnt.NumGlyphs()
	widths := make([]int, numGlyphs)
	for i := range widths {
		w, err := f.sfnt.GlyphAdvance(&buffer, sfnt.GlyphIndex(i), fixed.I(1000), font.HintingNone)
		if err == nil {
			widths[i] = w.Round()
		}
	}

	// Build the ToUnicode CMap.
	runes := make(sortedRunes, 0, len(f.usedGlyphs))
	for r := range f.usedGlyphs {
		runes = append(runes, r)
	}
	sort.Sort(runes)

	tu := new(stream)
	fmt.Fprintln(tu, "/CIDInit /ProcSet findresource begin")
	fmt.Fprintln(tu, "12 dict begin")
	fmt.Fprintln(tu, "begincmap")
	fmt.Fprintln(tu, "/CIDSystemInfo << /Registry (Adobe)\n/Ordering (UCS)\n/Supplement 0 >> def")
	fmt.Fprintln(tu, "/CMapName /Adobe-Identity-UCS def")
	fmt.Fprintln(tu, "/CMapType 2 def")
	fmt.Fprintln(tu, "1 begincodespacerange")
	fmt.Fprintln(tu, "<0000> <FFFF>")
	fmt.Fprintln(tu, "endcodespacerange")
	for i, r := range runes {
		if i%100 == 0 {
			if i != 0 {
				fmt.Fprintln(tu, "endbfchar")
			}
			if len(runes) < i+100 {
				fmt.Fprintf(tu, "%d beginbfchar\n", len(runes)-i)
			} else {
				fmt.Fprintln(tu, "100 beginbfchar")
			}
		}
		if r < 0x10000 {
			fmt.Fprintf(tu, "<%04X> <%04X>\n", f.usedGlyphs[r], r)
		} else {
			r1, r2 := utf16.EncodeRune(r)
			fmt.Fprintf(tu, "<%04X> <%04X%04X>\n", f.usedGlyphs[r], r1, r2)
		}
	}
	fmt.Fprintln(tu, "endbfchar")
	fmt.Fprintln(tu, "endcmap")
	fmt.Fprintln(tu, "CMapName currentdict /CMap defineresource pop")
	fmt.Fprintln(tu, "end\nend")

	fmt.Fprintf(e, "<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /ToUnicode %d 0 R\n", f.name, e.getRef(tu))
	fmt.Fprintf(e, "/DescendantFonts [ << /Type /Font /Subtype /CIDFontType%d /BaseFont /%s /CIDToGIDMap /Identity\n", CIDType, f.name)

	fmt.Fprintf(e, "/DW %d ", widths[0])
	fmt.Fprintf(e, "/W [0 %d]\n", widths)

	fmt.Fprintln(e, "/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>")
	fmt.Fprintf(e, "/FontDescriptor %d 0 R\n", e.getRef(&fontDescriptor{f}))
	fmt.Fprint(e, ">> ] >>")
}

func (f *Font) toGlyph(r rune) sfnt.GlyphIndex {
	var buffer sfnt.Buffer
	gi, ok := f.usedGlyphs[r]
	if ok {
		return gi
	}
	gi, err := f.sfnt.GlyphIndex(&buffer, r)
	if err != nil {
		return 0
	}
	f.usedGlyphs[r] = gi
	return gi
}

func (f *Font) toGlyphs(s string) []sfnt.GlyphIndex {
	glyphs := make([]sfnt.GlyphIndex, 0, len(s))
	for _, r := range s {
		glyphs = append(glyphs, f.toGlyph(r))
	}
	return glyphs
}

type fontDescriptor struct {
	f *Font
}

func (f *fontDescriptor) writeTo(e *encoder) {
	fontFile := &stream{}
	fontFile.enableFlate()
	fontFile.Write(f.f.data)
	switch f.f.typ {
	case trueType:
		fontFile.extraData = "/Subtype /TrueType"
	case openType:
		fontFile.extraData = "/Subtype /OpenType"
	}

	var buffer sfnt.Buffer

	var bounds []int
	rawBounds, err := f.f.sfnt.Bounds(&buffer, fixed.I(1000), font.HintingNone)
	if err == nil {
		bounds = []int{rawBounds.Min.X.Floor(), -rawBounds.Max.Y.Ceil(), rawBounds.Max.X.Ceil(), -rawBounds.Min.Y.Floor()}
	}

	var italicAngle float64
	if pt := f.f.sfnt.PostTable(); pt != nil {
		italicAngle = pt.ItalicAngle
	}

	metrics, _ := f.f.sfnt.Metrics(&buffer, fixed.I(1000), font.HintingNone)

	fmt.Fprintln(e, "<< /Type /FontDescriptor")
	fmt.Fprintf(e, "/FontName /%s\n", f.f.name)
	fmt.Fprintf(e, "/Flags 4\n")
	fmt.Fprintf(e, "/FontBBox %d\n", bounds)
	fmt.Fprintf(e, "/ItalicAngle %g\n", italicAngle)
	fmt.Fprintf(e, "/Ascent %d\n", metrics.Ascent.Round())
	fmt.Fprintf(e, "/Descent %d\n", metrics.Descent.Round())
	fmt.Fprintf(e, "/CapHeight %d\n", metrics.CapHeight.Round())
	fmt.Fprintf(e, "/StemV 80 /StemH 80\n")
	fmt.Fprintf(e, "/FontFile3 %d 0 R\n", e.getRef(fontFile))
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
	glyphs := f.toGlyphs(s)
	var buffer sfnt.Buffer

	chunkStart := 0
	for i, g := range glyphs {
		advance, err := f.sfnt.GlyphAdvance(&buffer, g, fixed.I(1000), font.HintingNone)
		if err == nil {
			oldWidth := width
			width += advance.Round()
			if maxWidth != 0 && width > maxWidth {
				tj = append(tj, quoteGlyphIndices(glyphs[chunkStart:i]))
				return tj, oldWidth
			}
		}
		if i != 0 {
			kern, err := f.sfnt.Kern(&buffer, glyphs[i-1], g, fixed.I(1000), font.HintingNone)
			if err == nil && kern != 0 {
				width += kern.Round()
				tj = append(tj,
					quoteGlyphIndices(glyphs[chunkStart:i]),
					strconv.Itoa(-kern.Round()),
				)
				chunkStart = i
			}
		}
	}
	tj = append(tj, quoteGlyphIndices(glyphs[chunkStart:]))

	return tj, width
}

var stringEscaper = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`, "(", `\(`, ")", `\)`, `\`, `\\`)

func quoteString(s string) string {
	return "(" + stringEscaper.Replace(s) + ")"
}

func quoteGlyphIndices(glyphs []sfnt.GlyphIndex) string {
	b := new(strings.Builder)
	binary.Write(b, binary.BigEndian, glyphs)
	return quoteString(b.String())
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
	ellipsis, ellipsisWidth := p.currentFont.encodeAndKern("…", 0)
	tj, _ := p.currentFont.encodeAndKern(s, scaledWidth-ellipsisWidth)
	tj = append(tj, ellipsis...)
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
