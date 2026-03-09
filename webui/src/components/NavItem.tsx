import React from 'react';
import { NavLink } from 'react-router-dom';
import { Check } from 'lucide-react';

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
    className={({ isActive }) => `w-full flex items-center ${collapsed ? 'justify-center' : 'gap-3'} px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-200 border ${
      isActive
        ? 'nav-item-active text-indigo-700 border-indigo-500/30'
        : 'text-zinc-400 border-transparent'
    }`}
  >
    {({ isActive }) => (
      <>
        <span className="shrink-0">{icon}</span>
        {!collapsed && <span className="min-w-0 flex-1 truncate">{label}</span>}
        {!collapsed && isActive && (
          <span className="ml-auto inline-flex h-5 w-5 items-center justify-center rounded-full bg-indigo-500/15 text-indigo-300">
            <Check className="w-3.5 h-3.5" />
          </span>
        )}
      </>
    )}
  </NavLink>
);

export default NavItem;
