import React, { useMemo, useState } from 'react';
import { Plus } from 'lucide-react';
import { useTranslation } from 'react-i18next';
import { FixedButton } from './Button';
import { SwitchCardField, SelectField, TextField, TextareaField } from './FormControls';

interface RecursiveConfigProps {
  data: any;
  labels: Record<string, string>;
  path?: string;
  onChange: (path: string, val: any) => void;
  hotPaths?: string[];
  onlyHot?: boolean;
}

const isPrimitive = (v: any) => ['string', 'number', 'boolean'].includes(typeof v) || v === null;
const isPathHot = (currentPath: string, hotPaths: string[]) => {
  if (!hotPaths.length) return true;
  return hotPaths.some((hp) => {
    const p = String(hp || '').replace(/\.\*$/, '');
    if (!p) return false;
    return currentPath === p || currentPath.startsWith(`${p}.`) || p.startsWith(`${currentPath}.`);
  });
};

const PrimitiveArrayEditor: React.FC<{
  value: any[];
  path: string;
  onChange: (next: any[]) => void;
}> = ({ value, path, onChange }) => {
  const { t } = useTranslation();
  const [draft, setDraft] = useState('');
  const [selected, setSelected] = useState('');

  const suggestions = useMemo(() => {
