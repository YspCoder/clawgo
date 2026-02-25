import { useEffect, useMemo, useState } from 'react'

type ChatItem = { role: 'user' | 'assistant'; text: string }
type Session = { key: string; title: string }
type CronJob = { id: string; name: string; enabled: boolean }
type Cfg = Record<string, any>
type View = 'chat' | 'config' | 'cron' | 'nodes'

const defaultSessions: Session[] = [{ key: 'webui:default', title: 'Default' }]

function getPath(obj: any, path: string, fallback: any = '') {
  return path.split('.').reduce((acc, k) => (acc && acc[k] !== undefined ? acc[k] : undefined), obj) ?? fallback
}
function setPath(obj: any, path: string, value: any) {
  const keys = path.split('.')
  const next = JSON.parse(JSON.stringify(obj || {}))
  let cur = next
  for (let i = 0; i < keys.length - 1; i++) {
    const k = keys[i]
    if (typeof cur[k] !== 'object' || cur[k] === null) cur[k] = {}
    cur = cur[k]
  }
  cur[keys[keys.length - 1]] = value
  return next
}

export function App() {
  const [view, setView] = useState<View>('chat')
  const [token, setToken] = useState('')
  const [cfg, setCfg] = useState<Cfg>({})
  const [cfgRaw, setCfgRaw] = useState('{}')
  const [showRaw, setShowRaw] = useState(false)
  const [sessions, setSessions] = useState<Session[]>(defaultSessions)
  const [active, setActive] = useState('webui:default')
  const [chat, setChat] = useState<Record<string, ChatItem[]>>({ 'webui:default': [] })
  const [msg, setMsg] = useState('')
  const [nodes, setNodes] = useState<string>('[]')
  const [cron, setCron] = useState<CronJob[]>([])
  const activeChat = useMemo(() => chat[active] || [], [chat, active])
  const q = token ? `?token=${encodeURIComponent(token)}` : ''

  async function loadConfig() {
    const r = await fetch(`/webui/api/config${q}`)
    const txt = await r.text()
    setCfgRaw(txt)
    try { setCfg(JSON.parse(txt)) } catch { setCfg({}) }
  }
  async function saveConfig() {
    const payload = showRaw ? JSON.parse(cfgRaw) : cfg
    const r = await fetch(`/webui/api/config${q}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload),
    })
    alert(await r.text())
  }
  async function refreshNodes() {
    const r = await fetch(`/webui/api/nodes${q}`)
    const j = await r.json()
    setNodes(JSON.stringify(j.nodes || [], null, 2))
  }
  async function refreshCron() {
    const r = await fetch(`/webui/api/cron${q}`)
    const j = await r.json()
    setCron(j.jobs || [])
  }
  async function cronAction(action: 'delete' | 'enable' | 'disable', id: string) {
    await fetch(`/webui/api/cron${q}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action, id }),
    })
    await refreshCron()
  }

  async function send() {
    let media = ''
    const input = document.getElementById('file') as HTMLInputElement | null
    const f = input?.files?.[0]
    if (f) {
      const fd = new FormData(); fd.append('file', f)
      const ur = await fetch(`/webui/api/upload${q}`, { method: 'POST', body: fd })
      const uj = await ur.json(); media = uj.path || ''
    }
    const userText = msg + (media ? ` [file:${media}]` : '')
    setChat((prev) => ({ ...prev, [active]: [...(prev[active] || []), { role: 'user', text: userText }] }))
    const r = await fetch(`/webui/api/chat${q}`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ session: active, message: msg, media }),
    })
    const t = await r.text()
    setChat((prev) => ({ ...prev, [active]: [...(prev[active] || []), { role: 'assistant', text: t }] }))
    setMsg('')
    if (input) input.value = ''
  }

  const bindText = (path: string) => ({ value: String(getPath(cfg, path, '')), onChange: (e: React.ChangeEvent<HTMLInputElement>) => setCfg((v) => setPath(v, path, e.target.value)) })
  const bindNum = (path: string) => ({ value: Number(getPath(cfg, path, 0)), onChange: (e: React.ChangeEvent<HTMLInputElement>) => setCfg((v) => setPath(v, path, Number(e.target.value || 0))) })
  const bindBool = (path: string) => ({ checked: Boolean(getPath(cfg, path, false)), onChange: (e: React.ChangeEvent<HTMLInputElement>) => setCfg((v) => setPath(v, path, e.target.checked)) })

  function addSession() {
    const n = `webui:${Date.now()}`
    const s = { key: n, title: `Session-${sessions.length + 1}` }
    setSessions((v) => [...v, s]); setActive(n); setChat((prev) => ({ ...prev, [n]: [] }))
  }

  useEffect(() => { loadConfig().catch(() => {}); refreshNodes().catch(() => {}); refreshCron().catch(() => {}) }, [])

  return (
    <div className='app'>
      <header className='topbar'>
        <strong>ClawGo Control</strong>
        <input value={token} onChange={(e) => setToken(e.target.value)} placeholder='gateway token' />
      </header>

      <div className='shell'>
        <nav className='left-menu'>
          <button className={view==='chat'?'on':''} onClick={() => setView('chat')}>💬 Chat</button>
          <button className={view==='config'?'on':''} onClick={() => setView('config')}>⚙️ Config</button>
          <button className={view==='cron'?'on':''} onClick={() => setView('cron')}>⏱ Cron</button>
          <button className={view==='nodes'?'on':''} onClick={() => setView('nodes')}>🧩 Nodes</button>
        </nav>

        <aside className='sessions'>
          <div className='panel-title'>Sessions <button onClick={addSession}>+</button></div>
          {sessions.map((s) => (
            <button key={s.key} className={s.key === active ? 'active' : ''} onClick={() => setActive(s.key)}>{s.title}</button>
          ))}
        </aside>

        <main className='main'>
          {view === 'chat' && (
            <section className='panel'>
              <div className='panel-title'>Chat</div>
              <div className='chatlog'>
                {activeChat.map((m, i) => <div key={i} className={`bubble ${m.role}`}>{m.text}</div>)}
              </div>
              <div className='composer'>
                <input value={msg} onChange={(e) => setMsg(e.target.value)} placeholder='Type message...' />
                <input id='file' type='file' />
                <button onClick={send}>Send</button>
              </div>
            </section>
          )}

          {view === 'config' && (
            <section className='panel'>
              <div className='panel-title'>Config Form</div>
              <div className='row'>
                <button onClick={loadConfig}>Load</button><button onClick={saveConfig}>Save+Reload</button><button onClick={() => setShowRaw(v=>!v)}>{showRaw?'Form':'Raw'}</button>
              </div>
              {!showRaw ? (
                <div className='form-grid'>
                  <label>gateway.host<input {...bindText('gateway.host')} /></label>
                  <label>gateway.port<input type='number' {...bindNum('gateway.port')} /></label>
                  <label>gateway.token<input {...bindText('gateway.token')} /></label>
                  <label>agents.defaults.max_tool_iterations<input type='number' {...bindNum('agents.defaults.max_tool_iterations')} /></label>
                  <label>agents.defaults.max_tokens<input type='number' {...bindNum('agents.defaults.max_tokens')} /></label>
                  <label>providers.proxy.timeout_sec<input type='number' {...bindNum('providers.proxy.timeout_sec')} /></label>
                  <label>tools.shell.enabled<input type='checkbox' {...bindBool('tools.shell.enabled')} /></label>
                  <label>logging.enabled<input type='checkbox' {...bindBool('logging.enabled')} /></label>
                </div>
              ) : <textarea value={cfgRaw} onChange={(e)=>setCfgRaw(e.target.value)} />}
            </section>
          )}

          {view === 'cron' && (
            <section className='panel'>
              <div className='panel-title'>Cron Jobs</div>
              <div className='row'><button onClick={refreshCron}>Refresh</button></div>
              <div className='cron-list'>
                {cron.map((j) => (
                  <div key={j.id} className='cron-item'>
                    <div><strong>{j.name || j.id}</strong><div className='muted'>{j.id}</div></div>
                    <div className='row'>
                      <button onClick={() => cronAction(j.enabled ? 'disable':'enable', j.id)}>{j.enabled ? 'Disable':'Enable'}</button>
                      <button onClick={() => cronAction('delete', j.id)}>Delete</button>
                    </div>
                  </div>
                ))}
              </div>
            </section>
          )}

          {view === 'nodes' && (
            <section className='panel'>
              <div className='panel-title'>Nodes</div>
              <div className='row'><button onClick={refreshNodes}>Refresh</button></div>
              <pre>{nodes}</pre>
            </section>
          )}
        </main>
      </div>
    </div>
  )
}
