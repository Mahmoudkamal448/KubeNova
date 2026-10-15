package integrations

import (
	"bytes"
	"compress/gzip"
	"io"
)

func gunzip(b []byte) ([]byte, error) {
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return b, nil // not compressed
	}
	defer r.Close()
	return io.ReadAll(r)
}
