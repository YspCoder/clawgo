import React, { useEffect, useMemo, useRef, useState } from 'react';
import { Paperclip, Send, MessageSquare, RefreshCw } from 'lucide-react';
import { motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { ChatItem } from '../types';

type StreamItem = {
  kind?: string;
  at?: number;
  task_id?: string;
  label?: string;
  agent_id?: string;
  event_type?: string;
  message?: string;
  message_type?: string;
  content?: string;
  from_agent?: string;
  to_agent?: string;
  reply_to?: string;
  message_id?: string;
  status?: string;
};

type RenderedChatItem = ChatItem & {
  id: string;
  actorKey?: string;
  actorName?: string;
  avatarText?: string;
  avatarClassName?: string;
  metaLine?: string;
  isReadonlyGroup?: boolean;
};

type RegistryAgent = {
  agent_id?: string;
  display_name?: string;
  role?: string;
  enabled?: boolean;
  transport?: string;
};

type RuntimeTask = {
  id?: string;
  agent_id?: string;
  status?: string;
  updated?: number;
  created?: number;
  waiting_for_reply?: boolean;
};

type AgentRuntimeBadge = {
  status: 'running' | 'waiting' | 'failed' | 'completed' | 'idle';
  text: string;
};

function formatAgentName(agentID?: string): string {
  const normalized = String(agentID || '').trim();
  if (!normalized) return 'Unknown Agent';
  if (normalized === 'main') return 'Main Agent';
  return normalized
    .split(/[-_.:]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function avatarSeed(key?: string): string {
  const palette = [
    'bg-emerald-600/80 text-white',
    'bg-sky-600/80 text-white',
    'bg-violet-600/80 text-white',
    'bg-amber-600/80 text-white',
    'bg-rose-600/80 text-white',
    'bg-cyan-600/80 text-white',
    'bg-fuchsia-600/80 text-white',
  ];
  const source = String(key || 'agent');
  let hash = 0;
  for (let i = 0; i < source.length; i += 1) {
    hash = (hash * 31 + source.charCodeAt(i)) | 0;
  }
  return palette[Math.abs(hash) % palette.length];
}

function avatarText(name?: string): string {
  const parts = String(name || '')
    .split(/\s+/)
    .filter(Boolean);
  if (parts.length === 0) return 'A';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return `${parts[0][0] || ''}${parts[1][0] || ''}`.toUpperCase();
}

function messageActorKey(item: StreamItem): string {
  return String(item.from_agent || item.agent_id || item.to_agent || 'subagent').trim() || 'subagent';
}

function collectActors(items: StreamItem[]): string[] {
  const set = new Set<string>();
  items.forEach((item) => {
    [item.agent_id, item.from_agent, item.to_agent].forEach((value) => {
      const v = String(value || '').trim();
      if (v) set.add(v);
    });
  });
  return Array.from(set).sort((a, b) => a.localeCompare(b));
}

const Chat: React.FC = () => {
  const { t } = useTranslation();
  const { q, sessions } = useAppContext();
  const ui = useUI();
  const [mainChat, setMainChat] = useState<RenderedChatItem[]>([]);
  const [subagentStream, setSubagentStream] = useState<StreamItem[]>([]);
  const [registryAgents, setRegistryAgents] = useState<RegistryAgent[]>([]);
  const [msg, setMsg] = useState('');
  const [fileSelected, setFileSelected] = useState(false);
  const [chatTab, setChatTab] = useState<'main' | 'subagents'>('main');
  const [sessionKey, setSessionKey] = useState('');
  const [selectedStreamAgents, setSelectedStreamAgents] = useState<string[]>([]);
  const [dispatchAgentID, setDispatchAgentID] = useState('');
  const [dispatchTask, setDispatchTask] = useState('');
  const [dispatchLabel, setDispatchLabel] = useState('');
  const [runtimeTasks, setRuntimeTasks] = useState<RuntimeTask[]>([]);
  const chatEndRef = useRef<HTMLDivElement>(null);
  const chatScrollRef = useRef<HTMLDivElement>(null);
  const shouldAutoScrollRef = useRef(true);
  const historyRequestRef = useRef(0);

  const isNearBottom = () => {
    const container = chatScrollRef.current;
    if (!container) return true;
    const threshold = 64;
    return container.scrollHeight - container.scrollTop - container.clientHeight < threshold;
  };

  const scrollToBottom = (behavior: ScrollBehavior = 'smooth') => {
    chatEndRef.current?.scrollIntoView({ behavior });
  };

  useEffect(() => {
    if (shouldAutoScrollRef.current) {
      scrollToBottom(chatTab === 'main' ? 'smooth' : 'auto');
    }
  }, [chatTab, mainChat, subagentStream]);

  const loadHistory = async (requestedSessionKey?: string) => {
    const targetSessionKey = String(requestedSessionKey || sessionKey || '').trim();
    if (!targetSessionKey) return;
    const requestID = ++historyRequestRef.current;
    try {
      shouldAutoScrollRef.current = isNearBottom() || chatTab !== 'main';
      const qs = q ? `${q}&session=${encodeURIComponent(targetSessionKey)}` : `?session=${encodeURIComponent(targetSessionKey)}`;
      const r = await fetch(`/webui/api/chat/history${qs}`);
      if (!r.ok) return;
      const j = await r.json();
      if (requestID !== historyRequestRef.current) return;
      const arr = Array.isArray(j.messages) ? j.messages : [];
      const mapped: RenderedChatItem[] = arr.map((m: any, index: number) => {
        const baseRole = String(m.role || 'assistant');
        let role: ChatItem['role'] = 'assistant';
        if (baseRole === 'user') role = 'user';
        else if (baseRole === 'tool') role = 'tool';
        else if (baseRole === 'system') role = 'system';

        let text = m.content || '';
        let label = role === 'user' ? t('user') : role === 'tool' ? t('exec') : role === 'system' ? t('system') : t('agent');

        if (Array.isArray(m.tool_calls) && m.tool_calls.length > 0) {
          role = 'exec';
          label = t('exec');
          text = `${text}\n[tool calls: ${m.tool_calls.map((x: any) => x?.function?.name || x?.name).filter(Boolean).join(', ')}]`;
        }
        if (baseRole === 'tool') {
          text = `[${t('toolOutput')}]\n${text}`;
        }

        const actorName = role === 'user' ? t('user') : role === 'tool' || role === 'exec' ? t('exec') : role === 'system' ? t('system') : t('agent');
        const avatarClassName = role === 'user'
          ? 'bg-indigo-600/90 text-white'
          : role === 'tool' || role === 'exec'
            ? 'bg-amber-600/80 text-white'
            : role === 'system'
              ? 'bg-zinc-700 text-zinc-100'
              : 'bg-emerald-600/80 text-white';

        return {
          id: `${targetSessionKey}-${index}`,
          role,
          text,
          label,
          actorName,
          avatarText: role === 'user' ? 'U' : role === 'tool' || role === 'exec' ? 'E' : role === 'system' ? 'S' : 'A',
          avatarClassName,
        };
      });
      setMainChat(mapped);
    } catch (e) {
      console.error(e);
    }
  };

  const loadSubagentGroup = async () => {
    try {
      shouldAutoScrollRef.current = isNearBottom() || chatTab !== 'subagents';
      const r = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'stream_all', limit: 300, task_limit: 36 }),
      });
      if (!r.ok) return;
      const j = await r.json();
      const arr = Array.isArray(j?.result?.items) ? j.result.items : [];
      setSubagentStream(arr);
    } catch (e) {
      console.error(e);
    }
  };

  const loadRegistryAgents = async () => {
    try {
      const r = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'registry' }),
      });
      if (!r.ok) return;
      const j = await r.json();
      const items = Array.isArray(j?.result?.items) ? j.result.items : [];
      const filtered = items.filter((item: RegistryAgent) => item?.agent_id && item.enabled !== false);
      setRegistryAgents(filtered);
      if (!dispatchAgentID && filtered.length > 0) {
        setDispatchAgentID(String(filtered[0].agent_id || ''));
      }
    } catch (e) {
      console.error(e);
    }
  };

  const loadRuntimeTasks = async () => {
    try {
      const r = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'list' }),
      });
      if (!r.ok) return;
      const j = await r.json();
      const items = Array.isArray(j?.result?.items) ? j.result.items : [];
      setRuntimeTasks(items);
    } catch (e) {
      console.error(e);
    }
  };

  async function send() {
    if (!msg.trim() && !fileSelected) return;

    let media = '';
    const input = document.getElementById('file') as HTMLInputElement | null;
    const f = input?.files?.[0];

    if (f) {
      const fd = new FormData();
      fd.append('file', f);
      try {
        const ur = await fetch(`/webui/api/upload${q}`, { method: 'POST', body: fd });
        const uj = await ur.json();
        media = uj.path || '';
      } catch (e) {
        console.error('L0053', e);
      }
    }

    const userText = msg + (media ? `\n[Attached File: ${f?.name}]` : '');
    shouldAutoScrollRef.current = true;
    setMainChat((prev) => [...prev, {
      id: `local-user-${Date.now()}`,
      role: 'user',
      text: userText,
      label: t('user'),
      actorName: t('user'),
      avatarText: 'U',
      avatarClassName: 'bg-indigo-600/90 text-white',
    }]);

    const currentMsg = msg;
    setMsg('');
    setFileSelected(false);
    if (input) input.value = '';

    try {
      const response = await fetch(`/webui/api/chat/stream${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session: sessionKey, message: currentMsg, media }),
      });

      if (!response.ok || !response.body) throw new Error('Chat request failed');

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantText = '';

      setMainChat((prev) => [...prev, {
        id: `local-assistant-${Date.now()}`,
        role: 'assistant',
        text: '',
        label: t('agent'),
        actorName: t('agent'),
        avatarText: 'A',
        avatarClassName: 'bg-emerald-600/80 text-white',
      }]);

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        const chunk = decoder.decode(value, { stream: true });
        assistantText += chunk;
        setMainChat((prev) => {
          const next = [...prev];
          next[next.length - 1] = {
            ...next[next.length - 1],
            text: assistantText,
          };
          return next;
        });
      }

      loadHistory();
    } catch (e) {
      setMainChat((prev) => [...prev, {
        id: `local-system-${Date.now()}`,
        role: 'system',
        text: t('chatServerError'),
        label: t('system'),
        actorName: t('system'),
        avatarText: 'S',
        avatarClassName: 'bg-zinc-700 text-zinc-100',
      }]);
    }
  }

  useEffect(() => {
    shouldAutoScrollRef.current = true;
    if (chatTab === 'main') {
      if (sessionKey) {
        loadHistory(sessionKey);
      }
      return;
    }
    loadSubagentGroup();
    loadRegistryAgents();
    loadRuntimeTasks();
  }, [q, chatTab, sessionKey]);

  useEffect(() => {
    if (chatTab !== 'subagents') return;
    const timer = window.setInterval(() => {
      loadSubagentGroup();
      loadRuntimeTasks();
    }, 5000);
    return () => window.clearInterval(timer);
  }, [q, chatTab]);

  const userSessions = (sessions || []).filter((s: any) => !String(s?.key || '').startsWith('subagent:'));

  useEffect(() => {
    if (chatTab !== 'main') return;
    if (!userSessions.length) return;
    if (!sessionKey || !userSessions.some((s: any) => s.key === sessionKey)) {
      setSessionKey(userSessions[0].key);
    }
  }, [chatTab, sessionKey, userSessions]);

  const streamActors = useMemo(() => collectActors(subagentStream), [subagentStream]);

  useEffect(() => {
    setSelectedStreamAgents((prev) => prev.filter((agent) => streamActors.includes(agent)));
  }, [streamActors]);

  const renderedSubagentChat = useMemo<RenderedChatItem[]>(() => {
    const replyIndex = new Map<string, { actor: string; messageType: string }>();
    subagentStream.forEach((item) => {
      const messageID = String(item.message_id || '').trim();
      if (!messageID) return;
      replyIndex.set(messageID, {
        actor: formatAgentName(item.from_agent || item.agent_id),
        messageType: String(item.message_type || 'message'),
      });
    });

    return subagentStream
      .filter((item) => {
        if (selectedStreamAgents.length === 0) return true;
        const actors = [item.agent_id, item.from_agent, item.to_agent]
          .map((value) => String(value || '').trim())
          .filter(Boolean);
        return actors.some((actor) => selectedStreamAgents.includes(actor));
      })
      .map((item, index) => {
        const actorKey = messageActorKey(item);
        const actorName = formatAgentName(actorKey);
        let metaLine = '';

        if (item.kind === 'message') {
          const replyMeta = String(item.reply_to || '').trim() ? replyIndex.get(String(item.reply_to || '').trim()) : null;
          if (replyMeta) {
            metaLine = `${t('replyTo')}: ${replyMeta.actor}`;
          } else if (item.from_agent && item.to_agent && item.from_agent === item.to_agent) {
            metaLine = t('selfRefresh');
          } else if (item.to_agent) {
            metaLine = `${t('toAgent')}: ${formatAgentName(item.to_agent)}`;
          }
          if (item.message_type) {
            metaLine = metaLine ? `${metaLine} · ${item.message_type}` : String(item.message_type);
          }
        } else {
          metaLine = item.event_type === 'resumed' || item.event_type === 'recovered'
            ? t('selfRefresh')
            : String(item.event_type || t('internalEvent'));
        }

        const text = item.kind === 'event' ? (item.message || '') : (item.content || '');
        const label = item.kind === 'event'
          ? `${actorName} · ${item.event_type || t('internalEvent')}`
          : actorName;

        return {
          id: `${item.kind || 'item'}-${item.message_id || item.task_id || item.at || index}-${index}`,
          role: 'assistant',
          text: text || t('empty'),
          label,
          actorKey,
          actorName,
          avatarText: avatarText(actorName),
          avatarClassName: avatarSeed(actorKey),
          metaLine,
          isReadonlyGroup: true,
        };
      });
  }, [selectedStreamAgents, subagentStream, t]);

  const displayedChat = chatTab === 'main' ? mainChat : renderedSubagentChat;

  const runtimeBadgeByAgent = useMemo<Record<string, AgentRuntimeBadge>>(() => {
    const latest = new Map<string, RuntimeTask>();
    runtimeTasks.forEach((task) => {
      const agentID = String(task.agent_id || '').trim();
      if (!agentID) return;
      const current = latest.get(agentID);
      const taskTime = Math.max(Number(task.updated || 0), Number(task.created || 0));
      const currentTime = current ? Math.max(Number(current.updated || 0), Number(current.created || 0)) : 0;
      if (!current || taskTime >= currentTime) {
        latest.set(agentID, task);
      }
    });
    const out: Record<string, AgentRuntimeBadge> = {};
    latest.forEach((task, agentID) => {
      if (task.waiting_for_reply) {
        out[agentID] = { status: 'waiting', text: t('statusWaiting') };
        return;
      }
      switch (String(task.status || '').trim()) {
        case 'running':
          out[agentID] = { status: 'running', text: t('statusRunning') };
          break;
        case 'failed':
          out[agentID] = { status: 'failed', text: t('statusError') };
          break;
        case 'completed':
          out[agentID] = { status: 'completed', text: t('statusSuccess') };
          break;
        default:
          out[agentID] = { status: 'idle', text: t('idle') };
      }
    });
    return out;
  }, [runtimeTasks, t]);

  const dispatchSubagentTask = async () => {
    const task = dispatchTask.trim();
    const agentID = dispatchAgentID.trim();
    if (!task || !agentID) return;
    await ui.withLoading(async () => {
      const r = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          action: 'spawn',
          agent_id: agentID,
          task,
          label: dispatchLabel.trim(),
          origin_channel: 'webui',
          origin_chat_id: 'subagent-group',
        }),
      });
      if (!r.ok) {
        throw new Error(await r.text());
      }
      setDispatchTask('');
      setDispatchLabel('');
      await Promise.all([loadSubagentGroup(), loadRegistryAgents(), loadRuntimeTasks()]);
      await ui.notify({ title: t('saved'), message: t('subagentTaskDispatched') });
    }, t('creating')).catch(async (err) => {
      await ui.notify({ title: t('requestFailed'), message: err instanceof Error ? err.message : String(err) });
    });
  };

  const toggleStreamAgent = (agent: string) => {
    setSelectedStreamAgents((prev) => (
      prev.includes(agent) ? prev.filter((item) => item !== agent) : [...prev, agent]
    ));
  };

  return (
    <div className="flex h-full min-w-0">
      <div className="flex-1 flex flex-col bg-zinc-950/50">
        <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-2 flex-wrap min-w-0">
            <button
              onClick={() => setChatTab('main')}
              className={`px-3 py-1.5 rounded-lg text-xs ${chatTab === 'main' ? 'bg-indigo-600 text-white' : 'bg-zinc-900 border border-zinc-700 text-zinc-300'}`}
            >
              {t('mainChat')}
            </button>
            <button
              onClick={() => setChatTab('subagents')}
              className={`px-3 py-1.5 rounded-lg text-xs ${chatTab === 'subagents' ? 'bg-amber-600 text-white' : 'bg-zinc-900 border border-zinc-700 text-zinc-300'}`}
            >
              {t('subagentGroup')}
            </button>
            {chatTab === 'main' && (
              <select value={sessionKey} onChange={(e) => setSessionKey(e.target.value)} className="max-w-full bg-zinc-900 border border-zinc-700 rounded px-2 py-1 text-xs text-zinc-200">
                {userSessions.map((s: any) => <option key={s.key} value={s.key}>{s.title || s.key}</option>)}
              </select>
            )}
          </div>
          <button onClick={chatTab === 'main' ? loadHistory : loadSubagentGroup} className="flex items-center gap-1 px-2 py-1 text-xs rounded bg-zinc-800 hover:bg-zinc-700"><RefreshCw className="w-3 h-3" />{t('reloadHistory')}</button>
        </div>

        {chatTab === 'subagents' && (
          <div className="px-4 py-3 border-b border-zinc-800 bg-zinc-950/40 flex flex-wrap gap-2">
            <button
              onClick={() => setSelectedStreamAgents([])}
              className={`px-2.5 py-1 rounded-full text-xs border ${selectedStreamAgents.length === 0 ? 'bg-amber-600 text-white border-amber-500' : 'bg-zinc-900 border-zinc-700 text-zinc-300'}`}
            >
              {t('allAgents')}
            </button>
            {streamActors.map((agent) => (
              <button
                key={agent}
                onClick={() => toggleStreamAgent(agent)}
                className={`px-2.5 py-1 rounded-full text-xs border ${selectedStreamAgents.includes(agent) ? 'bg-zinc-100 text-zinc-950 border-zinc-100' : 'bg-zinc-900 border-zinc-700 text-zinc-300'}`}
              >
                {formatAgentName(agent)}
              </button>
            ))}
          </div>
        )}

        <div className="flex-1 min-h-0 flex flex-col xl:flex-row">
          {chatTab === 'subagents' && (
            <div className="w-full xl:w-[320px] xl:shrink-0 border-b xl:border-b-0 xl:border-r border-zinc-800 bg-zinc-950/70 p-4 flex flex-col gap-4 max-h-[46vh] xl:max-h-none overflow-y-auto">
              <div>
                <div className="text-xs uppercase tracking-wider text-zinc-500 mb-1">{t('subagentDispatch')}</div>
                <div className="text-sm text-zinc-300">{t('subagentDispatchHint')}</div>
              </div>
              <div className="space-y-3">
                <select
                  value={dispatchAgentID}
                  onChange={(e) => setDispatchAgentID(e.target.value)}
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-xl px-3 py-2 text-sm text-zinc-200"
                >
                  {registryAgents.map((agent) => (
                    <option key={agent.agent_id} value={agent.agent_id}>
                      {formatAgentName(agent.display_name || agent.agent_id)} · {agent.role || '-'}
                    </option>
                  ))}
                </select>
                <textarea
                  value={dispatchTask}
                  onChange={(e) => setDispatchTask(e.target.value)}
                  placeholder={t('subagentTaskPlaceholder')}
                  className="w-full min-h-[180px] resize-none bg-zinc-900 border border-zinc-800 rounded-xl px-3 py-3 text-sm text-zinc-200 placeholder:text-zinc-500 focus:outline-none focus:border-amber-500"
                />
                <input
                  value={dispatchLabel}
                  onChange={(e) => setDispatchLabel(e.target.value)}
                  placeholder={t('subagentLabelPlaceholder')}
                  className="w-full bg-zinc-900 border border-zinc-800 rounded-xl px-3 py-2 text-sm text-zinc-200 placeholder:text-zinc-500 focus:outline-none focus:border-amber-500"
                />
                <button
                  onClick={dispatchSubagentTask}
                  disabled={!dispatchAgentID.trim() || !dispatchTask.trim()}
                  className="w-full px-3 py-2 rounded-xl bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-white text-sm font-medium"
                >
                  {t('dispatchToSubagent')}
                </button>
              </div>
              <div className="border-t border-zinc-800 pt-4 min-h-0 flex flex-col">
                <div className="text-xs uppercase tracking-wider text-zinc-500 mb-2">{t('agents')}</div>
                <div className="overflow-y-auto space-y-2 min-h-0">
                  {registryAgents.map((agent) => {
                    const active = dispatchAgentID === agent.agent_id;
                    const badge = runtimeBadgeByAgent[String(agent.agent_id || '')];
                    const badgeClass = badge?.status === 'running'
                      ? 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30'
                      : badge?.status === 'waiting'
                        ? 'bg-amber-500/15 text-amber-300 border-amber-500/30'
                        : badge?.status === 'failed'
                          ? 'bg-rose-500/15 text-rose-300 border-rose-500/30'
                          : badge?.status === 'completed'
                            ? 'bg-sky-500/15 text-sky-300 border-sky-500/30'
                            : 'bg-zinc-800 text-zinc-400 border-zinc-700';
                    return (
                      <button
                        key={agent.agent_id}
                        onClick={() => setDispatchAgentID(String(agent.agent_id || ''))}
                        className={`w-full text-left rounded-xl border px-3 py-2 ${active ? 'border-amber-500 bg-amber-500/10' : 'border-zinc-800 bg-zinc-900/70 hover:bg-zinc-900'}`}
                      >
                        <div className="flex items-center justify-between gap-2">
                          <div className="text-sm font-medium text-zinc-100">{formatAgentName(agent.display_name || agent.agent_id)}</div>
                          <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-[10px] ${badgeClass}`}>{badge?.text || t('idle')}</span>
                        </div>
                        <div className="text-xs text-zinc-500">{agent.agent_id} · {agent.role || '-'}</div>
                      </button>
                    );
                  })}
                </div>
              </div>
            </div>
          )}
          <div
            ref={chatScrollRef}
            onScroll={() => {
              shouldAutoScrollRef.current = isNearBottom();
            }}
            className="flex-1 overflow-y-auto p-4 sm:p-6 space-y-4 sm:space-y-6 min-w-0"
          >
            {displayedChat.length === 0 ? (
              <div className="h-full flex flex-col items-center justify-center text-zinc-500 space-y-4">
                <div className="w-16 h-16 rounded-2xl bg-zinc-900 flex items-center justify-center border border-zinc-800">
                  <MessageSquare className="w-8 h-8 text-zinc-600" />
                </div>
                <p className="text-sm font-medium">{chatTab === 'main' ? t('startConversation') : t('noSubagentStream')}</p>
              </div>
            ) : (
              displayedChat.map((m, i) => {
                const isUser = m.role === 'user';
                const isExec = m.role === 'tool' || m.role === 'exec';
                const isSystem = m.role === 'system';
                const bubbleClass = isUser
                  ? 'bg-indigo-600 text-white rounded-br-sm'
                  : isExec
                    ? 'bg-amber-500/10 text-amber-100 rounded-bl-sm border border-amber-500/30'
                    : isSystem
                      ? 'bg-zinc-700/40 text-zinc-100 rounded-bl-sm border border-zinc-600/40'
                      : m.isReadonlyGroup
                        ? 'bg-zinc-900/85 text-zinc-200 rounded-bl-sm border border-zinc-700/60'
                        : 'bg-zinc-800/80 text-zinc-200 rounded-bl-sm border border-zinc-700/50';

                return (
                  <motion.div
                    initial={{ opacity: 0, y: 10 }}
                    animate={{ opacity: 1, y: 0 }}
                    key={m.id || i}
                    className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}
                  >
                    <div className={`flex items-start gap-2 max-w-full sm:max-w-[96%] ${isUser ? 'flex-row-reverse' : 'flex-row'}`}>
                      <div className={`w-9 h-9 mt-1 rounded-full text-[11px] font-bold flex items-center justify-center shrink-0 ${m.avatarClassName || (isUser ? 'bg-indigo-600/90 text-white' : 'bg-emerald-600/80 text-white')}`}>{m.avatarText || (isUser ? 'U' : 'A')}</div>
                      <div className={`max-w-[calc(100vw-6rem)] sm:max-w-[92%] rounded-2xl px-4 py-3 shadow-sm ${bubbleClass}`}>
                        <div className="flex items-center justify-between gap-3 mb-1">
                          <div className="text-[11px] opacity-85">{m.actorName || m.label || (isUser ? t('user') : isExec ? t('exec') : isSystem ? t('system') : t('agent'))}</div>
                          {m.metaLine && <div className="text-[11px] text-zinc-400">{m.metaLine}</div>}
                        </div>
                        {m.label && m.actorName && m.label !== m.actorName && (
                          <div className="text-[11px] text-zinc-500 mb-2">{m.label}</div>
                        )}
                        <p className="whitespace-pre-wrap text-[14px] leading-relaxed">{m.text}</p>
                      </div>
                    </div>
                  </motion.div>
                );
              })
            )}
            <div ref={chatEndRef} />
          </div>
        </div>

        <div className="p-3 sm:p-4 bg-zinc-950 border-t border-zinc-800">
          <div className="w-full relative flex items-center">
            <input
              type="file"
              id="file"
              className="hidden"
              onChange={(e) => setFileSelected(!!e.target.files?.[0])}
            />
            <label
              htmlFor="file"
              className={`absolute left-3 p-2 rounded-full cursor-pointer transition-colors ${fileSelected ? 'text-indigo-400 bg-indigo-500/10' : 'text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200'}`}
            >
              <Paperclip className="w-5 h-5" />
            </label>
            <input
              value={msg}
              onChange={(e) => setMsg(e.target.value)}
              onKeyDown={(e) => chatTab === 'main' && e.key === 'Enter' && send()}
              placeholder={chatTab === 'main' ? t('typeMessage') : t('subagentGroupReadonly')}
              disabled={chatTab !== 'main'}
              className="w-full bg-zinc-900 border border-zinc-800 rounded-full pl-14 pr-14 py-3.5 text-[15px] focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-all placeholder:text-zinc-500 shadow-sm disabled:opacity-60"
            />
            <button
              onClick={send}
              disabled={chatTab !== 'main' || (!msg.trim() && !fileSelected)}
              className="absolute right-2 p-2.5 bg-indigo-600 hover:bg-indigo-500 disabled:opacity-50 disabled:hover:bg-indigo-600 text-white rounded-full transition-colors shadow-sm"
            >
              <Send className="w-4 h-4 ml-0.5" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default Chat;
