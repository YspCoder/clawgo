import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { TextField } from '../components/ui/FormControls';
import PageHeader from '../components/layout/PageHeader';

type CodeItem = {
  code: number;
  text: string;
};

const LogCodes: React.FC = () => {
  const { t } = useTranslation();
  const { q } = useAppContext();
  const [items, setItems] = useState<CodeItem[]>([]);
  const [kw, setKw] = useState('');

  useEffect(() => {
    const load = async () => {
      try {
        const paths = [`/webui/log-codes.json${q}`, '/log-codes.json'];
        for (const p of paths) {
          const r = await fetch(p);
          if (!r.ok) continue;
          const j = await r.json();
          if (Array.isArray(j?.items)) {
            setItems(j.items);
            return;
          }
        }
      } catch {
        setItems([]);
      }
    };
    load();
  }, [q]);

  const filtered = useMemo(() => {
    const k = kw.trim().toLowerCase();
    if (!k) return items;
    return items.filter((it) => String(it.code).includes(k) || it.text.toLowerCase().includes(k));
  }, [items, kw]);

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-6">
      <PageHeader
        title={t('logCodes')}
        titleClassName="ui-text-secondary"
        actions={
          <TextField
            value={kw}
            onChange={(e) => setKw(e.target.value)}
            placeholder={t('logCodesSearchPlaceholder')}
            className="w-full sm:w-80"
          />
        }
      />

      <div className="brand-card ui-border-subtle border rounded-2xl overflow-hidden">
        <table className="w-full text-sm">
          <thead className="ui-soft-panel ui-border-subtle border-b">
            <tr className="ui-text-secondary">
              <th className="text-left p-3 font-medium w-40">{t('code')}</th>
              <th className="text-left p-3 font-medium">{t('template')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((it) => (
              <tr key={it.code} className="ui-border-subtle ui-row-hover border-b">
                <td className="p-3">
                  <span className="ui-code-badge px-3 py-1 font-mono text-sm">{it.code}</span>
                </td>
                <td className="ui-text-secondary p-3 break-all">{it.text}</td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td className="ui-text-muted p-6" colSpan={2}>{t('logCodesNoCodes')}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};

export default LogCodes;
