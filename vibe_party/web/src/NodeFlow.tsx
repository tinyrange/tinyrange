import React, { useEffect, useMemo } from "react";
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  Position,
  addEdge,
  applyNodeChanges,
  applyEdgeChanges,
  useNodesState,
  type Node as RFNode,
  type Edge as RFEdge,
  type NodeChange,
  type EdgeChange,
  type Connection,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { enumValues, fieldKey, getInputsFor, getTopMessage, enumCommonPrefix, enumDisplayName } from "./descriptors";
import type { GraphNode } from "./types";

type FieldUI = {
  key: string;
  label: string;
  type: string;
  enumValues?: { name: string; number: number }[];
  connectable: boolean;
  connectedSourceId?: string | null;
  value: any;
};

const DefNode: React.FC<any> = ({ data }: any) => {
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
            const common = enumCommonPrefix(f.enumValues);
            return (
              <label key={f.key} className="flex items-center gap-2 text-[11px]">
                <span className="opacity-70 min-w-16">{f.label}</span>
                <select className="defnode-select flex-1 rounded px-2 py-1 text-xs" value={f.value ?? ""} onChange={(e) => data?.onChange?.(f.key, e.target.value)}>
                  {(f.enumValues || []).map((o: { name: string; number: number }) => (
                    <option key={o.name} value={o.name}>{enumDisplayName(o.name, common)}</option>
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
              <input className="defnode-input flex-1 rounded px-2 py-1 text-xs" type={inputType} value={f.value ?? ""} onChange={(e) => data?.onChange?.(f.key, inputType === 'number' ? Number(e.target.value) : e.target.value)} />
            </label>
          );
        })}
      </div>
      <Handle id="out" type="source" position={Position.Right} />
    </div>
  );
};

export type NodeFlowProps = {
  nodes: GraphNode[];
  edges: RFEdge[];
  setEdges: React.Dispatch<React.SetStateAction<RFEdge[]>>;
  onDrag: (id: string, pos: { x: number; y: number }) => void;
  onSelect: (id: string) => void;
  onDeleteNode: (id: string) => void;
  onBuildNode: (id: string) => void;
  onChangePayload: (id: string, key: string, value: any) => void;
  selectedId: string | null;
  theme: "light" | "dark";
  snap: number;
};

export function NodeFlow({ nodes, edges, setEdges, onDrag, onSelect, onDeleteNode, onBuildNode, onChangePayload, selectedId, theme, snap }: NodeFlowProps) {
  const nodeStyle = useMemo(() => (
    theme === "dark"
      ? { background: "rgba(39,39,42,0.85)", color: "rgba(255,255,255,0.87)", border: "1px solid rgba(255,255,255,0.10)", borderRadius: 10 }
      : { background: "#ffffff", color: "#18181b", border: "1px solid rgba(0,0,0,0.08)", borderRadius: 10 }
  ), [theme]);

  const [rfNodes, setRfNodes] = useNodesState<any>([] as any);
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
  }, [nodes, edges, selectedId, nodeStyle, setRfNodes, onChangePayload, onDeleteNode, onBuildNode]);

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
    // light validation here (full validation lives in descriptors if needed)
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
