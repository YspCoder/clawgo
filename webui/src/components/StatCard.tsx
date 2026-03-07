import React from 'react';

interface StatCardProps {
  title: string;
  value: string | number;
  icon: React.ReactNode;
}

const StatCard: React.FC<StatCardProps> = ({ title, value, icon }) => (
  <div className="brand-card h-full min-h-[124px] border border-zinc-800 p-6 flex items-center gap-4">
    <div className="w-12 h-12 rounded-2xl bg-zinc-800/50 flex items-center justify-center border border-zinc-700/50 shadow-[inset_0_1px_0_rgba(255,255,255,0.18)] relative z-[1]">
      {icon}
    </div>
    <div className="relative z-[1]">
      <div className="text-zinc-500 text-[11px] uppercase tracking-[0.24em] font-semibold mb-1">{title}</div>
      <div className="text-2xl font-semibold text-zinc-100">{value}</div>
    </div>
  </div>
);

export default StatCard;
