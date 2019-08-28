package pdf

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"sort"
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
	firstChar, lastChar := 32, 255
	widths := make([]int, lastChar-firstChar+1)
	cp := &charProcs{procs: make(map[string]*type3Glyph)}

	for i := firstChar; i <= lastChar; i++ {
		var buffer sfnt.Buffer
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
		name, err := f.sfnt.GlyphName(&buffer, g)
		if err != nil {
			continue
		}
		if name == "" {
			name = glyphName(r)
		}
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

	fmt.Fprintf(e, "<< /Type /Font /Subtype /Type3 /Encoding /WinAnsiEncoding\n")
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
	s.enableFlate()
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
