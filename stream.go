package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
)

type stream struct {
	b bytes.Buffer

	extraData string
}

func (s *stream) Write(p []byte) (n int, err error) {
	return s.b.Write(p)
}

func (s *stream) writeTo(e *encoder) {
	compressed := false
	cb := new(bytes.Buffer)
	zw := zlib.NewWriter(cb)
	if _, err := zw.Write(s.b.Bytes()); err == nil {
		if err := zw.Close(); err == nil {
			if cb.Len() < s.b.Len()-len("/Filter /FlateDecode ") {
				compressed = true
			}
		}
	}

	if compressed {
		fmt.Fprintf(e, "<< /Length %d /Filter /FlateDecode ", cb.Len())
	} else {
		fmt.Fprintf(e, "<< /Length %d ", s.b.Len())
	}
	if s.extraData != "" {
		fmt.Fprint(e, s.extraData, " ")
	}
	fmt.Fprintln(e, ">>")
	e.WriteString("stream\n")
	if compressed {
		e.Write(cb.Bytes())
	} else {
		e.Write(s.b.Bytes())
	}
	e.WriteString("\nendstream")
}
