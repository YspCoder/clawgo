import React, { useState, useRef, useEffect } from 'react';
import { Paperclip, Send, MessageSquare, RefreshCw } from 'lucide-react';
import { motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { ChatItem } from '../types';

const Chat: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [chat, setChat] = useState<ChatItem[]>([]);
  const [msg, setMsg] = useState('');
  const [fileSelected, setFileSelected] = useState(false);
  const chatEndRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [chat]);

  const loadHistory = async () => {
    try {
      const r = await fetch(`/webui/api/chat/history${q}`);
      if (!r.ok) return;
      const j = await r.json();
      const arr = Array.isArray(j.messages) ? j.messages : [];
      const mapped: ChatItem[] = arr.map((m: any) => {
        const role = (m.role === 'assistant' || m.role === 'user') ? m.role : 'assistant';
        let text = m.content || '';
        if (m.role === 'tool') {
          text = `[tool output]\n${text}`;
        }
        if (Array.isArray(m.tool_calls) && m.tool_calls.length > 0) {
          text = `${text}\n[tool calls: ${m.tool_calls.map((x: any) => x?.function?.name || x?.name).filter(Boolean).join(', ')}]`;
        }
        return { role, text };
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
        console.error('Upload failed', e);
      }
    }

    const userText = msg + (media ? `\n[Attached File: ${f?.name}]` : '');
    setChat((prev) => [...prev, { role: 'user', text: userText }]);

    const currentMsg = msg;
    setMsg('');
    setFileSelected(false);
    if (input) input.value = '';

    try {
      const response = await fetch(`/webui/api/chat/stream${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session: 'main', message: currentMsg, media }),
      });

      if (!response.ok || !response.body) throw new Error('Chat request failed');

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantText = '';

      setChat((prev) => [...prev, { role: 'assistant', text: '' }]);

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        const chunk = decoder.decode(value, { stream: true });
        assistantText += chunk;
        setChat((prev) => {
          const next = [...prev];
          next[next.length - 1] = { role: 'assistant', text: assistantText };
          return next;
        });
      }

      // refresh full persisted history (includes tool/internal traces)
      loadHistory();
    } catch (e) {
      setChat((prev) => [...prev, { role: 'assistant', text: 'Error: Failed to get response from server.' }]);
    }
  }

  useEffect(() => {
    loadHistory();
  }, [q]);

  return (
    <div className="flex h-full">
      <div className="flex-1 flex flex-col bg-zinc-950/50">
        <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
          <h2 className="text-sm text-zinc-300 font-medium">Main Agent</h2>
          <button onClick={loadHistory} className="flex items-center gap-1 px-2 py-1 text-xs rounded bg-zinc-800 hover:bg-zinc-700"><RefreshCw className="w-3 h-3"/>Reload History</button>
        </div>

        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {chat.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-zinc-500 space-y-4">
              <div className="w-16 h-16 rounded-2xl bg-zinc-900 flex items-center justify-center border border-zinc-800">
                <MessageSquare className="w-8 h-8 text-zinc-600" />
              </div>
              <p className="text-sm font-medium">{t('startConversation')}</p>
            </div>
          ) : (
            chat.map((m, i) => (
              <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                key={i}
                className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}
              >
                <div className={`max-w-[90%] rounded-2xl px-5 py-3.5 shadow-sm ${
                  m.role === 'user'
                    ? 'bg-indigo-600 text-white rounded-br-sm'
                    : 'bg-zinc-800/80 text-zinc-200 rounded-bl-sm border border-zinc-700/50'
                }`}>
                  <p className="whitespace-pre-wrap text-[15px] leading-relaxed">{m.text}</p>
                </div>
              </motion.div>
            ))
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
              onKeyDown={(e) => e.key === 'Enter' && send()}
              placeholder={t('typeMessage')}
              className="w-full bg-zinc-900 border border-zinc-800 rounded-full pl-14 pr-14 py-3.5 text-[15px] focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-all placeholder:text-zinc-500 shadow-sm"
            />
            <button
              onClick={send}
              disabled={!msg.trim() && !fileSelected}
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
