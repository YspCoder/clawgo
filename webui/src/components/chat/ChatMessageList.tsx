import React from 'react';
import { motion } from 'motion/react';
import type { RenderedChatItem } from './chatUtils';

type ChatMessageListProps = {
  items: RenderedChatItem[];
  t: (key: string) => string;
};

function messageTone(item: RenderedChatItem) {
  const isUser = item.role === 'user';
  const isExec = item.role === 'tool' || item.role === 'exec';
  const isSystem = item.role === 'system';

  const bubbleClass = isUser
    ? 'chat-bubble-user rounded-br-sm'
    : isExec
      ? 'chat-bubble-tool rounded-bl-sm'
      : isSystem
        ? 'chat-bubble-system rounded-bl-sm'
        : item.isReadonlyGroup
          ? 'chat-bubble-system rounded-bl-sm'
          : 'chat-bubble-agent rounded-bl-sm';

  const metaClass = isUser
    ? 'chat-meta-user'
    : isExec
      ? 'chat-meta-tool'
      : 'ui-text-muted';

  const subLabelClass = isUser
    ? 'chat-submeta-user'
    : isExec
      ? 'chat-submeta-tool'
      : 'ui-text-muted';

  return { bubbleClass, isExec, isSystem, isUser, metaClass, subLabelClass };
}

const ChatMessageList: React.FC<ChatMessageListProps> = ({
  items,
  t,
}) => {
  return (
    <>
      {items.map((item, index) => {
        const { bubbleClass, isExec, isSystem, isUser, metaClass, subLabelClass } = messageTone(item);
        return (
          <motion.div
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            key={item.id || index}
            className={`flex ${isUser ? 'justify-end' : 'justify-start'}`}
          >
            <div className={`flex items-start gap-2 max-w-full sm:max-w-[96%] ${isUser ? 'flex-row-reverse' : 'flex-row'}`}>
              <div className={`w-9 h-9 mt-1 rounded-full text-[11px] font-bold flex items-center justify-center shrink-0 ${item.avatarClassName || (isUser ? 'avatar-user' : 'avatar-agent')}`}>
                {item.avatarText || (isUser ? 'U' : 'A')}
              </div>
              <div className={`max-w-[calc(100vw-6rem)] sm:max-w-[92%] rounded-[24px] px-4 py-3 shadow-sm ${bubbleClass}`}>
                <div className="flex items-center justify-between gap-3 mb-1">
                  <div className={`text-[11px] font-medium ${metaClass}`}>
                    {item.actorName || item.label || (isUser ? t('user') : isExec ? t('exec') : isSystem ? t('system') : t('agent'))}
                  </div>
                  {item.metaLine ? <div className={`text-[11px] ${metaClass}`}>{item.metaLine}</div> : null}
                </div>
                {item.label && item.actorName && item.label !== item.actorName ? (
                  <div className={`text-[11px] mb-2 ${subLabelClass}`}>{item.label}</div>
                ) : null}
                <p className="whitespace-pre-wrap text-[14px] leading-relaxed">{item.text}</p>
              </div>
            </div>
          </motion.div>
        );
      })}
    </>
  );
};

export default ChatMessageList;
