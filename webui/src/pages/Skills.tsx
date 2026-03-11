import React, { useRef, useState } from 'react';
import { Plus, RefreshCw, Trash2, Zap, X, FileText, Save } from 'lucide-react';
import { motion, AnimatePresence } from 'motion/react';
import { useTranslation } from 'react-i18next';
import { useAppContext } from '../context/AppContext';
import { useUI } from '../context/UIContext';
import { Button, FixedButton } from '../components/Button';
import { TextField, TextareaField, ToolbarCheckboxField } from '../components/FormControls';
import { ModalBackdrop, ModalBody, ModalCard, ModalHeader, ModalShell } from '../components/ModalFrame';
import PageHeader from '../components/PageHeader';
import ToolbarRow from '../components/ToolbarRow';
import FileListItem from '../components/FileListItem';

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

      <PageHeader
        title={t('skills')}
        actions={
          <ToolbarRow className="justify-end">
            <div className={`ui-pill text-xs px-2.5 py-1 rounded-md border font-medium ${clawhubInstalled ? 'ui-pill-success' : 'ui-pill-warning'}`} title={clawhubPath || t('skillsClawhubNotFound')}>
              {t('skillsClawhubStatus')}: {clawhubInstalled ? t('installed') : t('notInstalled')}
            </div>
            {!clawhubInstalled && (
              <FixedButton onClick={installClawHubIfNeeded} variant="primary" shadow label={t('skillsInstallNow')}>
                <Zap className="w-4 h-4" />
              </FixedButton>
            )}
            <FixedButton onClick={() => refreshSkills()} label={t('refresh')}>
              <RefreshCw className="w-4 h-4" />
            </FixedButton>
            <FixedButton onClick={onAddSkillClick} variant="primary" shadow label={t('skillsAdd')}>
              <Plus className="w-4 h-4" />
            </FixedButton>
          </ToolbarRow>
        }
      />

      <ToolbarRow className="w-full flex-nowrap items-center">
        <TextField
          disabled={installingSkill}
          value={installName}
          onChange={(e) => setInstallName(e.target.value)}
          placeholder={t('skillsNamePlaceholder')}
          className="min-w-0 flex-1 disabled:opacity-60"
        />
        <Button
          disabled={installingSkill}
          onClick={installSkill}
          variant="success"
          size="md"
          noShrink
        >
          {installingSkill ? t('loading') : t('install')}
        </Button>
        <ToolbarCheckboxField
          checked={ignoreSuspicious}
          className={installingSkill ? 'pointer-events-none opacity-60' : 'shrink-0'}
          help={t('skillsIgnoreSuspiciousHint', { defaultValue: 'Use --force to ignore suspicious package warnings.' })}
          label={t('skillsIgnoreSuspicious')}
          onChange={setIgnoreSuspicious}
        />
      </ToolbarRow>

      {!clawhubInstalled && (
        <div className="rounded-2xl border border-zinc-800/80 bg-zinc-950/45 p-4 text-sm shadow-sm">
          <div className="flex items-start gap-3">
            <div className="ui-pill ui-pill-warning mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-xl border">
              <Zap className="w-4 h-4" />
            </div>
            <div className="min-w-0">
              <div className="font-medium text-zinc-100 mb-1">{t('skillsClawhubMissingTitle')}</div>
              <div className="text-zinc-400 leading-6">{t('skillsInstallPanelHint')}</div>
            </div>
          </div>
        </div>
      )}

      <div className="grid grid-cols-1 sm:grid-cols-2 xl:grid-cols-3 2xl:grid-cols-4 gap-6">
        {skills.map(s => (
          <div key={s.id} className="brand-card rounded-[28px] border border-zinc-800/80 p-6 flex flex-col shadow-sm group hover:border-zinc-700/50 transition-colors">
            <div className="flex items-start justify-between mb-4">
              <div className="flex items-center gap-4">
                <div className="w-12 h-12 rounded-2xl bg-zinc-800/50 flex items-center justify-center border border-zinc-700/50">
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
              <FixedButton onClick={() => openFileManager(s.id)} variant="accent" radius="lg" label={t('skillsFileEdit')}>
                <FileText className="w-4 h-4" />
              </FixedButton>
              <FixedButton onClick={() => deleteSkill(s.id)} variant="danger" radius="lg" label={t('delete')}>
                <Trash2 className="w-4 h-4" />
              </FixedButton>
            </div>
          </div>
        ))}
      </div>

      <AnimatePresence>
        {isFileModalOpen && (
          <ModalShell>
            <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} exit={{ opacity: 0 }} className="absolute inset-0" />
            <ModalBackdrop onClick={() => setIsFileModalOpen(false)} />
            <motion.div initial={{ opacity: 0, scale: 0.96 }} animate={{ opacity: 1, scale: 1 }} exit={{ opacity: 0, scale: 0.96 }} className="relative z-[1] w-full max-w-6xl h-[80vh]">
              <ModalCard className="h-full rounded-3xl bg-zinc-900 flex-row">
              <aside className="w-72 border-r border-zinc-800 bg-zinc-950/60 p-3 overflow-y-auto">
                <div className="text-sm font-semibold mb-3">{activeSkill} {t('files')}</div>
                <div className="space-y-1">
                  {skillFiles.map(f => (
                    <FileListItem key={f} active={activeFile === f} monospace onClick={() => openFile(activeSkill, f)}>
                      {f}
                    </FileListItem>
                  ))}
                </div>
              </aside>
                <main className="flex-1 flex flex-col">
                  <ModalHeader
                    title={activeFile || t('noFileSelected')}
                    className="px-4 py-3"
                    actions={
                      <>
                        <Button onClick={saveFile} variant="success" size="sm" radius="lg" gap="1">
                          <Save className="w-4 h-4" />
                          {t('save')}
                        </Button>
                        <FixedButton onClick={() => setIsFileModalOpen(false)} radius="full" label={t('close')}>
                          <X className="w-4 h-4" />
                        </FixedButton>
                      </>
                    }
                  />
                  <ModalBody className="flex flex-col">
                    <TextareaField
                      value={fileContent}
                      onChange={(e) => setFileContent(e.target.value)}
                      monospace
                      className="flex-1 rounded-none border-0 bg-zinc-950 text-zinc-200 p-4 resize-none outline-none"
                    />
                  </ModalBody>
                </main>
              </ModalCard>
            </motion.div>
          </ModalShell>
        )}
      </AnimatePresence>
    </div>
  );
};

export default Skills;
