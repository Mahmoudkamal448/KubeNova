package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestDecodeYAMLMultiDoc(t *testing.T) {
	objs, err := DecodeYAML("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\n\n---\napiVersion: v1\nkind: List\nitems:\n- {apiVersion: v1, kind: Secret, metadata: {name: b}}\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || objs[0].GetName() != "a" || objs[1].GetKind() != "Secret" {
		t.Fatalf("unexpected decode: %d objects", len(objs))
	}
}

func TestSummarizePod(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{"name": "p", "namespace": "ns"},
		"spec": map[string]any{"nodeName": "n1"},
		"status": map[string]any{"phase": "Running", "containerStatuses": []any{
			map[string]any{"name": "c", "ready": false, "restartCount": int64(4), "state": map[string]any{"waiting": map[string]any{"reason": "CrashLoopBackOff"}}},
		}},
	}}
	r := Summarize(u)
	if r.Status != "CrashLoopBackOff" || r.Health != "error" || r.Cells["restarts"] != "4" || r.Cells["ready"] != "0/1" {
		t.Fatalf("unexpected row: %+v", r)
	}
}

func TestCleanForExport(t *testing.T) {
	u := &unstructured.Unstructured{Object: map[string]any{
		"kind": "ConfigMap", "metadata": map[string]any{"name": "x", "uid": "1", "resourceVersion": "2", "managedFields": []any{}}, "status": map[string]any{"a": int64(1)},
	}}
	c := CleanForExport(u)
	if c.GetUID() != "" || c.GetResourceVersion() != "" || c.Object["status"] != nil {
		t.Fatalf("not cleaned: %+v", c.Object)
	}
}
