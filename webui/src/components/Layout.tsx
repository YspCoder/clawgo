import React from 'react';
import { Outlet, useLocation } from 'react-router-dom';
import { motion, AnimatePresence } from 'motion/react';
import Header from './Header';
import Sidebar from './Sidebar';
import { useAppContext } from '../context/AppContext';

const Layout: React.FC = () => {
  const location = useLocation();
  const { sidebarOpen, setSidebarOpen } = useAppContext();

  return (
    <div className="flex flex-col h-screen bg-zinc-950 text-zinc-50 overflow-hidden font-sans">
      <Header />
      <div className="flex flex-1 min-h-0">
        <Sidebar />
        {sidebarOpen && (
          <button className="fixed inset-0 top-16 bg-black/40 z-30 md:hidden" onClick={() => setSidebarOpen(false)} aria-label="close sidebar" />
        )}
        <main className="flex-1 flex flex-col min-w-0 relative bg-zinc-950/50">
          <AnimatePresence mode="wait">
            <motion.div 
              key={location.pathname}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              exit={{ opacity: 0, y: -10 }}
              transition={{ duration: 0.2 }}
              className="absolute inset-0 overflow-y-auto"
            >
              <Outlet />
            </motion.div>
          </AnimatePresence>
        </main>
      </div>
    </div>
  );
};

export default Layout;
