'use client';

import {
  forwardRef,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type AriaAttributes,
} from 'react';
import dynamic from 'next/dynamic';
import type { IAceEditorProps, ICommand } from 'react-ace';

import type { Ace } from 'ace-builds';
import { useTheme } from '@/components/ui/theme-provider';
import { cn } from '@/lib/utils';

import { loadAceAssets, type AceExtraModule, type AceMode, type AceTheme } from './ace-loader';

const AceEditor = dynamic(async () => {
  const ace = await import('react-ace');
  return ace.default;
}, {
  ssr: false,
});

const DEFAULT_SET_OPTIONS = {
  useWorker: false,
  displayIndentGuides: true,
  tabSize: 2,
} satisfies IAceEditorProps['setOptions'];

export interface CodeEditorProps
  extends Omit<
    IAceEditorProps,
    | 'mode'
    | 'theme'
    | 'onChange'
    | 'value'
    | 'className'
    | 'commands'
    | 'setOptions'
  > {
  /**
   * Identifier applied to the outer container.
   */
  id?: string;
  value: string;
  onChange: (next: string) => void;
  mode?: AceMode;
  theme?: AceTheme;
  extras?: AceExtraModule[];
  /**
   * Tailwind / CSS classes applied to the outer container that wraps Ace.
   */
  className?: string;
  /**
   * Additional classes applied directly to the Ace component.
   */
  editorClassName?: string;
  /**
   * Merge additional Ace commands. `onSubmitShortcut` is appended automatically if provided.
   */
  commands?: ICommand[];
  /**
   * Merge additional Ace setOptions.
   */
  setOptions?: IAceEditorProps['setOptions'];
  /**
   * Triggered when the user presses Ctrl/Cmd+Enter.
   */
  onSubmitShortcut?: () => void;
  /**
   * Enables the Ace horizontal scrollbar and disables soft wrapping.
   */
  allowHorizontalScroll?: boolean;
}

