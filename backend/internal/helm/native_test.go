package helm

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"testing"
)

func TestDecodeReleaseAndValues(t *testing.T) {
	js := `{"name":"web","namespace":"default","version":3,"info":{"status":"deployed"},
	  "chart":{"metadata":{"name":"nginx","version":"18.1.0"},"values":{"replicaCount":1,"image":{"repository":"nginx","tag":"1.27"}}},
	  "config":{"image":{"tag":"1.27.2"},"extra":true}}`
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write([]byte(js))
	_ = w.Close()
	// What client-go returns for the Secret's "release" key.
	data := []byte(base64.StdEncoding.EncodeToString(gz.Bytes()))
	r, err := decodeRelease(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "web" || r.Version != 3 || chartName(r) != "nginx-18.1.0" {
		t.Fatalf("decoded %+v", r)
	}
	v := mergeValues(r.Chart.Values, r.Config)
	img := v["image"].(map[string]any)
	if img["repository"] != "nginx" || img["tag"] != "1.27.2" || v["extra"] != true || v["replicaCount"] != float64(1) {
		t.Fatalf("merged values: %v", v)
	}
}
