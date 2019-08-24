package pdf

import "fmt"

// MoveTo starts a new path or subpath at x, y.
func (p *Page) MoveTo(x, y float64) {
	fmt.Fprint(p.contents, x, y, " m ")
}

// LineTo adds a straight line to the current path.
func (p *Page) LineTo(x, y float64) {
	fmt.Fprint(p.contents, x, y, " l ")
}

// CurveTo appends a cubic Bézier curve to the current path.
func (p *Page) CurveTo(x1, y1, x2, y2, x3, y3 float64) {
	fmt.Fprint(p.contents, x1, y1, x2, y2, x3, y3, " c ")
}

// ClosePath closes the current subpath with a straight line to its starting
// point.
func (p *Page) ClosePath() {
	fmt.Fprint(p.contents, "h ")
}

// Stroke strokes the current path.
func (p *Page) Stroke() {
	fmt.Fprint(p.contents, "S\n")
}

// Fill fills the current path.
func (p *Page) Fill() {
	fmt.Fprint(p.contents, "f\n")
}

// FillAndStroke fills and strokes the current path.
func (p *Page) FillAndStroke() {
	fmt.Fprint(p.contents, "B\n")
}

// SetLineWidth sets the width of the line to be drawn by Stroke.
func (p *Page) SetLineWidth(w float64) {
	fmt.Fprint(p.contents, w, " w ")
}

// FillGray sets a grayscale value to be used by Fill.
// 0 is black and 1 is white.
func (p *Page) FillGray(g float64) {
	fmt.Fprint(p.contents, g, " g ")
}

// StrokeGray sets a grayscale value to be used by Stroke.
// 0 is black and 1 is white.
func (p *Page) StrokeGray(g float64) {
	fmt.Fprint(p.contents, g, " G ")
}

// FillRGB sets an RGB color to be used by Fill.
// Each component is in the range from 0 to 1.
func (p *Page) FillRGB(r, g, b float64) {
	fmt.Fprint(p.contents, r, g, b, " rg ")
}

// StrokeRGB sets an RGB color to be used by Stroke.
// Each component is in the range from 0 to 1.
func (p *Page) StrokeRGB(r, g, b float64) {
	fmt.Fprint(p.contents, r, g, b, " RG ")
}

// FillCMYK sets an CMYK color to be used by Fill.
// Each component is in the range from 0 to 1.
func (p *Page) FillCMYK(c, m, y, k float64) {
	fmt.Fprint(p.contents, c, m, y, k, " k ")
}

// StrokeCMYK sets an CMYK color to be used by Stroke.
// Each component is in the range from 0 to 1.
func (p *Page) StrokeCMYK(c, m, y, k float64) {
	fmt.Fprint(p.contents, c, m, y, k, " K ")
}
