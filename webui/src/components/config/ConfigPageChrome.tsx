import React from 'react';
import { RefreshCw, Save } from 'lucide-react';
import { Button, FixedButton } from '../ui/Button';
import { TextField, ToolbarSwitchField } from '../ui/FormControls';
import CodeBlockPanel from '../data-display/CodeBlockPanel';
import { ModalBackdrop, ModalCard, ModalHeader, ModalShell } from '../ui/ModalFrame';

type Translate = (key: string, options?: any) => string;

type ConfigHeaderProps = {
  onSave: () => void;
  onShowForm: () => void;
  onShowRaw: () => void;
  showRaw: boolean;
  t: Translate;
};

export function ConfigHeader({ onSave, onShowForm, onShowRaw, showRaw, t }: ConfigHeaderProps) {
  return (
    <div className="flex items-center justify-between gap-3 flex-wrap">
      <h1 className="ui-text-primary text-2xl font-semibold tracking-tight">{t('configuration')}</h1>
      <div className="flex items-center gap-2 flex-wrap justify-end">
        <div className="ui-toolbar-chip flex items-center gap-1 p-1 rounded-xl">
          <Button onClick={onShowForm} variant={!showRaw ? 'primary' : 'neutral'} size="sm" radius="lg">{t('form')}</Button>
          <Button onClick={onShowRaw} variant={showRaw ? 'primary' : 'neutral'} size="sm" radius="lg">{t('rawJson')}</Button>
        </div>
        <Button onClick={onSave} variant="primary" size="sm" radius="lg" gap="1">
          <Save className="w-4 h-4" />
          {t('saveChanges')}
        </Button>
      </div>
    </div>
  );
}

type ConfigToolbarProps = {
  basicMode: boolean;
  hotOnly: boolean;
  onHotOnlyChange: (checked: boolean) => void;
  onReload: () => void;
  onSearchChange: (value: string) => void;
  onShowDiff: () => void;
  onToggleBasicMode: () => void;
  search: string;
  t: Translate;
};

export function ConfigToolbar({
  basicMode,
  hotOnly,
  onHotOnlyChange,
  onReload,
  onSearchChange,
  onShowDiff,
  onToggleBasicMode,
  search,
  t,
}: ConfigToolbarProps) {
  return (
    <div className="flex items-center justify-between gap-3 flex-wrap">
      <div className="flex items-center gap-2 flex-wrap">
        <FixedButton onClick={onReload} label={t('reload')}>
          <RefreshCw className="w-4 h-4" />
        </FixedButton>
        <Button onClick={onShowDiff} size="sm">{t('configDiffPreview')}</Button>
        <Button onClick={onToggleBasicMode} size="sm">
          {basicMode ? t('configBasicMode') : t('configAdvancedMode')}
        </Button>
        <ToolbarSwitchField
          checked={hotOnly}
          help={t('configHotOnlyHint', { defaultValue: 'Only show fields that support hot reload.' })}
          label={t('configHotOnly')}
          onChange={onHotOnlyChange}
        />
        <TextField value={search} onChange={(e) => onSearchChange(e.target.value)} placeholder={t('configSearchPlaceholder')} className="min-w-[240px] flex-1" />
      </div>
    </div>
  );
}

type ConfigSidebarProps = {
  activeTop: string;
  configLabels: Record<string, string>;
  filteredTopKeys: string[];
  hotReloadTabKey: string;
  onSelectTop: (key: string) => void;
  t: Translate;
};

export function ConfigSidebar({
  activeTop,
  configLabels,
  filteredTopKeys,
  hotReloadTabKey,
  onSelectTop,
  t,
}: ConfigSidebarProps) {
  return (
    <aside className="sidebar-section w-44 md:w-56 border-r border-zinc-800/60 p-2 md:p-3 overflow-y-auto shrink-0 flex flex-col gap-1">
      <div className="ui-text-secondary text-[10px] uppercase font-bold tracking-widest mb-1 px-3">{t('configTopLevel')}</div>
      <div className="space-y-0.5">
        {filteredTopKeys.map((key) => {
          const isActive = activeTop === key;
          return (
            <button
              key={key}
              onClick={() => onSelectTop(key)}
              className={`w-full text-left px-3 py-2.5 rounded-lg text-sm font-medium transition-all duration-300 border hover-lift ${isActive ? 'nav-item-active text-indigo-700 border-indigo-500/30 shadow-[0_4px_20px_rgba(79,70,229,0.15)]' : 'text-zinc-400 border-transparent hover:bg-zinc-800/40 hover:text-zinc-200'}`}
            >
              {key === hotReloadTabKey ? t('configHotFieldsFull') : (configLabels[key] || key)}
            </button>
          );
        })}
      </div>
    </aside>
  );
}

type ConfigDiffModalProps = {
  diffRows: Array<{ path: string; before: any; after: any }>;
  onClose: () => void;
  t: Translate;
};

export function ConfigDiffModal({ diffRows, onClose, t }: ConfigDiffModalProps) {
  return (
    <ModalShell>
      <ModalBackdrop />
      <ModalCard className="max-h-[85vh] max-w-4xl rounded-2xl">
        <ModalHeader
          title={t('configDiffPreviewCount', { count: diffRows.length })}
          actions={<Button onClick={onClose} size="xs" radius="xl">{t('close')}</Button>}
        />
        <div className="overflow-auto text-xs">
          <table className="w-full">
            <thead className="sticky top-0 bg-zinc-900 text-zinc-300">
              <tr>
                <th className="text-left p-2">{t('path')}</th>
                <th className="text-left p-2">{t('before')}</th>
                <th className="text-left p-2">{t('after')}</th>
              </tr>
            </thead>
            <tbody>
              {diffRows.map((row, index) => (
                <tr key={index} className="border-t border-zinc-900 align-top">
                  <td className="p-2 font-mono text-zinc-400">{row.path}</td>
                  <td className="p-2 text-zinc-300 break-all">{JSON.stringify(row.before)}</td>
                  <td className="p-2 text-emerald-300 break-all">{JSON.stringify(row.after)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </ModalCard>
    </ModalShell>
  );
}
