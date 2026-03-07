import React, { useState, useRef, useEffect } from 'react';
import { Paperclip, Send, MessageSquare, RefreshCw } from 'lucide-react';
import { motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
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
  status?: string;
};

const Chat: React.FC = () => {
  const { t } = useTranslation();
  const { q, sessions } = useAppContext();
  const [chat, setChat] = useState<ChatItem[]>([]);
  const [msg, setMsg] = useState('');
  const [fileSelected, setFileSelected] = useState(false);
  const [chatTab, setChatTab] = useState<'main' | 'subagents'>('main');
  const [sessionKey, setSessionKey] = useState('main');
  const chatEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chat]);

  const loadHistory = async () => {
    try {
      const qs = q ? `${q}&session=${encodeURIComponent(sessionKey)}` : `?session=${encodeURIComponent(sessionKey)}`;
      const r = await fetch(`/webui/api/chat/history${qs}`);
      if (!r.ok) return;
      const j = await r.json();
      const arr = Array.isArray(j.messages) ? j.messages : [];
      const mapped: ChatItem[] = arr.map((m: any) => {
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
        return { role, text, label };
      });
      setChat(mapped);
    } catch (e) {
      console.error(e);
    }
  };

  const loadSubagentGroup = async () => {
    try {
      const r = await fetch(`/webui/api/subagents_runtime${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'stream_all', limit: 200, task_limit: 24 }),
      });
      if (!r.ok) return;
      const j = await r.json();
      const arr = Array.isArray(j?.result?.items) ? j.result.items : [];
      const mapped: ChatItem[] = arr.map((item: StreamItem) => {
        const isEvent = item.kind === 'event';
        const label = isEvent
          ? `${item.agent_id || 'subagent'} · ${item.event_type || 'event'}`
          : `${item.from_agent || '-'} -> ${item.to_agent || '-'} · ${item.message_type || 'message'}`;
        const body = isEvent ? (item.message || '') : (item.content || '');
        return {
          role: 'assistant',
          label,
          text: `${body}${item.status ? `\n\nstatus: ${item.status}` : ''}`,
        };
      });
      setChat(mapped);
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
      const fd = new FormData(); fd.append('file', f);
      try {
        const ur = await fetch(`/webui/api/upload${q}`, { method: 'POST', body: fd });
        const uj = await ur.json(); media = uj.path || '';
      } catch (e) {
        console.error('L0053', e);
      }
    }

    const userText = msg + (media ? `\n[Attached File: ${f?.name}]` : '');
    setChat((prev) => [...prev, { role: 'user', text: userText, label: t('user') }]);

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

      setChat((prev) => [...prev, { role: 'assistant', text: '', label: t('agent') }]);

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        const chunk = decoder.decode(value, { stream: true });
        assistantText += chunk;
        setChat((prev) => {
          const next = [...prev];
          next[next.length - 1] = { role: 'assistant', text: assistantText, label: t('agent') };
          return next;
        });
      }

      // refresh full persisted history (includes tool/internal traces)
      loadHistory();
    } catch (e) {
      setChat((prev) => [...prev, { role: 'system', text: t('chatServerError'), label: t('system') }]);
    }
  }

  useEffect(() => {
    if (chatTab === 'main') {
      loadHistory();
      return;
    }
    loadSubagentGroup();
  }, [q, chatTab, sessionKey]);

  useEffect(() => {
    if (chatTab !== 'subagents') return;
    const timer = window.setInterval(() => {
      loadSubagentGroup();
    }, 5000);
    return () => window.clearInterval(timer);
  }, [q, chatTab]);

  const userSessions = (sessions || []).filter((s: any) => !String(s?.key || '').startsWith('subagent:'));

  useEffect(() => {
    if (chatTab !== 'main') return;
    if (!userSessions.length) return;
    if (!userSessions.some((s: any) => s.key === sessionKey)) {
      setSessionKey(userSessions[0].key);
    }
  }, [chatTab, sessionKey, userSessions]);

  return (
    <div className="flex h-full">
      <div className="flex-1 flex flex-col bg-zinc-950/50">
        <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between gap-3 flex-wrap">
          <div className="flex items-center gap-2">
            <button
              onClick={() => setChatTab('main')}
              className={`px-3 py-1.5 rounded-lg text-xs ${chatTab === 'main' ? 'bg-indigo-600 text-white' : 'bg-zinc-900 border border-zinc-700 text-zinc-300'}`}
            >
              Main Chat
            </button>
            <button
              onClick={() => setChatTab('subagents')}
              className={`px-3 py-1.5 rounded-lg text-xs ${chatTab === 'subagents' ? 'bg-amber-600 text-white' : 'bg-zinc-900 border border-zinc-700 text-zinc-300'}`}
            >
              {t('subagentGroup')}
            </button>
            {chatTab === 'main' && (
              <select value={sessionKey} onChange={(e) => setSessionKey(e.target.value)} className="bg-zinc-900 border border-zinc-700 rounded px-2 py-1 text-xs text-zinc-200">
                {userSessions.map((s: any) => <option key={s.key} value={s.key}>{s.title || s.key}</option>)}
              </select>
            )}
          </div>
          <button onClick={chatTab === 'main' ? loadHistory : loadSubagentGroup} className="flex items-center gap-1 px-2 py-1 text-xs rounded bg-zinc-800 hover:bg-zinc-700"><RefreshCw className="w-3 h-3"/>{t('reloadHistory')}</button>
        </div>

        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {chat.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-zinc-500 space-y-4">
              <div className="w-16 h-16 rounded-2xl bg-zinc-900 flex items-center justify-center border border-zinc-800">
                <MessageSquare className="w-8 h-8 text-zinc-600" />
              </div>
              <p className="text-sm font-medium">{chatTab === 'main' ? t('startConversation') : t('noSubagentStream')}</p>
            </div>
          ) : (
            chat.map((m, i) => {
              const isUser = m.role === 'user';
              const isExec = m.role === 'tool' || m.role === 'exec';
              const isSystem = m.role === 'system';
              const avatar = isUser ? 'U' : isExec ? 'E' : isSystem ? 'S' : 'A';
              const avatarClass = isUser
                ? 'bg-indigo-600/90 text-white'
                : isExec
                  ? 'bg-amber-600/80 text-white'
                  : isSystem
                    ? 'bg-zinc-700 text-zinc-100'
                    : 'bg-emerald-600/80 text-white';
              const bubbleClass = isUser
                ? 'bg-indigo-600 text-white rounded-br-sm'
                : isExec
                  ? 'bg-amber-500/10 text-amber-100 rounded-bl-sm border border-amber-500/30'
                  : isSystem
                    ? 'bg-zinc-700/40 text-zinc-100 rounded-bl-sm border border-zinc-600/40'
                    : 'bg-zinc-800/80 text-zinc-200 rounded-bl-sm border border-zinc-700/50';

              return (
                <motion.div
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  key={i}
                  className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}
                >
                  <div className={`flex items-start gap-2 max-w-[96%] ${isUser ? 'flex-row-reverse' : 'flex-row'}`}>
                    <div className={`w-7 h-7 mt-1 rounded-full text-xs font-bold flex items-center justify-center shrink-0 ${avatarClass}`}>{avatar}</div>
                    <div className={`max-w-[92%] rounded-2xl px-4 py-3 shadow-sm ${bubbleClass}`}>
                      <div className="text-[11px] opacity-80 mb-1">{m.label || (isUser ? t('user') : isExec ? t('exec') : isSystem ? t('system') : t('agent'))}</div>
                      <p className="whitespace-pre-wrap text-[14px] leading-relaxed">{m.text}</p>
                    </div>
                  </div>
                </motion.div>
              );
            })
          )}
          <div ref={chatEndRef} />
        </div>

        <div className="p-4 bg-zinc-950 border-t border-zinc-800">
          <div className="max-w-4xl mx-auto relative flex items-center">
            <input
              type="file"
              id="file"
              className="hidden"
              onChange={(e) => setFileSelected(!!e.target.files?.[0])}
            />
            <label
              htmlFor="file"
              className={`absolute left-3 p-2 rounded-full cursor-pointer transition-colors ${
                fileSelected ? 'text-indigo-400 bg-indigo-500/10' : 'text-zinc-400 hover:bg-zinc-800 hover:text-zinc-200'
              }`}
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
