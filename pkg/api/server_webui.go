package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

func (s *Server) setWebUISessionCookie(w http.ResponseWriter, r *http.Request) {
	if s == nil || w == nil || strings.TrimSpace(s.token) == "" {
		return
	}
	sameSite := http.SameSiteLaxMode
	if s.shouldUseCrossSiteCookie(r) {
		sameSite = http.SameSiteNoneMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "clawgo_webui_token",
		Value:    s.token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestUsesTLS(r),
		SameSite: sameSite,
		MaxAge:   86400,
	})
}

func (s *Server) handleWebUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.token != "" && s.isBearerAuthorized(r) {
		s.setWebUISessionCookie(w, r)
	}
	if s.tryServeWebUIDist(w, r, "/index.html") {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(webUIHTML))
}

func (s *Server) handleWebUIAuthSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.isBearerAuthorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.setWebUISessionCookie(w, r)
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (s *Server) handleWebUIAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/" {
		s.handleWebUI(w, r)
		return
	}
	if s.tryServeWebUIDist(w, r, r.URL.Path) {
		return
	}
	if s.tryServeWebUIDist(w, r, "/index.html") {
		return
	}
	http.NotFound(w, r)
}

func (s *Server) tryServeWebUIDist(w http.ResponseWriter, r *http.Request, reqPath string) bool {
	dir := strings.TrimSpace(s.webUIDir)
	if dir == "" {
		return false
	}
	p := strings.TrimPrefix(reqPath, "/")
	if reqPath == "/" || reqPath == "/index.html" {
		p = "index.html"
	}
	p = filepath.Clean(strings.TrimPrefix(p, "/"))
	if strings.HasPrefix(p, "..") {
		return false
	}
	full := filepath.Join(dir, p)
	fi, err := os.Stat(full)
	if err != nil || fi.IsDir() {
		return false
	}
	http.ServeFile(w, r, full)
	return true
}

func (s *Server) handleWebUIUpload(w http.ResponseWriter, r *http.Request) {
	if !s.checkAuth(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f, h, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "file required", http.StatusBadRequest)
		return
	}
	defer f.Close()
	dir := filepath.Join(os.TempDir(), "clawgo_webui_uploads")
	_ = os.MkdirAll(dir, 0755)
	name := fmt.Sprintf("%d_%s", time.Now().UnixNano(), filepath.Base(h.Filename))
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, f); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "media": name, "name": h.Filename})
}

func gatewayBuildVersion() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil {
		ver := strings.TrimSpace(bi.Main.Version)
		rev := ""
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value
				break
			}
		}
		if len(rev) > 8 {
			rev = rev[:8]
		}
		if ver == "" || ver == "(devel)" {
			ver = "devel"
		}
		if rev != "" {
			return ver + "+" + rev
		}
		return ver
	}
	return "unknown"
}

func detectWebUIVersion(webUIDir string) string {
	_ = webUIDir
	return "dev"
}

