import React from 'react';
import { motion } from 'motion/react';

type InsetCardProps = {
  children: React.ReactNode;
  className?: string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const InsetCard: React.FC<InsetCardProps> = ({ children, className }) => {
  return (
    <motion.div 
      whileHover={{ scale: 1.01, backgroundColor: 'var(--card-bg-a)' }}
      transition={{ duration: 0.2 }}
      className={joinClasses('glass-panel rounded-2xl p-4 transition-colors', className)}
    >
      {children}
    </motion.div>
  );
};

export default React.memo(InsetCard);
