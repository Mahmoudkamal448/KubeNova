import CodeMirror from "@uiw/react-codemirror";
import { yaml } from "@codemirror/lang-yaml";
import { EditorView } from "@codemirror/view";
import { useStore } from "../store";

export function YamlEditor({ value, onChange, readOnly, height = "100%" }: { value: string; onChange?: (v: string) => void; readOnly?: boolean; height?: string }) {
  const theme = useStore((s) => s.theme);
  return (
    <CodeMirror
      value={value}
      height={height}
      theme={theme === "light" ? "light" : "dark"}
      extensions={[yaml(), EditorView.lineWrapping]}
      readOnly={readOnly}
      onChange={onChange}
      basicSetup={{ foldGutter: true, highlightActiveLine: !readOnly }}
      style={{ height, border: "1px solid var(--border)", borderRadius: 8, overflow: "hidden" }}
    />
  );
}
