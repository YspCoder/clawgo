import express from "express";
import { createServer as createViteServer } from "vite";
import { EventEmitter } from "events";
import multer from "multer";
import fs from "fs";

const app = express();
const PORT = 3000;
const logEmitter = new EventEmitter();

// In-memory only for local dev fallback (no sqlite persistence)
const mem = {
  skills: [] as any[],
  cronJobs: [] as any[],
};

const uploadDir = "uploads";
if (!fs.existsSync(uploadDir)) fs.mkdirSync(uploadDir);

const storage = multer.diskStorage({
  destination: (_, __, cb) => cb(null, uploadDir),
  filename: (_, file, cb) => cb(null, Date.now() + "-" + file.originalname),
});
const upload = multer({ storage });

app.use(express.json());
app.use("/uploads", express.static(uploadDir));

function addLog(level: string, msg: string, extra = {}) {
  const entry = { time: new Date().toISOString(), level, msg, ...extra };
  logEmitter.emit("log", entry);
  console.log(`[${level}] ${msg}`);
}

app.get("/webui/api/config", (_req, res) => {
  res.json({
    gateway: { host: "0.0.0.0", port: 18790, token: "cg_nLnov7DPd9yqZDYPEU5pHnoa" },
    agents: { defaults: { max_tool_iterations: 10, max_tokens: 4000 } },
    system: { logging: { level: "info" } },
  });
});

app.post("/webui/api/config", (_req, res) => {
  addLog("INFO", "Configuration updated");
  res.send("Config saved successfully (simulated)");
});

app.get("/webui/api/nodes", (_req, res) => {
  res.json({ nodes: [{ id: "node-1", name: "Main Node", online: true, version: "v1.0.0", ip: "127.0.0.1" }] });
});

app.get("/webui/api/cron", (req, res) => {
  const id = String(req.query.id || "");
  if (id) {
    const job = mem.cronJobs.find((j) => j.id === id);
    if (!job) return res.status(404).json({ error: "Job not found" });
    return res.json({ job });
  }
  res.json({ jobs: mem.cronJobs });
});

app.post("/webui/api/cron", (req, res) => {
  const { action, id, ...rest } = req.body || {};
  if (action === "create") {
    const newId = Math.random().toString(36).slice(2);
    const job = { id: newId, enabled: true, ...rest };
    mem.cronJobs.push(job);
    addLog("INFO", `Created cron job: ${job.name || newId}`);
    return res.json({ id: newId, status: "ok" });
  }
  if (action === "update") {
    const idx = mem.cronJobs.findIndex((j) => j.id === id);
    if (idx >= 0) mem.cronJobs[idx] = { ...mem.cronJobs[idx], ...rest };
    addLog("INFO", `Updated cron job: ${id}`);
    return res.json({ status: "ok" });
  }
  if (action === "delete") {
    mem.cronJobs = mem.cronJobs.filter((j) => j.id !== id);
    addLog("INFO", `Deleted cron job: ${id}`);
    return res.json({ status: "ok" });
  }
  return res.status(400).json({ error: "unsupported action" });
});

app.post("/webui/api/upload", upload.single("file"), (req: any, res) => {
  if (!req.file) return res.status(400).json({ error: "No file uploaded" });
  const filePath = `/uploads/${req.file.filename}`;
  addLog("INFO", `File uploaded: ${req.file.originalname} -> ${filePath}`);
  res.json({ path: filePath });
});

app.get("/webui/api/skills", (_req, res) => {
  res.json({ skills: mem.skills });
});

app.post("/webui/api/skills", (req, res) => {
  const { action, id, ...rest } = req.body || {};
  if (action === "create") {
    const newId = Math.random().toString(36).slice(2);
    mem.skills.push({ id: newId, ...rest });
    addLog("INFO", `Created skill: ${rest.name || newId}`);
    return res.json({ id: newId, status: "ok" });
  }
  if (action === "update") {
    const idx = mem.skills.findIndex((s) => s.id === id);
    if (idx >= 0) mem.skills[idx] = { ...mem.skills[idx], ...rest };
    addLog("INFO", `Updated skill: ${id}`);
    return res.json({ status: "ok" });
  }
  if (action === "delete") {
    mem.skills = mem.skills.filter((s) => s.id !== id);
    addLog("INFO", `Deleted skill: ${id}`);
    return res.json({ status: "ok" });
  }
  return res.status(400).json({ error: "unsupported action" });
});

app.get("/webui/api/logs/stream", (req, res) => {
  res.setHeader("Content-Type", "application/x-ndjson");
  res.setHeader("Cache-Control", "no-cache");
  res.setHeader("Connection", "keep-alive");

  const onLog = (log: any) => res.write(JSON.stringify(log) + "\n");
  logEmitter.on("log", onLog);
  onLog({ time: new Date().toISOString(), level: "INFO", msg: "Log stream connected" });

  req.on("close", () => logEmitter.off("log", onLog));
});

app.post("/webui/api/chat/stream", async (req, res) => {
  const { message } = req.body || {};
  res.setHeader("Content-Type", "text/plain");
  res.setHeader("Transfer-Encoding", "chunked");
  const words = `Simulated streaming response: ${String(message || "")}`.split(" ");
  for (const w of words) {
    res.write(w + " ");
    await new Promise((r) => setTimeout(r, 40));
  }
  res.end();
});

if (process.env.NODE_ENV !== "production") {
  const vite = await createViteServer({ server: { middlewareMode: true }, appType: "spa" });
  app.use(vite.middlewares);
} else {
  app.use(express.static("dist"));
  app.get("*", (_req, res) => {
    res.sendFile("dist/index.html", { root: "." });
  });
}

app.listen(PORT, "0.0.0.0", () => {
  console.log(`Server running on http://localhost:${PORT}`);
  addLog("INFO", "Gateway WebUI Server started");
});
