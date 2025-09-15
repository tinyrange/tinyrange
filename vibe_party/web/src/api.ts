export function getApiBase(): string {
  const g = globalThis as any;
  if (g && typeof g.BUN_PUBLIC_API_BASE === "string") return g.BUN_PUBLIC_API_BASE as string;
  const meta = typeof document !== "undefined" ? document.querySelector('meta[name="api-base"]') : null;
  if (meta && meta instanceof HTMLMetaElement && meta.content) return meta.content;
  return "";
}

export const API_BASE = getApiBase();

import type { BuilderMetadata } from "./types";
import { BuilderList, BuildReceipt } from "./gen/build/proto/build";

export async function fetchBuilders(signal?: AbortSignal): Promise<BuilderMetadata[]> {
  const res = await fetch(`${API_BASE}/builders`, { headers: { Accept: "application/protobuf" }, signal });
  if (!res.ok) throw new Error(`failed to fetch builders: ${res.status}`);
  const buf = new Uint8Array(await res.arrayBuffer());
  const list = BuilderList.decode(buf);
  return (list.builders ?? []) as BuilderMetadata[];
}

export async function postBuild(req: any): Promise<BuildReceipt | any> {
  // We send JSON for the request body because the Definition payload is dynamic
  // and represented via google.protobuf.Any using the JSON "@type" form.
  const body = JSON.stringify(req);
  const res = await fetch(`${API_BASE}/build`, {
    method: "POST",
    headers: { "Content-Type": "application/json", Accept: "application/protobuf" },
    body,
  });
  if (!res.ok) {
    // Try to parse error text for better message
    const text = await res.text();
    throw new Error(text || res.statusText);
  }
  // Try to decode a protobuf BuildReceipt if provided
  const buf = new Uint8Array(await res.arrayBuffer());
  try {
    return BuildReceipt.decode(buf);
  } catch {
    // Fallback: if server returned JSON (unexpected), try to parse it
    try {
      const text = new TextDecoder().decode(buf);
      return text ? JSON.parse(text) : null;
    } catch {
      return null;
    }
  }
}
