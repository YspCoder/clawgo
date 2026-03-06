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
    <div className="flex h-full">
      <aside className="w-72 border-r border-zinc-800 p-4 space-y-2 overflow-y-auto">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">{t('memoryFiles')}</h2>
          <button onClick={createFile} className="px-2 py-1 rounded bg-zinc-800">+</button>
        </div>
        {files.map((f) => (
          <div key={f} className={`flex items-center justify-between p-2 rounded ${active === f ? 'bg-zinc-800' : 'hover:bg-zinc-900'}`}>
            <button className="text-left flex-1" onClick={() => openFile(f)}>{f}</button>
            <button className="text-red-400" onClick={() => removeFile(f)}>x</button>
          </div>
        ))}
      </aside>
      <main className="flex-1 p-4 space-y-3">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">{active || t('noFileSelected')}</h2>
          <button onClick={saveFile} className="px-3 py-1 rounded bg-indigo-600">{t('save')}</button>
        </div>
        <textarea value={content} onChange={(e) => setContent(e.target.value)} className="w-full h-[80vh] bg-zinc-900 border border-zinc-800 rounded p-3" />
      </main>
    </div>
  );
};

export default Memory;
