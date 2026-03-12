import React from 'react';

type NoticeTone = 'warning' | 'danger' | 'info';

type NoticePanelProps = {
  children: React.ReactNode;
  className?: string;
  tone?: NoticeTone;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

function toneClass(tone: NoticeTone) {
  switch (tone) {
    case 'danger':
      return 'ui-notice-danger';
    case 'info':
      return 'ui-notice-info';
    case 'warning':
    default:
      return 'ui-notice-warning';
  }
}

const NoticePanel: React.FC<NoticePanelProps> = ({
  children,
  className,
  tone = 'warning',
}) => {
  return (
    <div className={joinClasses(toneClass(tone), 'rounded-2xl border px-4 py-3 text-sm', className)}>
      {children}
    </div>
  );
};

export default NoticePanel;
