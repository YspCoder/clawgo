# ClawGo WebUI

ClawGo 的前端控制台，基于 **React + Vite**。

## 功能简介

- Dashboard（状态看板）
- Chat（对话与流式回复）
- Logs（实时日志 / Raw）
- Skills（技能列表、安装、管理）
- Config（配置编辑与热更新）
- Cron（定时任务管理）
- Nodes（节点状态与管理）
- Memory（记忆文件管理）

## 本地使用

### 1) 安装依赖

```bash
npm install
```

### 2) 开发模式

```bash
npm run dev
```

### 3) 构建

```bash
npm run build
```

### 4) 预览构建产物

```bash
npm run preview
```

## 线上访问

部署后通过 gateway 访问：

```text
http://<host>:<port>/webui?token=<gateway_token>
```

例如：

```text
http://134.195.210.114:18790/webui?token=xxxxx
```