const webUIHTML = `<!doctype html>
<html><head><meta charset="utf-8"/><meta name="viewport" content="width=device-width,initial-scale=1"/>
<title>ClawGo WebUI</title>
<style>
body{font-family:Georgia,"Times New Roman",serif;margin:0;background:linear-gradient(180deg,#f5efe2 0%,#e7dcc7 100%);color:#2f2419}
.page{max-width:1180px;margin:0 auto;padding:24px}
.hero{display:flex;justify-content:space-between;gap:20px;align-items:end;margin-bottom:24px;padding:20px 24px;border:1px solid #c6b89a;background:linear-gradient(135deg,#f7f0df 0%,#e2d2af 100%)}
.hero h2{margin:0;font-size:34px;letter-spacing:.04em;text-transform:uppercase}
.hero p{margin:6px 0 0 0;max-width:680px}
.grid{display:grid;grid-template-columns:1.2fr .8fr;gap:18px}
.panel{border:1px solid #b7a785;background:rgba(255,251,242,.82);box-shadow:0 10px 24px rgba(89,61,29,.08);padding:16px}
.panel h3{margin:0 0 12px 0;font-size:18px;text-transform:uppercase;letter-spacing:.06em}
.panel h4{margin:12px 0 8px 0;font-size:14px;text-transform:uppercase;letter-spacing:.06em;color:#6b5439}
textarea{width:100%;min-height:220px;background:#fffdf8;border:1px solid #c7b48d;padding:10px;color:#2f2419}
input,button{font:inherit}
input[type="text"],input:not([type]),#token,#session,#msg{background:#fffdf8;border:1px solid #c7b48d;padding:8px 10px;color:#2f2419}
button{background:#2f2419;color:#f5efe2;border:none;padding:9px 14px;cursor:pointer}
button.secondary{background:#8b6b42}
button:disabled{opacity:.5;cursor:default}
#chatlog,#worldlog,pre.snapshot{white-space:pre-wrap;border:1px solid #d1c4aa;padding:12px;background:#fffdf8;min-height:180px;overflow:auto}
.toolbar{display:flex;gap:10px;flex-wrap:wrap;align-items:center;margin-bottom:10px}
.stack{display:grid;gap:18px}
.mini{font-size:13px;color:#6d5a42}
.world-cols{display:grid;grid-template-columns:1fr 1fr;gap:12px}
.world-stat{border:1px solid #d3c3a2;background:#fbf6ea;padding:10px}
.world-stat strong{display:block;font-size:22px}
.mapwrap{border:1px solid #d3c3a2;background:#fffdf8;padding:8px}
#worldmap{width:100%;height:320px;display:block;background:radial-gradient(circle at top,#fffdf7 0%,#f0e5cf 100%)}
.legend{display:flex;gap:12px;flex-wrap:wrap;font-size:12px;color:#6d5a42;margin-top:8px}
.legend span{display:inline-flex;align-items:center;gap:6px}
.swatch{width:12px;height:12px;display:inline-block}
.detailbox{border:1px solid #d3c3a2;background:#fbf6ea;padding:12px;min-height:84px;white-space:pre-wrap}
.formgrid{display:grid;grid-template-columns:1fr 1fr;gap:10px}
.formgrid input{width:100%;box-sizing:border-box}
.formgrid textarea{width:100%;min-height:90px;box-sizing:border-box}
.chiprow{display:flex;gap:8px;flex-wrap:wrap;margin-top:10px}
.chiprow button{padding:6px 10px;background:#8b6b42}
@media (max-width: 900px){.grid{grid-template-columns:1fr}.world-cols{grid-template-columns:1fr}.hero{flex-direction:column;align-items:start}}
</style>
</head><body>
<div class="page">
<div class="hero">
  <div>
    <h2>ClawGo World Console</h2>
    <p>主对话、任务、NPC 世界状态都在这里。当前页面直接消费 <code>/api/runtime</code> 和 <code>/api/world</code>。</p>
  </div>
  <div class="mini">Token: <input id="token" placeholder="gateway token" style="width:320px"/></div>
</div>
<div class="grid">
  <div class="stack">
    <div class="panel">
      <h3>Chat</h3>
      <div class="toolbar">
        <span>Session:</span>
        <input id="session" value="webui:default"/>
        <input id="msg" placeholder="message" style="min-width:280px;flex:1"/>
        <input id="file" type="file"/>
        <button onclick="sendChat()">Send</button>
      </div>
      <div id="chatlog"></div>
    </div>
    <div class="panel">
      <h3>Config</h3>
      <div class="toolbar">
        <button class="secondary" onclick="loadCfg()">Load Config</button>
        <button onclick="saveCfg()">Save + Reload</button>
      </div>
      <textarea id="cfg"></textarea>
    </div>
  </div>
  <div class="stack">
    <div class="panel">
      <h3>World</h3>
      <div class="toolbar">
        <button onclick="loadWorld()">Refresh World</button>
        <button class="secondary" onclick="tickWorld()">Advance Tick</button>
      </div>
      <div class="world-cols">
        <div class="world-stat"><span class="mini">World</span><strong id="world-id">-</strong></div>
        <div class="world-stat"><span class="mini">Tick</span><strong id="world-tick">-</strong></div>
        <div class="world-stat"><span class="mini">NPC Count</span><strong id="world-npcs">-</strong></div>
        <div class="world-stat"><span class="mini">Quests</span><strong id="world-quests">-</strong></div>
      </div>
      <h4>Map</h4>
      <div class="mapwrap">
        <svg id="worldmap" viewBox="0 0 640 320" preserveAspectRatio="xMidYMid meet"></svg>
        <div class="legend">
          <span><i class="swatch" style="background:#2f2419"></i> Location</span>
          <span><i class="swatch" style="background:#b5482f"></i> Selected</span>
          <span><i class="swatch" style="background:#8b6b42"></i> Connection</span>
          <span><i class="swatch" style="background:#c96f3b"></i> NPC count</span>
          <span><i class="swatch" style="background:#497a63"></i> Entity count</span>
        </div>
      </div>
      <h4>Location Detail</h4>
      <div id="locationdetail" class="detailbox">Click a location on the map to inspect it.</div>
      <div id="locationnpcs" class="chiprow"></div>
      <h4>NPC Detail</h4>
      <div id="npcdetail" class="detailbox">Click an NPC in a location to inspect it.</div>
      <div id="locationentities" class="chiprow"></div>
      <h4>Entity Detail</h4>
      <div id="entitydetail" class="detailbox">Click an entity in a location to inspect it.</div>
      <h4>Create Entity</h4>
      <div class="formgrid">
        <input id="entity-id" placeholder="entity id"/>
        <input id="entity-name" placeholder="display name"/>
        <input id="entity-type" placeholder="entity type"/>
        <input id="entity-location" placeholder="location id"/>
      </div>
      <div class="toolbar" style="margin-top:10px">
        <button onclick="createEntity()">Create Entity</button>
      </div>
      <h4>Create Quest</h4>
      <div class="formgrid">
        <input id="quest-id" placeholder="quest id"/>
        <input id="quest-title" placeholder="quest title"/>
        <input id="quest-owner" placeholder="owner npc id"/>
        <input id="quest-status" placeholder="status (default: open)"/>
        <input id="quest-participants" placeholder="participants (comma separated)"/>
        <input id="quest-location" placeholder="anchor location"/>
        <textarea id="quest-summary" placeholder="quest summary" style="grid-column:1 / -1"></textarea>
      </div>
      <div class="toolbar" style="margin-top:10px">
        <button onclick="createQuest()">Create Quest</button>
      </div>
      <h4>World Log</h4>
      <div id="worldlog"></div>
      <h4>Quest Board</h4>
      <div id="questlog"></div>
      <h4>Occupancy</h4>
      <div id="occupancylog"></div>
      <h4>Snapshot</h4>
      <pre id="worldsnapshot" class="snapshot"></pre>
    </div>
  </div>
</div>
</div>
<script>
let selectedLocation='';
let selectedNPC='';
let selectedEntity='';
function authHeaders(extra){const h=Object.assign({},extra||{});const t=document.getElementById('token').value.trim();if(t)h['Authorization']='Bearer '+t;return h}
async function loadCfg(){let r=await fetch('/api/config',{headers:authHeaders()});document.getElementById('cfg').value=await r.text()}
async function saveCfg(){let j=JSON.parse(document.getElementById('cfg').value);let r=await fetch('/api/config',{method:'POST',headers:authHeaders({'Content-Type':'application/json'}),body:JSON.stringify(j)});alert(await r.text())}
async function sendChat(){
 let media='';const f=document.getElementById('file').files[0];
 if(f){let fd=new FormData();fd.append('file',f);let ur=await fetch('/api/upload',{method:'POST',headers:authHeaders(),body:fd});let uj=await ur.json();media=uj.media||uj.name||''}
 const payload={session:document.getElementById('session').value,message:document.getElementById('msg').value,media};
 let r=await fetch('/api/chat',{method:'POST',headers:authHeaders({'Content-Type':'application/json'}),body:JSON.stringify(payload)});let t=await r.text();
 document.getElementById('chatlog').textContent += '\nUSER> '+payload.message+(media?(' [file:'+media+']'):'')+'\nBOT> '+t+'\n';
 await loadWorld();
}
function renderNPCButtons(npcIDs){
 const host=document.getElementById('locationnpcs');
 if(!host)return;
 if(!npcIDs.length){
   host.innerHTML='';
   return;
 }
 host.innerHTML=npcIDs.map(function(id){
   const style=id===selectedNPC?' style="background:#b5482f"':'';
   return '<button'+style+' onclick="loadNPCDetail(\''+id.replace(/'/g,"&#39;")+'\')">'+id+'</button>';
 }).join('');
}
function renderEntityButtons(entityIDs){
 const host=document.getElementById('locationentities');
 if(!host)return;
 if(!entityIDs.length){
   host.innerHTML='';
   return;
 }
 host.innerHTML=entityIDs.map(function(id){
   const style=id===selectedEntity?' style="background:#497a63"':'';
   return '<button'+style+' onclick="loadEntityDetail(\''+id.replace(/'/g,"&#39;")+'\')">'+id+'</button>';
 }).join('');
}
function selectLocation(id,data){
 selectedLocation=id||'';
 const locations=(data&&data.locations)||{};
 const occupancy=(data&&data.occupancy)||{};
 const entityOccupancy=(data&&data.entity_occupancy)||{};
 const detail=document.getElementById('locationdetail');
 const target=document.getElementById('entity-location');
 if(target&&selectedLocation)target.value=selectedLocation;
 if(!detail)return;
 if(!selectedLocation||!locations[selectedLocation]){
   detail.textContent='Click a location on the map to inspect it.';
   renderNPCButtons([]);
   renderEntityButtons([]);
   return;
 }
 const loc=locations[selectedLocation]||{};
 const npcIDs=(occupancy[selectedLocation]||[]);
 const entityIDs=(entityOccupancy[selectedLocation]||[]);
 const npcs=npcIDs.join(', ')||'-';
 const entities=entityIDs.join(', ')||'-';
 const lines=[
   'ID: '+selectedLocation,
   'Name: '+(loc.name||selectedLocation),
   'Neighbors: '+((loc.neighbors||[]).join(', ')||'-'),
   'NPCs: '+npcs,
   'Entities: '+entities,
   'Description: '+(loc.description||'-'),
 ];
 detail.textContent=lines.join('\n');
 renderNPCButtons(npcIDs);
 renderEntityButtons(entityIDs);
 const questLoc=document.getElementById('quest-location');
 if(questLoc&&selectedLocation)questLoc.value=selectedLocation;
}
async function loadNPCDetail(id){
 selectedNPC=id||'';
 const detail=document.getElementById('npcdetail');
 renderNPCButtons(selectedLocation?((JSON.parse(document.getElementById('worldsnapshot').textContent||'{}').occupancy||{})[selectedLocation]||[]):[]);
 if(!detail||!selectedNPC){
   if(detail)detail.textContent='Click an NPC in a location to inspect it.';
   return;
 }
 let r=await fetch('/api/runtime_admin',{
   method:'POST',
   headers:authHeaders({'Content-Type':'application/json'}),
   body:JSON.stringify({action:'world_npc_get',id:selectedNPC}),
 });
 let text=await r.text();
 if(!r.ok){
   detail.textContent=text||'Failed to load NPC detail.';
   return;
 }
 let data={};
 try{data=JSON.parse(text)}catch(e){detail.textContent=text||'Failed to parse NPC detail.';return;}
 const item=data.item||{};
 const profile=item.profile||{};
 const state=item.state||{};
 const lines=[
   'NPC: '+(profile.name||profile.agent_id||selectedNPC),
   'ID: '+(profile.agent_id||selectedNPC),
   'Kind: '+(profile.kind||'npc'),
   'Persona: '+(profile.persona||'-'),
   'Faction: '+(profile.faction||'-'),
   'Location: '+(state.current_location||profile.home_location||'-'),
   'Status: '+(state.status||'-'),
   'Mood: '+(state.mood||'-'),
   'Traits: '+((profile.traits||[]).join(', ')||'-'),
   'Goals(short): '+(((state.goals||{}).short_term||[]).join(', ')||'-'),
   'Goals(long): '+(((state.goals||{}).long_term||profile.default_goals||[]).join(', ')||'-'),
   'Beliefs: '+(Object.keys(state.beliefs||{}).length?JSON.stringify(state.beliefs):'-'),
   'Relationships: '+(Object.keys(state.relationships||{}).length?JSON.stringify(state.relationships):'-'),
   'Inventory: '+(Object.keys(state.inventory||{}).length?JSON.stringify(state.inventory):'-'),
   'Memory: '+(state.private_memory_summary||'-'),
 ];
 detail.textContent=lines.join('\n');
 renderNPCButtons(selectedLocation?((JSON.parse(document.getElementById('worldsnapshot').textContent||'{}').occupancy||{})[selectedLocation]||[]):[]);
}
async function loadEntityDetail(id){
 selectedEntity=id||'';
 const detail=document.getElementById('entitydetail');
 renderEntityButtons(selectedLocation?((JSON.parse(document.getElementById('worldsnapshot').textContent||'{}').entity_occupancy||{})[selectedLocation]||[]):[]);
 if(!detail||!selectedEntity){
   if(detail)detail.textContent='Click an entity in a location to inspect it.';
   return;
 }
 let r=await fetch('/api/runtime_admin',{
   method:'POST',
   headers:authHeaders({'Content-Type':'application/json'}),
   body:JSON.stringify({action:'world_entity_get',id:selectedEntity}),
 });
 let text=await r.text();
 if(!r.ok){
   detail.textContent=text||'Failed to load entity detail.';
   return;
 }
 let data={};
 try{data=JSON.parse(text)}catch(e){detail.textContent=text||'Failed to parse entity detail.';return;}
 const item=data.item||{};
 const lines=[
   'Entity: '+(item.name||selectedEntity),
   'ID: '+(item.id||selectedEntity),
   'Type: '+(item.type||'-'),
   'Location: '+(item.location_id||'-'),
   'State: '+(Object.keys(item.state||{}).length?JSON.stringify(item.state,null,2):'-'),
 ];
 detail.textContent=lines.join('\n');
 renderEntityButtons(selectedLocation?((JSON.parse(document.getElementById('worldsnapshot').textContent||'{}').entity_occupancy||{})[selectedLocation]||[]):[]);
}
function renderWorldMap(data){
 const svg=document.getElementById('worldmap');
 if(!svg)return;
 const locations=data.locations||{};
 const occupancy=data.occupancy||{};
 const entityOccupancy=data.entity_occupancy||{};
 const ids=Object.keys(locations).sort();
 if(!ids.length){svg.innerHTML='';return;}
 const width=640, height=320, radius=24, centerX=width/2, centerY=height/2, outer=Math.min(width,height)*0.34;
 const pos={};
 if(ids.length===1){pos[ids[0]]={x:centerX,y:centerY}}
 else{
   ids.forEach(function(id,idx){
     const angle=(Math.PI*2*idx/ids.length)-(Math.PI/2);
     pos[id]={x:centerX+Math.cos(angle)*outer,y:centerY+Math.sin(angle)*(outer*0.68)};
   });
 }
 let html='';
 const drawn={};
 ids.forEach(function(id){
   const loc=locations[id]||{};
   (loc.neighbors||[]).forEach(function(nb){
     const key=[id,nb].sort().join('::');
     if(drawn[key]||!pos[nb])return;
     drawn[key]=true;
     html+='<line x1="'+pos[id].x+'" y1="'+pos[id].y+'" x2="'+pos[nb].x+'" y2="'+pos[nb].y+'" stroke="#8b6b42" stroke-width="2" opacity="0.75"/>';
   });
 });
 ids.forEach(function(id){
   const p=pos[id], npcs=(occupancy[id]||[]), ents=(entityOccupancy[id]||[]);
   const fill=id===selectedLocation?'#b5482f':'#2f2419';
   html+='<circle data-loc="'+id+'" cx="'+p.x+'" cy="'+p.y+'" r="'+radius+'" fill="'+fill+'" style="cursor:pointer"/>';
   html+='<circle cx="'+(p.x+radius-6)+'" cy="'+(p.y-radius+8)+'" r="10" fill="#c96f3b"/>';
   html+='<text x="'+(p.x+radius-6)+'" y="'+(p.y-radius+12)+'" text-anchor="middle" font-size="10" fill="#fff">'+npcs.length+'</text>';
   html+='<circle cx="'+(p.x-radius+6)+'" cy="'+(p.y-radius+8)+'" r="10" fill="#497a63"/>';
   html+='<text x="'+(p.x-radius+6)+'" y="'+(p.y-radius+12)+'" text-anchor="middle" font-size="10" fill="#fff">'+ents.length+'</text>';
   html+='<text data-loc="'+id+'" x="'+p.x+'" y="'+(p.y+4)+'" text-anchor="middle" font-size="12" fill="#fff" style="cursor:pointer">'+id+'</text>';
 }
 svg.innerHTML=html;
 svg.querySelectorAll('[data-loc]').forEach(function(node){
   node.addEventListener('click',function(){
     selectLocation(node.getAttribute('data-loc'),data);
     renderWorldMap(data);
   });
 });
}
function renderWorld(world){
 const data=world&&world.world?world.world:world||{};
 document.getElementById('world-id').textContent=data.world_id||'-';
 document.getElementById('world-tick').textContent=(data.tick??'-');
 document.getElementById('world-npcs').textContent=(data.npc_count??(data.active_npcs?data.active_npcs.length:'-'));
 const questCount=(data.active_quests?Object.keys(data.active_quests).length:(data.quests?data.quests.length:0));
 document.getElementById('world-quests').textContent=questCount;
 const events=data.recent_events||[];
 document.getElementById('worldlog').textContent=events.length?events.map(function(e){return ('['+(e.tick??'-')+'] '+(e.type||'')+' '+(e.actor_id||'')+' '+(e.content||'')).trim()}).join('\n'):'No recent world events.';
 const quests=data.active_quests?Object.values(data.active_quests):(data.quests||[]);
 document.getElementById('questlog').textContent=quests.length?quests.map(function(q){return (q.title||q.id)+' ['+(q.status||'open')+']'}).join('\n'):'No active quests.';
 const occupancy=data.occupancy||{};
 const entityOccupancy=data.entity_occupancy||{};
 const locs=Array.from(new Set([...Object.keys(occupancy),...Object.keys(entityOccupancy)])).sort();
 document.getElementById('occupancylog').textContent=locs.length?locs.map(function(loc){return loc+': npcs='+(((occupancy[loc]||[]).join(','))||'-')+' | entities='+(((entityOccupancy[loc]||[]).join(','))||'-')}).join('\n'):'No occupancy data.';
 document.getElementById('worldsnapshot').textContent=JSON.stringify(data,null,2);
 renderWorldMap(data);
 if(selectedLocation&&!(data.locations||{})[selectedLocation])selectedLocation='';
 if(selectedNPC){
   const occ=data.occupancy||{};
   const allNPCs=Object.keys(occ).reduce(function(acc,key){return acc.concat(occ[key]||[])},[]);
   if(allNPCs.indexOf(selectedNPC)===-1)selectedNPC='';
 }
 if(selectedEntity){
   const entOcc=data.entity_occupancy||{};
   const allEntities=Object.keys(entOcc).reduce(function(acc,key){return acc.concat(entOcc[key]||[])},[]);
   if(allEntities.indexOf(selectedEntity)===-1)selectedEntity='';
 }
 selectLocation(selectedLocation,data);
 if(selectedNPC)loadNPCDetail(selectedNPC);
 else document.getElementById('npcdetail').textContent='Click an NPC in a location to inspect it.';
 if(selectedEntity)loadEntityDetail(selectedEntity);
 else document.getElementById('entitydetail').textContent='Click an entity in a location to inspect it.';
}
async function loadWorld(){
 let r=await fetch('/api/world?limit=20',{headers:authHeaders()});
 let j=await r.json();
 renderWorld(j);
}
async function tickWorld(){
 let r=await fetch('/api/runtime_admin?action=world_tick&source=webui',{headers:authHeaders()});
 try{let j=await r.json();console.log(j)}catch(e){}
 await loadWorld();
}
async function createEntity(){
 const payload={
   action:'world_entity_create',
   entity_id:document.getElementById('entity-id').value.trim(),
   name:document.getElementById('entity-name').value.trim(),
   entity_type:document.getElementById('entity-type').value.trim(),
   location_id:document.getElementById('entity-location').value.trim(),
 };
 if(!payload.entity_id||!payload.name||!payload.entity_type||!payload.location_id){
   alert('entity id, name, type, and location are required');
   return;
 }
 let r=await fetch('/api/runtime_admin',{
   method:'POST',
   headers:authHeaders({'Content-Type':'application/json'}),
   body:JSON.stringify(payload),
 });
 let text=await r.text();
 if(!r.ok){
   alert(text||'create entity failed');
   return;
 }
 document.getElementById('entity-id').value='';
 document.getElementById('entity-name').value='';
 document.getElementById('entity-type').value='';
 await loadWorld();
}
async function createQuest(){
 const participants=document.getElementById('quest-participants').value.split(',').map(function(v){return v.trim()}).filter(Boolean);
 const summary=document.getElementById('quest-summary').value.trim();
 const locationID=document.getElementById('quest-location').value.trim();
 const payload={
   action:'world_quest_create',
   id:document.getElementById('quest-id').value.trim(),
   title:document.getElementById('quest-title').value.trim(),
   owner_npc_id:document.getElementById('quest-owner').value.trim(),
   status:document.getElementById('quest-status').value.trim()||'open',
   participants:participants,
   summary:summary,
 };
 if(locationID){
   payload.summary=(summary?summary+' ':'')+'[location:'+locationID+']';
 }
 if(!payload.id&&!payload.title){
   alert('quest id or title is required');
   return;
 }
 let r=await fetch('/api/runtime_admin',{
   method:'POST',
   headers:authHeaders({'Content-Type':'application/json'}),
   body:JSON.stringify(payload),
 });
 let text=await r.text();
 if(!r.ok){
   alert(text||'create quest failed');
   return;
 }
 document.getElementById('quest-id').value='';
 document.getElementById('quest-title').value='';
 document.getElementById('quest-owner').value='';
 document.getElementById('quest-status').value='';
 document.getElementById('quest-participants').value='';
 document.getElementById('quest-summary').value='';
 await loadWorld();
}
loadCfg();
loadWorld();
</script></body></html>`
