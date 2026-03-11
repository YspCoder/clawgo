import React from 'react';
import { Package, Wrench } from 'lucide-react';
import { Button, FixedButton } from '../Button';
import { CheckboxCardField, SelectField, TextField } from '../FormControls';
import NoticePanel from '../NoticePanel';

type MCPDraftServer = {
  enabled: boolean;
  transport: string;
  url: string;
  command: string;
  args: string[];
  env: Record<string, string>;
  permission: string;
  working_dir: string;
  description: string;
  package: string;
  installer?: string;
};

type MCPServerCheck = {
  status?: string;
  message?: string;
  package?: string;
  installable?: boolean;
};

type MCPServerEditorProps = {
  activeCheck?: MCPServerCheck | null;
  addDraftArg: (rawValue: string) => void;
  draft: MCPDraftServer;
  draftArgInput: string;
  draftName: string;
  installCheckPackage: (check: MCPServerCheck) => void;
  installDraftPackage: () => void;
  removeDraftArg: (index: number) => void;
  setDraftArgInput: (value: string) => void;
  setDraftName: (value: string) => void;
  t: (key: string, options?: any) => string;
  updateDraftField: <K extends keyof MCPDraftServer>(field: K, value: MCPDraftServer[K]) => void;
};

const MCPServerEditor: React.FC<MCPServerEditorProps> = ({
  activeCheck,
  addDraftArg,
  draft,
  draftArgInput,
  draftName,
  installCheckPackage,
  installDraftPackage,
  removeDraftArg,
  setDraftArgInput,
  setDraftName,
  t,
  updateDraftField,
}) => {
  return (
    <div className="space-y-5">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        <label className="space-y-2">
          <div className="text-xs text-zinc-400">{t('configNewMCPServerName')}</div>
          <TextField value={draftName} onChange={(e) => setDraftName(e.target.value)} className="h-11" />
        </label>
        <label className="space-y-2">
          <div className="text-xs text-zinc-400">{t('configLabels.description')}</div>
          <TextField value={draft.description} onChange={(e) => updateDraftField('description', e.target.value)} className="h-11" />
        </label>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        <label className="space-y-2">
          <div className="text-xs text-zinc-400">transport</div>
          <SelectField value={draft.transport} onChange={(e) => updateDraftField('transport', e.target.value)} className="h-11">
            <option value="stdio">stdio</option>
            <option value="http">http</option>
            <option value="streamable_http">streamable_http</option>
            <option value="sse">sse</option>
          </SelectField>
        </label>
        <div className="space-y-2">
          <div className="text-xs text-zinc-400">enabled</div>
          <CheckboxCardField
            checked={draft.enabled}
            className="min-h-[76px]"
            label={t('enable')}
            onChange={(checked) => updateDraftField('enabled', checked)}
          />
        </div>
        {draft.transport === 'stdio' ? (
          <label className="space-y-2">
            <div className="text-xs text-zinc-400">permission</div>
            <SelectField value={draft.permission} onChange={(e) => updateDraftField('permission', e.target.value)} className="h-11">
              <option value="workspace">workspace</option>
              <option value="full">full</option>
            </SelectField>
          </label>
        ) : null}
        {draft.transport === 'stdio' ? (
          <label className="space-y-2">
            <div className="text-xs text-zinc-400">{t('configLabels.package')}</div>
            <TextField value={draft.package} onChange={(e) => updateDraftField('package', e.target.value)} className="h-11" />
          </label>
        ) : null}
      </div>

      {draft.transport === 'stdio' ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <label className="space-y-2">
            <div className="text-xs text-zinc-400">{t('configLabels.command')}</div>
            <TextField value={draft.command} onChange={(e) => updateDraftField('command', e.target.value)} className="h-11" />
          </label>
          <label className="space-y-2">
            <div className="text-xs text-zinc-400">{t('configLabels.working_dir')}</div>
            <TextField value={draft.working_dir} onChange={(e) => updateDraftField('working_dir', e.target.value)} className="h-11" />
          </label>
        </div>
      ) : (
        <label className="space-y-2">
          <div className="text-xs text-zinc-400">{t('configLabels.url')}</div>
          <TextField value={draft.url} onChange={(e) => updateDraftField('url', e.target.value)} className="h-11" />
        </label>
      )}

      {draft.transport === 'stdio' ? (
        <div className="ui-soft-panel rounded-[24px] p-4 space-y-3">
          <div className="flex items-center justify-between gap-3 flex-wrap">
            <div>
              <div className="text-sm font-medium text-zinc-200">Args</div>
              <div className="text-xs text-zinc-500">{t('configMCPArgsEnterHint')}</div>
            </div>
            <FixedButton onClick={installDraftPackage} variant="success" label={t('install')}>
              <Package className="w-4 h-4" />
            </FixedButton>
          </div>

          <div className="flex max-h-28 flex-wrap gap-2 overflow-y-auto pr-1">
            {draft.args.map((arg, index) => (
              <span key={`draft-arg-${index}`} className="inline-flex max-w-full items-center gap-2 rounded-xl ui-soft-panel px-2.5 py-1.5 text-[11px] text-zinc-700 dark:text-zinc-200">
                <span className="font-mono break-all">{arg}</span>
                <button type="button" onClick={() => removeDraftArg(index)} className="shrink-0 text-zinc-400 hover:text-zinc-100">x</button>
              </span>
            ))}
          </div>
          <TextField
            value={draftArgInput}
            onChange={(e) => setDraftArgInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                e.preventDefault();
                addDraftArg(draftArgInput);
              }
            }}
            onBlur={() => addDraftArg(draftArgInput)}
            placeholder={t('configMCPArgsEnterHint')}
            className="h-11"
          />
        </div>
      ) : null}

      {activeCheck && activeCheck.status !== 'ok' && activeCheck.status !== 'disabled' && activeCheck.status !== 'not_applicable' ? (
        <NoticePanel className="text-xs space-y-2">
          <div>{activeCheck.message || t('configMCPCommandMissing')}</div>
          {activeCheck.package ? (
            <div className="text-amber-300/80">{t('configMCPInstallSuggested', { pkg: activeCheck.package })}</div>
          ) : null}
          {activeCheck.installable ? (
            <FixedButton onClick={() => installCheckPackage(activeCheck)} variant="warning" label={t('install')}>
              <Wrench className="w-4 h-4" />
            </FixedButton>
          ) : null}
        </NoticePanel>
      ) : null}
    </div>
  );
};

export default MCPServerEditor;
