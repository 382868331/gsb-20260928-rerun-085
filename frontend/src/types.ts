export interface Dependency {
  component: string;
  constraint: string;
  optional?: boolean;
}

export interface VersionInfo {
  version: string;
  released?: string;
  dependencies?: Dependency[];
}

export interface ComponentInfo {
  name: string;
  description: string;
  versions: VersionInfo[];
}

export interface Catalog {
  components: ComponentInfo[];
}

export interface Edge {
  from: string;
  fromVersion: string;
  to: string;
  constraint: string;
  optional?: boolean;
}

export interface Change {
  component: string;
  from: string;
  to: string;
  reason: string;
  isTarget: boolean;
  isOptional?: boolean;
}

export interface Conflict {
  kind: string;
  component?: string;
  version?: string;
  constraint?: string;
  message: string;
  chain: Edge[];
}

export interface Report {
  satisfied: boolean;
  changes: Change[];
  choices: Record<string, string>;
  messages: string[];
  conflicts?: Conflict[];
  cycles?: Edge[][];
}

export interface Plan {
  id?: string;
  name: string;
  current: Record<string, string>;
  choices: Record<string, string>;
  target: string;
}
