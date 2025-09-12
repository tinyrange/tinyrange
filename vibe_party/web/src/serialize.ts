import type { BuilderMetadata, GraphNode } from "./types";
import { fieldKey, getTopMessage } from "./descriptors";
import type { Edge as RFEdge } from "@xyflow/react";
import { FieldDescriptorProto_Type as FType } from "./gen/google/protobuf/descriptor";

export function makeAny(typeName: string, pkg: string, message: Record<string, any>) {
  const typeUrl = `type.googleapis.com/${pkg}.${typeName}`;
  return { "@type": typeUrl, ...message } as const;
}

export function isProbablyBase64(s: string): boolean {
  return /^[A-Za-z0-9+/=]*$/.test(s) && s.length % 4 === 0;
}

export function stringToBase64Utf8(s: string): string {
  const bytes = new TextEncoder().encode(s);
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]!);
  return btoa(bin);
}

export function defFromNode(n: GraphNode, allNodes: GraphNode[], edges: RFEdge[]): { typeName: string; payload: any } {
  const top = getTopMessage(n.builder);
  const payload: Record<string, any> = {};
  for (const f of top?.field || []) {
    const key = fieldKey(f);
    if (f.type === FType.TYPE_MESSAGE) {
      if (/(\.proto\.FileSource|FileSource)$/.test(f.typeName || "")) {
        const e = edges.find((ed) => ed.target === n.id && ed.targetHandle === `in:${key}`);
        if (!e) continue;
        const src = allNodes.find((x) => x.id === e.source);
        if (!src) continue;
        if (/FetchHttpDefinition$/.test(src.builder.topLevelType)) {
          payload[key] = { fetchHttp: { url: String(src.payload["url"] ?? "") } };
        } else {
          throw new Error(`Unsupported source for FileSource: ${src.builder.topLevelType}`);
        }
      }
    } else if (f.type === FType.TYPE_ENUM) {
      payload[key] = n.payload[key];
    } else if (f.type === FType.TYPE_BYTES) {
      const v = n.payload[key];
      if (typeof v === "string" && v.length > 0) {
        payload[key] = isProbablyBase64(v) ? v : stringToBase64Utf8(v);
      } else if (v == null) {
        payload[key] = "";
      } else {
        const s = typeof v === "string" ? v : JSON.stringify(v);
        payload[key] = stringToBase64Utf8(s);
      }
    } else {
      payload[key] = n.payload[key];
    }
  }
  return { typeName: n.builder.typeName, payload: makeAny(n.builder.topLevelType, n.builder.definition?.package || "proto", payload) };
}

export function buildRequestForRoot(nodes: GraphNode[], edges: RFEdge[], rootId: string) {
  const root = nodes.find((n) => n.id === rootId);
  if (!root) throw new Error("root node not found");
  const rootDef = defFromNode(root, nodes, edges);
  return { closure: { root: rootDef, dependencies: [] as any[] } };
}
