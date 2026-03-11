import React from 'react';
import { MessageSquare } from 'lucide-react';

type ChatEmptyStateProps = {
  message: string;
};

const ChatEmptyState: React.FC<ChatEmptyStateProps> = ({ message }) => {
  return (
    <div className="ui-text-muted h-full flex flex-col items-center justify-center space-y-4">
      <div className="ui-border-subtle w-16 h-16 rounded-[24px] brand-card-subtle flex items-center justify-center border">
        <MessageSquare className="ui-icon-muted w-8 h-8" />
      </div>
      <p className="text-sm font-medium">{message}</p>
    </div>
  );
};

export default ChatEmptyState;
