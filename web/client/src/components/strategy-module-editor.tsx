'use client';

import dynamic from 'next/dynamic';
import { useMemo, useRef, type KeyboardEvent } from 'react';
import { Textarea } from '@/components/ui/textarea';
import type { StrategyDiagnostic } from '@/lib/types';
import { cn } from '@/lib/utils';

const AceEditor = dynamic(
  async () => {
    const ace = await import('react-ace');
    await Promise.all([
      import('ace-builds/src-noconflict/mode-javascript'),
      import('ace-builds/src-noconflict/theme-tomorrow'),
      import('ace-builds/src-noconflict/ext-language_tools'),
    ]);
    return ace;
  },
  { ssr: false },
);

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
  const textareaRef = useRef<HTMLTextAreaElement | null>(null);

  const handleKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (!onSubmit) {
      return;
    }
    const isCtrlOrCmd = event.ctrlKey || event.metaKey;
    if (isCtrlOrCmd && event.key === 'Enter') {
      event.preventDefault();
      onSubmit();
    }
  };

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

  if (!useEnhancedEditor) {
    return (
      <Textarea
        ref={textareaRef}
        value={value}
        onChange={(event) => onChange(event.target.value)}
        disabled={disabled}
        readOnly={readOnly}
        placeholder={placeholder}
        spellCheck={false}
        onKeyDown={handleKeyDown}
        className={cn('min-h-[320px] max-h-[60vh] resize-y bg-transparent font-mono text-sm', className)}
        aria-label={ariaLabel}
      />
    );
  }

  return (
    <div className={cn('relative w-full rounded-md border', className)}>
      <AceEditor
        mode="javascript"
        theme="tomorrow"
        name="strategy-module-editor"
        width="100%"
        height="100%"
        minLines={18}
        maxLines={Infinity}
        fontSize={14}
        showPrintMargin={false}
        highlightActiveLine={!readOnly}
        readOnly={readOnly || disabled}
        value={value}
        onChange={(next) => onChange(next)}
        placeholder={placeholder}
        setOptions={{
          useWorker: false,
          wrap: true,
          tabSize: 2,
          displayIndentGuides: true,
          enableBasicAutocompletion: false,
          enableLiveAutocompletion: false,
        }}
        annotations={annotations}
        editorProps={{ $blockScrolling: true }}
        commands={
          onSubmit
            ? [
                {
                  name: 'submit-strategy-module',
                  bindKey: { win: 'Ctrl-Enter', mac: 'Command-Enter' },
                  exec: () => onSubmit(),
                },
              ]
            : []
        }
        aria-label={ariaLabel}
        className="min-h-[320px] max-h-[60vh]"
      />
    </div>
  );
}
