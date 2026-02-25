import express from "express";
import { createServer as createViteServer } from "vite";
import Database from "better-sqlite3";
import { EventEmitter } from "events";
import multer from "multer";
import path from "path";
import fs from "fs";

const app = express();
const PORT = 3000;
const db = new Database("gateway.db");
const logEmitter = new EventEmitter();

// Setup upload directory
const uploadDir = "uploads";
if (!fs.existsSync(uploadDir)) {
  fs.mkdirSync(uploadDir);
}

const storage = multer.diskStorage({
  destination: (req, file, cb) => cb(null, uploadDir),
  filename: (req, file, cb) => cb(null, Date.now() + "-" + file.originalname)
});
const upload = multer({ storage });

// Initialize DB
db.exec(`
  CREATE TABLE IF NOT EXISTS skills (
    id TEXT PRIMARY KEY,
    name TEXT,
    description TEXT,
    tools TEXT,
    system_prompt TEXT
  );
  CREATE TABLE IF NOT EXISTS cron_jobs (
    id TEXT PRIMARY KEY,
    name TEXT,
    enabled INTEGER,
    kind TEXT,
    everyMs INTEGER,
    expr TEXT,
    message TEXT,
    deliver INTEGER,
    channel TEXT,
    "to" TEXT
  );
`);

app.use(express.json());
app.use("/uploads", express.static(uploadDir));

// Helper for logs
function addLog(level: string, msg: string, extra = {}) {
  const entry = { time: new Date().toISOString(), level, msg, ...extra };
  logEmitter.emit("log", entry);
  console.log(`[${level}] ${msg}`);
}

// API Routes
app.get("/webui/api/config", (req, res) => {
  res.json({
    gateway: { host: "0.0.0.0", port: 18790, token: "cg_nLnov7DPd9yqZDYPEU5pHnoa" },
    agents: { defaults: { max_tool_iterations: 10, max_tokens: 4000 } },
    system: { logging: { level: "info" } }
  });
});

app.post("/webui/api/config", (req, res) => {
  addLog("INFO", "Configuration updated");
  res.send("Config saved successfully (simulated)");
});

app.get("/webui/api/nodes", (req, res) => {
  res.json({ nodes: [{ id: "node-1", name: "Main Node", online: true, version: "v1.0.0", ip: "127.0.0.1" }] });
});

app.get("/webui/api/cron", (req, res) => {
  const { id } = req.query;
  if (id) {
    const job = db.prepare("SELECT * FROM cron_jobs WHERE id = ?").get(id);
    if (job) {
      res.json({ job: { ...job, enabled: Boolean(job.enabled), deliver: Boolean(job.deliver) } });
    } else {
      res.status(404).json({ error: "Job not found" });
    }
    return;
  }
  const jobs = db.prepare("SELECT * FROM cron_jobs").all().map((j: any) => ({
    ...j,
    enabled: Boolean(j.enabled),
    deliver: Boolean(j.deliver)
  }));
  res.json({ jobs });
});

app.post("/webui/api/cron", (req, res) => {
  const { action, id, name, enabled, kind, everyMs, expr, message, deliver, channel, to } = req.body;
  if (action === "create") {
    const newId = Math.random().toString(36).substring(7);
    db.prepare(`
      INSERT INTO cron_jobs (id, name, enabled, kind, everyMs, expr, message, deliver, channel, "to")
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
    `).run(newId, name, enabled ? 1 : 0, kind, everyMs, expr, message, deliver ? 1 : 0, channel, to);
    addLog("INFO", `Created cron job: ${name}`);
    res.json({ id: newId, status: "ok" });
  } else if (action === "update") {
    db.prepare(`
      UPDATE cron_jobs SET name = ?, enabled = ?, kind = ?, everyMs = ?, expr = ?, message = ?, deliver = ?, channel = ?, "to" = ?
      WHERE id = ?
    `).run(name, enabled ? 1 : 0, kind, everyMs, expr, message, deliver ? 1 : 0, channel, to, id);
    addLog("INFO", `Updated cron job: ${name}`);
    res.json({ status: "ok" });
  }
});

app.delete("/webui/api/cron", (req, res) => {
  const { id } = req.query;
  db.prepare("DELETE FROM cron_jobs WHERE id = ?").run(id);
  addLog("INFO", `Deleted cron job: ${id}`);
  res.json({ status: "ok" });
});

app.post("/webui/api/upload", upload.single("file"), (req: any, res) => {
  if (!req.file) {
    return res.status(400).json({ error: "No file uploaded" });
  }
  const filePath = `/uploads/${req.file.filename}`;
  addLog("INFO", `File uploaded: ${req.file.originalname} -> ${filePath}`);
  res.json({ path: filePath });
});

app.get("/webui/api/skills", (req, res) => {
  const skills = db.prepare("SELECT * FROM skills").all().map((s: any) => ({
    ...s,
    tools: JSON.parse(s.tools)
  }));
  res.json({ skills });
});

app.post("/webui/api/skills", (req, res) => {
  const { action, id, name, description, tools, system_prompt } = req.body;
  if (action === "create") {
    const newId = Math.random().toString(36).substring(7);
    db.prepare("INSERT INTO skills (id, name, description, tools, system_prompt) VALUES (?, ?, ?, ?, ?)").run(newId, name, description, JSON.stringify(tools), system_prompt);
    addLog("INFO", `Created skill: ${name}`);
    res.json({ id: newId, status: "ok" });
  } else if (action === "update") {
    db.prepare("UPDATE skills SET name = ?, description = ?, tools = ?, system_prompt = ? WHERE id = ?").run(name, description, JSON.stringify(tools), system_prompt, id);
    addLog("INFO", `Updated skill: ${name}`);
    res.json({ status: "ok" });
  }
});

app.delete("/webui/api/skills", (req, res) => {
  const { id } = req.query;
  db.prepare("DELETE FROM skills WHERE id = ?").run(id);
  addLog("INFO", `Deleted skill: ${id}`);
  res.json({ status: "ok" });
});

app.get("/webui/api/logs/stream", (req, res) => {
  res.setHeader("Content-Type", "text/event-stream");
  res.setHeader("Cache-Control", "no-cache");
  res.setHeader("Connection", "keep-alive");
  res.flushHeaders();

  const onLog = (log: any) => {
    res.write(`${JSON.stringify(log)}\n`);
  };

  logEmitter.on("log", onLog);
  
  // Send initial log
  onLog({ time: new Date().toISOString(), level: "INFO", msg: "Log stream connected" });

  req.on("close", () => {
    logEmitter.off("log", onLog);
  });
});

app.post("/webui/api/chat/stream", async (req, res) => {
  const { message } = req.body;
  res.setHeader("Content-Type", "text/plain");
  res.setHeader("Transfer-Encoding", "chunked");

  addLog("DEBUG", `User message: ${message}`);

  const words = `This is a simulated streaming response from the ClawGo gateway. You said: "${message}". The gateway is currently running in local simulation mode because the external connection was refused.`.split(" ");
  
  for (const word of words) {
    res.write(word + " ");
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  
  res.end();
});

// Vite middleware for development
if (process.env.NODE_ENV !== "production") {
  const vite = await createViteServer({
    server: { middlewareMode: true },
    appType: "spa",
  });
  app.use(vite.middlewares);
} else {
  app.use(express.static("dist"));
  app.get("*", (req, res) => {
    res.sendFile("dist/index.html", { root: "." });
  });
}

app.listen(PORT, "0.0.0.0", () => {
  console.log(`Server running on http://localhost:${PORT}`);
  addLog("INFO", "Gateway WebUI Server started");
});
