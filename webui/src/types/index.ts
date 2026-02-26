export type ChatRole = 'user' | 'assistant' | 'tool' | 'system' | 'exec';
export type ChatItem = { role: ChatRole; text: string; label?: string };
export type Session = { key: string; title: string };
export type CronJob = { 
  id: string; 
  name: string; 
  enabled: boolean;
  kind?: string;
  everyMs?: number;
  expr?: string;
  message?: string;
  deliver?: boolean;
  channel?: string;
  to?: string;
};
export type Cfg = Record<string, any>;
export type View = 'dashboard' | 'chat' | 'config' | 'cron' | 'nodes' | 'memory';
export type Lang = 'en' | 'zh';

export type LogEntry = {
  time: string;
  level: string;
  msg: string;
  [key: string]: any;
};

export type Skill = {
  id: string;
  name: string;
  description: string;
  tools: string[];
  system_prompt?: string;
};
