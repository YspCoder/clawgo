import React from 'react';
import { NavLink } from 'react-router-dom';

interface NavItemProps {
  icon: React.ReactNode;
  label: string;
  to: string;
  collapsed?: boolean;
  nested?: boolean;
}

const NavItem: React.FC<NavItemProps> = ({ icon, label, to, collapsed = false, nested = false }) => (
  <NavLink
    to={to}
    title={collapsed ? label : undefined}
    className={({ isActive }) => `w-full flex items-center ${collapsed ? 'justify-center' : 'gap-3'} ${nested ? 'px-3 py-2' : 'px-3 py-2.5'} rounded-lg text-sm font-medium transition-all duration-200 border ${
      isActive
        ? 'nav-item-active text-indigo-700 border-indigo-500/30'
        : 'text-zinc-400 border-transparent'
    }`}
  >
    {({ isActive }) => (
      <>
        <span className="shrink-0">{icon}</span>
        {!collapsed && <span className="min-w-0 flex-1 truncate">{label}</span>}
      </>
    )}
  </NavLink>
);

export default NavItem;
