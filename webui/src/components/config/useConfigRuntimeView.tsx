import React, { useState } from 'react';
import { filterRuntimeEventsByWindow, RuntimeWindow } from './configUtils';

type RuntimeSection = 'candidates' | 'hits' | 'errors' | 'changes';

export function useConfigRuntimeView(runtimeWindow: RuntimeWindow) {
  const [runtimeSections, setRuntimeSections] = useState<Record<string, { candidates?: boolean; hits?: boolean; errors?: boolean; changes?: boolean }>>({});

  function filterRuntimeEvents(items: any[]) {
    return filterRuntimeEventsByWindow(items, runtimeWindow);
  }

  function toggleRuntimeSection(name: string, section: RuntimeSection) {
    setRuntimeSections((prev) => ({
      ...prev,
      [name]: {
        ...(prev[name] || {}),
        [section]: !(prev[name]?.[section] ?? true),
      },
    }));
  }

  function runtimeSectionOpen(name: string, section: RuntimeSection) {
    return runtimeSections[name]?.[section] ?? true;
  }

  function renderRuntimeEventList(items: any[], prefix: string) {
    if (!items.length) return <div className="text-zinc-500">-</div>;
    return (
      <div className="space-y-1">
        {items.map((item: any, idx: number) => (
          <div key={`${prefix}-${idx}`} className="rounded-lg border border-zinc-800 bg-zinc-900/30 px-3 py-2">
            <div>{`${item?.when || '-'} ${item?.kind || '-'} ${item?.target || '-'}${item?.reason ? ` (${item.reason})` : ''}`}</div>
            {item?.detail ? <div className="mt-1 text-zinc-500">{String(item.detail)}</div> : null}
          </div>
        ))}
      </div>
    );
  }

  return {
    filterRuntimeEvents,
    renderRuntimeEventList,
    runtimeSectionOpen,
    toggleRuntimeSection,
  };
}
