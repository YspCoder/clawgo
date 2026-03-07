import React, { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

const Memory: React.FC = () => {
  const { t } = useTranslation();
  const ui = useUI();
  const { q } = useAppContext();
  const [files, setFiles] = useState<string[]>([]);
  const [active, setActive] = useState('');
  const [content, setContent] = useState('');

  async function loadFiles() {
    const r = await fetch(`/webui/api/memory${q}`);
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return;
    }
    const j = await r.json();
    setFiles(Array.isArray(j.files) ? j.files : []);
  }

  const qp = (k: string, v: string) => `${q}${q ? '&' : '?'}${k}=${encodeURIComponent(v)}`;

  async function openFile(path: string) {
    const r = await fetch(`/webui/api/memory${qp('path', path)}`);
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return;
    }
    const j = await r.json();
    setActive(path);
    setContent(j.content || '');
  }

  async function saveFile() {
    if (!active) return;
    try {
      await ui.withLoading(async () => {
        const r = await fetch(`/webui/api/memory${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: active, content }),
        });
        if (!r.ok) {
          throw new Error(await r.text());
        }
        await loadFiles();
      }, t('saving'));
      await ui.notify({ title: t('saved'), message: t('memoryFileSaved') });
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: String(e) });
    }
  }

  async function removeFile(path: string) {
    const ok = await ui.confirmDialog({
      title: t('memoryDeleteConfirmTitle'),
      message: t('memoryDeleteConfirmMessage', { path }),
      danger: true,
      confirmText: t('delete'),
    });
    if (!ok) return;
    try {
      await ui.withLoading(async () => {
        const r = await fetch(`/webui/api/memory${qp('path', path)}`, { method: 'DELETE' });
        if (!r.ok) {
          throw new Error(await r.text());
        }
        if (active === path) {
          setActive('');
          setContent('');
        }
        await loadFiles();
      }, t('deleting'));
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: String(e) });
    }
  }

  async function createFile() {
    const name = await ui.promptDialog({
      title: t('memoryCreateTitle'),
      message: t('memoryFileNamePrompt'),
      confirmText: t('create'),
      initialValue: `note-${Date.now()}.md`,
      inputPlaceholder: 'note.md',
    });
    if (!name) return;
    try {
      await ui.withLoading(async () => {
        const r = await fetch(`/webui/api/memory${q}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ path: name, content: '' }),
        });
        if (!r.ok) {
          throw new Error(await r.text());
        }
        await loadFiles();
        await openFile(name);
      }, t('creating'));
    } catch (e) {
      await ui.notify({ title: t('requestFailed'), message: String(e) });
    }
  }

  useEffect(() => {
    loadFiles().catch(() => {});
  }, [q]);

  return (
    <div className="flex h-full flex-col lg:flex-row brand-card rounded-[30px] border border-zinc-800 overflow-hidden">
      <aside className="w-full lg:w-72 border-b lg:border-b-0 lg:border-r border-zinc-800 p-4 space-y-2 overflow-y-auto bg-zinc-950/20">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">{t('memoryFiles')}</h2>
          <button onClick={createFile} className="brand-button px-2.5 py-1 rounded-xl text-white">+</button>
        </div>
        {files.map((f) => (
          <div key={f} className={`flex items-center justify-between p-2.5 rounded-2xl ${active === f ? 'nav-item-active' : 'hover:bg-zinc-900/30'}`}>
            <button className="text-left flex-1" onClick={() => openFile(f)}>{f}</button>
            <button className="text-red-400" onClick={() => removeFile(f)}>x</button>
          </div>
        ))}
      </aside>
      <main className="flex-1 p-4 md:p-6 space-y-3 min-h-0">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">{active || t('noFileSelected')}</h2>
          <button onClick={saveFile} className="brand-button px-3 py-1.5 rounded-xl text-white">{t('save')}</button>
        </div>
        <textarea value={content} onChange={(e) => setContent(e.target.value)} className="w-full h-[50vh] lg:h-[80vh] bg-zinc-900/70 border border-zinc-800 rounded-[24px] p-4 focus:outline-none focus:ring-2 focus:ring-indigo-500/20" />
      </main>
    </div>
  );
};

export default Memory;
