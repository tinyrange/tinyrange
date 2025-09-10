import "./index.css";
import { useEffect, useMemo, useRef, useState } from "react";
import { ReactFlow, Background, Controls, Handle, Position, addEdge, applyNodeChanges, applyEdgeChanges, useNodesState, type Node as RFNode, type Edge as RFEdge, type NodeChange, type EdgeChange, type Connection, type NodeProps } from "@xyflow/react";
import "@xyflow/react/dist/style.css";

type DescriptorField = {
  name: string;
  jsonName?: string;
  number: number;
  label?: string;
  type: string;
  typeName?: string;
};
type DescriptorMessageType = {
  name: string;
  field?: DescriptorField[];
};
type FileDescriptorProto = {
  name: string;
  package?: string;
  dependency?: string[];
  messageType?: DescriptorMessageType[];
  enumType?: { name: string; value?: { name: string; number: number }[] }[];
  syntax?: string;
};

type BuilderMetadata = {
  // Canonical type name from server (kept for future use)
  typeName: string;
  // Top-level protobuf message type name to expose in UI
  topLevelType: string;
  // Full descriptor for schema-based inputs/outputs
  definition: FileDescriptorProto;
};

type Point = { x: number; y: number };

type GraphNode = {
  id: string;
  typeName: string; // display name
  position: Point;
  builder: BuilderMetadata;
  payload: Record<string, any>;
};

type Viewport = {
  offset: Point; // screen-space translation in px
  scale: number; // zoom factor (1 = 100%)
};

function getApiBase(): string {
  const g = globalThis as any;
  if (g && typeof g.BUN_PUBLIC_API_BASE === "string") return g.BUN_PUBLIC_API_BASE as string;
  const meta = typeof document !== "undefined" ? document.querySelector('meta[name="api-base"]') : null;
  if (meta && meta instanceof HTMLMetaElement && meta.content) return meta.content;
  return "";
}
const API_BASE = getApiBase();

function getTopMessage(meta: BuilderMetadata): DescriptorMessageType | null {
  const mt = meta.definition?.messageType || [];
  return mt.find((m) => m.name === meta.topLevelType) || null;
}

function fieldKey(f: DescriptorField): string { return f.jsonName || f.name; }

function enumValues(def: FileDescriptorProto, typeName?: string): { name: string; number: number }[] {
  if (!typeName) return [];
  // typeName like ".proto.ArchiveType" -> "ArchiveType"
  const parts = typeName.split(".").filter(Boolean);
  const name = parts[parts.length - 1];
  const e = def.enumType?.find((x) => x.name === name);
  return e?.value || [];
}

function defaultForField(def: FileDescriptorProto, f: DescriptorField): any {
  switch (f.type) {
    case "TYPE_STRING":
      return "";
    case "TYPE_INT32":
    case "TYPE_INT64":
    case "TYPE_UINT32":
    case "TYPE_UINT64":
    case "TYPE_FLOAT":
    case "TYPE_DOUBLE":
      return 0;
    case "TYPE_BOOL":
      return false;
    case "TYPE_BYTES":
      return "";
    case "TYPE_ENUM": {
      const vals = enumValues(def, f.typeName);
      return (vals[0]?.name) || "";
    }
    case "TYPE_MESSAGE":
    default:
      return null;
  }
}

async function fetchBuilders(signal?: AbortSignal): Promise<BuilderMetadata[]> {
  const res = await fetch(`${API_BASE}/builders`, {
    headers: { Accept: "application/json" },
    signal,
  });
  if (!res.ok) throw new Error(`failed to fetch builders: ${res.status}`);
  const data = await res.json();
  if (!data || !Array.isArray(data.builders)) return [];
  // Trust server protojson camelCase: typeName, topLevelType, definition
  return data.builders as BuilderMetadata[];
}

function useBuilders() {
  const [builders, setBuilders] = useState<BuilderMetadata[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const c = new AbortController();
    setLoading(true);
    fetchBuilders(c.signal)
      .then(setBuilders)
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false));
    return () => c.abort();
  }, []);

  return { builders, error, loading };
}

