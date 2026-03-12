import React from 'react';
import { Pencil, Trash2 } from 'lucide-react';
import { FixedButton } from '../ui/Button';
import InfoTile from '../data-display/InfoTile';
import NoticePanel from '../layout/NoticePanel';

type MCPServerCheck = {
  name: string;
  status?: string;
  message?: string;
  package?: string;
};

type MCPServerCardProps = {
  check?: MCPServerCheck | null;
  name: string;
  onEdit: () => void;
  onRemove: () => void;
  server: any;
  t: (key: string, options?: any) => string;
};

const MCPServerCard: React.FC<MCPServerCardProps> = ({
  check,
  name,
  onEdit,
  onRemove,
  server,
  t,
}) => {
  const transport = String(server?.transport || 'stdio');

  return (
    <div className="brand-card ui-panel rounded-[24px] p-4 space-y-4">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <div className="flex items-center gap-2 flex-wrap">
            <div className="font-mono text-sm text-zinc-100">{name}</div>
            <span className={`ui-pill inline-flex items-center rounded-full px-2 py-0.5 text-[11px] border ${server?.enabled ? 'ui-pill-success' : 'ui-pill-neutral'}`}>
              {server?.enabled ? t('enable') : t('paused')}
            </span>
            <span className="inline-flex items-center rounded-full px-2 py-0.5 text-[11px] bg-zinc-900/70 text-zinc-400 border border-zinc-800">
              {transport}
            </span>
          </div>
          <div className="text-sm text-zinc-400 break-all">
            {transport === 'stdio' ? String(server?.command || '-') : String(server?.url || '-')}
          </div>
          {server?.description ? (
            <div className="text-xs text-zinc-500 line-clamp-2">{String(server.description)}</div>
          ) : null}
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <FixedButton onClick={onEdit} radius="xl" label={t('edit')}>
            <Pencil className="w-4 h-4" />
          </FixedButton>
          <FixedButton onClick={onRemove} variant="danger" radius="xl" label={t('delete')}>
            <Trash2 className="w-4 h-4" />
          </FixedButton>
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 text-xs">
        <InfoTile
          label={t('mcpPackage')}
          className="ui-code-panel px-3 py-2"
          contentClassName="mt-1 break-all text-zinc-200 text-sm"
        >
          {String(server?.package || '-')}
        </InfoTile>
        <InfoTile
          label={t('mcpArgs')}
          className="ui-code-panel px-3 py-2"
          contentClassName="mt-1 text-zinc-200 text-sm"
        >
          {Array.isArray(server?.args) ? server.args.length : 0}
        </InfoTile>
        <InfoTile
          label={t('mcpPermission')}
          className="ui-code-panel px-3 py-2"
          contentClassName="mt-1 text-zinc-200 text-sm"
        >
          {String(server?.permission || 'workspace')}
        </InfoTile>
      </div>

      {check && check.status !== 'ok' && check.status !== 'disabled' && check.status !== 'not_applicable' ? (
        <NoticePanel className="px-3 py-2 text-xs">
          <div>{check.message || t('configMCPCommandMissing')}</div>
          {check.package ? (
            <div className="mt-1 text-amber-300/80">{t('configMCPInstallSuggested', { pkg: check.package })}</div>
          ) : null}
        </NoticePanel>
      ) : null}
    </div>
  );
};

export default MCPServerCard;
