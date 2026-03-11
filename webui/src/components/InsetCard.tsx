import React from 'react';

type InsetCardProps = {
  children: React.ReactNode;
  className?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const InsetCard: React.FC<InsetCardProps> = ({ children, className }) => {
  return (
    <div className={joinClasses('brand-card-subtle rounded-2xl border border-zinc-800 p-4', className)}>
      {children}
    </div>
  );
};

export default InsetCard;
