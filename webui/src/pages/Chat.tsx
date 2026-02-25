import React, { useState, useRef, useEffect, useMemo } from 'react';
import { Plus, MessageSquare, Paperclip, Send } from 'lucide-react';
import { motion } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { ChatItem } from '../types';

const Chat: React.FC = () => {
  const { t } = useTranslation();
  const { sessions, setSessions, q } = useAppContext();
  const [active, setActive] = useState('webui:default');
  const [chat, setChat] = useState<Record<string, ChatItem[]>>({ 'webui:default': [] });
  const [msg, setMsg] = useState('');
  const [fileSelected, setFileSelected] = useState(false);
  const chatEndRef = useRef<HTMLDivElement>(null);

  const activeChat = useMemo(() => chat[active] || [], [chat, active]);

  useEffect(() => {
    chatEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [activeChat]);

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
        console.error("Upload failed", e);
      }
    }
    
    const userText = msg + (media ? `\n[Attached File: ${f?.name}]` : '');
    setChat((prev) => ({ ...prev, [active]: [...(prev[active] || []), { role: 'user', text: userText }] }));
    
    const currentMsg = msg;
    setMsg('');
    setFileSelected(false);
    if (input) input.value = '';

    try {
      const response = await fetch(`/webui/api/chat/stream${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ session: active, message: currentMsg, media }),
      });

      if (!response.ok) throw new Error('Chat request failed');
      if (!response.body) throw new Error('No response body');

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantText = '';

      // Add placeholder for assistant response
      setChat((prev) => ({
        ...prev,
        [active]: [...(prev[active] || []), { role: 'assistant', text: '' }]
      }));

      while (true) {
        const { value, done } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        assistantText += chunk;

        setChat((prev) => {
          const currentChat = [...(prev[active] || [])];
          if (currentChat.length > 0) {
            currentChat[currentChat.length - 1] = { role: 'assistant', text: assistantText };
          }
          return { ...prev, [active]: currentChat };
        });
      }
    } catch (e) {
      setChat((prev) => ({
        ...prev,
        [active]: [...(prev[active] || []), { role: 'assistant', text: 'Error: Failed to get response from server.' }]
      }));
    }
  }

  function addSession() {
    const n = `webui:${Date.now()}`;
    const s = { key: n, title: `${t('sessions')} ${sessions.length + 1}` };
    setSessions((v) => [...v, s]); setActive(n); setChat((prev) => ({ ...prev, [n]: [] }));
  }

  return (
    <div className="flex h-full">
      <div className="w-64 border-r border-zinc-800 bg-zinc-900/20 flex flex-col shrink-0">
        <div className="p-4 border-b border-zinc-800 flex items-center justify-between">
          <h2 className="font-medium text-zinc-200">{t('sessions')}</h2>
          <button onClick={addSession} className="p-1.5 hover:bg-zinc-800 rounded-md transition-colors text-zinc-400 hover:text-zinc-200">
            <Plus className="w-4 h-4" />
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-3 space-y-1">
          {sessions.map(s => (
            <button 
              key={s.key} 
              onClick={() => setActive(s.key)}
              className={`w-full text-left px-3 py-2.5 rounded-lg text-sm transition-all duration-200 ${
                s.key === active 
                  ? 'bg-indigo-500/15 text-indigo-300 font-medium shadow-sm' 
                  : 'text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-200'
              }`}
            >
              {s.title}
            </button>
          ))}
        </div>
      </div>
      
      <div className="flex-1 flex flex-col bg-zinc-950/50">
        <div className="flex-1 overflow-y-auto p-6 space-y-6">
          {activeChat.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-zinc-500 space-y-4">
              <div className="w-16 h-16 rounded-2xl bg-zinc-900 flex items-center justify-center border border-zinc-800">
                <MessageSquare className="w-8 h-8 text-zinc-600" />
              </div>
              <p className="text-sm font-medium">{t('startConversation')}</p>
            </div>
          ) : (
            activeChat.map((m, i) => (
              <motion.div 
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                key={i} 
                className={`flex ${m.role === 'user' ? 'justify-end' : 'justify-start'}`}
              >
                <div className={`max-w-[80%] rounded-2xl px-5 py-3.5 shadow-sm ${
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
