import React, { useEffect, useState } from 'react';
import { useAppContext } from '../context/AppContext';

const Memory: React.FC = () => {
  const { q } = useAppContext();
  const [files, setFiles] = useState<string[]>([]);
  const [active, setActive] = useState('');
  const [content, setContent] = useState('');

  async function loadFiles() {
    const r = await fetch(`/webui/api/memory${q}`);
    const j = await r.json();
    setFiles(Array.isArray(j.files) ? j.files : []);
  }

  const qp = (k: string, v: string) => `${q}${q ? '&' : '?'}${k}=${encodeURIComponent(v)}`;

  async function openFile(path: string) {
    const r = await fetch(`/webui/api/memory${qp('path', path)}`);
    const j = await r.json();
    setActive(path);
    setContent(j.content || '');
  }

  async function saveFile() {
    if (!active) return;
    await fetch(`/webui/api/memory${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: active, content }),
    });
    await loadFiles();
  }

  async function removeFile(path: string) {
    await fetch(`/webui/api/memory${qp('path', path)}`, { method: 'DELETE' });
    if (active === path) {
      setActive('');
      setContent('');
    }
    await loadFiles();
  }

  async function createFile() {
    const name = prompt('memory file name', `note-${Date.now()}.md`);
    if (!name) return;
    await fetch(`/webui/api/memory${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ path: name, content: '' }),
    });
    await loadFiles();
    await openFile(name);
  }

  useEffect(() => {
    loadFiles().catch(() => {});
  }, [q]);

  return (
    <div className="flex h-full">
      <aside className="w-72 border-r border-zinc-800 p-4 space-y-2 overflow-y-auto">
        <div className="flex items-center justify-between">
          <h2 className="font-semibold">Memory Files</h2>
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
          <h2 className="font-semibold">{active || 'No file selected'}</h2>
          <button onClick={saveFile} className="px-3 py-1 rounded bg-indigo-600">Save</button>
        </div>
        <textarea value={content} onChange={(e) => setContent(e.target.value)} className="w-full h-[80vh] bg-zinc-900 border border-zinc-800 rounded p-3" />
      </main>
    </div>
  );
};

export default Memory;