function Palette({ builders, onAdd }: { builders: BuilderMetadata[]; onAdd: (b: BuilderMetadata) => void }) {
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

function NodeView({ node, onDrag, onSelect, selected, viewport }: {
  node: GraphNode;
  onDrag: (id: string, pos: Point) => void;
  onSelect: (id: string) => void;
  selected: boolean;
  viewport: Viewport;
}) {
  const ref = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    let start: Point | null = null;

    const onPointerDown = (e: PointerEvent) => {
      (e.target as Element).setPointerCapture?.(e.pointerId);
      const worldX = (e.clientX - viewport.offset.x) / viewport.scale;
      const worldY = (e.clientY - viewport.offset.y) / viewport.scale;
      start = { x: worldX - node.position.x, y: worldY - node.position.y };
      onSelect(node.id);
    };
    const onPointerMove = (e: PointerEvent) => {
      if (!start) return;
      const worldX = (e.clientX - viewport.offset.x) / viewport.scale;
      const worldY = (e.clientY - viewport.offset.y) / viewport.scale;
      onDrag(node.id, { x: worldX - start.x, y: worldY - start.y });
    };
    const onPointerUp = (_e: PointerEvent) => {
      start = null;
    };

    el.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    return () => {
      el.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
    };
  }, [node.id, node.position.x, node.position.y, onDrag, onSelect, viewport.offset.x, viewport.offset.y, viewport.scale]);

  return (
    <div
      ref={ref}
      style={{ left: node.position.x, top: node.position.y }}
      className={`absolute select-none cursor-grab active:cursor-grabbing`}
      onDoubleClick={() => onSelect(node.id)}
    >
      <div className={`rounded-lg shadow-lg border ${selected ? "border-cyan-400" : "border-white/10"} bg-zinc-800/80 backdrop-blur px-3 py-2 min-w-48`}>
        <div className="text-[10px] uppercase tracking-wide opacity-60">Node</div>
        <div className="text-sm font-medium break-all">{node.typeName}</div>
      </div>
    </div>
  );
}

function Canvas({ nodes, onDrag, onSelect, selectedId, viewport, setViewport, snap }: {
  nodes: GraphNode[];
  onDrag: (id: string, pos: Point) => void;
  onSelect: (id: string) => void;
  selectedId: string | null;
  viewport: Viewport;
  setViewport: (v: Viewport) => void;
  snap: number | null;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const panning = useRef<{ start: Point; origin: Point } | null>(null);
  const spaceHeld = useRef(false);
  const viewportRef = useRef<Viewport>(viewport);
  useEffect(() => {
    viewportRef.current = viewport;
  }, [viewport]);

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.code === "Space") spaceHeld.current = true;
    };
    const onKeyUp = (e: KeyboardEvent) => {
      if (e.code === "Space") spaceHeld.current = false;
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
    };
  }, []);

  // Zoom with Ctrl+wheel; keep cursor point stable.
  const onWheel: React.WheelEventHandler<HTMLDivElement> = (e) => {
    if (!e.ctrlKey) return; // avoid hijacking normal scroll
    e.preventDefault();
    const rect = (e.currentTarget as HTMLDivElement).getBoundingClientRect();
    const cx = e.clientX - rect.left;
    const cy = e.clientY - rect.top;
    const { offset, scale } = viewportRef.current;
    const worldX = (cx - offset.x) / scale;
    const worldY = (cy - offset.y) / scale;

    const factor = Math.exp(-e.deltaY * 0.0015); // smooth zoom
    const nextScale = Math.min(3, Math.max(0.25, scale * factor));
    const nextOffset: Point = {
      x: cx - worldX * nextScale,
      y: cy - worldY * nextScale,
    };
    setViewport({ offset: nextOffset, scale: nextScale });
  };

  const onBackgroundPointerDown: React.PointerEventHandler<HTMLDivElement> = (e) => {
    const middle = e.button === 1;
    if (spaceHeld.current || middle) {
      (e.currentTarget as Element).setPointerCapture?.(e.pointerId);
      panning.current = {
        start: { x: e.clientX, y: e.clientY },
        origin: { ...viewportRef.current.offset },
      };
    }
  };
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const onMove = (e: PointerEvent) => {
      if (!panning.current) return;
      const dx = e.clientX - panning.current.start.x;
      const dy = e.clientY - panning.current.start.y;
      const vp = viewportRef.current;
      setViewport({ offset: { x: panning.current.origin.x + dx, y: panning.current.origin.y + dy }, scale: vp.scale });
    };
    const onUp = () => {
      panning.current = null;
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
    };
  }, []);

  // No DOM bridging needed; we rely on refs for latest viewport.

  return (
    <div
      ref={containerRef}
      className="relative flex-1 overflow-hidden"
      onWheel={onWheel}
      onPointerDown={onBackgroundPointerDown}
    >
      {/* World group with transform for pan/zoom */}
      <div
        className="absolute inset-0"
        style={{
          transform: `translate(${viewport.offset.x}px, ${viewport.offset.y}px) scale(${viewport.scale})`,
          transformOrigin: "0 0",
        }}
      >
        {/* Grid that scales with world */}
        <div className="absolute inset-0 bg-[radial-gradient(circle_at_1px_1px,rgba(0,0,0,0.1)_1px,transparent_0)] dark:bg-[radial-gradient(circle_at_1px_1px,rgba(255,255,255,0.08)_1px,transparent_0)] [background-size:24px_24px]" />

        {/* Nodes */}
        <div className="absolute inset-0">
          {nodes.map((n) => (
            <NodeView key={n.id} node={n} viewport={viewport} onDrag={(id, pos) => {
              // Optional grid snapping
              const p = pos;
              const snapped = (() => {
                if (!snap) return p;
                const s = snap;
                return { x: Math.round(p.x / s) * s, y: Math.round(p.y / s) * s };
              })();
              onDrag(id, snapped);
            }} onSelect={onSelect} selected={selectedId === n.id} />
          ))}
        </div>
      </div>
    </div>
  );
}

