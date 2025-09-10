import React from "react";
import type { BuilderMetadata } from "./types";

export function Palette({ builders, onAdd }: { builders: BuilderMetadata[]; onAdd: (b: BuilderMetadata) => void }) {
  return (
    <div className="w-64 shrink-0 border-r border-white/10 p-3 overflow-auto">
      <div className="text-left text-sm font-semibold uppercase tracking-wide mb-3 opacity-80">Builders</div>
      {builders.length === 0 ? (
        <div className="text-xs opacity-70">No builders found.</div>
      ) : (
        <div className="flex flex-col gap-2">
          {builders.map((b, idx) => (
            <button
              key={`${b.topLevelType}:${idx}`}
              onClick={() => onAdd(b)}
              className="text-left px-3 py-2 rounded bg-white/5 hover:bg-white/10 active:bg-white/15 transition-colors text-sm"
            >
              {b.topLevelType}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}

