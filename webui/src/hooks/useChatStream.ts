import { useState, useRef, useCallback } from 'react';
import { useTranslation } from 'react-i18next';
import { RenderedChatItem } from '../components/chat/chatUtils';

interface UseChatStreamOptions {
  q: string;
  onHistoryRequest: () => void;
  setMainChat: React.Dispatch<React.SetStateAction<RenderedChatItem[]>>;
}

export function useChatStream({ q, onHistoryRequest, setMainChat }: UseChatStreamOptions) {
  const { t } = useTranslation();
  
  const sendMessageStream = useCallback(async (sessionKey: string, currentMsg: string, media: string) => {
    try {
      const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const url = new URL(`${proto}//${window.location.host}/webui/api/chat/live`);
      const token = new URLSearchParams(q.startsWith('?') ? q.slice(1) : q).get('token');
      if (token) url.searchParams.set('token', token);
      
      let assistantText = '';

      setMainChat((prev) => [...prev, {
        id: `local-assistant-${Date.now()}`,
        role: 'assistant',
        text: '',
        label: t('agent'),
        actorName: t('agent'),
        avatarText: 'A',
        avatarClassName: 'avatar-agent',
      }]);

      await new Promise<void>((resolve, reject) => {
        const ws = new WebSocket(url.toString());
        let settled = false;
        
        ws.onopen = () => {
          ws.send(JSON.stringify({ session: sessionKey, message: currentMsg, media }));
        };
        
        ws.onmessage = (event) => {
          try {
            const payload = JSON.parse(event.data);
            if (payload?.type === 'chat_chunk' && typeof payload?.delta === 'string') {
              assistantText += payload.delta;
              setMainChat((prev) => {
                const next = [...prev];
                next[next.length - 1] = {
                  ...next[next.length - 1],
                  text: assistantText,
                };
                return next;
              });
              return;
            }
            if (payload?.type === 'chat_done') {
              settled = true;
              ws.close();
              resolve();
              return;
            }
            if (payload?.type === 'chat_error') {
              settled = true;
              ws.close();
              reject(new Error(payload?.error || 'Chat request failed'));
            }
          } catch (e) {
            settled = true;
            ws.close();
            reject(e);
          }
        };
        
        ws.onerror = () => {
          settled = true;
          ws.close();
          reject(new Error('Chat request failed'));
        };
        
        ws.onclose = () => {
          if (!settled && !assistantText) {
            reject(new Error('Chat request failed'));
          }
        };
      });

      onHistoryRequest();
    } catch (e) {
      setMainChat((prev) => [...prev, {
        id: `local-system-${Date.now()}`,
        role: 'system',
        text: t('chatServerError'),
        label: t('system'),
        actorName: t('system'),
        avatarText: 'S',
        avatarClassName: 'avatar-system',
      }]);
    }
  }, [q, t, setMainChat, onHistoryRequest]);

  return { sendMessageStream };
}
