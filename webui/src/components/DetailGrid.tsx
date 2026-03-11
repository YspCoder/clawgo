import React from 'react';

type DetailGridItem = {
  key?: string;
  label: React.ReactNode;
  value: React.ReactNode;
  valueClassName?: string;
};

type DetailGridProps = {
  className?: string;
  columnsClassName?: string;
  items: DetailGridItem[];
};

function joinClasses(...values: Array<string | undefined | false>) {
  return values.filter(Boolean).join(' ');
}

const DetailGrid: React.FC<DetailGridProps> = ({
  className,
  columnsClassName = 'grid-cols-2 md:grid-cols-3',
  items,
}) => {
  return (
    <div className={joinClasses('grid gap-3', columnsClassName, className)}>
      {items.map((item, index) => (
        <div key={item.key || String(index)}>
          <div className="text-zinc-500 text-xs">{item.label}</div>
          <div className={item.valueClassName}>{item.value}</div>
        </div>
      ))}
    </div>
  );
};

export default DetailGrid;
export type { DetailGridItem, DetailGridProps };
