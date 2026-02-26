import React, { useState } from 'react';
import { Plus, RefreshCw, CheckCircle2, Pause, Edit2, Trash2, X, Play, Clock } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { CronJob } from '../types';

const initialCronForm = {
  name: '',
  kind: 'cron',
  everyMs: 600000,
  expr: '*/10 * * * *',
  message: '',
  deliver: false,
  channel: 'telegram',
  to: '',
  enabled: true
}

const Cron: React.FC = () => {
  const { t } = useTranslation();
  const { cron, refreshCron, q } = useAppContext();
  const [isCronModalOpen, setIsCronModalOpen] = useState(false);
  const [editingCron, setEditingCron] = useState<CronJob | null>(null);
  const [cronForm, setCronForm] = useState(initialCronForm);

  async function cronAction(action: 'delete' | 'enable' | 'disable', id: string) {
    try {
      await fetch(`/webui/api/cron${q}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action, id }),
      });
      await refreshCron();
    } catch (e) {
      console.error(e);
    }
  }

  async function openCronModal(job?: CronJob) {
    if (job) {
      try {
        const r = await fetch(`/webui/api/cron${q}&id=${job.id}`);
        if (r.ok) {
          const details = await r.json();
          setEditingCron(details.job);
          setCronForm({
            name: details.job.name || '',
            kind: details.job.kind || 'cron',
            everyMs: details.job.everyMs || 600000,
            expr: details.job.expr || '',
            message: details.job.message || '',
            deliver: details.job.deliver || false,
            channel: details.job.channel || 'telegram',
            to: details.job.to || '',
            enabled: details.job.enabled ?? true
          });
        }
      } catch (e) {
        console.error("Failed to fetch job details", e);
      }
    } else {
      setEditingCron(null);
      setCronForm(initialCronForm);
    }
    setIsCronModalOpen(true);
  }

  async function handleCronSubmit() {
    try {
      const action = editingCron ? 'update' : 'create';
      const payload = {
        action,
        ...(editingCron && { id: editingCron.id }),
        ...cronForm
      };
      
      const r = await fetch(`/webui/api/cron${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });
      
      if (r.ok) {
        setIsCronModalOpen(false);
        await refreshCron();
      } else {
        const err = await r.text();
        alert("Action failed: " + err);
      }
    } catch (e) {
      alert("Action failed: " + e);
    }
  }

  return (
    <div className="p-8 max-w-7xl mx-auto space-y-8">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold tracking-tight">{t('cronJobs')}</h1>
        <div className="flex items-center gap-3">
          <button onClick={() => refreshCron()} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
            <RefreshCw className="w-4 h-4" /> {t('refresh')}
          </button>
          <button onClick={() => openCronModal()} className="flex items-center gap-2 px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium transition-colors shadow-sm">
            <Plus className="w-4 h-4" /> {t('addJob')}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
        {cron.map(j => (
          <div key={j.id} className="bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-6 flex flex-col shadow-sm group hover:border-zinc-700/50 transition-colors">
            <div className="flex items-start justify-between mb-4">
              <div>
                <h3 className="font-semibold text-zinc-100 mb-1">{j.name || j.id}</h3>
                <div className="flex items-center gap-2">
                  <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-wider bg-zinc-800/50 px-2 py-0.5 rounded">ID: {j.id.slice(-6)}</span>
                  <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-wider bg-zinc-800/50 px-2 py-0.5 rounded">{j.kind}</span>
                </div>
              </div>
              {j.enabled ? (
                <span className="flex items-center gap-1.5 text-xs font-medium text-emerald-400 bg-emerald-400/10 px-2.5 py-1 rounded-full border border-emerald-500/20">
                  <CheckCircle2 className="w-3.5 h-3.5"/> {t('active')}
                </span>
              ) : (
                <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-400 bg-zinc-800 px-2.5 py-1 rounded-full border border-zinc-700/50">
                  <Pause className="w-3.5 h-3.5"/> {t('paused')}
                </span>
              )}
            </div>
            
            <div className="flex-1 space-y-3 mb-6">
              <div className="text-sm text-zinc-400 line-clamp-2 italic">"{j.message}"</div>
              <div className="grid grid-cols-2 gap-2">
                <div className="bg-zinc-950/50 rounded-lg p-2 border border-zinc-800/50">
                  <div className="text-[10px] text-zinc-500 uppercase mb-0.5">{t('kind')}</div>
                  <div className="text-xs text-zinc-300 font-medium">{j.kind}</div>
                </div>
                <div className="bg-zinc-950/50 rounded-lg p-2 border border-zinc-800/50">
                  <div className="text-[10px] text-zinc-500 uppercase mb-0.5">{j.kind === 'cron' ? t('cronExpression') : t('everyMs')}</div>
                  <div className="text-xs text-zinc-300 font-medium truncate">{j.kind === 'cron' ? j.expr : j.everyMs}</div>
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-4 border-t border-zinc-800/50">
              <button 
                onClick={() => openCronModal(j)}
                className="flex-1 flex items-center justify-center gap-2 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-xs font-medium transition-colors text-zinc-300"
              >
                <Edit2 className="w-3.5 h-3.5" /> {t('editJob')}
              </button>
              <button 
                onClick={() => cronAction(j.enabled ? 'disable' : 'enable', j.id)}
                className={`p-2 rounded-lg transition-colors ${j.enabled ? 'bg-amber-500/10 text-amber-500 hover:bg-amber-500/20' : 'bg-emerald-500/10 text-emerald-500 hover:bg-emerald-500/20'}`}
                title={j.enabled ? t('pauseJob') : t('startJob')}
              >
                {j.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
              </button>
              <button 
                onClick={() => cronAction('delete', j.id)}
                className="p-2 bg-red-500/10 text-red-500 hover:bg-red-500/20 rounded-lg transition-colors"
                title={t('deleteJob')}
              >
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          </div>
        ))}
        {cron.length === 0 && (
          <div className="col-span-full py-20 bg-zinc-900/20 border border-dashed border-zinc-800 rounded-3xl flex flex-col items-center justify-center text-zinc-500">
            <Clock className="w-12 h-12 mb-4 opacity-20" />
            <p className="text-lg font-medium">{t('noCronJobs')}</p>
          </div>
        )}
      </div>

      {/* Cron Modal */}
      <AnimatePresence>
        {isCronModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div 
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              onClick={() => setIsCronModalOpen(false)}
              className="absolute inset-0 bg-black/60 backdrop-blur-sm"
            />
            <motion.div 
              initial={{ opacity: 0, scale: 0.95, y: 20 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.95, y: 20 }}
              className="relative w-full max-w-lg bg-zinc-900 border border-zinc-800 rounded-3xl shadow-2xl overflow-hidden"
            >
              <div className="p-6 border-b border-zinc-800 flex items-center justify-between bg-zinc-900/50">
                <h2 className="text-xl font-semibold text-zinc-100">{editingCron ? t('editJob') : t('addJob')}</h2>
                <button onClick={() => setIsCronModalOpen(false)} className="p-2 hover:bg-zinc-800 rounded-full transition-colors text-zinc-400 hover:text-zinc-200">
                  <X className="w-5 h-5" />
                </button>
              </div>
              
              <div className="p-6 space-y-4 max-h-[70vh] overflow-y-auto">
                <div className="grid grid-cols-2 gap-4">
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('jobName')}</span>
                    <input 
                      type="text" 
                      value={cronForm.name} 
                      onChange={(e) => setCronForm({...cronForm, name: e.target.value})}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors" 
                    />
                  </label>
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('kind')}</span>
                    <select 
                      value={cronForm.kind} 
                      onChange={(e) => setCronForm({...cronForm, kind: e.target.value})}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors"
                    >
                      <option value="cron">Cron</option>
                      <option value="every">Every</option>
                      <option value="once">Once</option>
                    </select>
                  </label>
                  <label className="block">
                    {cronForm.kind === 'cron' ? (
                      <>
                        <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('cronExpression')}</span>
                        <input 
                          type="text" 
                          value={cronForm.expr} 
                          onChange={(e) => setCronForm({...cronForm, expr: e.target.value})}
                          placeholder="*/5 * * * *"
                          className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors" 
                        />
                      </>
                    ) : (
                      <>
                        <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('everyMs')}</span>
                        <input 
                          type="number" 
                          value={cronForm.everyMs} 
                          onChange={(e) => setCronForm({...cronForm, everyMs: Number(e.target.value)})}
                          className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors" 
                        />
                      </>
                    )}
                  </label>
                </div>
                
                <label className="block">
                  <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('message')}</span>
                  <textarea 
                    value={cronForm.message} 
                    onChange={(e) => setCronForm({...cronForm, message: e.target.value})}
                    rows={3}
                    className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors resize-none" 
                  />
                </label>

                <div className="grid grid-cols-2 gap-4">
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('channel')}</span>
                    <input 
                      type="text" 
                      value={cronForm.channel} 
                      onChange={(e) => setCronForm({...cronForm, channel: e.target.value})}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors" 
                    />
                  </label>
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('to')}</span>
                    <input 
                      type="text" 
                      value={cronForm.to} 
                      onChange={(e) => setCronForm({...cronForm, to: e.target.value})}
                      className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-1 focus:ring-indigo-500 transition-colors" 
                    />
                  </label>
                </div>

                <div className="flex items-center gap-6 pt-2">
                  <label className="flex items-center gap-3 cursor-pointer group">
                    <input 
                      type="checkbox" 
                      checked={cronForm.deliver} 
                      onChange={(e) => setCronForm({...cronForm, deliver: e.target.checked})}
                      className="w-4 h-4 rounded border-zinc-700 text-indigo-500 focus:ring-indigo-500 focus:ring-offset-zinc-900 bg-zinc-950" 
                    />
                    <span className="text-sm font-medium text-zinc-400 group-hover:text-zinc-200 transition-colors">{t('deliver')}</span>
                  </label>
                  <label className="flex items-center gap-3 cursor-pointer group">
                    <input 
                      type="checkbox" 
                      checked={cronForm.enabled} 
                      onChange={(e) => setCronForm({...cronForm, enabled: e.target.checked})}
                      className="w-4 h-4 rounded border-zinc-700 text-indigo-500 focus:ring-indigo-500 focus:ring-offset-zinc-900 bg-zinc-950" 
                    />
                    <span className="text-sm font-medium text-zinc-400 group-hover:text-zinc-200 transition-colors">{t('active')}</span>
                  </label>
                </div>
              </div>

              <div className="p-6 border-t border-zinc-800 bg-zinc-900/50 flex items-center justify-end gap-3">
                <button 
                  onClick={() => setIsCronModalOpen(false)}
                  className="px-4 py-2 text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors"
                >
                  {t('cancel')}
                </button>
                <button 
                  onClick={handleCronSubmit}
                  className="px-6 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-xl text-sm font-medium transition-all shadow-lg shadow-indigo-600/20"
                >
                  {editingCron ? t('update') : t('create')}
                </button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
};

export default Cron;