export function App() {
  const { builders, error, loading } = useBuilders();
  const [nodes, setNodes] = useState<GraphNode[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [viewport, setViewport] = useState<Viewport>({ offset: { x: 0, y: 0 }, scale: 1 });
  const [snap, setSnap] = useState<number | null>(24);
  const [edges, setEdges] = useState<RFEdge[]>([]);
  const [busy, setBusy] = useState<string | null>(null);
  const [lastReceipt, setLastReceipt] = useState<any | null>(null);

  // Theme toggle
  const [theme, setTheme] = useState<"light" | "dark">(() => {
    const saved = localStorage.getItem("theme");
    if (saved === "light" || saved === "dark") return saved;
    return matchMedia && matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  });
  useEffect(() => {
    document.documentElement.classList.toggle("dark", theme === "dark");
    localStorage.setItem("theme", theme);
  }, [theme]);

  const addNode = (b: BuilderMetadata) => {
    const id = `n_${crypto.randomUUID().slice(0, 8)}`;
    const offset = 40 + nodes.length * 20;
    // Initialize payload with defaults for all fields
    const top = getTopMessage(b);
    const payload: Record<string, any> = {};
    (top?.field || []).forEach((f) => {
      payload[fieldKey(f)] = defaultForField(b.definition, f);
    });
    setNodes((cur) => cur.concat([{ id, typeName: b.topLevelType, position: { x: offset, y: offset }, builder: b, payload }]));
    setSelectedId(id);
  };

  const onDrag = (id: string, pos: Point) => {
    setNodes((cur) => cur.map((n) => (n.id === id ? { ...n, position: pos } : n)));
  };

  function makeAny(typeName: string, pkg: string, message: Record<string, any>) {
    const typeUrl = `type.googleapis.com/${pkg}.${typeName}`;
    return { '@type': typeUrl, ...message } as const;
  }

  function isProbablyBase64(s: string): boolean {
    // quick heuristic: valid charset and length multiple of 4
    return /^[A-Za-z0-9+/=]*$/.test(s) && s.length % 4 === 0;
  }

  function stringToBase64Utf8(s: string): string {
    const bytes = new TextEncoder().encode(s);
    let bin = "";
    for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]!);
    return btoa(bin);
  }

  function defFromNode(n: GraphNode): { typeName: string; payload: any } {
    const top = getTopMessage(n.builder);
    const payload: Record<string, any> = {};
    for (const f of top?.field || []) {
      const key = fieldKey(f);
      if (f.type === 'TYPE_MESSAGE') {
        // Only support FileSource inline for now
        if (/(\.proto\.FileSource|FileSource)$/.test(f.typeName || '')) {
          const e = edges.find((ed) => ed.target === n.id && ed.targetHandle === `in:${key}`);
          if (!e) continue;
          const src = nodes.find((x) => x.id === e.source);
          if (!src) continue;
          if (/FetchHttpDefinition$/.test(src.builder.topLevelType)) {
            payload[key] = { fetchHttp: { url: String(src.payload['url'] ?? '') } };
          } else {
            // Fallback: reference by hash in future
            throw new Error(`Unsupported source for FileSource: ${src.builder.topLevelType}`);
          }
        }
      } else if (f.type === 'TYPE_ENUM') {
        payload[key] = n.payload[key];
      } else if (f.type === 'TYPE_BYTES') {
        const v = n.payload[key];
        if (typeof v === 'string' && v.length > 0) {
          payload[key] = isProbablyBase64(v) ? v : stringToBase64Utf8(v);
        } else if (v == null) {
          payload[key] = "";
        } else {
          // fallback: serialize non-strings as JSON then base64-encode
          const s = typeof v === 'string' ? v : JSON.stringify(v);
          payload[key] = stringToBase64Utf8(s);
        }
      } else {
        payload[key] = n.payload[key];
      }
    }
    return { typeName: n.builder.typeName, payload: makeAny(n.builder.topLevelType, n.builder.definition?.package || 'proto', payload) };
  }

  async function buildFromNode(rootId: string) {
    const root = nodes.find((n) => n.id === rootId);
    if (!root) return;
    try {
      setBusy('Building…');
      setLastReceipt(null);
      const rootDef = defFromNode(root);
      const req = { closure: { root: rootDef, dependencies: [] as any[] } };
      const body = JSON.stringify(req);
      const res = await fetch(`${API_BASE}/build`, { method: 'POST', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body });
      const text = await res.text();
      let data: any = null;
      try {
        data = text ? JSON.parse(text) : null;
      } catch (_e) {
        // Non-JSON error from server; fall through
      }
      if (!res.ok) {
        const msg = data?.error || text || res.statusText;
        throw new Error(msg);
      }
      setLastReceipt(data);
    } catch (e) {
      alert(`Build failed: ${e}`);
    } finally {
      setBusy(null);
    }
  }

  function deleteNode(id: string) {
    if (!confirm('Delete this node?')) return;
    setNodes((cur) => cur.filter((n) => n.id !== id));
    setEdges((eds) => eds.filter((e) => e.source !== id && e.target !== id));
    if (selectedId === id) setSelectedId(null);
  }

  const selectedNode = useMemo(() => nodes.find((n) => n.id === selectedId) ?? null, [nodes, selectedId]);

  return (
    <div className="flex h-screen w-screen">
      <Palette builders={builders} onAdd={addNode} />
      <div className="flex-1 flex flex-col">
        <div className="flex items-center justify-between border-b border-white/10 px-4 py-2">
          <div className="text-sm opacity-80">{loading ? "Loading builders…" : error ? `Error: ${error}` : `${builders.length} builder types`}</div>
          <div className="flex items-center gap-2">
            <button
              className="text-xs px-2 py-1 rounded bg-white/5 hover:bg-white/10 border border-white/10"
              onClick={() => setTheme((t) => (t === "dark" ? "light" : "dark"))}
              title="Toggle theme"
            >
              {theme === "dark" ? "Dark" : "Light"}
            </button>
            <div className="h-4 w-px bg-white/10" />
            <span className="text-xs opacity-60">API</span>
            <code className="text-xs bg-white/5 px-2 py-1 rounded">{API_BASE || "/"}</code>
          </div>
        </div>

        <div className="flex flex-1 min-h-0">
          <NodeFlow
            nodes={nodes}
            edges={edges}
            setEdges={setEdges}
            onDrag={onDrag}
            onSelect={setSelectedId}
            onDeleteNode={deleteNode}
            onBuildNode={buildFromNode}
            onChangePayload={(id, key, value) => setNodes((cur) => cur.map((n) => (n.id === id ? { ...n, payload: { ...n.payload, [key]: value } } : n)))}
            selectedId={selectedId}
            theme={theme}
            snap={snap ?? 24}
          />
        </div>
      </div>
    </div>
  );
}

