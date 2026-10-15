import React, { useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp } from "lucide-react";

export interface Column<T> {
  id: string;
  title: string;
  width?: string; // CSS grid track, e.g. "2fr" or "120px"
  render: (row: T) => React.ReactNode;
  sort?: (row: T) => string | number;
}

/** Virtualized grid that stays fast with 100k+ rows. */
export function VirtualTable<T>({ rows, columns, rowKey, onRowClick, selected, onContextMenu, rowHeight = 32 }: {
  rows: T[];
  columns: Column<T>[];
  rowKey: (r: T) => string;
  onRowClick?: (r: T) => void;
  selected?: string;
  onContextMenu?: (r: T, e: React.MouseEvent) => void;
  rowHeight?: number;
}) {
  const parent = useRef<HTMLDivElement>(null);
  const [sort, setSort] = useState<{ id: string; dir: 1 | -1 } | null>(null);
  const sorted = useMemo(() => {
    if (!sort) return rows;
    const col = columns.find((c) => c.id === sort.id);
    if (!col?.sort) return rows;
    const key = col.sort;
    return [...rows].sort((a, b) => {
      const x = key(a), y = key(b);
      return (x < y ? -1 : x > y ? 1 : 0) * sort.dir;
    });
  }, [rows, sort, columns]);
  const v = useVirtualizer({ count: sorted.length, getScrollElement: () => parent.current, estimateSize: () => rowHeight, overscan: 20 });
  const template = columns.map((c) => c.width || "1fr").join(" ");
  return (
    <div ref={parent} className="vt">
      <div className="vt-head" style={{ gridTemplateColumns: template, minWidth: "100%" }}>
        {columns.map((c) => (
          <div key={c.id} onClick={() => c.sort && setSort((s) => (s?.id === c.id ? (s.dir === 1 ? { id: c.id, dir: -1 } : null) : { id: c.id, dir: 1 }))}>
            {c.title}
            {sort?.id === c.id && (sort.dir === 1 ? <ArrowUp size={11} /> : <ArrowDown size={11} />)}
          </div>
        ))}
      </div>
      <div style={{ height: v.getTotalSize(), position: "relative" }}>
        {v.getVirtualItems().map((vi) => {
          const r = sorted[vi.index];
          const k = rowKey(r);
          return (
            <div
              key={k}
              className={`vt-row ${selected === k ? "sel" : ""}`}
              style={{ gridTemplateColumns: template, position: "absolute", top: 0, left: 0, right: 0, height: vi.size, transform: `translateY(${vi.start}px)` }}
              onClick={() => onRowClick?.(r)}
              onContextMenu={(e) => onContextMenu?.(r, e)}
            >
              {columns.map((c) => (
                <div key={c.id}>{c.render(r)}</div>
              ))}
            </div>
          );
        })}
      </div>
    </div>
  );
}
