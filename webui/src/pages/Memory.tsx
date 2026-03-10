import React, { useEffect, useState } from 'react';
import { Trash2 } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/Button';

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
    <div className="h-full p-4 md:p-5 xl:p-6">
      <div className="flex h-full flex-col overflow-hidden rounded-[30px] border brand-card ui-border-subtle lg:flex-row">
        <aside className="ui-border-subtle w-full overflow-y-auto border-b p-2 md:p-3 lg:w-72 lg:border-r lg:border-b-0">
          <div className="sidebar-section rounded-[24px] p-2 md:p-2.5 space-y-1">
            <div className="flex items-center justify-between">
              <h2 className="ui-text-primary font-semibold">{t('memoryFiles')}</h2>
              <FixedButton onClick={createFile} variant="primary" shape="square" radius="xl" label={t('add')}>
                +
              </FixedButton>
            </div>
            <div className="space-y-1">
              {files.map((f) => (
                <div key={f} className={`flex items-center justify-between px-2.5 py-2 rounded-2xl ${active === f ? 'nav-item-active' : 'ui-row-hover'}`}>
                  <button className={`text-left flex-1 min-w-0 break-all pr-2 ${active === f ? 'ui-text-primary font-medium' : 'ui-text-primary'}`} onClick={() => openFile(f)}>{f}</button>
                  <button
                    className="ui-text-danger ui-text-danger-hover shrink-0 p-1"
                    onClick={() => removeFile(f)}
                    aria-label={t('delete')}
                    title={t('delete')}
                  >
                    <Trash2 className="h-4 w-4" />
                  </button>
                </div>
              ))}
            </div>
          </div>
        </aside>
        <main className="flex-1 overflow-y-auto p-4 md:p-5">
          <div className="space-y-4">
            <div className="flex items-center justify-between">
              <h2 className="ui-text-primary font-semibold">{active || t('noFileSelected')}</h2>
              <Button onClick={saveFile} variant="primary" size="sm" radius="xl">{t('save')}</Button>
            </div>
            <textarea value={content} onChange={(e) => setContent(e.target.value)} className="ui-textarea w-full h-[50vh] lg:h-[80vh] rounded-[24px] p-4" />
          </div>
        </main>
      </div>
    </div>
  );
};

export default Memory;