export default App;

// --- React Flow integration (third-party node editor) ---

type NodeFlowProps = {
  nodes: GraphNode[];
  edges: RFEdge[];
  setEdges: React.Dispatch<React.SetStateAction<RFEdge[]>>;
  onDrag: (id: string, pos: Point) => void;
  onSelect: (id: string) => void;
  onDeleteNode: (id: string) => void;
  onBuildNode: (id: string) => void;
  onChangePayload: (id: string, key: string, value: any) => void;
  selectedId: string | null;
  theme: "light" | "dark";
  snap: number;
};

function NodeInspector({ node, edges, onChange }: { node: GraphNode; edges: RFEdge[]; onChange: (key: string, value: any) => void }) {
  const top = getTopMessage(node.builder);
  const fields = top?.field || [];
  return (
    <div className="flex flex-col gap-2">
      {fields.map((f) => {
        const key = fieldKey(f);
        const label = key;
        const t = f.type;
        if (t === "TYPE_ENUM") {
          const opts = enumValues(node.builder.definition, f.typeName);
          return (
            <label key={key} className="block">
              <div className="mb-1 opacity-70">{label}</div>
              <select className="w-full bg-white/5 border border-white/10 rounded px-2 py-1 text-sm" value={node.payload[key] ?? ""} onChange={(e) => onChange(key, e.target.value)}>
                {opts.map((o) => (
                  <option key={o.name} value={o.name}>{o.name}</option>
                ))}
              </select>
            </label>
          );
        }
        if (t === "TYPE_BOOL") {
          return (
            <label key={key} className="flex items-center gap-2">
              <input type="checkbox" checked={!!node.payload[key]} onChange={(e) => onChange(key, e.target.checked)} />
              <span className="opacity-70">{label}</span>
            </label>
          );
        }
        if (t === "TYPE_MESSAGE") {
          // Show connection status; editing via edge
          const connected = edges.find((ed) => ed.target === node.id && ed.targetHandle === `in:${key}`);
          return (
            <div key={key} className="text-xs">
              <div className="opacity-70">{label}</div>
              <div className="mt-1">
                {connected ? (
                  <span className="px-2 py-1 rounded bg-emerald-500/20 text-emerald-300 border border-emerald-400/20">Linked to {connected.source}</span>
                ) : (
                  <span className="opacity-60">Connect another node to this input</span>
                )}
              </div>
            </div>
          );
        }
        // String/number/bytes as text input
        return (
          <label key={key} className="block">
            <div className="mb-1 opacity-70">{label}</div>
            <input
              className="w-full bg-white/5 border border-white/10 rounded px-2 py-1 text-sm"
              type="text"
              value={node.payload[key] ?? ""}
              onChange={(e) => onChange(key, e.target.value)}
              placeholder={t.replace("TYPE_", "").toLowerCase()}
            />
          </label>
        );
      })}
    </div>
  );
}

