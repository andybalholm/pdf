package pdf

import (
	"bytes"
	"fmt"
)

type stream struct {
	b bytes.Buffer
}

func (s *stream) Write(p []byte) (n int, err error) {
	return s.b.Write(p)
}

func (s *stream) writeTo(e *encoder) {
	fmt.Fprintf(e, "<< /Length %d >>\n", s.b.Len())
	e.WriteString("stream\n")
	e.Write(s.b.Bytes())
	e.WriteString("\nendstream")
}
