export interface Health {
  status: "healthy" | "degraded" | "unreachable" | "unknown";
  version?: string;
  platform?: string;
  distribution?: string;
  latencyMs: number;
  checkedAt: string;
  error?: string;
  nodes: number;
  nodesReady: number;
  namespaces: number;
  pods: number;
  podsRunning: number;
  cpuCapacityMilli: number;
  memoryCapacityBytes: number;
  certExpiry?: string;
  certWarning?: string;
  cached?: boolean;
}

export interface Cluster {
  id: string;
  name: string;
  context: string;
  source: string;
  server: string;
  user: string;
  namespace: string;
  authType: string;
  group: string;
  favorite: boolean;
  color?: string;
  tags?: string[];
  lastOpened?: string;
  health?: Health;
}

export interface APIResource {
  group: string;
  version: string;
  resource: string;
  kind: string;
  namespaced: boolean;
  verbs: string[];
  shortNames?: string[];
  categories?: string[];
  custom: boolean;
  section: string;
}

export interface Row {
  uid: string;
  name: string;
  namespace?: string;
  kind: string;
  created: string;
  status: string;
  health: "ok" | "warn" | "error" | "neutral";
  cells: Record<string, string>;
  labels?: Record<string, string>;
  rv: string;
}

export interface LogLine {
  pod: string;
  container: string;
  time?: string;
  line: string;
}