// Derive connectable input fields from a builder descriptor
function getInputsFor(meta: BuilderMetadata): { key: string; label: string; typeName?: string; connectable: boolean }[] {
  const top = getTopMessage(meta);
  const fields = top?.field || [];
  return fields.map((f) => {
    const typeName = f.typeName;
    const isMsg = f.type === "TYPE_MESSAGE";
    const connectable = isMsg && !!typeName && (/(FileSource|DefinitionReference|Definition)$/.test(typeName));
    return {
      key: fieldKey(f),
      label: fieldKey(f),
      typeName,
      connectable,
    };
  });
}

function canConnectInput(targetNode: GraphNode, inputKey: string, sourceNode: GraphNode): { ok: boolean; reason?: string } {
  const inputs = getInputsFor(targetNode.builder);
  const spec = inputs.find((i) => i.key === inputKey);
  if (!spec) return { ok: false, reason: `Unknown input ${inputKey}` };
  if (!spec.connectable) return { ok: false, reason: `Input ${inputKey} is not connectable` };
  const srcType = sourceNode.builder.topLevelType;
  const expected = spec.typeName || "";
  // Heuristics:
  // - FileSource can accept FetchHttpDefinition (oneof branch)
  if (/FileSource$/.test(expected)) {
    if (/FetchHttpDefinition$/.test(srcType)) return { ok: true };
    return { ok: false, reason: `FileSource expects e.g. FetchHttpDefinition` };
  }
  // - DefinitionReference is a link to another definition; allow link to any definition output
  if (/DefinitionReference$/.test(expected)) return { ok: true };
  // - Direct message type match
  if (expected.endsWith(srcType)) return { ok: true };
  return { ok: false, reason: `Type mismatch: ${srcType} -> ${expected}` };
}

