'use client';

import { forwardRef, useMemo } from 'react';

import type { CodeEditorProps } from './code-editor';
import { CodeEditor } from './code-editor';

type ViewerOmittedProps =
  | 'onChange'
  | 'readOnly'
  | 'onSubmitShortcut'
  | 'enableBasicAutocompletion'
  | 'enableLiveAutocompletion'
  | 'enableSnippets';

export interface CodeViewerProps extends Omit<CodeEditorProps, ViewerOmittedProps> {
  onCopyRequest?: () => void;
}

export const CodeViewer = forwardRef<unknown, CodeViewerProps>(function CodeViewer(
  {
    value,
    className,
    editorClassName,
    setOptions,
    highlightActiveLine,
    showPrintMargin,
    showGutter,
    allowHorizontalScroll,
    wrapEnabled,
    onCopyRequest,
    ...rest
  },
  ref,
) {
  const mergedSetOptions = useMemo(() => ({
    ...setOptions,
    readOnly: true,
  }), [setOptions]);

  return (
    <CodeEditor
      ref={ref}
      value={value}
      onChange={() => {
        // noop to satisfy required handler
      }}
      className={className}
      editorClassName={editorClassName}
      readOnly
      highlightActiveLine={highlightActiveLine ?? false}
      showPrintMargin={showPrintMargin ?? false}
      showGutter={showGutter ?? false}
      allowHorizontalScroll={allowHorizontalScroll ?? true}
      wrapEnabled={wrapEnabled ?? false}
      setOptions={mergedSetOptions}
      onCopy={onCopyRequest}
      {...rest}
    />
  );
});
