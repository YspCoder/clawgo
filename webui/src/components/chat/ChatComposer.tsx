import React from 'react';
import { Paperclip, Send } from 'lucide-react';
import { TextField } from '../FormControls';

type ChatComposerProps = {
  chatTab: 'main' | 'subagents';
  disabled: boolean;
  fileSelected: boolean;
  msg: string;
  onFileChange: React.ChangeEventHandler<HTMLInputElement>;
  onMsgChange: (value: string) => void;
  onSend: () => void;
  placeholder: string;
};

const ChatComposer: React.FC<ChatComposerProps> = ({
  chatTab,
  disabled,
  fileSelected,
  msg,
  onFileChange,
  onMsgChange,
  onSend,
  placeholder,
}) => {
  return (
    <div className="ui-soft-panel ui-border-subtle p-3 sm:p-4 border-t">
      <div className="ui-composer w-full relative flex items-center px-2">
        <input
          type="file"
          id="file"
          className="hidden"
          onChange={onFileChange}
        />
        <label
          htmlFor="file"
          className={`absolute left-3 p-2 rounded-full cursor-pointer transition-colors ${fileSelected ? 'ui-icon-info ui-surface-muted' : 'ui-text-muted ui-row-hover'}`}
        >
          <Paperclip className="w-5 h-5" />
        </label>
        <TextField
          value={msg}
          onChange={(e) => onMsgChange(e.target.value)}
          onKeyDown={(e) => chatTab === 'main' && e.key === 'Enter' && onSend()}
          placeholder={placeholder}
          disabled={disabled}
          className="ui-composer-input w-full pl-14 pr-14 py-3.5 text-[15px] transition-all disabled:opacity-60"
        />
        <button
          onClick={onSend}
          disabled={disabled || (!msg.trim() && !fileSelected)}
          className="absolute right-2 p-2.5 brand-button disabled:opacity-50 ui-text-primary rounded-full transition-colors"
        >
          <Send className="w-4 h-4 ml-0.5" />
        </button>
      </div>
    </div>
  );
};

export default ChatComposer;
