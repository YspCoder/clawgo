import React from 'react';
import { Save, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button, FixedButton } from '../ui/Button';
import { SwitchCardField, FieldBlock, SelectField, TextField, TextareaField } from '../ui/FormControls';
import type { SubagentProfile, ToolAllowlistGroup } from './profileDraft';
import { parseAllowlist } from './profileDraft';

type ProfileEditorPanelProps = {
  draft: SubagentProfile;
  groups: ToolAllowlistGroup[];
  idLabel: string;
  isExisting: boolean;
  memoryNamespaceLabel: string;
  nameLabel: string;
  onAddAllowlistToken: (token: string) => void;
  onChange: (next: SubagentProfile) => void;
  onDelete: () => void;
  onPromptContentChange: (value: string) => void;
  onSave: () => void;
  onSavePromptFile: () => void;
  promptContent: string;
  promptMeta: string;
  promptPlaceholder: string;
  promptPathHint: string;
  promptPathInvalid: boolean;
  roleLabel: string;
  saving: boolean;
  statusLabel: string;
  toolAllowlistHint: React.ReactNode;
  toolAllowlistLabel: string;
  maxRetriesLabel: string;
  retryBackoffLabel: string;
  maxTaskCharsLabel: string;
  maxResultCharsLabel: string;
};

