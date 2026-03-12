import React from 'react';

type ArtifactPreviewCardProps = {
  artifact: any;
  className?: string;
  dataUrl: string;
  fallbackName: string;
  formatBytes: (value: unknown) => string;
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const ArtifactPreviewCard: React.FC<ArtifactPreviewCardProps> = ({
  artifact,
  className,
  dataUrl,
  fallbackName,
  formatBytes,
}) => {
  const kind = String(artifact?.kind || '').trim().toLowerCase();
  const mime = String(artifact?.mime_type || '').trim().toLowerCase();
  const isImage = kind === 'image' || mime.startsWith('image/');
  const isVideo = kind === 'video' || mime.startsWith('video/');
  const displayName = String(artifact?.name || artifact?.source_path || fallbackName);
  const meta = [artifact?.kind, artifact?.mime_type, formatBytes(artifact?.size_bytes)].filter(Boolean).join(' · ');
  const pathText = String(artifact?.source_path || artifact?.path || artifact?.url || '-');
  const contentText = String(artifact?.content_text || '').trim();

  return (
    <div className={joinClasses('rounded-2xl border border-zinc-800 bg-zinc-950/40 p-3', className)}>
      <div className="flex items-center justify-between gap-3 mb-2">
        <div className="min-w-0">
          <div className="text-xs font-medium text-zinc-200 truncate">{displayName}</div>
          <div className="text-[11px] text-zinc-500 truncate">{meta}</div>
        </div>
        <div className="text-[11px] text-zinc-500">{String(artifact?.storage || '-')}</div>
      </div>
      {isImage && dataUrl ? (
        <img src={dataUrl} alt={displayName} className="ui-media-surface-strong max-h-48 rounded-xl border object-contain" />
      ) : null}
      {isVideo && dataUrl ? (
        <video src={dataUrl} controls className="ui-media-surface-strong max-h-48 w-full rounded-xl border" />
      ) : null}
      {!isImage && !isVideo && contentText !== '' ? (
        <pre className="ui-media-surface rounded-xl border p-3 text-[11px] text-zinc-300 whitespace-pre-wrap overflow-auto max-h-48">{contentText}</pre>
      ) : null}
      {!isImage && !isVideo && contentText === '' ? (
        <div className="text-[11px] text-zinc-500 break-all">{pathText}</div>
      ) : null}
    </div>
  );
};

export default ArtifactPreviewCard;
