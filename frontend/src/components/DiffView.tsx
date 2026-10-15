import { useEffect, useRef } from "react";
import { MergeView } from "@codemirror/merge";
import { EditorView, basicSetup } from "codemirror";
import { yaml } from "@codemirror/lang-yaml";
import { EditorState } from "@codemirror/state";
import { oneDark } from "@codemirror/theme-one-dark";
import { useStore } from "../store";

/** Side-by-side diff viewer. */
export function DiffView({ a, b, height = "100%" }: { a: string; b: string; height?: string }) {
  const ref = useRef<HTMLDivElement>(null);
  const theme = useStore((s) => s.theme);
  useEffect(() => {
    if (!ref.current) return;
    const ext = [basicSetup, yaml(), EditorView.editable.of(false), EditorState.readOnly.of(true), ...(theme === "light" ? [] : [oneDark])];
    const mv = new MergeView({ a: { doc: a, extensions: ext }, b: { doc: b, extensions: ext }, parent: ref.current, collapseUnchanged: { margin: 3, minSize: 6 } });
    return () => mv.destroy();
  }, [a, b, theme]);
  return <div ref={ref} style={{ height, overflow: "auto", border: "1px solid var(--border)", borderRadius: 8 }} />;
}

/** Unified diff text with colouring. */
export function UnifiedDiff({ text }: { text: string }) {
  if (!text) return <div className="muted">No differences.</div>;
  return (
    <pre style={{ maxHeight: "60vh" }}>
      {text.split("\n").map((l, i) => (
        <div key={i} style={{ color: l.startsWith("+") ? "var(--ok)" : l.startsWith("-") ? "var(--err)" : l.startsWith("@@") ? "var(--info)" : undefined }}>{l || " "}</div>
      ))}
    </pre>
  );
}
