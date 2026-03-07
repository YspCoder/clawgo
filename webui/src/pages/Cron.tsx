import React, { useState } from 'react';
import { Plus, RefreshCw, CheckCircle2, Pause, Edit2, Trash2, X, Play, Clock } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { CronJob } from '../types';
import { formatLocalDateTime } from '../utils/time';

const initialCronForm = {
  name: '',
  expr: '*/10 * * * *',
  message: '',
  deliver: false,
  channel: 'telegram',
  to: '',
  enabled: true,
};

const isNonGroupRecipient = (channel: string, id: string) => {
  const ch = String(channel || '').toLowerCase();
  const v = String(id || '').trim();
  if (!v) return false;
  if (ch === 'telegram') {
    if (v.startsWith('-')) return false;
    if (v.startsWith('telegram:')) {
      const raw = v.slice('telegram:'.length);
      if (raw.startsWith('-')) return false;
    }
  }
  if (ch === 'discord') {
    if (v.startsWith('#') || v.startsWith('discord:channel:')) return false;
  }
  return true;
};

const formatSchedule = (job: CronJob, t: (key: string) => string) => {
  const kind = String(job.schedule?.kind || '').toLowerCase();
  if (kind === 'at' && job.schedule?.atMs) {
    return {
      label: t('runAt'),
      value: formatLocalDateTime(job.schedule.atMs),
    };
  }
  return {
    label: t('cronExpression'),
    value: job.expr || '-',
  };
};

