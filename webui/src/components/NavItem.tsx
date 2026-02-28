import React from 'react';
import { NavLink } from 'react-router-dom';

interface NavItemProps {
  icon: React.ReactNode;
  label: string;
  to: string;
}

const NavItem: React.FC<NavItemProps> = ({ icon, label, to }) => (
  <NavLink 
    to={to}
    className={({ isActive }) => `w-full flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 ${
      isActive
        ? 'bg-indigo-500/15 text-indigo-300 border border-indigo-500/30'
        : 'text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-200 border border-transparent'
    }`}
  >
    {icon}
    {label}
  </NavLink>
);

export default NavItem;
