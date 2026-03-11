import React from 'react';
import { Plus } from 'lucide-react';
import { FixedButton } from '../Button';
import { CheckboxField, PanelField, SelectField, TextField, TextareaField } from '../FormControls';

type Translate = (key: string) => string;

type GatewayIceServerRowProps = {
  credential: string;
  onCredentialChange: (value: string) => void;
  onRemove: () => void;
  onUrlsChange: (value: string) => void;
  onUsernameChange: (value: string) => void;
  t: Translate;
  urls: string;
  username: string;
};

export function GatewayIceServerRow({
  credential,
  onCredentialChange,
  onRemove,
  onUrlsChange,
  onUsernameChange,
  t,
  urls,
  username,
}: GatewayIceServerRowProps) {
  return (
    <div className="grid grid-cols-1 md:grid-cols-7 gap-2 rounded-xl border border-zinc-800 bg-zinc-900/30 p-2 text-xs">
      <TextField
        value={urls}
        onChange={(e) => onUrlsChange(e.target.value)}
        placeholder={t('configNodeP2PIceUrlsPlaceholder')}
        dense
        className="md:col-span-3"
      />
      <TextField
        value={username}
        onChange={(e) => onUsernameChange(e.target.value)}
        placeholder={t('configNodeP2PIceUsername')}
        dense
        className="md:col-span-1"
      />
      <TextField
        value={credential}
        onChange={(e) => onCredentialChange(e.target.value)}
        placeholder={t('configNodeP2PIceCredential')}
        dense
        className="md:col-span-2"
      />
      <button onClick={onRemove} className="ui-button-danger rounded-xl px-3 py-2 text-xs font-medium transition-colors">
        {t('delete')}
      </button>
    </div>
  );
}

type GatewayP2PSectionProps = {
  addGatewayIceServer: () => void;
  iceServers: Array<any>;
  onP2PFieldChange: (field: string, value: any) => void;
  onRemoveGatewayIceServer: (index: number) => void;
  onUpdateGatewayIceServer: (index: number, field: string, value: any) => void;
  p2p: any;
  t: Translate;
};

export function GatewayP2PSection({
  addGatewayIceServer,
  iceServers,
  onP2PFieldChange,
  onRemoveGatewayIceServer,
  onUpdateGatewayIceServer,
  p2p,
  t,
}: GatewayP2PSectionProps) {
  return (
    <>
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div className="text-sm font-semibold text-zinc-200">{t('configNodeP2P')}</div>
        <div className="text-xs text-zinc-500">{t('configNodeP2PHint')}</div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs">
        <PanelField label={t('enable')}>
          <CheckboxField
            checked={Boolean(p2p?.enabled)}
            onChange={(e) => onP2PFieldChange('enabled', e.target.checked)}
          />
        </PanelField>
        <PanelField label={t('dashboardNodeP2PTransport')}>
          <SelectField
            value={String(p2p?.transport || 'websocket_tunnel')}
            onChange={(e) => onP2PFieldChange('transport', e.target.value)}
            dense
            className="w-full min-h-0"
          >
            <option value="websocket_tunnel">websocket_tunnel</option>
            <option value="webrtc">webrtc</option>
          </SelectField>
        </PanelField>
        <PanelField label={t('dashboardNodeP2PIce')}>
          <TextField
            value={Array.isArray(p2p?.stun_servers) ? p2p.stun_servers.join(', ') : ''}
            onChange={(e) => onP2PFieldChange('stun_servers', e.target.value.split(',').map((s) => s.trim()).filter(Boolean))}
            placeholder={t('configNodeP2PStunPlaceholder')}
            dense
            className="w-full"
          />
        </PanelField>
      </div>
      <div className="space-y-2">
        <div className="flex items-center justify-between gap-2 flex-wrap">
          <div className="text-sm font-medium text-zinc-200">{t('configNodeP2PIceServers')}</div>
          <FixedButton onClick={addGatewayIceServer} variant="primary" label={t('add')}>
            <Plus className="w-4 h-4" />
          </FixedButton>
        </div>
        {iceServers.length > 0 ? (
          iceServers.map((server, index) => (
            <GatewayIceServerRow
              key={`ice-${index}`}
              credential={String(server?.credential || '')}
              onCredentialChange={(value) => onUpdateGatewayIceServer(index, 'credential', value)}
              onRemove={() => onRemoveGatewayIceServer(index)}
              onUrlsChange={(value) => onUpdateGatewayIceServer(index, 'urls', value.split(',').map((s) => s.trim()).filter(Boolean))}
              onUsernameChange={(value) => onUpdateGatewayIceServer(index, 'username', value)}
              t={t}
              urls={Array.isArray(server?.urls) ? server.urls.join(', ') : ''}
              username={String(server?.username || '')}
            />
          ))
        ) : (
          <div className="text-xs text-zinc-500">{t('configNodeP2PIceServersEmpty')}</div>
        )}
      </div>
    </>
  );
}

type GatewayDispatchSectionProps = {
  dispatch: any;
  formatTagRuleText: (value: unknown) => string;
  onDispatchFieldChange: (field: string, value: any) => void;
  parseTagRuleText: (raw: string) => Record<string, string[]>;
  t: Translate;
};