const ProfileEditorPanel: React.FC<ProfileEditorPanelProps> = ({
  draft,
  groups,
  idLabel,
  isExisting,
  memoryNamespaceLabel,
  nameLabel,
  onAddAllowlistToken,
  onChange,
  onDelete,
  onPromptContentChange,
  onSave,
  onSavePromptFile,
  promptContent,
  promptMeta,
  promptPlaceholder,
  promptPathHint,
  promptPathInvalid,
  roleLabel,
  saving,
  statusLabel,
  toolAllowlistHint,
  toolAllowlistLabel,
  maxRetriesLabel,
  retryBackoffLabel,
  maxTaskCharsLabel,
  maxResultCharsLabel,
}) => {
  const { t } = useTranslation();
  const allowlistText = (draft.tool_allowlist || []).join(', ');
  const statusEnabled = (draft.status || 'active') === 'active';
  const notifyPolicyOptions = [
    { value: 'final_only', label: t('profileNotifyFinalOnlyLabel'), help: t('profileNotifyFinalOnlyHelp') },
    { value: 'internal_only', label: t('profileNotifyInternalOnlyLabel'), help: t('profileNotifyInternalOnlyHelp') },
    { value: 'milestone', label: t('profileNotifyMilestoneLabel'), help: t('profileNotifyMilestoneHelp') },
    { value: 'on_blocked', label: t('profileNotifyOnBlockedLabel'), help: t('profileNotifyOnBlockedHelp') },
    { value: 'always', label: t('profileNotifyAlwaysLabel'), help: t('profileNotifyAlwaysHelp') },
  ];
  const notifyPolicy = notifyPolicyOptions.find((option) => option.value === (draft.notify_main_policy || 'final_only')) || notifyPolicyOptions[0];

  return (
    <div className="brand-card ui-border-subtle rounded-2xl border p-4 flex flex-col min-h-0 h-full">
      <div className="space-y-3 flex-1 min-h-0 overflow-y-auto overflow-x-hidden pr-2">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <FieldBlock label={idLabel}>
          <TextField
            value={draft.agent_id || ''}
            disabled={isExisting}
            onChange={(e) => onChange({ ...draft, agent_id: e.target.value })}
            dense
            className="w-full disabled:opacity-60"
            placeholder={t('profileIdPlaceholder')}
          />
        </FieldBlock>
        <FieldBlock label={nameLabel}>
          <TextField
            value={draft.name || ''}
            onChange={(e) => onChange({ ...draft, name: e.target.value })}
            dense
            className="w-full"
            placeholder={t('profileNamePlaceholder')}
          />
        </FieldBlock>
        <FieldBlock label={roleLabel}>
          <TextField
            value={draft.role || ''}
            onChange={(e) => onChange({ ...draft, role: e.target.value })}
            dense
            className="w-full"
            placeholder={t('profileRolePlaceholder')}
          />
        </FieldBlock>
        <FieldBlock
          label={statusLabel}
        >
          <SwitchCardField
            checked={statusEnabled}
            help={statusEnabled ? t('profileStatusEnabledHelp') : t('profileStatusDisabledHelp')}
            label={statusEnabled ? t('profileStatusEnabledLabel') : t('profileStatusDisabledLabel')}
            onChange={(checked) => onChange({ ...draft, status: checked ? 'active' : 'disabled' })}
          />
        </FieldBlock>
        <FieldBlock
          label={t('profileNotifyMain')}
          help={notifyPolicy.help}
        >
          <SelectField
            value={draft.notify_main_policy || 'final_only'}
            onChange={(e) => onChange({ ...draft, notify_main_policy: e.target.value })}
            dense
            className="w-full"
          >
            {notifyPolicyOptions.map((option) => (
              <option key={option.value} value={option.value}>{option.label}</option>
            ))}
          </SelectField>
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label="system_prompt_file">
          <TextField
            value={draft.system_prompt_file || ''}
            onChange={(e) => onChange({ ...draft, system_prompt_file: e.target.value.replace(/\\/g, '/') })}
            dense
            className={`w-full ${promptPathInvalid ? 'border-rose-400/70 focus:border-rose-300' : ''}`}
            placeholder={t('profilePromptFilePlaceholder')}
          />
          <div className={`mt-1 text-[11px] ${promptPathInvalid ? 'text-rose-300' : 'ui-text-muted'}`}>{promptPathHint}</div>
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label={memoryNamespaceLabel}>
          <TextField
            value={draft.memory_namespace || ''}
            onChange={(e) => onChange({ ...draft, memory_namespace: e.target.value })}
            dense
            className="w-full"
            placeholder={t('profileNamespacePlaceholder')}
          />
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label={toolAllowlistLabel}>
          <TextField
            value={allowlistText}
            onChange={(e) => onChange({ ...draft, tool_allowlist: parseAllowlist(e.target.value) })}
            dense
            className="w-full"
            placeholder={t('profileAllowlistPlaceholder')}
          />
          <div className="ui-text-muted mt-1 text-[11px]">{toolAllowlistHint}</div>
          {groups.length > 0 ? (
            <div className="mt-2 flex flex-wrap gap-2">
              {groups.map((group) => (
                <Button key={group.name} type="button" onClick={() => onAddAllowlistToken(`group:${group.name}`)} size="xs" radius="lg" title={group.description || group.name}>
                  {`group:${group.name}`}
                </Button>
              ))}
            </div>
          ) : null}
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label={t('profilePromptFileContentLabel')} meta={promptMeta}>
          <TextareaField
            value={promptContent}
            onChange={(e) => onPromptContentChange(e.target.value)}
            dense
            className="w-full min-h-[220px]"
            placeholder={promptPlaceholder}
          />
          <div className="mt-2 flex items-center gap-2">
            <Button type="button" onClick={onSavePromptFile} disabled={!String(draft.system_prompt_file || '').trim() || promptPathInvalid} variant="primary" size="sm" radius="lg" gap="1">
              <Save className="w-4 h-4" />
              {t('savePromptFile')}
            </Button>
          </div>
        </FieldBlock>
        <FieldBlock label={maxRetriesLabel}>
          <TextField
            type="number"
            min={0}
            value={Number(draft.max_retries || 0)}
            onChange={(e) => onChange({ ...draft, max_retries: Number(e.target.value) || 0 })}
            dense
            className="w-full"
          />
        </FieldBlock>
        <FieldBlock label={retryBackoffLabel}>
          <TextField
            type="number"
            min={0}
            value={Number(draft.retry_backoff_ms || 0)}
            onChange={(e) => onChange({ ...draft, retry_backoff_ms: Number(e.target.value) || 0 })}
            dense
            className="w-full"
          />
        </FieldBlock>
        <FieldBlock label={maxTaskCharsLabel}>
          <TextField
            type="number"
            min={0}
            value={Number(draft.max_task_chars || 0)}
            onChange={(e) => onChange({ ...draft, max_task_chars: Number(e.target.value) || 0 })}
            dense
            className="w-full"
          />
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label={maxResultCharsLabel}>
          <TextField
            type="number"
            min={0}
            value={Number(draft.max_result_chars || 0)}
            onChange={(e) => onChange({ ...draft, max_result_chars: Number(e.target.value) || 0 })}
            dense
            className="w-full"
          />
        </FieldBlock>
      </div>
      </div>
      <div className="flex items-center gap-2 pt-4 mt-auto">
        <Button onClick={onSave} disabled={saving} variant="primary" size="sm" radius="lg" gap="1">
          <Save className="w-4 h-4" />
          {isExisting ? t('update') : t('create')}
        </Button>
        <FixedButton onClick={onDelete} disabled={!draft.agent_id} variant="danger" label={t('delete')}>
          <Trash2 className="w-4 h-4" />
        </FixedButton>
      </div>
    </div>
  );
};

export default ProfileEditorPanel;
