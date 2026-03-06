# ClawGo WebUI ✨

ClawGo WebUI 是项目的前端控制台，基于 React 19 + Vite 6，服务于网关模式下的统一运维与操作。

## 功能范围

- 🗺️ `Agents`
  - 统一 agent 拓扑
  - 本地 subagent 与远端 branch 展示
  - 悬浮查看状态与运行信息
- ⚙️ `Config`
  - 配置编辑
  - 热更新字段参考
- 📜 `Logs`
  - 实时日志查看
- 🧠 `Skills`
  - 技能安装、浏览、编辑
- 🗂️ `Memory`
  - 记忆文件查看与编辑
- 🧾 `Task Audit`
  - 执行链路和审计记录

## 开发命令

安装依赖：

```bash
npm install
```

开发模式：

```bash
npm run dev
```

构建：

```bash
npm run build
```

预览构建产物：

```bash
npm run preview
```

类型检查：

```bash
npm run lint
```

## 运行方式

WebUI 通常通过 gateway 提供：

```text
http://<host>:<port>/webui?token=<gateway_token>
```

本地开发时，前端开发服务由：

```bash
npm run dev
```

启动。

## 技术栈

- React 19
- React Router 7
- Vite 6
- TypeScript
- Tailwind CSS 4
- i18next

## 约定

- UI 以 gateway API 为主，不单独维护复杂业务状态源
- 页面命名与后端能力保持一致，避免重复概念
- `Agents` 页面展示的是统一 agent 拓扑，不再拆分独立 `Nodes` 页
- `system_prompt_file` 编辑能力由 agent/profile 页面承载
