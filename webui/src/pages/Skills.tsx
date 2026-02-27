import React, { useState } from 'react';
import { Plus, RefreshCw, Trash2, Edit2, Zap, X, FileText, Save } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Skill } from '../types';

const initialSkillForm: Omit<Skill, 'id'> = {
  name: '',
  description: '',
  tools: [],
  system_prompt: ''
};

const Skills: React.FC = () => {
  const { t } = useTranslation();
  const { skills, refreshSkills, q } = useAppContext();
  const ui = useUI();
  const [installName, setInstallName] = useState('');
  const qp = (k: string, v: string) => `${q}${q ? '&' : '?'}${k}=${encodeURIComponent(v)}`;
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingSkill, setEditingSkill] = useState<Skill | null>(null);
  const [form, setForm] = useState<Omit<Skill, 'id'>>(initialSkillForm);

  const [isFileModalOpen, setIsFileModalOpen] = useState(false);
  const [activeSkill, setActiveSkill] = useState<string>('');
  const [skillFiles, setSkillFiles] = useState<string[]>([]);
  const [activeFile, setActiveFile] = useState<string>('');
  const [fileContent, setFileContent] = useState('');

  async function deleteSkill(id: string) {
    if (!await ui.confirmDialog({ title: 'Delete Skill', message: 'Are you sure you want to delete this skill?', danger: true, confirmText: 'Delete' })) return;
    try {
      await fetch(`/webui/api/skills${qp('id', id)}`, { method: 'DELETE' });
      await refreshSkills();
    } catch (e) {
      console.error(e);
    }
  }

  async function installSkill() {
    const name = installName.trim();
    if (!name) return;
    const r = await fetch(`/webui/api/skills${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'install', name }),
    });
    if (!r.ok) {
      await ui.notify({ title: 'Request Failed', message: await r.text() });
      return;
    }
    setInstallName('');
    await refreshSkills();
  }

  async function handleSubmit() {
    try {
      const action = editingSkill ? 'update' : 'create';
      const r = await fetch(`/webui/api/skills${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action, ...(editingSkill && { id: editingSkill.id }), ...form })
      });

      if (r.ok) {
        setIsModalOpen(false);
        await refreshSkills();
      } else {
        await ui.notify({ title: 'Request Failed', message: await r.text() });
      }
    } catch (e) {
      await ui.notify({ title: 'Error', message: String(e) });
    }
  }

  async function openFileManager(skillId: string) {
    setActiveSkill(skillId);
    setIsFileModalOpen(true);
    const r = await fetch(`/webui/api/skills${q ? `${q}&id=${encodeURIComponent(skillId)}&files=1` : `?id=${encodeURIComponent(skillId)}&files=1`}`);
    if (!r.ok) {
      await ui.notify({ title: 'Request Failed', message: await r.text() });
      return;
    }
    const j = await r.json();
    const files = Array.isArray(j.files) ? j.files : [];
    setSkillFiles(files);
    if (files.length > 0) {
      await openFile(skillId, files[0]);
    } else {
      setActiveFile('');
      setFileContent('');
    }
  }

  async function openFile(skillId: string, file: string) {
    const url = `/webui/api/skills${q ? `${q}&id=${encodeURIComponent(skillId)}&file=${encodeURIComponent(file)}` : `?id=${encodeURIComponent(skillId)}&file=${encodeURIComponent(file)}`}`;
    const r = await fetch(url);
    if (!r.ok) {
      await ui.notify({ title: 'Request Failed', message: await r.text() });
      return;
    }
    const j = await r.json();
    setActiveFile(file);
    setFileContent(String(j.content || ''));
  }

  async function saveFile() {
    if (!activeSkill || !activeFile) return;
    const r = await fetch(`/webui/api/skills${q}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ action: 'write_file', id: activeSkill, file: activeFile, content: fileContent }),
    });
    if (!r.ok) {
      await ui.notify({ title: 'Request Failed', message: await r.text() });
      return;
    }
    await ui.notify({ title: 'Saved', message: 'Skill file saved successfully.' });
  }

  return (
    <div className="p-8 max-w-7xl mx-auto space-y-8">
      <div className="flex items-center justify-between gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold tracking-tight">{t('skills')}</h1>
        <div className="flex items-center gap-2">
          <input value={installName} onChange={(e) => setInstallName(e.target.value)} placeholder="skill name" className="px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-sm" />
          <button onClick={installSkill} className="px-3 py-2 bg-emerald-600 hover:bg-emerald-500 text-white rounded-lg text-sm font-medium">Install</button>
        </div>
        <div className="flex items-center gap-3">
          <button onClick={() => refreshSkills()} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
            <RefreshCw className="w-4 h-4" /> {t('refresh')}
          </button>
          <button onClick={() => { setEditingSkill(null); setForm(initialSkillForm); setIsModalOpen(true); }} className="flex items-center gap-2 px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium transition-colors shadow-sm">
            <Plus className="w-4 h-4" /> Add Skill
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        {skills.map(s => (
          <div key={s.id} className="bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-6 flex flex-col shadow-sm group hover:border-zinc-700/50 transition-colors">
            <div className="flex items-start justify-between mb-4">
              <div className="flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-zinc-800/50 flex items-center justify-center border border-zinc-700/50">
                  <Zap className="w-6 h-6 text-amber-400" />
                </div>
                <div>
                  <h3 className="font-semibold text-zinc-100">{s.name}</h3>
                  <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest">ID: {s.id.slice(-6)}</span>
                </div>
              </div>
            </div>

            <p className="text-sm text-zinc-400 mb-6 line-clamp-2">{s.description || 'No description provided.'}</p>

            <div className="space-y-4 mb-6">
              <div>
                <div className="text-[10px] text-zinc-500 uppercase tracking-widest mb-2">Tools</div>
                <div className="flex flex-wrap gap-2">
                  {(Array.isArray(s.tools) ? s.tools : []).map(tool => (
                    <span key={tool} className="px-2 py-1 bg-zinc-800/50 text-zinc-300 text-[10px] font-mono rounded border border-zinc-700/50">{tool}</span>
                  ))}
                  {(!Array.isArray(s.tools) || s.tools.length === 0) && <span className="text-xs text-zinc-600 italic">No tools defined</span>}
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-4 border-t border-zinc-800/50 mt-auto">
              <button
                onClick={() => {
                  setEditingSkill(s);
                  setForm({
                    name: s.name,
                    description: s.description,
                    tools: Array.isArray(s.tools) ? s.tools : [],
                    system_prompt: s.system_prompt || ''
                  });
                  setIsModalOpen(true);
                }}
                className="flex-1 flex items-center justify-center gap-2 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-xs font-medium transition-colors text-zinc-300"
              >
                <Edit2 className="w-3.5 h-3.5" /> Edit Skill
              </button>
              <button
                onClick={() => openFileManager(s.id)}
                className="p-2 bg-indigo-500/10 text-indigo-400 hover:bg-indigo-500/20 rounded-lg transition-colors"
                title="Files"
              >
                <FileText className="w-4 h-4" />
              </button>
              <button
                onClick={() => deleteSkill(s.id)}
                className="p-2 bg-red-500/10 text-red-500 hover:bg-red-500/20 rounded-lg transition-colors"
              >
                <Trash2 className="w-4 h-4" />
              </button>
            </div>
          </div>
        ))}
      </div>

      <AnimatePresence>
        {isModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setIsModalOpen(false)} className="absolute inset-0 bg-black/60 backdrop-blur-sm" />
            <motion.div initial={{ opacity: 0, scale: 0.95 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.95 }} className="relative w-full max-w-2xl bg-zinc-900 border border-zinc-800 rounded-3xl shadow-2xl overflow-hidden">
              <div className="p-6 border-b border-zinc-800 flex items-center justify-between">
                <h2 className="text-xl font-semibold">{editingSkill ? 'Edit Skill' : 'Add Skill'}</h2>
                <button onClick={() => setIsModalOpen(false)} className="p-2 hover:bg-zinc-800 rounded-full transition-colors text-zinc-400"><X className="w-5 h-5" /></button>
              </div>
              <div className="p-6 space-y-4 max-h-[70vh] overflow-y-auto">
                <label className="block">
                  <span className="text-sm font-medium text-zinc-400 mb-1.5 block">Name</span>
                  <input type="text" value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:border-indigo-500 outline-none" />
                </label>
                <label className="block">
                  <span className="text-sm font-medium text-zinc-400 mb-1.5 block">Description</span>
                  <input type="text" value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:border-indigo-500 outline-none" />
                </label>
                <label className="block">
                  <span className="text-sm font-medium text-zinc-400 mb-1.5 block">Tools (Comma separated)</span>
                  <input
                    type="text"
                    value={form.tools.join(', ')}
                    onChange={e => setForm({ ...form, tools: e.target.value.split(',').map(t => t.trim()).filter(t => t) })}
                    className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm font-mono focus:border-indigo-500 outline-none"
                  />
                </label>
                <label className="block">
                  <span className="text-sm font-medium text-zinc-400 mb-1.5 block">System Prompt</span>
                  <textarea value={form.system_prompt} onChange={e => setForm({ ...form, system_prompt: e.target.value })} rows={6} className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-2 text-sm focus:border-indigo-500 outline-none resize-none" />
                </label>
              </div>
              <div className="p-6 border-t border-zinc-800 bg-zinc-900/50 flex justify-end gap-3">
                <button onClick={() => setIsModalOpen(false)} className="px-4 py-2 text-sm text-zinc-400">Cancel</button>
                <button onClick={handleSubmit} className="px-6 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-xl text-sm font-medium shadow-lg shadow-indigo-600/20">
                  {editingSkill ? 'Update' : 'Create'}
                </button>
              </div>
            </motion.div>
          </div>
        )}
      </AnimatePresence>

      <AnimatePresence>
        {isFileModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setIsFileModalOpen(false)} className="absolute inset-0 bg-black/60 backdrop-blur-sm" />
            <motion.div initial={{ opacity: 0, scale: 0.96 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.96 }} className="relative w-full max-w-6xl h-[80vh] bg-zinc-900 border border-zinc-800 rounded-3xl shadow-2xl overflow-hidden flex">
              <aside className="w-72 border-r border-zinc-800 bg-zinc-950/60 p-3 overflow-y-auto">
                <div className="text-sm font-semibold mb-3">{activeSkill} files</div>
                <div className="space-y-1">
                  {skillFiles.map(f => (
                    <button key={f} onClick={() => openFile(activeSkill, f)} className={`w-full text-left px-2 py-1.5 rounded text-xs font-mono ${activeFile===f ? 'bg-indigo-500/20 text-indigo-200' : 'text-zinc-300 hover:bg-zinc-800'}`}>{f}</button>
                  ))}
                </div>
              </aside>
              <main className="flex-1 flex flex-col">
                <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
                  <div className="text-sm text-zinc-300 font-mono truncate">{activeFile || '(no file selected)'}</div>
                  <div className="flex items-center gap-2">
                    <button onClick={saveFile} className="px-3 py-1.5 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-xs flex items-center gap-1"><Save className="w-3 h-3"/>Save</button>
                    <button onClick={() => setIsFileModalOpen(false)} className="p-2 hover:bg-zinc-800 rounded-full transition-colors text-zinc-400"><X className="w-4 h-4" /></button>
                  </div>
                </div>
                <textarea value={fileContent} onChange={(e)=>setFileContent(e.target.value)} className="flex-1 bg-zinc-950 text-zinc-200 font-mono text-sm p-4 resize-none outline-none" />
              </main>
            </motion.div>
          </div>
        )}
      </AnimatePresence>
    </div>
  );
};

export default Skills;
