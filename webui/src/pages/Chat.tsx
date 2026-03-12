import React, { useEffect, useMemo, useRef, useState } from 'react';
import { RefreshCw } from 'lucide-react';
import { motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/ui/Button';
import ChatComposer from '../components/chat/ChatComposer';
import ChatEmptyState from '../components/chat/ChatEmptyState';
import ChatMessageList from '../components/chat/ChatMessageList';
import SubagentSidebar from '../components/chat/SubagentSidebar';
import SubagentStreamFilters from '../components/chat/SubagentStreamFilters';
import {
  avatarSeed,
  avatarText,
  collectActors,
  formatAgentName,
  isUserFacingMainSession,
  messageActorKey,
  type AgentRuntimeBadge,
  type RegistryAgent,
  type RenderedChatItem,
  type RuntimeTask,
  type StreamItem,
} from '../components/chat/chatUtils';
import { useSubagentChatRuntime } from '../components/chat/useSubagentChatRuntime';
import { SelectField } from '../components/ui/FormControls';
import { ChatItem } from '../types';
import { useChatStream } from '../hooks/useChatStream';

const Chat: React.FC = () => {
  const { t } = useTranslation();
  const { q, sessions, subagentRuntimeItems, subagentRegistryItems, subagentStreamItems } = useAppContext();
  const ui = useUI();
  const [mainChat, setMainChat] = useState<RenderedChatItem[]>([]);
  const [msg, setMsg] = useState('');
  const [fileSelected, setFileSelected] = useState(false);
  const [chatTab, setChatTab] = useState<'main' | 'subagents'>('main');
  const [sessionKey, setSessionKey] = useState('');
  const [selectedStreamAgents, setSelectedStreamAgents] = useState<string[]>([]);
  const [dispatchAgentID, setDispatchAgentID] = useState('');
  const [dispatchTask, setDispatchTask] = useState('');
  const [dispatchLabel, setDispatchLabel] = useState('');
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

  const {
    loadRegistryAgents,
    loadRuntimeTasks,
    loadSubagentGroup,
    registryAgents,
    runtimeTasks,
    subagentStream,
  } = useSubagentChatRuntime({
    dispatchAgentID,
    q,
    subagentRegistryItems,
    subagentRuntimeItems,
    subagentStreamItems,
  });

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
          ? 'avatar-user'
          : role === 'tool' || role === 'exec'
            ? 'avatar-tool'
            : role === 'system'
              ? 'avatar-system'
              : 'avatar-agent';

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

  const { sendMessageStream } = useChatStream({
    q,
    onHistoryRequest: () => loadHistory(),
    setMainChat,
  });

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
      avatarClassName: 'avatar-user',
    }]);

    const currentMsg = msg;
    setMsg('');
    setFileSelected(false);
    if (input) input.value = '';

    await sendMessageStream(sessionKey, currentMsg, media);
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
  }, [q, chatTab, sessionKey, subagentRuntimeItems, subagentRegistryItems, subagentStreamItems]);

  useEffect(() => {
    if (dispatchAgentID || registryAgents.length === 0) return;
    const first = String(registryAgents[0]?.agent_id || '').trim();
    if (first) setDispatchAgentID(first);
  }, [dispatchAgentID, registryAgents]);

  const userSessions = (sessions || []).filter((s: any) => isUserFacingMainSession(s?.key));

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
        actor: formatAgentName(item.from_agent || item.agent_id, t),
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
        const actorName = formatAgentName(actorKey, t);
        let metaLine = '';

        if (item.kind === 'message') {
          const replyMeta = String(item.reply_to || '').trim() ? replyIndex.get(String(item.reply_to || '').trim()) : null;
          if (replyMeta) {
            metaLine = `${t('replyTo')}: ${replyMeta.actor}`;
          } else if (item.from_agent && item.to_agent && item.from_agent === item.to_agent) {
            metaLine = t('selfRefresh');
          } else if (item.to_agent) {
            metaLine = `${t('toAgent')}: ${formatAgentName(item.to_agent, t)}`;
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
    <div className="flex h-full min-w-0 p-4 md:p-6 xl:p-8">
      <div className="flex-1 flex flex-col brand-card ui-panel rounded-[30px] overflow-hidden">
        <div className="ui-surface-muted ui-border-subtle px-4 py-3 border-b flex items-center gap-2 min-w-0 overflow-x-auto">
          <div className="flex items-center gap-2 min-w-0 shrink-0">
            <Button onClick={() => setChatTab('main')} variant={chatTab === 'main' ? 'primary' : 'neutral'} size="xs">{t('mainChat')}</Button>
            <Button onClick={() => setChatTab('subagents')} variant={chatTab === 'subagents' ? 'primary' : 'neutral'} size="xs">{t('subagentGroup')}</Button>
          </div>
          {chatTab === 'main' && (
            <SelectField dense value={sessionKey} onChange={(e) => setSessionKey(e.target.value)} className="min-w-[220px] flex-1">
              {userSessions.map((s: any) => <option key={s.key} value={s.key}>{s.title || s.key}</option>)}
            </SelectField>
          )}
          <FixedButton
            onClick={() => {
              if (chatTab === 'main') {
                void loadHistory();
              } else {
                void loadSubagentGroup();
              }
            }}
            noShrink
            label={t('reloadHistory')}
          >
            <RefreshCw className="h-4 w-4" />
          </FixedButton>
        </div>

        {chatTab === 'subagents' && (
          <SubagentStreamFilters
            agents={streamActors}
            allAgentsLabel={t('allAgents')}
            formatAgentName={(agent) => formatAgentName(agent, t)}
            onReset={() => setSelectedStreamAgents([])}
            onToggle={toggleStreamAgent}
            selectedAgents={selectedStreamAgents}
          />
        )}

        <div className="flex-1 min-h-0 flex flex-col xl:flex-row">
          {chatTab === 'subagents' && (
            <SubagentSidebar
              agentLabel={t('agent')}
              agentsLabel={t('agents')}
              dispatchAgentID={dispatchAgentID}
              dispatchHint={t('subagentDispatchHint')}
              dispatchLabel={dispatchLabel}
              dispatchTask={dispatchTask}
              dispatchTitle={t('subagentDispatch')}
              dispatchToSubagentLabel={t('dispatchToSubagent')}
              formatAgentName={(value) => formatAgentName(value, t)}
              idleLabel={t('idle')}
              onAgentChange={setDispatchAgentID}
              onDispatch={dispatchSubagentTask}
              onLabelChange={setDispatchLabel}
              onTaskChange={setDispatchTask}
              registryAgents={registryAgents}
              runtimeBadgeByAgent={runtimeBadgeByAgent}
              subagentLabelPlaceholder={t('subagentLabelPlaceholder')}
              subagentTaskPlaceholder={t('subagentTaskPlaceholder')}
            />
          )}
          <div
            ref={chatScrollRef}
            onScroll={() => {
              shouldAutoScrollRef.current = isNearBottom();
            }}
            className="flex-1 overflow-y-auto p-4 sm:p-6 space-y-4 sm:space-y-6 min-w-0"
          >
            {displayedChat.length === 0 ? (
              <ChatEmptyState message={chatTab === 'main' ? t('startConversation') : t('noSubagentStream')} />
            ) : (
              <ChatMessageList items={displayedChat} t={t} />
            )}
            <div ref={chatEndRef} />
          </div>
        </div>

        <ChatComposer
          chatTab={chatTab}
          disabled={chatTab !== 'main'}
          fileSelected={fileSelected}
          msg={msg}
          onFileChange={(e) => setFileSelected(!!e.target.files?.[0])}
          onMsgChange={setMsg}
          onSend={send}
          placeholder={chatTab === 'main' ? t('typeMessage') : t('subagentGroupReadonly')}
        />
      </div>
    </div>
  );
};

export default Chat;
