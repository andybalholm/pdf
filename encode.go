package pdf

import (
	"bytes"
	"fmt"
)

type object interface {
	writeTo(e *encoder)
}

type encoder struct {
	bytes.Buffer

	objects []object
	offsets []int
	refs    map[object]int
}

// getRef returns the 1-based index of o in e's list of objects. If v is not in
// the list, it is added.
func (e *encoder) getRef(o object) int {
	if ref, ok := e.refs[o]; ok {
		return ref
	}

	e.objects = append(e.objects, o)
	ref := len(e.objects)
	e.refs[o] = ref
	return ref
}

func (e *encoder) encode(root object) []byte {
	e.Reset()
	e.offsets = nil
	e.refs = make(map[object]int)

	e.WriteString("%%PDF-1.7\n")
	rootRef := e.getRef(root)

	for i := 0; i < len(e.objects); i++ {
		e.offsets = append(e.offsets, e.Len())
		fmt.Fprintf(e, "%d 0 obj\n", i+1)
		e.objects[i].writeTo(e)
		e.WriteString("\nendobj\n")
	}

	startxref := e.Len()
	e.WriteString("xref\n")
	fmt.Fprintf(e, "0 %d\n", len(e.objects)+1)
	e.WriteString("0000000000 65535 f\n")
	for _, offset := range e.offsets {
		fmt.Fprintf(e, "%010d 00000 n\n", offset)
	}

	e.WriteString("trailer\n")
	fmt.Fprintf(e, "<< /Root %d 0 R /Size %d >>\n", rootRef, len(e.objects)+1)
	e.WriteString("startxref\n")
	fmt.Fprintln(e, startxref)
	e.WriteString("%%EOF\n")

	return e.Bytes()
}
