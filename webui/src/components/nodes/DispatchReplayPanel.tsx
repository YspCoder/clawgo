import React from 'react';
import CodeBlockPanel from '../CodeBlockPanel';
import NoticePanel from '../NoticePanel';
import { SelectField, TextField, TextareaField } from '../FormControls';

type DispatchReplayPanelProps = {
  argsLabel: string;
  modeLabel: string;
  modelLabel: string;
  onArgsChange: (value: string) => void;
  onModeChange: (value: string) => void;
  onModelChange: (value: string) => void;
  onTaskChange: (value: string) => void;
  replayArgsDraft: string;
  replayError: string;
  replayModeDraft: string;
  replayModelDraft: string;
  replayResult: any;
  replayTaskDraft: string;
  resultTitle: string;
  taskLabel: string;
  title: string;
};

const DispatchReplayPanel: React.FC<DispatchReplayPanelProps> = ({
  argsLabel,
  modeLabel,
  modelLabel,
  onArgsChange,
  onModeChange,
  onModelChange,
  onTaskChange,
  replayArgsDraft,
  replayError,
  replayModeDraft,
  replayModelDraft,
  replayResult,
  replayTaskDraft,
  resultTitle,
  taskLabel,
  title,
}) => {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
      <div>
        <div className="text-zinc-500 text-xs mb-1">{title}</div>
        <div className="space-y-2">
          <div className="grid grid-cols-3 gap-2">
            <label className="space-y-1">
              <div className="text-zinc-500 text-[11px]">{modeLabel}</div>
              <SelectField dense value={replayModeDraft} onChange={(e) => onModeChange(e.target.value)} className="w-full">
                <option value="auto">auto</option>
                <option value="p2p">p2p</option>
                <option value="relay">relay</option>
              </SelectField>
            </label>
            <label className="space-y-1 col-span-2">
              <div className="text-zinc-500 text-[11px]">{modelLabel}</div>
              <TextField dense value={replayModelDraft} onChange={(e) => onModelChange(e.target.value)} className="w-full" />
            </label>
          </div>
          <label className="space-y-1 block">
            <div className="text-zinc-500 text-[11px]">{taskLabel}</div>
            <TextareaField dense value={replayTaskDraft} onChange={(e) => onTaskChange(e.target.value)} className="min-h-24 w-full p-3" />
          </label>
          <label className="space-y-1 block">
            <div className="text-zinc-500 text-[11px]">{argsLabel}</div>
            <TextareaField dense monospace value={replayArgsDraft} onChange={(e) => onArgsChange(e.target.value)} className="min-h-40 w-full p-3" />
          </label>
        </div>
      </div>
      <div>
        <div className="text-zinc-500 text-xs mb-1">{resultTitle}</div>
        {replayError ? (
          <NoticePanel tone="danger">{replayError}</NoticePanel>
        ) : (
          <CodeBlockPanel label="" pre>{replayResult ? JSON.stringify(replayResult, null, 2) : '-'}</CodeBlockPanel>
        )}
      </div>
    </div>
  );
};

export default DispatchReplayPanel;
