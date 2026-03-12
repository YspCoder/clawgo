import React from 'react';

type FileListItemProps = {
  active?: boolean;
  monospace?: boolean;
  onClick?: () => void;
  actions?: React.ReactNode;
  children: React.ReactNode;
};

const FileListItem: React.FC<FileListItemProps> = ({ active = false, monospace = false, onClick, actions, children }) => (
  <div className={`flex items-center justify-between rounded-2xl px-2.5 py-2 ${active ? 'nav-item-active' : 'ui-row-hover'}`}>
    <button
      type="button"
      className={`min-w-0 flex-1 break-all pr-2 text-left ${monospace ? 'font-mono text-xs' : ''} ${active ? 'ui-text-primary font-medium' : 'ui-text-primary'}`}
      onClick={onClick}
    >
      {children}
    </button>
    {actions ? <div className="shrink-0">{actions}</div> : null}
  </div>
);

export default FileListItem;
