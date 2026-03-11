export type AgentTaskStats = {
  total: number;
  running: number;
  failed: number;
  waiting: number;
  latestStatus: string;
  latestUpdated: number;
  active: Array<{ id: string; status: string; title: string }>;
};

type TaskLike = {
  id: string;
  agent_id?: string;
  status?: string;
  label?: string;
  created?: number;
  updated?: number;
  task?: string;
  waiting_for_reply?: boolean;
};

export function normalizeTitle(value?: string, fallback = '-'): string {
  const trimmed = `${value || ''}`.trim();
  return trimmed || fallback;
}

export function summarizeTask(task?: string, label?: string): string {
  const text = normalizeTitle(label || task, '-');
  return text.length > 52 ? `${text.slice(0, 49)}...` : text;
}

export function formatStreamTime(ts?: number): string {
  if (!ts) return '--:--:--';
  return new Date(ts).toLocaleTimeString([], { hour12: false });
}

export function formatRuntimeTimestamp(value?: string): string {
  const raw = `${value || ''}`.trim();
  if (!raw || raw === '0001-01-01T00:00:00Z') return '-';
  const ts = Date.parse(raw);
  if (Number.isNaN(ts)) return raw;
  return new Date(ts).toLocaleString();
}

export function summarizePreviewText(value?: string, limit = 180): string {
  const compact = `${value || ''}`.replace(/\s+/g, ' ').trim();
  if (!compact) return '(empty)';
  return compact.length > limit ? `${compact.slice(0, limit - 3)}...` : compact;
}

export function tokenFromQuery(q: string): string {
  const raw = String(q || '').trim();
  if (!raw) return '';
  const search = raw.startsWith('?') ? raw.slice(1) : raw;
  const params = new URLSearchParams(search);
  return params.get('token') || '';
}

export function bezierCurve(x1: number, y1: number, x2: number, y2: number): string {
  const offset = Math.max(Math.abs(y2 - y1) * 0.5, 60);
  return `M ${x1} ${y1} C ${x1} ${y1 + offset} ${x2} ${y2 - offset} ${x2} ${y2}`;
}

export function horizontalBezierCurve(x1: number, y1: number, x2: number, y2: number): string {
  const offset = Math.max(Math.abs(x2 - x1) * 0.5, 60);
  return `M ${x1} ${y1} C ${x1 + offset} ${y1} ${x2 - offset} ${y2} ${x2} ${y2}`;
}

export function buildTaskStats(tasks: TaskLike[]): Record<string, AgentTaskStats> {
  return tasks.reduce<Record<string, AgentTaskStats>>((acc, task) => {
    const agentID = normalizeTitle(task.agent_id, '');
    if (!agentID) return acc;
    if (!acc[agentID]) {
      acc[agentID] = { total: 0, running: 0, failed: 0, waiting: 0, latestStatus: '', latestUpdated: 0, active: [] };
    }
    const item = acc[agentID];
    item.total += 1;
    if (task.status === 'running') item.running += 1;
    if (task.waiting_for_reply) item.waiting += 1;
    const updatedAt = Math.max(task.updated || 0, task.created || 0);
    if (updatedAt >= item.latestUpdated) {
      item.latestUpdated = updatedAt;
      item.latestStatus = normalizeTitle(task.status, '');
      item.failed = task.status === 'failed' ? 1 : 0;
    }
    if (task.status === 'running' || task.waiting_for_reply) {
      item.active.push({
        id: task.id,
        status: task.status || '-',
        title: summarizeTask(task.task, task.label),
      });
    }
    return acc;
  }, {});
}

export function getTopologyTooltipPosition(clientX: number, clientY: number, tooltipWidth = 360, tooltipHeight = 420) {
  let x = clientX + 14;
  let y = clientY + 14;

  if (x + tooltipWidth > window.innerWidth) {
    x = clientX - tooltipWidth - 14;
  }
  if (y + tooltipHeight > window.innerHeight) {
    y = clientY - tooltipHeight - 14;
  }

  return { x, y };
}
