export function dataUrlForArtifact(artifact: any) {
  const mime = String(artifact?.mime_type || '').trim() || 'application/octet-stream';
  const content = String(artifact?.content_base64 || '').trim();
  if (!content) return '';
  return `data:${mime};base64,${content}`;
}

export function formatArtifactBytes(value: unknown) {
  const size = Number(value || 0);
  if (!Number.isFinite(size) || size <= 0) return '-';
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MB`;
}