// Custom node with left inputs and right output
type FieldUI = {
  key: string;
  label: string;
  type: string;
  enumValues?: { name: string; number: number }[];
  connectable: boolean;
  connectedSourceId?: string | null;
  value: any;
};

const DefNode: React.FC<any> = ({ id, data }: any) => {
  return (
    <div className="rounded-lg border border-white/10 dark:border-white/10 bg-white/90 dark:bg-zinc-800/80 text-zinc-900 dark:text-white min-w-60 shadow">
      <div className="flex items-center justify-between px-3 py-2 border-b border-white/10">
        <div className="text-[11px] uppercase tracking-wide opacity-70">Node</div>
        <div className="flex items-center gap-2">
          <button className="text-[11px] px-2 py-0.5 rounded bg-cyan-500/20 text-cyan-300 border border-cyan-400/20 hover:bg-cyan-500/30" onClick={data?.onBuild}>Build</button>
          <button className="text-[11px] px-2 py-0.5 rounded bg-red-500/20 text-red-300 border border-red-400/20 hover:bg-red-500/30" onClick={data?.onDelete}>Delete</button>
        </div>
      </div>
      <div className="px-3 py-2 text-sm font-medium break-all">{data?.label}</div>
      {/* Inline fields */}
      <div className="flex flex-col gap-2 px-3 pb-3">
        {(((data?.fields as FieldUI[]) || [])).map((f: FieldUI) => {
          if (f.connectable) {
            return (
              <div key={f.key} className="flex items-center gap-2 text-[11px]">
                <Handle id={`in:${f.key}`} type="target" position={Position.Left} />
                <span className="opacity-70 min-w-16">{f.label}</span>
                {f.connectedSourceId ? (
                  <span className="px-1.5 py-0.5 rounded bg-emerald-500/20 text-emerald-300 border border-emerald-400/20 text-[10px]">linked: {f.connectedSourceId}</span>
                ) : (
                  <span className="opacity-50 text-[10px]">connect node</span>
                )}
              </div>
            );
          }
          if (f.enumValues && f.enumValues.length > 0) {
            return (
              <label key={f.key} className="flex items-center gap-2 text-[11px]">
                <span className="opacity-70 min-w-16">{f.label}</span>
                <select className="flex-1 bg-white/5 border border-white/10 rounded px-2 py-1 text-xs" value={f.value ?? ""} onChange={(e) => data?.onChange?.(f.key, e.target.value)}>
                  {(f.enumValues || []).map((o: { name: string; number: number }) => (
                    <option key={o.name} value={o.name}>{o.name}</option>
                  ))}
                </select>
              </label>
            );
          }
          if (f.type === "TYPE_BOOL") {
            return (
              <label key={f.key} className="flex items-center gap-2 text-[11px]">
                <span className="opacity-70 min-w-16">{f.label}</span>
                <input type="checkbox" checked={!!f.value} onChange={(e) => data?.onChange?.(f.key, e.target.checked)} />
              </label>
            );
          }
          const inputType = f.type.startsWith("TYPE_INT") || f.type.startsWith("TYPE_UINT") || f.type === "TYPE_FLOAT" || f.type === "TYPE_DOUBLE" ? "number" : "text";
          return (
            <label key={f.key} className="flex items-center gap-2 text-[11px]">
              <span className="opacity-70 min-w-16">{f.label}</span>
              <input className="flex-1 bg-white/5 border border-white/10 rounded px-2 py-1 text-xs" type={inputType} value={f.value ?? ""} onChange={(e) => data?.onChange?.(f.key, inputType === 'number' ? Number(e.target.value) : e.target.value)} />
            </label>
          );
        })}
      </div>
      {/* Single output */}
      <Handle id="out" type="source" position={Position.Right} />
    </div>
  );
};

