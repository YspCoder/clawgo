import React, { useState } from 'react';
import { Plus, RefreshCw, CheckCircle2, Pause, Edit2, Trash2, X, Play, Clock } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/ui/Button';
import EmptyState from '../components/data-display/EmptyState';
import { SwitchCardField, FieldBlock, SelectField, TextField, TextareaField } from '../components/ui/FormControls';
import { ModalBackdrop, ModalBody, ModalCard, ModalFooter, ModalHeader, ModalShell } from '../components/ui/ModalFrame';
import PageHeader from '../components/layout/PageHeader';
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
      <PageHeader
        title={t('cronJobs')}
        actions={
          <>
          <FixedButton onClick={() => refreshCron()} label={t('refresh')}>
            <RefreshCw className="w-4 h-4" />
          </FixedButton>
          <FixedButton onClick={() => openCronModal()} variant="primary" label={t('addJob')}>
            <Plus className="w-4 h-4" />
          </FixedButton>
          </>
        }
      />

      <div className="grid grid-cols-1 sm:grid-cols-2 2xl:grid-cols-4 gap-6">
        {cron.map((j) => {
          const schedule = formatSchedule(j, t);
          return (
          <div key={j.id} className="brand-card rounded-2xl border border-zinc-800/80 p-6 flex flex-col group hover:border-zinc-700/50 transition-colors">
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
              <FixedButton onClick={() => openCronModal(j)} radius="lg" label={t('editJob')}>
                <Edit2 className="w-4 h-4" />
              </FixedButton>
              <FixedButton
                onClick={() => cronAction(j.enabled ? 'disable' : 'enable', j.id)}
                variant={j.enabled ? 'warning' : 'success'}
                radius="lg"
                label={j.enabled ? t('pauseJob') : t('startJob')}
              >
                {j.enabled ? <Pause className="w-4 h-4" /> : <Play className="w-4 h-4" />}
              </FixedButton>
              <FixedButton onClick={() => cronAction('delete', j.id)} variant="danger" radius="lg" label={t('deleteJob')}>
                <Trash2 className="w-4 h-4" />
              </FixedButton>
            </div>
          </div>
          );
        })}
        {cron.length === 0 && (
          <EmptyState
            centered
            dashed
            panel
            className="col-span-full py-20 rounded-[32px]"
            icon={<Clock className="w-12 h-12 opacity-20" />}
            title={t('noCronJobs')}
            message={null}
          />
        )}
      </div>

      <AnimatePresence>
        {isCronModalOpen && (
          <ModalShell>
            <motion.div
              initial={{ opacity: 0 }}
              animate={{ opacity: 1 }}
              exit={{ opacity: 0 }}
              className="absolute inset-0"
            />
            <ModalBackdrop onClick={() => setIsCronModalOpen(false)} />
            <motion.div
              initial={{ opacity: 0, scale: 0.95, y: 20 }}
              animate={{ opacity: 1, scale: 1, y: 0 }}
              exit={{ opacity: 0, scale: 0.95, y: 20 }}
              className="relative z-[1] w-full max-w-lg"
            >
              <ModalCard className="ui-panel rounded-[32px]">
                <ModalHeader
                  className="bg-zinc-900/20 px-6 py-6"
                  title={editingCron ? t('editJob') : t('addJob')}
                  actions={
                    <FixedButton onClick={() => setIsCronModalOpen(false)} radius="full" label={t('close')}>
                      <X className="w-5 h-5" />
                    </FixedButton>
                  }
                />

                <ModalBody className="max-h-[70vh] overflow-y-auto p-6 space-y-4">
                <div className="grid grid-cols-2 gap-4">
                  <FieldBlock label={t('jobName')}>
                    <TextField
                      type="text"
                      value={cronForm.name}
                      onChange={(e) => setCronForm({ ...cronForm, name: e.target.value })}
                    />
                  </FieldBlock>
                  <FieldBlock label={t('cronExpression')}>
                    <TextField
                      type="text"
                      value={cronForm.expr}
                      onChange={(e) => setCronForm({ ...cronForm, expr: e.target.value })}
                      placeholder={t('cronExpressionPlaceholder')}
                    />
                  </FieldBlock>
                </div>

                <FieldBlock label={t('message')}>
                  <TextareaField
                    value={cronForm.message}
                    onChange={(e) => setCronForm({ ...cronForm, message: e.target.value })}
                    rows={3}
                    className="resize-none"
                  />
                </FieldBlock>

                <div className="grid grid-cols-2 gap-4">
                  <FieldBlock label={t('channel')}>
                    <SelectField
                      value={cronForm.channel}
                      onChange={(e) => {
                        const nextChannel = e.target.value;
                        const candidates = channelRecipients[nextChannel] || [];
                        const nextTo = candidates.includes(cronForm.to) ? cronForm.to : (candidates[0] || '');
                        setCronForm({ ...cronForm, channel: nextChannel, to: nextTo });
                      }}
                    >
                      {(enabledChannels.length > 0 ? enabledChannels : [cronForm.channel]).map((ch) => (
                        <option key={ch} value={ch}>{ch}</option>
                      ))}
                    </SelectField>
                  </FieldBlock>
                  <FieldBlock label={t('to')}>
                    {((channelRecipients[cronForm.channel] || []).length > 0) ? (
                      <SelectField
                        value={cronForm.to}
                        onChange={(e) => setCronForm({ ...cronForm, to: e.target.value })}
                      >
                        {(channelRecipients[cronForm.channel] || []).map((id) => (
                          <option key={id} value={id}>{id}</option>
                        ))}
                      </SelectField>
                    ) : (
                      <TextField
                        type="text"
                        value={cronForm.to}
                        onChange={(e) => setCronForm({ ...cronForm, to: e.target.value })}
                        placeholder={t('recipientId')}
                      />
                    )}
                  </FieldBlock>
                </div>

                  <div className="grid grid-cols-1 gap-3 pt-2 md:grid-cols-2">
                    <SwitchCardField
                      checked={cronForm.deliver}
                      help={t('cronDeliverHint')}
                      label={t('deliver')}
                      onChange={(checked) => setCronForm({ ...cronForm, deliver: checked })}
                    />
                    <SwitchCardField
                      checked={cronForm.enabled}
                      help={t('cronEnabledHint')}
                      label={t('active')}
                      onChange={(checked) => setCronForm({ ...cronForm, enabled: checked })}
                    />
                  </div>
                </ModalBody>

                <ModalFooter className="bg-zinc-900/20">
                  <Button onClick={() => setIsCronModalOpen(false)}>{t('cancel')}</Button>
                  <FixedButton onClick={handleCronSubmit} variant="primary" label={editingCron ? t('update') : t('create')}>
                    <Edit2 className="w-4 h-4" />
                  </FixedButton>
                </ModalFooter>
              </ModalCard>
            </motion.div>
          </ModalShell>
        )}
      </AnimatePresence>
    </div>
  );
};

export default Cron;
