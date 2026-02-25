import React from 'react';

interface StatCardProps {
  title: string;
  value: string | number;
  icon: React.ReactNode;
}

const StatCard: React.FC<StatCardProps> = ({ title, value, icon }) => (
  <div className="bg-zinc-900/50 border border-zinc-800 rounded-2xl p-6 flex items-center gap-4">
    <div className="w-12 h-12 rounded-xl bg-zinc-800/50 flex items-center justify-center border border-zinc-700/50">
      {icon}
    </div>
    <div>
      <div className="text-zinc-400 text-sm font-medium mb-1">{title}</div>
      <div className="text-2xl font-semibold text-zinc-100">{value}</div>
    </div>
  </div>
);

export default StatCard;
