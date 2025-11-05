'use client';

import { useMemo } from 'react';
import { CodeEditor } from '@/components/code';
import type { StrategyDiagnostic } from '@/lib/types';
import { cn } from '@/lib/utils';

type StrategyModuleEditorProps = {
  value: string;
  onChange: (value: string) => void;
  diagnostics?: StrategyDiagnostic[];
  disabled?: boolean;
  readOnly?: boolean;
  placeholder?: string;
  useEnhancedEditor: boolean;
  onSubmit?: () => void;
  className?: string;
  'aria-label'?: string;
};

export function StrategyModuleEditor({
  value,
  onChange,
  diagnostics = [],
  disabled = false,
  readOnly = false,
  placeholder,
  useEnhancedEditor,
  onSubmit,
  className,
  'aria-label': ariaLabel,
}: StrategyModuleEditorProps) {
  const annotations = useMemo(() => {
    if (!useEnhancedEditor) {
      return [];
    }
    return diagnostics
      .filter((entry) => typeof entry.line === 'number' && (entry.line ?? 0) > 0)
      .map((entry, index) => ({
        row: Math.max(0, (entry.line ?? 1) - 1),
        column: Math.max(0, (entry.column ?? 1) - 1),
        type: 'error' as const,
        text: entry.message || `Validation error ${index + 1}`,
      }));
  }, [diagnostics, useEnhancedEditor]);

  const isReadOnly = readOnly || disabled;

  return (
    <CodeEditor
      value={value}
      onChange={onChange}
      mode="javascript"
      fontSize={14}
      allowHorizontalScroll={!isReadOnly}
      wrapEnabled={isReadOnly}
      height="100%"
      showPrintMargin={false}
      highlightActiveLine={useEnhancedEditor && !readOnly}
      showGutter={useEnhancedEditor}
      readOnly={isReadOnly}
      placeholder={placeholder}
      setOptions={{
        displayIndentGuides: useEnhancedEditor,
      }}
      enableBasicAutocompletion={useEnhancedEditor}
      enableLiveAutocompletion={useEnhancedEditor}
      enableSnippets={false}
      editorProps={{ $blockScrolling: true }}
      annotations={annotations}
      onSubmitShortcut={onSubmit}
      className={cn(
        'relative w-full rounded-md border h-[320px] max-h-[60vh] lg:h-[440px]',
        className,
      )}
      editorClassName="h-full font-mono text-sm"
      aria-label={ariaLabel}
    />
  );
}
