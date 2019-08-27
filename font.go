package pdf

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/encoding/charmap"
)

type fontType int

const (
	trueType fontType = iota
	openType
)

func (t fontType) String() string {
	switch t {
	case trueType:
		return "TrueType"
	case openType:
		return "OpenType"
	default:
		return "invalid"
	}
}

type Font struct {
	typ  fontType
	name string
	data []byte
	sfnt *sfnt.Font
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
		data: b,
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
	var buffer sfnt.Buffer

	firstChar, lastChar := 32, 255
	widths := make([]int, lastChar-firstChar+1)
	for i := firstChar; i <= lastChar; i++ {
		r := charmap.Windows1252.DecodeByte(byte(i))
		g, err := f.sfnt.GlyphIndex(&buffer, r)
		if err != nil {
			continue
		}
		w, err := f.sfnt.GlyphAdvance(&buffer, g, fixed.I(1000), font.HintingNone)
		if err != nil {
			continue
		}
		widths[i-firstChar] = w.Round()
	}

	fmt.Fprintf(e, "<< /Type /Font /Subtype /%v /BaseFont /%s /Encoding /WinAnsiEncoding\n", f.typ, f.name)
	fmt.Fprintf(e, "/FontDescriptor %d 0 R\n", e.getRef(&fontDescriptor{f}))
	fmt.Fprintf(e, "/FirstChar %d /LastChar %d\n", firstChar, lastChar)
	fmt.Fprintf(e, "/Widths %d\n", widths)
	fmt.Fprint(e, ">>")
}

type fontDescriptor struct {
	f *Font
}

func (f *fontDescriptor) writeTo(e *encoder) {
	fontFile := &stream{}
	fontFile.enableFlate()
	fontFile.Write(f.f.data)
	fontFile.extraData = fmt.Sprintf("/Subtype /%v", f.f.typ)

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
	s, err := charmap.Windows1252.NewEncoder().String(s)
	if err != nil {
		return
	}
	fmt.Fprintf(p.contents, "(%s) Tj ", stringEscaper.Replace(s))
}
