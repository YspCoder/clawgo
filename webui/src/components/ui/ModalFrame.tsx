import React from 'react';

type ModalShellProps = {
  children: React.ReactNode;
  className?: string;
};

type ModalBackdropProps = {
  className?: string;
  onClick?: () => void;
};

type ModalCardProps = {
  children: React.ReactNode;
  className?: string;
};

type ModalHeaderProps = {
  actions?: React.ReactNode;
  className?: string;
  subtitle?: React.ReactNode;
  title: React.ReactNode;
};

type ModalBodyProps = {
  children: React.ReactNode;
  className?: string;
};

type ModalFooterProps = {
  children: React.ReactNode;
  className?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

export const ModalShell: React.FC<ModalShellProps> = ({ children, className }) => (
  <div className={joinClasses('fixed inset-0 z-50 flex items-center justify-center p-4', className)}>
    {children}
  </div>
);

export const ModalBackdrop: React.FC<ModalBackdropProps> = ({ className, onClick }) => (
  <div
    className={joinClasses('ui-overlay-strong absolute inset-0 backdrop-blur-sm', className)}
    onClick={onClick}
  />
);

export const ModalCard: React.FC<ModalCardProps> = ({ children, className }) => (
  <div
    className={joinClasses(
      'brand-card relative z-[1] w-full overflow-hidden border border-zinc-800 shadow-2xl flex flex-col',
      className,
    )}
  >
    {children}
  </div>
);

export const ModalHeader: React.FC<ModalHeaderProps> = ({
  actions,
  className,
  subtitle,
  title,
}) => (
  <div
    className={joinClasses(
      'flex items-center justify-between gap-3 border-b border-zinc-800 px-4 py-3 dark:border-zinc-700',
      className,
    )}
  >
    <div className="min-w-0">
      <div className="text-lg font-semibold text-zinc-100">{title}</div>
      {subtitle ? <div className="mt-1 text-xs text-zinc-500">{subtitle}</div> : null}
    </div>
    {actions ? <div className="flex items-center gap-2">{actions}</div> : null}
  </div>
);

export const ModalBody: React.FC<ModalBodyProps> = ({ children, className }) => (
  <div className={joinClasses('flex-1', className)}>{children}</div>
);

export const ModalFooter: React.FC<ModalFooterProps> = ({ children, className }) => (
  <div
    className={joinClasses(
      'flex items-center justify-end gap-3 border-t border-zinc-800 bg-zinc-900/20 px-6 py-4 dark:border-zinc-700',
      className,
    )}
  >
    {children}
  </div>
);
