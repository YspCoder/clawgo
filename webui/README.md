# ClawGo WebUI

React + Vite frontend for ClawGo WebUI.

## What was cleaned

- Removed unused packages:
  - `@google/genai`
  - `better-sqlite3`
  - `dotenv`
  - `@types/react-router-dom` (v5 typings, not used)
- Moved build-only tooling to `devDependencies` (`vite`, `@vitejs/plugin-react`, `@tailwindcss/vite`, etc.).
- Updated package metadata/name to `clawgo-webui`.

## Development

### Prerequisites

- Node.js 18+

### Install

```bash
npm install
```

### Start local dev server

```bash
npm run dev
```

### Build

```bash
npm run build
```

### Preview build

```bash
npm run preview
```

## Notes

- `server.ts` is a local dev fallback API server.
- Production deployment uses ClawGo gateway `/webui/api/*` endpoints, not SQLite.
