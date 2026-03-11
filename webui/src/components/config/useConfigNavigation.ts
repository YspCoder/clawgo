import { useMemo } from 'react';

type Translation = (key: string, options?: any) => string;

export function useConfigNavigation({
  basicMode,
  cfg,
  hotOnly,
  hotReloadFieldDetails,
  search,
  selectedTop,
  t,
}: {
  basicMode: boolean;
  cfg: any;
  hotOnly: boolean;
  hotReloadFieldDetails: Array<any>;
  search: string;
  selectedTop: string;
  t: Translation;
}) {
  const configLabels = useMemo(
    () => t('configLabels', { returnObjects: true }) as unknown as Record<string, string>,
    [t]
  );

  const hotPrefixes = useMemo(
    () => hotReloadFieldDetails.map((item) => String(item.path || '').replace(/\.\*$/, '')).filter(Boolean),
    [hotReloadFieldDetails],
  );

  const hotReloadTabKey = '__hot_reload__';

  const allTopKeys = useMemo(
    () => Object.keys(cfg || {}).filter((key) => key !== 'providers' && typeof (cfg as any)?.[key] === 'object' && (cfg as any)?.[key] !== null),
    [cfg],
  );

  const basicTopKeys = useMemo(() => {
    const preferred = ['gateway', 'tools', 'cron', 'agents', 'logging'];
    return preferred.filter((key) => allTopKeys.includes(key));
  }, [allTopKeys]);

  const filteredTopKeys = useMemo(() => {
    let keys = basicMode ? basicTopKeys : allTopKeys;
    if (hotOnly) {
      keys = keys.filter((key) => hotPrefixes.some((prefix) => prefix === key || prefix.startsWith(`${key}.`) || key.startsWith(`${prefix}.`)));
    }
    if (search.trim()) {
      const normalized = search.trim().toLowerCase();
      keys = keys.filter((key) => key.toLowerCase().includes(normalized));
    }
    return [hotReloadTabKey, ...keys];
  }, [allTopKeys, basicTopKeys, basicMode, hotOnly, hotPrefixes, search]);

  const activeTop = filteredTopKeys.includes(selectedTop) ? selectedTop : (filteredTopKeys[0] || '');

  return {
    activeTop,
    configLabels,
    filteredTopKeys,
    hotReloadTabKey,
  };
}
