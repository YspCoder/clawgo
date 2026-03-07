import React, { useRef, useState } from 'react';
import { Plus, RefreshCw, Trash2, Zap, X, FileText, Save } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';

const Skills: React.FC = () => {
  const { t } = useTranslation();
  const { skills, refreshSkills, q, clawhubInstalled, clawhubPath } = useAppContext();
  const ui = useUI();
  const [installName, setInstallName] = useState('');
  const [installingSkill, setInstallingSkill] = useState(false);
  const [ignoreSuspicious, setIgnoreSuspicious] = useState(false);
  const qp = (k: string, v: string) => `${q}${q ? '&' : '?'}${k}=${encodeURIComponent(v)}`;

  const [isFileModalOpen, setIsFileModalOpen] = useState(false);
  const [activeSkill, setActiveSkill] = useState<string>('');
  const [skillFiles, setSkillFiles] = useState<string[]>([]);
  const [activeFile, setActiveFile] = useState<string>('');
  const [fileContent, setFileContent] = useState('');
  const uploadRef = useRef<HTMLInputElement>(null);

  async function deleteSkill(id: string) {
    if (!await ui.confirmDialog({ title: t('skillsDeleteTitle'), message: t('skillsDeleteMessage'), danger: true, confirmText: t('delete') })) return;
    try {
      await fetch(`/webui/api/skills${qp('id', id)}`, { method: 'DELETE' });
      await refreshSkills();
    } catch (e) {
      console.error(e);
    }
  }

  async function installClawHubIfNeeded() {
    if (clawhubInstalled) return true;
    const confirm = await ui.confirmDialog({
      title: t('skillsClawhubMissingTitle'),
      message: t('skillsClawhubMissingMessage'),
      confirmText: t('skillsInstallNow')
    });
    if (!confirm) return false;

    ui.showLoading(t('skillsInstallingDeps'));
    try {
      const r = await fetch(`/webui/api/skills${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'install_clawhub' }),
      });
      const text = await r.text();
      if (!r.ok) {
        ui.hideLoading();
        await ui.notify({ title: t('skillsInstallFailedTitle'), message: text || t('skillsInstallFailedMessage') });
        return false;
      }
      ui.hideLoading();
      await ui.notify({ title: t('skillsInstallDoneTitle'), message: t('skillsInstallDoneMessage') });
      await refreshSkills();
      return true;
    } finally {
      // loading is explicitly closed before notify, keep this as fallback.
      ui.hideLoading();
    }
  }

  async function installSkill() {
    if (installingSkill) return;
    const name = installName.trim();
    if (!name) return;

    setInstallingSkill(true);
    ui.showLoading(t('skillsInstallingSkill'));
    try {
      const ready = await installClawHubIfNeeded();
      if (!ready) return;

      const r = await fetch(`/webui/api/skills${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ action: 'install', name, ignore_suspicious: ignoreSuspicious }),
      });
      if (!r.ok) {
        ui.hideLoading();
        await ui.notify({ title: t('requestFailed'), message: await r.text() });
        return;
      }
      setInstallName('');
      await refreshSkills();
      ui.hideLoading();
      await ui.notify({ title: t('skillsInstallSkillDoneTitle'), message: t('skillsInstallSkillDoneMessage', { name }) });
    } finally {
      ui.hideLoading();
      setInstallingSkill(false);
    }
  }

  async function onAddSkillClick() {
    const yes = await ui.confirmDialog({
      title: t('skillsAddTitle'),
      message: t('skillsAddMessage'),
      confirmText: t('skillsSelectArchive')
    });
    if (!yes) return;
    uploadRef.current?.click();
  }

  async function onArchiveSelected(e: React.ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0];
    e.target.value = '';
    if (!f) return;

    const fd = new FormData();
    fd.append('file', f);

    ui.showLoading(t('skillsImporting'));
    try {
      const r = await fetch(`/webui/api/skills${q}`, {
        method: 'POST',
        body: fd,
      });
      const text = await r.text();
      if (!r.ok) {
        await ui.notify({ title: t('skillsImportFailedTitle'), message: text || t('skillsImportFailedMessage') });
        return;
      }
      let imported: string[] = [];
      try {
        const j = JSON.parse(text);
        imported = Array.isArray(j.imported) ? j.imported : [];
      } catch {
        imported = [];
      }
      await ui.notify({ title: t('skillsImportDoneTitle'), message: imported.length > 0 ? `${t('skillsImportedPrefix')}: ${imported.join(', ')}` : t('skillsImportDoneMessage') });
      await refreshSkills();
    } finally {
      ui.hideLoading();
    }
  }

  async function openFileManager(skillId: string) {
    setActiveSkill(skillId);
    setIsFileModalOpen(true);
    const r = await fetch(`/webui/api/skills${q ? `${q}&id=${encodeURIComponent(skillId)}&files=1` : `?id=${encodeURIComponent(skillId)}&files=1`}`);
    if (!r.ok) {
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
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
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
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
      await ui.notify({ title: t('requestFailed'), message: await r.text() });
      return;
    }
    await ui.notify({ title: t('saved'), message: t('skillsFileSaved') });
  }

  return (
    <div className="p-4 md:p-6 xl:p-8 w-full space-y-6 xl:space-y-8">
      <input ref={uploadRef} type="file" accept=".zip,.tar,.tar.gz,.tgz" className="hidden" onChange={onArchiveSelected} />

      <div className="flex items-start justify-between gap-3 flex-wrap">
        <h1 className="text-2xl font-semibold tracking-tight">{t('skills')}</h1>
        <div className="flex items-center gap-2 flex-wrap w-full xl:w-auto">
          <input disabled={installingSkill} value={installName} onChange={(e) => setInstallName(e.target.value)} placeholder={t('skillsNamePlaceholder')} className="w-full sm:w-72 px-3 py-2 bg-zinc-950 border border-zinc-800 rounded-lg text-sm disabled:opacity-60" />
          <button disabled={installingSkill} onClick={installSkill} className="px-3 py-2 bg-emerald-600 hover:bg-emerald-500 disabled:opacity-60 disabled:cursor-not-allowed text-white rounded-lg text-sm font-medium">{installingSkill ? t('loading') : t('install')}</button>
          <label className="flex items-center gap-2 text-xs text-zinc-400">
            <input
              type="checkbox"
              checked={ignoreSuspicious}
              disabled={installingSkill}
              onChange={(e) => setIgnoreSuspicious(e.target.checked)}
            />
            {t('skillsIgnoreSuspicious')}
          </label>
        </div>
        <div className="flex items-center gap-3 flex-wrap">
          <div className={`text-xs px-2 py-1 rounded-md border ${clawhubInstalled ? 'text-emerald-300 border-emerald-700/50 bg-emerald-900/20' : 'text-amber-300 border-amber-700/50 bg-amber-900/20'}`} title={clawhubPath || t('skillsClawhubNotFound')}>
            {t('skillsClawhubStatus')}: {clawhubInstalled ? t('installed') : t('notInstalled')}
          </div>
          {!clawhubInstalled && (
            <button
              onClick={installClawHubIfNeeded}
              className="flex items-center gap-2 px-4 py-2 bg-amber-600 hover:bg-amber-500 text-white rounded-lg text-sm font-medium transition-colors shadow-sm"
            >
              <Zap className="w-4 h-4" /> {t('skillsInstallNow')}
            </button>
          )}
          <button onClick={() => refreshSkills()} className="flex items-center gap-2 px-4 py-2 bg-zinc-800 hover:bg-zinc-700 rounded-lg text-sm font-medium transition-colors">
            <RefreshCw className="w-4 h-4" /> {t('refresh')}
          </button>
          <button onClick={onAddSkillClick} className="flex items-center gap-2 px-4 py-2 bg-indigo-600 hover:bg-indigo-500 text-white rounded-lg text-sm font-medium transition-colors shadow-sm">
            <Plus className="w-4 h-4" /> {t('skillsAdd')}
          </button>
        </div>
      </div>

      {!clawhubInstalled && (
        <div className="rounded-2xl border border-amber-700/40 bg-amber-950/20 p-4 text-sm text-amber-100">
          <div className="font-medium mb-1">{t('skillsClawhubMissingTitle')}</div>
          <div className="text-amber-200/90">{t('skillsInstallPanelHint')}</div>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6">
        {skills.map(s => (
          <div key={s.id} className="bg-zinc-900/40 border border-zinc-800/80 rounded-2xl p-6 flex flex-col shadow-sm group hover:border-zinc-700/50 transition-colors">
            <div className="flex items-start justify-between mb-4">
              <div className="flex items-center gap-4">
                <div className="w-12 h-12 rounded-xl bg-zinc-800/50 flex items-center justify-center border border-zinc-700/50">
                  <Zap className="w-6 h-6 text-amber-400" />
                </div>
                <div>
                  <h3 className="font-semibold text-zinc-100">{s.name}</h3>
                  <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest">{t('id')}: {s.id.slice(-6)}</span>
                </div>
              </div>
            </div>

            <p className="text-sm text-zinc-400 mb-6 line-clamp-2">{s.description || t('noDescription')}</p>

            <div className="space-y-4 mb-6">
              <div>
                <div className="text-[10px] text-zinc-500 uppercase tracking-widest mb-2">{t('tools')}</div>
                <div className="flex flex-wrap gap-2">
                  {(Array.isArray(s.tools) ? s.tools : []).map(tool => (
                    <span key={tool} className="px-2 py-1 bg-zinc-800/50 text-zinc-300 text-[10px] font-mono rounded border border-zinc-700/50">{tool}</span>
                  ))}
                  {(!Array.isArray(s.tools) || s.tools.length === 0) && <span className="text-xs text-zinc-600 italic">{t('skillsNoTools')}</span>}
                </div>
              </div>
            </div>

            <div className="flex items-center gap-2 pt-4 border-t border-zinc-800/50 mt-auto">
              <button
                onClick={() => openFileManager(s.id)}
                className="flex-1 flex items-center justify-center gap-2 py-2 bg-indigo-500/10 text-indigo-300 hover:bg-indigo-500/20 rounded-lg text-xs font-medium transition-colors"
                title={t('files')}
              >
                <FileText className="w-4 h-4" /> {t('skillsFileEdit')}
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
        {isFileModalOpen && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4">
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} onClick={() => setIsFileModalOpen(false)} className="absolute inset-0 bg-black/60 backdrop-blur-sm" />
            <motion.div initial={{ opacity: 0, scale: 0.96 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.96 }} className="relative w-full max-w-6xl h-[80vh] bg-zinc-900 border border-zinc-800 rounded-3xl shadow-2xl overflow-hidden flex">
              <aside className="w-72 border-r border-zinc-800 bg-zinc-950/60 p-3 overflow-y-auto">
                <div className="text-sm font-semibold mb-3">{activeSkill} {t('files')}</div>
                <div className="space-y-1">
                  {skillFiles.map(f => (
                    <button key={f} onClick={() => openFile(activeSkill, f)} className={`w-full text-left px-2 py-1.5 rounded text-xs font-mono ${activeFile===f ? 'bg-indigo-500/20 text-indigo-200' : 'text-zinc-300 hover:bg-zinc-800'}`}>{f}</button>
                  ))}
                </div>
              </aside>
              <main className="flex-1 flex flex-col">
                <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
                  <div className="text-sm text-zinc-300 font-mono truncate">{activeFile || t('noFileSelected')}</div>
                  <div className="flex items-center gap-2">
                    <button onClick={saveFile} className="px-3 py-1.5 rounded bg-emerald-600 hover:bg-emerald-500 text-white text-xs flex items-center gap-1"><Save className="w-3 h-3"/>{t('save')}</button>
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
