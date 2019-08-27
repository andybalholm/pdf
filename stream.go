package pdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
)

type stream struct {
	b  bytes.Buffer
	fw *zlib.Writer

	extraData string
}

func (s *stream) Write(p []byte) (n int, err error) {
	if s.fw != nil {
		return s.fw.Write(p)
	}
	return s.b.Write(p)
}

func (s *stream) writeTo(e *encoder) {
	if s.fw != nil {
		s.fw.Close()
	}
	fmt.Fprintf(e, "<< /Length %d ", s.b.Len())
	if s.fw != nil {
		fmt.Fprint(e, "/Filter /FlateDecode ")
	}
	if s.extraData != "" {
		fmt.Fprint(e, s.extraData, " ")
	}
	fmt.Fprintln(e, ">>")
	e.WriteString("stream\n")
	e.Write(s.b.Bytes())
	e.WriteString("\nendstream")
}

func (s *stream) enableFlate() {
	s.fw = zlib.NewWriter(&s.b)
}
