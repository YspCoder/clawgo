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
    className={({ isActive }) => `w-full flex items-center gap-3 px-4 py-3 rounded-xl text-sm font-medium transition-all duration-200 ${
      isActive 
        ? 'bg-indigo-500/10 text-indigo-400' 
        : 'text-zinc-400 hover:bg-zinc-800/50 hover:text-zinc-200'
    }`}
  >
    {icon}
    {label}
  </NavLink>
);

export default NavItem;
