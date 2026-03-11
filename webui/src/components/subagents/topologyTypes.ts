export type GraphAccentTone = 'success' | 'danger' | 'warning' | 'info' | 'accent' | 'neutral';

export type GraphCardSpec = {
  key: string;
  branch: string;
  agentID?: string;
  transportType?: 'local' | 'remote';
  x: number;
  y: number;
  w: number;
  h: number;
  kind: 'node' | 'agent';
  title: string;
  subtitle: string;
  meta: string[];
  accentTone: GraphAccentTone;
  online?: boolean;
  clickable?: boolean;
  highlighted?: boolean;
  dimmed?: boolean;
  hidden?: boolean;
  scale: number;
  onClick?: () => void;
};

export type GraphLineSpec = {
  path: string;
  dashed?: boolean;
  branch: string;
  highlighted?: boolean;
  dimmed?: boolean;
  hidden?: boolean;
};