const Cron: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { cron, refreshCron, q, cfg } = useAppContext();
  const [isCronModalOpen, setIsCronModalOpen] = useState(false);
  const [editingCron, setEditingCron] = useState<CronJob | null>(null);
  const [cronForm, setCronForm] = useState(initialCronForm);

  const enabledChannels = React.useMemo(() => {
    const channels = (cfg as any)?.channels || {};
    return Object.keys(channels).filter((k) => {
      const v = channels[k];
      return v && typeof v === 'object' && v.enabled === true;
    });
  }, [cfg]);

  const channelRecipients = React.useMemo(() => {
    const channels = (cfg as any)?.channels || {};
    const out: Record<string, string[]> = {};
    enabledChannels.forEach((ch) => {
      const arr = Array.isArray(channels?.[ch]?.allow_from) ? channels[ch].allow_from : [];
      out[ch] = arr.map((x: any) => String(x || '').trim()).filter((id: string) => isNonGroupRecipient(ch, id));
    });
    return out;
  }, [cfg, enabledChannels]);

  async function cronAction(action: 'delete' | 'enable' | 'disable', id: string) {
    if (action === 'delete') {
      const ok = await ui.confirmDialog({
        title: t('cronDeleteConfirmTitle'),
        message: t('cronDeleteConfirmMessage'),
        danger: true,
        confirmText: t('delete'),
      });
      if (!ok) return;
    }
    if (action === 'disable') {
      const ok = await ui.confirmDialog({
        title: t('cronDisableConfirmTitle'),
        message: t('cronDisableConfirmMessage'),
        confirmText: t('pause'),
      });
      if (!ok) return;
    }
    try {
      await ui.withLoading(async () => {
        const r = await fetch(`/webui/api/cron${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action, id }),
        });
        if (!r.ok) {
          throw new Error(await r.text());
        }
      }, t('loading'));
      await refreshCron();
    } catch (e) {
      await ui.notify({ title: t('actionFailed'), message: String(e) });
    }
  }

  async function openCronModal(job?: CronJob) {
    if (job) {
      try {
        const r = await fetch(`/webui/api/cron${q}&id=${job.id}`);
        if (r.ok) {
          const details = await r.json();
          const isAtSchedule = String(details.job?.schedule?.kind || '').toLowerCase() === 'at';
          setEditingCron(details.job);
          setCronForm({
            name: details.job.name || '',
            expr: isAtSchedule ? '' : (details.job.expr || ''),
            message: details.job.message || '',
            deliver: details.job.deliver || false,
            channel: details.job.channel || 'telegram',
            to: details.job.to || '',
            enabled: details.job.enabled ?? true,
          });
        }
      } catch (e) {
        console.error('L0068', e);
      }
    } else {
      setEditingCron(null);
      const defaultChannel = enabledChannels[0] || initialCronForm.channel;
      const defaultTo = (channelRecipients[defaultChannel] && channelRecipients[defaultChannel][0]) || '';
      setCronForm({ ...initialCronForm, channel: defaultChannel, to: defaultTo });
    }
    setIsCronModalOpen(true);
  }

  async function handleCronSubmit() {
    try {
      const action = editingCron ? 'update' : 'create';
      const payload = {
        action,
        ...(editingCron && { id: editingCron.id }),
        ...cronForm,
      };

      const r = await fetch(`/webui/api/cron${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (r.ok) {
        setIsCronModalOpen(false);
        await refreshCron();
        await ui.notify({ title: t('saved'), message: t('cronSaved') });
      } else {
        const err = await r.text();
        await ui.notify({ title: t('actionFailed'), message: err });
      }
    } catch (e) {
      await ui.notify({ title: t('actionFailed'), message: String(e) });
    }
  }

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-6 xl:space-y-8">
      <div className="flex items-center justify-between flex-wrap gap-3">
        <h1 className="text-2xl font-semibold tracking-tight">{t('cronJobs')}</h1>
        <div className="flex items-center gap-3 flex-wrap">
          <button onClick={() => refreshCron()} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
            <RefreshCw className="w-4 h-4" /> {t('refresh')}
          </button>
          <button onClick={() => openCronModal()} className="brand-button flex items-center gap-2 px-4 py-2 text-white rounded-xl text-sm font-medium transition-colors shadow-sm">
            <Plus className="w-4 h-4" /> {t('addJob')}
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 2xl:grid-cols-4 gap-6">
        {cron.map((j) => {
          const schedule = formatSchedule(j, t);
          return (
          <div key={j.id} className="brand-card rounded-[30px] border border-zinc-800/80 p-6 flex flex-col group hover:border-zinc-700/50 transition-colors">
            <div className="flex items-start justify-between mb-4">
              <div>
                <h3 className="font-semibold text-zinc-100 mb-1">{j.name || j.id}</h3>
                <div className="flex items-center gap-2">
                  <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-wider bg-zinc-800/40 px-2 py-0.5 rounded-full">{t('id')}: {j.id.slice(-6)}</span>
                </div>
              </div>
              {j.enabled ? (
                <span className="flex items-center gap-1.5 text-xs font-medium text-emerald-400 bg-emerald-400/10 px-2.5 py-1 rounded-full border border-emerald-500/20">
                  <CheckCircle2 className="w-3.5 h-3.5" /> {t('active')}
                </span>
              ) : (
                <span className="flex items-center gap-1.5 text-xs font-medium text-zinc-400 bg-zinc-800 px-2.5 py-1 rounded-full border border-zinc-700/50">
                  <Pause className="w-3.5 h-3.5" /> {t('paused')}
                </span>
              )}
            </div>

            <div className="flex-1 space-y-3 mb-6">
              <div className="text-sm text-zinc-400 line-clamp-2 italic">"{j.message}"</div>
              <div className="brand-card-subtle rounded-2xl p-3 border border-zinc-800/50">
                <div className="text-[10px] text-zinc-500 uppercase mb-0.5">{schedule.label}</div>
                <div className="text-xs text-zinc-300 font-medium break-all">{schedule.value}</div>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-4 border-t border-zinc-800/50">
              <button
                onClick={() => openCronModal(j)}
                className="flex-1 flex items-center justify-center gap-2 py-2 bg-zinc-800/70 hover:bg-zinc-700 rounded-xl text-xs font-medium transition-colors text-zinc-300"
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
          );
        })}
        {cron.length === 0 && (
          <div className="col-span-full py-20 brand-card border border-dashed border-zinc-800 rounded-[32px] flex flex-col items-center justify-center text-zinc-500">
            <Clock className="w-12 h-12 mb-4 opacity-20" />
            <p className="text-lg font-medium">{t('noCronJobs')}</p>
          </div>
        )}
      </div>

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
              className="brand-card relative w-full max-w-lg border border-zinc-800 rounded-[32px] shadow-2xl overflow-hidden"
            >
              <div className="p-6 border-b border-zinc-800 flex items-center justify-between bg-zinc-900/20 relative z-[1]">
                <h2 className="text-xl font-semibold text-zinc-100">{editingCron ? t('editJob') : t('addJob')}</h2>
                <button onClick={() => setIsCronModalOpen(false)} className="p-2 hover:bg-zinc-800 rounded-full transition-colors text-zinc-400 hover:text-zinc-200">
                  <X className="w-5 h-5" />
                </button>
              </div>

              <div className="p-6 space-y-4 max-h-[70vh] overflow-y-auto relative z-[1]">
                <div className="grid grid-cols-2 gap-4">
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('jobName')}</span>
                    <input
                      type="text"
                      value={cronForm.name}
                      onChange={(e) => setCronForm({ ...cronForm, name: e.target.value })}
                      className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors"
                    />
                  </label>
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('cronExpression')}</span>
                    <input
                      type="text"
                      value={cronForm.expr}
                      onChange={(e) => setCronForm({ ...cronForm, expr: e.target.value })}
                      placeholder={t('cronExpressionPlaceholder')}
                      className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors"
                    />
                  </label>
                </div>

                <label className="block">
                  <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('message')}</span>
                  <textarea
                    value={cronForm.message}
                    onChange={(e) => setCronForm({ ...cronForm, message: e.target.value })}
                    rows={3}
                    className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors resize-none"
                  />
                </label>

                <div className="grid grid-cols-2 gap-4">
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('channel')}</span>
                    <select
                      value={cronForm.channel}
                      onChange={(e) => {
                        const nextChannel = e.target.value;
                        const candidates = channelRecipients[nextChannel] || [];
                        const nextTo = candidates.includes(cronForm.to) ? cronForm.to : (candidates[0] || '');
                        setCronForm({ ...cronForm, channel: nextChannel, to: nextTo });
                      }}
                      className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors"
                    >
                      {(enabledChannels.length > 0 ? enabledChannels : [cronForm.channel]).map((ch) => (
                        <option key={ch} value={ch}>{ch}</option>
                      ))}
                    </select>
                  </label>
                  <label className="block">
                    <span className="text-sm font-medium text-zinc-400 mb-1.5 block">{t('to')}</span>
                    {((channelRecipients[cronForm.channel] || []).length > 0) ? (
                      <select
                        value={cronForm.to}
                        onChange={(e) => setCronForm({ ...cronForm, to: e.target.value })}
                        className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors"
                      >
                        {(channelRecipients[cronForm.channel] || []).map((id) => (
                          <option key={id} value={id}>{id}</option>
                        ))}
                      </select>
                    ) : (
                      <input
                        type="text"
                        value={cronForm.to}
                        onChange={(e) => setCronForm({ ...cronForm, to: e.target.value })}
                        placeholder={t('recipientId')}
                        className="w-full bg-zinc-950/70 border border-zinc-800 rounded-xl px-3 py-2 text-sm focus:outline-none focus:border-indigo-500 focus:ring-2 focus:ring-indigo-500/20 transition-colors"
                      />
                    )}
                  </label>
                </div>

                <div className="flex items-center gap-6 pt-2">
                  <label className="flex items-center gap-3 cursor-pointer group">
                    <input
                      type="checkbox"
                      checked={cronForm.deliver}
                      onChange={(e) => setCronForm({ ...cronForm, deliver: e.target.checked })}
                      className="w-4 h-4 rounded border-zinc-700 text-indigo-500 focus:ring-indigo-500 focus:ring-offset-zinc-900 bg-zinc-950"
                    />
                    <span className="text-sm font-medium text-zinc-400 group-hover:text-zinc-200 transition-colors">{t('deliver')}</span>
                  </label>
                  <label className="flex items-center gap-3 cursor-pointer group">
                    <input
                      type="checkbox"
                      checked={cronForm.enabled}
                      onChange={(e) => setCronForm({ ...cronForm, enabled: e.target.checked })}
                      className="w-4 h-4 rounded border-zinc-700 text-indigo-500 focus:ring-indigo-500 focus:ring-offset-zinc-900 bg-zinc-950"
                    />
                    <span className="text-sm font-medium text-zinc-400 group-hover:text-zinc-200 transition-colors">{t('active')}</span>
                  </label>
                </div>
              </div>

              <div className="p-6 border-t border-zinc-800 bg-zinc-900/20 flex items-center justify-end gap-3 relative z-[1]">
                <button
                  onClick={() => setIsCronModalOpen(false)}
                  className="px-4 py-2 text-sm font-medium text-zinc-400 hover:text-zinc-200 transition-colors"
                >
                  {t('cancel')}
                </button>
                <button
                  onClick={handleCronSubmit}
                  className="brand-button px-6 py-2 text-white rounded-xl text-sm font-medium transition-all"
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
