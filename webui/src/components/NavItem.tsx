import React from 'react';
import { NavLink } from 'react-router-dom';

interface NavItemProps {
  icon: React.ReactNode;
  label: string;
  to: string;
  collapsed?: boolean;
}

const NavItem: React.FC<NavItemProps> = ({ icon, label, to, collapsed = false }) => (
  <NavLink 
    to={to}
    title={collapsed ? label : undefined}
    className={({ isActive }) => `w-full flex items-center ${collapsed ? 'justify-center' : 'gap-3'} px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 ${
      isActive
        ? 'nav-item-active text-indigo-700 border border-indigo-500/30'
        : 'text-zinc-400 hover:bg-zinc-800/30 hover:text-zinc-200 border border-transparent'
    }`}
  >
    {icon}
    {!collapsed && label}
  </NavLink>
);

export default NavItem;
