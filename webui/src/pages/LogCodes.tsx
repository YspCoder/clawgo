import React, { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';

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
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold tracking-tight">{t('logCodes')}</h1>
        <input
          value={kw}
          onChange={(e) => setKw(e.target.value)}
          placeholder={t('logCodesSearchPlaceholder')}
          className="w-full sm:w-80 bg-zinc-900 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500"
        />
      </div>

      <div className="bg-zinc-950 border border-zinc-800 rounded-2xl overflow-hidden">
        <table className="w-full text-sm">
          <thead className="bg-zinc-900/90 border-b border-zinc-800">
            <tr className="text-zinc-400">
              <th className="text-left p-3 font-medium w-40">{t('code')}</th>
              <th className="text-left p-3 font-medium">{t('template')}</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((it) => (
              <tr key={it.code} className="border-b border-zinc-900 hover:bg-zinc-900/40">
                <td className="p-3 font-mono text-indigo-300">{it.code}</td>
                <td className="p-3 text-zinc-200 break-all">{it.text}</td>
              </tr>
            ))}
            {filtered.length === 0 && (
              <tr>
                <td className="p-6 text-zinc-500" colSpan={2}>{t('logCodesNoCodes')}</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
};

export default LogCodes;
