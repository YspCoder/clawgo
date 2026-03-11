import React from 'react';
import { Pause, Play, Save, Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { Button, FixedButton } from '../Button';
import { FieldBlock, SelectField, TextField, TextareaField } from '../FormControls';
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
  onDisable: () => void;
  onEnable: () => void;
  onPromptContentChange: (value: string) => void;
  onSave: () => void;
  onSavePromptFile: () => void;
  promptContent: string;
  promptMeta: string;
  promptPlaceholder: string;
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
  onDisable,
  onEnable,
  onPromptContentChange,
  onSave,
  onSavePromptFile,
  promptContent,
  promptMeta,
  promptPlaceholder,
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

  return (
    <div className="brand-card ui-border-subtle rounded-[28px] border p-4 space-y-3">
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
        <FieldBlock label={idLabel}>
          <TextField
            value={draft.agent_id || ''}
            disabled={isExisting}
            onChange={(e) => onChange({ ...draft, agent_id: e.target.value })}
            dense
            className="w-full disabled:opacity-60"
            placeholder="coder"
          />
        </FieldBlock>
        <FieldBlock label={nameLabel}>
          <TextField
            value={draft.name || ''}
            onChange={(e) => onChange({ ...draft, name: e.target.value })}
            dense
            className="w-full"
            placeholder="Code Agent"
          />
        </FieldBlock>
        <FieldBlock label={roleLabel}>
          <TextField
            value={draft.role || ''}
            onChange={(e) => onChange({ ...draft, role: e.target.value })}
            dense
            className="w-full"
            placeholder="coding"
          />
        </FieldBlock>
        <FieldBlock label={statusLabel}>
          <SelectField
            value={draft.status || 'active'}
            onChange={(e) => onChange({ ...draft, status: e.target.value })}
            dense
            className="w-full"
          >
            <option value="active">active</option>
            <option value="disabled">disabled</option>
          </SelectField>
        </FieldBlock>
        <FieldBlock label="notify_main_policy">
          <SelectField
            value={draft.notify_main_policy || 'final_only'}
            onChange={(e) => onChange({ ...draft, notify_main_policy: e.target.value })}
            dense
            className="w-full"
          >
            <option value="final_only">final_only</option>
            <option value="internal_only">internal_only</option>
            <option value="milestone">milestone</option>
            <option value="on_blocked">on_blocked</option>
            <option value="always">always</option>
          </SelectField>
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label="system_prompt_file">
          <TextField
            value={draft.system_prompt_file || ''}
            onChange={(e) => onChange({ ...draft, system_prompt_file: e.target.value })}
            dense
            className="w-full"
            placeholder="agents/coder/AGENT.md"
          />
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label={memoryNamespaceLabel}>
          <TextField
            value={draft.memory_namespace || ''}
            onChange={(e) => onChange({ ...draft, memory_namespace: e.target.value })}
            dense
            className="w-full"
            placeholder="coder"
          />
        </FieldBlock>
        <FieldBlock className="md:col-span-2" label={toolAllowlistLabel}>
          <TextField
            value={allowlistText}
            onChange={(e) => onChange({ ...draft, tool_allowlist: parseAllowlist(e.target.value) })}
            dense
            className="w-full"
            placeholder="read_file, list_files, memory_search"
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
        <FieldBlock className="md:col-span-2" label="system_prompt_file content" meta={promptMeta}>
          <TextareaField
            value={promptContent}
            onChange={(e) => onPromptContentChange(e.target.value)}
            dense
            className="w-full min-h-[220px]"
            placeholder={promptPlaceholder}
          />
          <div className="mt-2 flex items-center gap-2">
            <FixedButton type="button" onClick={onSavePromptFile} disabled={!String(draft.system_prompt_file || '').trim()} radius="lg" label={t('savePromptFile')}>
              <Save className="w-4 h-4" />
            </FixedButton>
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
      <div className="flex items-center gap-2">
        <FixedButton onClick={onSave} disabled={saving} variant="primary" label={isExisting ? t('update') : t('create')}>
          <Save className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onEnable} disabled={!draft.agent_id} variant="success" label={t('enable')}>
          <Play className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onDisable} disabled={!draft.agent_id} variant="warning" label={t('disable')}>
          <Pause className="w-4 h-4" />
        </FixedButton>
        <FixedButton onClick={onDelete} disabled={!draft.agent_id} variant="danger" label={t('delete')}>
          <Trash2 className="w-4 h-4" />
        </FixedButton>
      </div>
    </div>
  );
};

export default ProfileEditorPanel;
