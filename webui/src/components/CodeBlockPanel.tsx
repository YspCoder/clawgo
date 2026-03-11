import React from 'react';
import InfoBlock from './InfoBlock';

type CodeBlockPanelProps = {
  children: React.ReactNode;
  className?: string;
  codeClassName?: string;
  danger?: boolean;
  label: React.ReactNode;
  pre?: boolean;
};

const CodeBlockPanel: React.FC<CodeBlockPanelProps> = ({
  children,
  className,
  codeClassName,
  danger = false,
  label,
  pre = false,
}) => {
  const content = pre ? (
    <pre className={codeClassName || `text-xs overflow-auto ${danger ? 'ui-code-danger' : ''}`.trim()}>{children}</pre>
  ) : (
    <div className={codeClassName || `whitespace-pre-wrap ${danger ? 'ui-code-danger' : ''}`.trim()}>{children}</div>
  );

  return (
    <InfoBlock
      label={label}
      className={className}
      contentClassName={pre ? 'p-3' : 'p-3 whitespace-pre-wrap'}
    >
      {content}
    </InfoBlock>
  );
};

export default CodeBlockPanel;
