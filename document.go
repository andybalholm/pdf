package pdf

import "fmt"

// A Document represents a PDF document.
type Document struct {
	pages pageTree
}

func (d *Document) NewPage(width, height float64) *Page {
	p := &Page{
		parent:   &d.pages,
		width:    width,
		height:   height,
		contents: new(stream),
	}
	d.pages.pages = append(d.pages.pages, p)
	return p
}

func (d *Document) writeTo(e *encoder) {
	pagesRef := e.getRef(&d.pages)
	fmt.Fprintf(e, "<< /Type /Catalog /Pages %d 0 R >>", pagesRef)
}

func (d *Document) Encode() []byte {
	return new(encoder).encode(d)
}

type pageTree struct {
	pages []*Page
}

func (p *pageTree) writeTo(e *encoder) {
	fmt.Fprintf(e, "<< /Type /Pages /Count %d /Kids [", len(p.pages))
	for i, page := range p.pages {
		pageRef := e.getRef(page)
		if i > 0 {
			e.WriteByte(' ')
		}
		fmt.Fprintf(e, "%d 0 R", pageRef)
	}
	e.WriteString("] >>")
}

type Page struct {
	parent   *pageTree
	width    float64
	height   float64
	contents *stream
}

func (p *Page) writeTo(e *encoder) {
	fmt.Fprint(e, "<< /Type /Page ")
	fmt.Fprintf(e, "/Parent %d 0 R ", e.getRef(p.parent))
	fmt.Fprintf(e, "/Contents %d 0 R ", e.getRef(p.contents))
	fmt.Fprint(e, "/Resources << >> ")
	fmt.Fprintf(e, "/MediaBox [0 0 %g %g] ", p.width, p.height)
	fmt.Fprint(e, ">>")
}
