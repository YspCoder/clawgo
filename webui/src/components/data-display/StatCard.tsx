import React from 'react';
import { motion } from 'motion/react';

interface StatCardProps {
  title: string;
  value: string | number;
  icon: React.ReactNode;
}

const StatCard: React.FC<StatCardProps> = ({ title, value, icon }) => (
  <motion.div 
    whileHover={{ scale: 1.02 }}
    whileTap={{ scale: 0.98 }}
    transition={{ type: "spring", stiffness: 400, damping: 25 }}
    className="brand-card hover-lift h-full min-h-[96px] border border-zinc-800 p-5 flex items-center gap-4 cursor-pointer"
  >
    <div className="card-icon-shell w-12 h-12 rounded-2xl bg-zinc-800/50 flex items-center justify-center border border-zinc-700/50 relative z-[1]">
      {icon}
    </div>
    <div className="relative z-[1]">
      <div className="text-zinc-500 text-[11px] uppercase tracking-[0.24em] font-semibold mb-1">{title}</div>
      <div className="text-2xl font-semibold text-zinc-100">{value}</div>
    </div>
  </motion.div>
);

export default React.memo(StatCard);
