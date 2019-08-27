package pdf

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"sort"
	"strings"

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

	runes := make(sortedRunes, 0, len(f.usedGlyphs))
	for r := range f.usedGlyphs {
		runes = append(runes, r)
	}
	sort.Sort(runes)

	fmt.Fprintf(e, "<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H\n", f.name)
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

var stringEscaper = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`, "(", `\(`, ")", `\)`, `\`, `\\`)

// BeginText begins a text object. All text output and positioning must happen
// between calls to BeginText and EndText.
func (p *Page) BeginText() {
	fmt.Fprint(p.contents, "BT ")
}

func (p *Page) EndText() {
	fmt.Fprint(p.contents, "ET ")
}

// Show puts s on the page.
func (p *Page) Show(s string) {
	glyphs := p.currentFont.toGlyphs(s)
	b := new(strings.Builder)
	binary.Write(b, binary.BigEndian, glyphs)
	fmt.Fprintf(p.contents, "(%s) Tj ", stringEscaper.Replace(b.String()))
}