export const CodeEditor = forwardRef<unknown, CodeEditorProps>(function CodeEditor(
  props,
  ref,
) {
  const {
    value,
    onChange,
    mode = 'javascript',
    theme,
    extras,
    className,
    editorClassName,
    commands,
    setOptions,
    onSubmitShortcut,
    enableBasicAutocompletion,
    enableLiveAutocompletion,
    enableSnippets,
    height,
    width,
    allowHorizontalScroll = false,
    wrapEnabled: wrapEnabledProp,
    onLoad: onEditorLoad,
    id,
    ...rest
  } = props;
  const ariaProps = props as CodeEditorProps & AriaAttributes;
  const ariaLabel = ariaProps['aria-label'];
  const ariaLabelledBy = ariaProps['aria-labelledby'];
  const ariaDescribedBy = ariaProps['aria-describedby'];
  const { theme: appTheme } = useTheme();
  const [isReady, setIsReady] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const editorRef = useRef<Ace.Editor | null>(null);

  const resolvedTheme = useMemo<AceTheme>(() => {
    if (!theme || theme === 'tomorrow' || theme === 'tomorrow_night') {
      return appTheme === 'dark' ? 'tomorrow_night' : 'tomorrow';
    }
    return theme;
  }, [appTheme, theme]);

  const resolvedExtras = useMemo(() => {
    const extrasSet = new Set<AceExtraModule>(extras ?? []);
    if (enableBasicAutocompletion || enableLiveAutocompletion || enableSnippets) {
      extrasSet.add('language-tools');
    }
    extrasSet.add('searchbox');
    return Array.from(extrasSet);
  }, [extras, enableBasicAutocompletion, enableLiveAutocompletion, enableSnippets]);

  useEffect(() => {
    let cancelled = false;
    loadAceAssets({
      mode,
      theme: resolvedTheme,
      extras: resolvedExtras,
    }).then(() => {
      if (!cancelled) {
        setIsReady(true);
      }
    });

    return () => {
      cancelled = true;
      setIsReady(false);
    };
  }, [mode, resolvedTheme, resolvedExtras]);

  const mergedCommands = useMemo(() => {
    if (!onSubmitShortcut) {
      return commands;
    }
    const submitCommand: ICommand = {
      name: 'code-editor-submit',
      bindKey: { win: 'Ctrl-Enter', mac: 'Command-Enter' },
      exec: () => onSubmitShortcut(),
    };

    const baseCommands = Array.isArray(commands) ? commands : [];

    return [...baseCommands.filter((command) => command?.name !== submitCommand.name), submitCommand];
  }, [commands, onSubmitShortcut]);

  const finalWrapEnabled = wrapEnabledProp ?? !allowHorizontalScroll;

  const mergedSetOptions = useMemo(() => {
    const options = {
      ...DEFAULT_SET_OPTIONS,
      ...(setOptions ?? {}),
    } as NonNullable<IAceEditorProps['setOptions']> & { wrap?: boolean };

    options.wrap = finalWrapEnabled;

    return options;
  }, [finalWrapEnabled, setOptions]);

  const handleLoad = useCallback<NonNullable<IAceEditorProps['onLoad']>>(
    (editor) => {
      editorRef.current = editor;
      const textInput = editor.textInput?.getElement?.();
      if (textInput) {
        if (id) {
          textInput.setAttribute('id', id);
        }
        if (ariaLabel) {
          textInput.setAttribute('aria-label', ariaLabel);
        } else {
          textInput.removeAttribute('aria-label');
        }
        if (ariaLabelledBy) {
          textInput.setAttribute('aria-labelledby', ariaLabelledBy);
        } else {
          textInput.removeAttribute('aria-labelledby');
        }
        if (ariaDescribedBy) {
          textInput.setAttribute('aria-describedby', ariaDescribedBy);
        } else {
          textInput.removeAttribute('aria-describedby');
        }
      }
      onEditorLoad?.(editor);
    },
    [ariaDescribedBy, ariaLabel, ariaLabelledBy, id, onEditorLoad],
  );

  const containerClassName = cn('relative w-full', className);

  useEffect(() => () => {
    editorRef.current = null;
  }, []);

  useEffect(() => {
    const editor = editorRef.current;
    const container = containerRef.current;
    if (!isReady || !editor || !container) {
      return;
    }

    editor.resize();

    if (typeof window === 'undefined' || typeof window.ResizeObserver !== 'function') {
      return;
    }

    const ResizeObserverConstructor = window.ResizeObserver;
    const resizeObserver = new ResizeObserverConstructor(() => {
      editor.resize();
    });

    resizeObserver.observe(container);

    return () => {
      resizeObserver.disconnect();
    };
  }, [height, isReady, width]);

  if (!isReady) {
    return (
      <div id={id} className={containerClassName} data-slot="code-editor" ref={containerRef}>
        <div
          className={cn(
            'min-h-32 w-full rounded-md border border-dashed border-muted-foreground/40 bg-transparent',
            editorClassName,
          )}
          aria-label={rest['aria-label']}
          role="presentation"
        />
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      id={id ? `${id}-container` : undefined}
      className={cn('relative w-full', className)}
      data-slot="code-editor"
    >
      <AceEditor
        ref={ref}
        id={id}
        mode={mode}
        theme={resolvedTheme}
        value={value}
        width={width ?? '100%'}
        height={height ?? '100%'}
        className={editorClassName}
        onChange={(next) => onChange(next ?? '')}
        commands={mergedCommands}
        setOptions={mergedSetOptions}
        wrapEnabled={finalWrapEnabled}
        onLoad={handleLoad}
        enableBasicAutocompletion={enableBasicAutocompletion}
        enableLiveAutocompletion={enableLiveAutocompletion}
        enableSnippets={enableSnippets}
        {...rest}
      />
    </div>
  );
});
