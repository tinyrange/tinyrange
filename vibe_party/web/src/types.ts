// Re-export protobuf types used by the UI
export type { BuilderMetadata } from "./gen/build/proto/build";
export type {
  FileDescriptorProto,
  DescriptorProto as DescriptorMessageType,
  FieldDescriptorProto as DescriptorField,
  FieldDescriptorProto_Type as DescriptorFieldType,
} from "./gen/google/protobuf/descriptor";

export type Point = { x: number; y: number };

export type GraphNode = {
  id: string;
  typeName: string; // display label (topLevelType)
  position: Point;
  builder: import("./gen/build/proto/build").BuilderMetadata;
  payload: Record<string, any>;
};

export type Viewport = {
  offset: Point;
  scale: number;
};