export function GatewayDispatchSection({
  dispatch,
  formatTagRuleText,
  onDispatchFieldChange,
  parseTagRuleText,
  t,
}: GatewayDispatchSectionProps) {
  return (
    <div className="border-t border-zinc-800/70 pt-3 space-y-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div className="text-sm font-semibold text-zinc-200">{t('configNodeDispatch')}</div>
        <div className="text-xs text-zinc-500">{t('configNodeDispatchHint')}</div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs">
        <PanelField label={t('configNodeDispatchPreferLocal')}>
          <CheckboxField checked={Boolean(dispatch?.prefer_local)} onChange={(e) => onDispatchFieldChange('prefer_local', e.target.checked)} />
        </PanelField>
        <PanelField label={t('configNodeDispatchPreferP2P')}>
          <CheckboxField checked={Boolean(dispatch?.prefer_p2p ?? true)} onChange={(e) => onDispatchFieldChange('prefer_p2p', e.target.checked)} />
        </PanelField>
        <PanelField label={t('configNodeDispatchAllowRelay')}>
          <CheckboxField checked={Boolean(dispatch?.allow_relay_fallback ?? true)} onChange={(e) => onDispatchFieldChange('allow_relay_fallback', e.target.checked)} />
        </PanelField>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
        <PanelField label={t('configNodeDispatchActionTags')}>
          <TextareaField
            value={formatTagRuleText(dispatch?.action_tags)}
            onChange={(e) => onDispatchFieldChange('action_tags', parseTagRuleText(e.target.value))}
            placeholder={t('configNodeDispatchActionTagsPlaceholder')}
            className="min-h-28 w-full bg-zinc-950/70 border-zinc-800"
          />
        </PanelField>
        <PanelField label={t('configNodeDispatchAgentTags')}>
          <TextareaField
            value={formatTagRuleText(dispatch?.agent_tags)}
            onChange={(e) => onDispatchFieldChange('agent_tags', parseTagRuleText(e.target.value))}
            placeholder={t('configNodeDispatchAgentTagsPlaceholder')}
            className="min-h-28 w-full bg-zinc-950/70 border-zinc-800"
          />
        </PanelField>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
        <PanelField label={t('configNodeDispatchAllowActions')}>
          <TextareaField
            value={formatTagRuleText(dispatch?.allow_actions)}
            onChange={(e) => onDispatchFieldChange('allow_actions', parseTagRuleText(e.target.value))}
            placeholder={t('configNodeDispatchAllowActionsPlaceholder')}
            className="min-h-28 w-full bg-zinc-950/70 border-zinc-800"
          />
        </PanelField>
        <PanelField label={t('configNodeDispatchDenyActions')}>
          <TextareaField
            value={formatTagRuleText(dispatch?.deny_actions)}
            onChange={(e) => onDispatchFieldChange('deny_actions', parseTagRuleText(e.target.value))}
            placeholder={t('configNodeDispatchDenyActionsPlaceholder')}
            className="min-h-28 w-full bg-zinc-950/70 border-zinc-800"
          />
        </PanelField>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-3 text-xs">
        <PanelField label={t('configNodeDispatchAllowAgents')}>
          <TextareaField
            value={formatTagRuleText(dispatch?.allow_agents)}
            onChange={(e) => onDispatchFieldChange('allow_agents', parseTagRuleText(e.target.value))}
            placeholder={t('configNodeDispatchAllowAgentsPlaceholder')}
            className="min-h-28 w-full bg-zinc-950/70 border-zinc-800"
          />
        </PanelField>
        <PanelField label={t('configNodeDispatchDenyAgents')}>
          <TextareaField
            value={formatTagRuleText(dispatch?.deny_agents)}
            onChange={(e) => onDispatchFieldChange('deny_agents', parseTagRuleText(e.target.value))}
            placeholder={t('configNodeDispatchDenyAgentsPlaceholder')}
            className="min-h-28 w-full bg-zinc-950/70 border-zinc-800"
          />
        </PanelField>
      </div>
    </div>
  );
}

type GatewayArtifactsSectionProps = {
  artifacts: any;
  onArtifactsFieldChange: (field: string, value: any) => void;
  t: Translate;
};

export function GatewayArtifactsSection({ artifacts, onArtifactsFieldChange, t }: GatewayArtifactsSectionProps) {
  return (
    <div className="border-t border-zinc-800/70 pt-3 space-y-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div className="text-sm font-semibold text-zinc-200">{t('configNodeArtifacts')}</div>
        <div className="text-xs text-zinc-500">{t('configNodeArtifactsHint')}</div>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-3 gap-2 text-xs">
        <PanelField label={t('enable')}>
          <CheckboxField checked={Boolean(artifacts?.enabled)} onChange={(e) => onArtifactsFieldChange('enabled', e.target.checked)} />
        </PanelField>
        <PanelField label={t('configNodeArtifactsKeepLatest')}>
          <TextField
            type="number"
            min={1}
            value={Number(artifacts?.keep_latest || 500)}
            onChange={(e) => onArtifactsFieldChange('keep_latest', Math.max(1, Number.parseInt(e.target.value || '0', 10) || 1))}
            dense
            className="w-full"
          />
        </PanelField>
        <PanelField label={t('configNodeArtifactsPruneOnRead')}>
          <CheckboxField checked={Boolean(artifacts?.prune_on_read ?? true)} onChange={(e) => onArtifactsFieldChange('prune_on_read', e.target.checked)} />
        </PanelField>
      </div>
      <div className="grid grid-cols-1 md:grid-cols-2 gap-2 text-xs">
        <PanelField label={t('configNodeArtifactsRetainDays')}>
          <TextField
            type="number"
            min={0}
            value={Number(artifacts?.retain_days ?? 7)}
            onChange={(e) => onArtifactsFieldChange('retain_days', Math.max(0, Number.parseInt(e.target.value || '0', 10) || 0))}
            dense
            className="w-full"
          />
        </PanelField>
      </div>
    </div>
  );
}
