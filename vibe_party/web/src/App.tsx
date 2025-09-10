import "./index.css";
import { useEffect, useState } from "react";
import { NodeFlow } from "./NodeFlow";
import { Palette } from "./Palette";
import { API_BASE, fetchBuilders, postBuild } from "./api";
import type { BuilderMetadata, GraphNode, Point, Viewport } from "./types";
import { defaultForField, getTopMessage, fieldKey } from "./descriptors";
import { buildRequestForRoot } from "./serialize";
import type { Edge as RFEdge } from "@xyflow/react";

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

export function App() {
  const { builders, error, loading } = useBuilders();
  const [nodes, setNodes] = useState<GraphNode[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [viewport, setViewport] = useState<Viewport>({ offset: { x: 0, y: 0 }, scale: 1 });
  const [snap, setSnap] = useState<number | null>(24);
  const [edges, setEdges] = useState<RFEdge[]>([]);
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

  async function buildFromNode(rootId: string) {
    try {
      const req = buildRequestForRoot(nodes, edges, rootId);
      await postBuild(req);
    } catch (e) {
      alert(`Build failed: ${e}`);
    }
  }

  function deleteNode(id: string) {
    if (!confirm("Delete this node?")) return;
    setNodes((cur) => cur.filter((n) => n.id !== id));
    setEdges((eds) => eds.filter((e) => e.source !== id && e.target !== id));
    if (selectedId === id) setSelectedId(null);
  }

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

