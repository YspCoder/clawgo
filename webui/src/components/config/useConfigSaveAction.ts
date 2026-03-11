import { cloneJSON } from '../../utils/object';

type UI = {
  confirmDialog: (options: any) => Promise<boolean>;
  notify: (options: any) => Promise<void>;
  withLoading: <T>(fn: () => Promise<T>, label: string) => Promise<T>;
};

type UseConfigSaveActionArgs = {
  cfg: any;
  cfgRaw: string;
  loadConfig: (force?: boolean, tokenOverride?: string) => Promise<any>;
  q: string;
  setBaseline: React.Dispatch<React.SetStateAction<any>>;
  setConfigEditing: (editing: boolean) => void;
  setToken: (token: string) => void;
  setShowDiff: React.Dispatch<React.SetStateAction<boolean>>;
  showRaw: boolean;
  t: (key: string, options?: any) => string;
  ui: UI;
};

export function useConfigSaveAction({
  cfg,
  cfgRaw,
  loadConfig,
  q,
  setBaseline,
  setConfigEditing,
  setToken,
  setShowDiff,
  showRaw,
  t,
  ui,
}: UseConfigSaveActionArgs) {
  async function saveConfig() {
    try {
      const payload = showRaw ? JSON.parse(cfgRaw) : cfg;
      const submit = async (confirmRisky: boolean) => {
        const body = confirmRisky ? { ...payload, confirm_risky: true } : payload;
        return ui.withLoading(async () => {
          const response = await fetch(`/webui/api/config${q}`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
          });
          const text = await response.text();
          let data: any = null;
          try {
            data = text ? JSON.parse(text) : null;
          } catch {
            data = null;
          }
          return { ok: response.ok, text, data };
        }, t('saving'));
      };

      let result = await submit(false);
      if (!result.ok && result.data?.requires_confirm) {
        const changedFields = Array.isArray(result.data?.changed_fields) ? result.data.changed_fields.join(', ') : '';
        const ok = await ui.confirmDialog({
          title: t('configRiskyChangeConfirmTitle'),
          message: t('configRiskyChangeConfirmMessage', { fields: changedFields || '-' }),
          danger: true,
          confirmText: t('saveChanges'),
        });
        if (!ok) return;
        result = await submit(true);
      }

      if (!result.ok) {
        throw new Error(result.data?.error || result.text || 'save failed');
      }

      const hasGatewayToken = typeof payload?.gateway?.token === 'string';
      const nextToken = hasGatewayToken ? payload.gateway.token.trim() : '';
      const reloaded = await loadConfig(true, nextToken || undefined);
      if (hasGatewayToken) {
        setToken(nextToken);
      }
      await ui.notify({ title: t('saved'), message: t('configSaved') });
      setBaseline(cloneJSON(reloaded ?? payload));
      setConfigEditing(false);
      setShowDiff(false);
    } catch (error) {
      await ui.notify({ title: t('requestFailed'), message: `${t('saveConfigFailed')}: ${error}` });
    }
  }

  return { saveConfig };
}
