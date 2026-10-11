export interface User {
  id: string;
  username: string;
  role: string;
}

export interface LbNode {
  id: string;
  name: string;
  address: string;
  sshPort: number;
  sshUser: string;
  authType: "password" | "key";
  fingerprint: string;
  os: string;
  status: string;
  lastError: string;
  lastProbeAt?: string;
  createdAt: string;
}

export interface Member {
  nodeId: string;
  nodeName: string;
  address: string;
  priority: number;
  role: "both" | "lvs" | "nginx";
}

export interface Cluster {
  id: string;
  name: string;
  vip: string;
  prefix: number;
  vrid: number;
  interface: string;
  advertInt: number;
  layout: "combined" | "split";
  hasAuth: boolean;
  members: Member[];
  createdAt: string;
}

export interface Server {
  address: string;
  port: number;
  weight: number;
  maxFails: number;
  failTimeout: number;
  backup: boolean;
  down: boolean;
}

export interface Upstream {
  id: string;
  clusterId: string;
  name: string;
  layer: "http" | "stream";
  method: string;
  servers: Server[];
  createdAt: string;
}

export interface Site {
  id: string;
  clusterId: string;
  name: string;
  listenPort: number;
  serverName: string;
  tls: boolean;
  certificateId: string;
  upstreamId: string;
  webSocket: boolean;
  createdAt: string;
}

export interface StreamProxy {
  id: string;
  clusterId: string;
  name: string;
  protocol: "tcp" | "udp";
  listenPort: number;
  upstreamId: string;
  createdAt: string;
}

export interface Certificate {
  id: string;
  clusterId: string;
  name: string;
  subject: string;
  fingerprint: string;
  dnsNames: string;
  notAfter: string;
  createdAt: string;
}

export interface Job {
  id: string;
  clusterId: string;
  revisionId?: string;
  action: string;
  status: string;
  log: string;
  createdBy: string;
  createdAt: string;
  finishedAt?: string;
}

export interface Revision {
  id: string;
  clusterId: string;
  hash: string;
  status: string;
  note: string;
  createdBy: string;
  createdAt: string;
}

export interface ConfigFile {
  path: string;
  nodeId?: string;
  content: string;
  mode: number;
}

export interface Bundle {
  files: ConfigFile[];
  hash: string;
}

export interface RevisionDetail extends Revision {
  files: ConfigFile[];
}