function NodeFlow({ nodes, edges, setEdges, onDrag, onSelect, onDeleteNode, onBuildNode, onChangePayload, selectedId, theme, snap }: NodeFlowProps) {
  const nodeStyle = useMemo(() => (
    theme === "dark"
      ? { background: "rgba(39,39,42,0.85)", color: "rgba(255,255,255,0.87)", border: "1px solid rgba(255,255,255,0.10)", borderRadius: 10 }
      : { background: "#ffffff", color: "#18181b", border: "1px solid rgba(0,0,0,0.08)", borderRadius: 10 }
  ), [theme]);

  // Controlled nodes for React Flow; keep in sync with props
  const [rfNodes, setRfNodes, onNodesChangeBase] = useNodesState<any>([] as any);
  useEffect(() => {
    const mapped: any[] = nodes.map((n) => {
      const top = getTopMessage(n.builder);
      const fields: FieldUI[] = (top?.field || []).map((f) => {
        const key = fieldKey(f);
        const connectable = f.type === 'TYPE_MESSAGE' && !!f.typeName && (/(FileSource|DefinitionReference|Definition)$/.test(f.typeName));
        const connectedEdge = edges.find((ed) => ed.target === n.id && ed.targetHandle === `in:${key}`);
        return {
          key,
          label: key,
          type: f.type,
          enumValues: f.type === 'TYPE_ENUM' ? enumValues(n.builder.definition, f.typeName) : undefined,
          connectable,
          connectedSourceId: connectedEdge ? connectedEdge.source : null,
          value: n.payload?.[key],
        } as FieldUI;
      });
      return {
        id: n.id,
        position: n.position,
        data: { label: n.typeName, fields, onChange: (key: string, value: any) => onChangePayload(n.id, key, value), onDelete: () => onDeleteNode(n.id), onBuild: () => onBuildNode(n.id) },
        type: "defNode",
        selected: n.id === selectedId,
        style: nodeStyle as any,
      } as RFNode;
    });
    setRfNodes(mapped as any);
  }, [nodes, edges, selectedId, nodeStyle, setRfNodes, onChangePayload]);

  const onNodeDragStop = (_: any, n: any) => {
    if (!n || !n.id || !n.position) return;
    onDrag(n.id, n.position);
  };
  const onNodeClick = (_: any, n: any) => {
    if (n?.id) onSelect(n.id);
  };

  const nodeTypes = useMemo(() => ({ defNode: DefNode as any }), []);

  const onConnect = (conn: Connection) => {
    if (!conn.source || !conn.target || !conn.targetHandle) return;
    const sourceNode = nodes.find((n) => n.id === conn.source);
    const targetNode = nodes.find((n) => n.id === conn.target);
    const inputKey = String(conn.targetHandle).replace(/^in:/, "");
    if (!sourceNode || !targetNode) return;
    const { ok } = canConnectInput(targetNode, inputKey, sourceNode);
    if (!ok) return; // reject invalid connections
    setEdges((eds) => addEdge(conn, eds));
  };

  const onNodesChange = (changes: NodeChange[]) => {
    setRfNodes((nds: any) => applyNodeChanges(changes as any, nds as any) as any);
  };

  const onEdgesChange = (changes: EdgeChange[]) => {
    setEdges((eds) => applyEdgeChanges(changes, eds));
  };

  return (
    <div className="flex-1 min-h-0">
      <div style={{ width: "100%", height: "100%" }}>
        <ReactFlow
          nodes={rfNodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeDragStop={onNodeDragStop}
          onNodeClick={onNodeClick}
          onConnect={onConnect}
          fitView
          nodeTypes={nodeTypes}
          snapToGrid
          snapGrid={[snap, snap]}
          deleteKeyCode={[]}
        >
          <Background gap={24} color={theme === "dark" ? "rgba(255,255,255,0.08)" : "rgba(0,0,0,0.08)"} />
          <Controls showInteractive={false} position="bottom-left" className="rf-controls" />
        </ReactFlow>
      </div>
    </div>
  );
}
