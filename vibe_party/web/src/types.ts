export type DescriptorField = {
  name: string;
  jsonName?: string;
  number: number;
  label?: string;
  type: string;
  typeName?: string;
};

export type DescriptorMessageType = {
  name: string;
  field?: DescriptorField[];
};

export type FileDescriptorProto = {
  name: string;
  package?: string;
  dependency?: string[];
  messageType?: DescriptorMessageType[];
  enumType?: { name: string; value?: { name: string; number: number }[] }[];
  syntax?: string;
};

export type BuilderMetadata = {
  typeName: string; // canonical server type name
  topLevelType: string; // top-level message type for UI
  definition: FileDescriptorProto; // descriptor for forms/ports
};

export type Point = { x: number; y: number };

export type GraphNode = {
  id: string;
  typeName: string; // display label (topLevelType)
  position: Point;
  builder: BuilderMetadata;
  payload: Record<string, any>;
};

export type Viewport = {
  offset: Point;
  scale: number;
};

