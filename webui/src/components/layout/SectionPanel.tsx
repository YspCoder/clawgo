import React from 'react';
import { motion } from 'motion/react';

type SectionPanelProps = {
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
  headerClassName?: string;
  icon?: React.ReactNode;
  subtitle?: React.ReactNode;
  title?: React.ReactNode;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const containerVariants = {
  hidden: { opacity: 0 },
  visible: {
    opacity: 1,
    transition: {
      staggerChildren: 0.05
    }
  }
};

const SectionPanel: React.FC<SectionPanelProps> = ({
  actions,
  children,
  className,
  headerClassName,
  icon,
  subtitle,
  title,
}) => {
  return (
    <motion.div 
      variants={containerVariants}
      initial="hidden"
      animate="visible"
      className={joinClasses('brand-card glass-panel rounded-[30px] border border-zinc-800/80 p-6 glow-effect', className)}
    >
      <div className="relative z-10">
        {title || subtitle || actions || icon ? (
          <motion.div 
            initial={{ opacity: 0, y: -10 }}
            animate={{ opacity: 1, y: 0 }}
            className={joinClasses('mb-5 flex items-center justify-between gap-3 flex-wrap', headerClassName)}
          >
            <div>
              {title ? (
                <div className="flex items-center gap-2 text-zinc-200">
                  {icon}
                  <h2 className="text-lg font-medium">{title}</h2>
                </div>
              ) : null}
              {subtitle ? <div className="mt-1 text-xs text-zinc-500">{subtitle}</div> : null}
            </div>
            {actions ? <div className="text-xs text-zinc-500">{actions}</div> : null}
          </motion.div>
        ) : null}
        {children}
      </div>
    </motion.div>
  );
};

export default React.memo(SectionPanel);
