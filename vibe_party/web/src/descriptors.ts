import type { BuilderMetadata, DescriptorField, DescriptorMessageType, FileDescriptorProto } from "./types";

export function getTopMessage(meta: BuilderMetadata): DescriptorMessageType | null {
  const mt = meta.definition?.messageType || [];
  return mt.find((m) => m.name === meta.topLevelType) || null;
}

export function fieldKey(f: DescriptorField): string {
  return f.jsonName || f.name;
}

export function enumValues(def: FileDescriptorProto, typeName?: string): { name: string; number: number }[] {
  if (!typeName) return [];
  const parts = typeName.split(".").filter(Boolean);
  const name = parts[parts.length - 1];
  const e = def.enumType?.find((x) => x.name === name);
  return e?.value || [];
}

// Compute a common ALL-CAPS underscore prefix across enum value names, trimmed to last underscore
export function enumCommonPrefix(values: { name: string }[]): string {
  if (!values.length) return "";
  let prefix = values[0]!.name;
  for (let i = 1; i < values.length; i++) {
    const n = values[i]!.name;
    let j = 0;
    const max = Math.min(prefix.length, n.length);
    while (j < max && prefix.charAt(j) === n.charAt(j)) j++;
    prefix = prefix.slice(0, j);
    if (!prefix) break;
  }
  const k = prefix.lastIndexOf("_");
  return k >= 0 ? prefix.slice(0, k + 1) : ""; // include trailing underscore if present
}

export function toTitleCaseFromUnderscore(s: string): string {
  return s
    .toLowerCase()
    .split("_")
    .filter(Boolean)
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(" ");
}

export function enumDisplayName(raw: string, commonPrefix: string): string {
  const trimmed = commonPrefix && raw.startsWith(commonPrefix) ? raw.slice(commonPrefix.length) : raw;
  return toTitleCaseFromUnderscore(trimmed);
}

export function defaultForField(def: FileDescriptorProto, f: DescriptorField): any {
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
      return vals[0]?.name || "";
    }
    case "TYPE_MESSAGE":
    default:
      return null;
  }
}

export function getInputsFor(meta: BuilderMetadata): { key: string; label: string; typeName?: string; connectable: boolean }[] {
  const top = getTopMessage(meta);
  const fields = top?.field || [];
  return fields.map((f) => {
    const typeName = f.typeName;
    const isMsg = f.type === "TYPE_MESSAGE";
    const connectable = isMsg && !!typeName && /(FileSource|DefinitionReference|Definition)$/.test(typeName);
    return { key: fieldKey(f), label: fieldKey(f), typeName, connectable };
  });
}

export function canConnectInput(targetMeta: BuilderMetadata, inputKey: string, sourceTopLevelType: string): { ok: boolean; reason?: string } {
  const inputs = getInputsFor(targetMeta);
  const spec = inputs.find((i) => i.key === inputKey);
  if (!spec) return { ok: false, reason: `Unknown input ${inputKey}` };
  if (!spec.connectable) return { ok: false, reason: `Input ${inputKey} is not connectable` };
  const expected = spec.typeName || "";
  if (/FileSource$/.test(expected)) {
    if (/FetchHttpDefinition$/.test(sourceTopLevelType)) return { ok: true };
    return { ok: false, reason: `FileSource expects e.g. FetchHttpDefinition` };
  }
  if (/DefinitionReference$/.test(expected)) return { ok: true };
  if (expected.endsWith(sourceTopLevelType)) return { ok: true };
  return { ok: false, reason: `Type mismatch: ${sourceTopLevelType} -> ${expected}` };
}
