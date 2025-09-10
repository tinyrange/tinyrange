export function getApiBase(): string {
  const g = globalThis as any;
  if (g && typeof g.BUN_PUBLIC_API_BASE === "string") return g.BUN_PUBLIC_API_BASE as string;
  const meta = typeof document !== "undefined" ? document.querySelector('meta[name="api-base"]') : null;
  if (meta && meta instanceof HTMLMetaElement && meta.content) return meta.content;
  return "";
}

export const API_BASE = getApiBase();

import type { BuilderMetadata } from "./types";

export async function fetchBuilders(signal?: AbortSignal): Promise<BuilderMetadata[]> {
  const res = await fetch(`${API_BASE}/builders`, { headers: { Accept: "application/json" }, signal });
  if (!res.ok) throw new Error(`failed to fetch builders: ${res.status}`);
  const data = await res.json();
  if (!data || !Array.isArray(data.builders)) return [];
  return data.builders as BuilderMetadata[];
}

export async function postBuild(req: any): Promise<any> {
  const body = JSON.stringify(req);
  const res = await fetch(`${API_BASE}/build`, { method: "POST", headers: { "Content-Type": "application/json", Accept: "application/json" }, body });
  const text = await res.text();
  try {
    const data = text ? JSON.parse(text) : null;
    if (!res.ok) throw new Error(data?.error || text || res.statusText);
    return data;
  } catch (e) {
    if (!res.ok) throw new Error(text || res.statusText);
    throw e;
  }
}

