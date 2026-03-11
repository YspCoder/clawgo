import React from 'react';
import ArtifactPreviewCard from '../ArtifactPreviewCard';
import EmptyState from '../EmptyState';
import InfoBlock from '../InfoBlock';

type DispatchArtifactPreviewSectionProps = {
  artifacts: any[];
  emptyLabel: string;
  formatBytes: (value: unknown) => string;
  getDataUrl: (artifact: any) => string;
  title: string;
};

const DispatchArtifactPreviewSection: React.FC<DispatchArtifactPreviewSectionProps> = ({
  artifacts,
  emptyLabel,
  formatBytes,
  getDataUrl,
  title,
}) => {
  return (
    <InfoBlock label={title} contentClassName="p-3 space-y-3">
      {artifacts.length > 0 ? artifacts.map((artifact, artifactIndex) => (
        <ArtifactPreviewCard
          key={`artifact-${artifactIndex}`}
          artifact={artifact}
          dataUrl={getDataUrl(artifact)}
          fallbackName={`artifact-${artifactIndex + 1}`}
          formatBytes={formatBytes}
        />
      )) : (
        <EmptyState message={emptyLabel} className="p-0" />
      )}
    </InfoBlock>
  );
};

export default DispatchArtifactPreviewSection;
