'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { X } from 'lucide-react';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';

type ToastVariant = 'default' | 'destructive' | 'success';

interface ToastOptions {
  title?: string;
  description?: string;
  variant?: ToastVariant;
  duration?: number;
}

interface ToastRecord extends ToastOptions {
  id: number;
  variant: ToastVariant;
  isVisible: boolean;
}

interface ToastContextValue {
  show: (options: ToastOptions) => number;
  dismiss: (id: number) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<ToastRecord[]>([]);

  const removeToast = useCallback((id: number) => {
    setToasts((prev) => prev.filter((toast) => toast.id !== id));
  }, []);

  const beginExit = useCallback((id: number) => {
    setToasts((prev) =>
      prev.map((toast) =>
        toast.id === id ? { ...toast, isVisible: false } : toast,
      ),
    );
    if (typeof window !== 'undefined') {
      window.setTimeout(() => {
        removeToast(id);
      }, 220);
    }
  }, [removeToast]);

  const show = useCallback(
    (options: ToastOptions) => {
      const id = Number(`${Date.now()}${Math.floor(Math.random() * 1_000)}`);
      const duration = options.duration ?? 4000;
      setToasts((prev) => [
        ...prev,
        {
          id,
          title: options.title,
          description: options.description,
          variant: options.variant ?? 'default',
          duration,
          isVisible: false,
        } as ToastRecord,
      ]);
      if (typeof window !== 'undefined') {
        window.requestAnimationFrame(() => {
          setToasts((prev) =>
            prev.map((toast) =>
              toast.id === id ? { ...toast, isVisible: true } : toast,
            ),
          );
        });
        window.setTimeout(() => {
          beginExit(id);
        }, duration);
      }
      return id;
    },
    [beginExit],
  );

  const dismiss = useCallback(
    (id: number) => {
      beginExit(id);
    },
    [beginExit],
  );

  const value = useMemo(
    () => ({
      show,
      dismiss,
    }),
    [dismiss, show],
  );

  return (
    <ToastContext.Provider value={value}>
      {children}
      <ToastViewport toasts={toasts} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

export function useToast() {
  const context = useContext(ToastContext);
  if (!context) {
    throw new Error('useToast must be used within a ToastProvider');
  }
  return context;
}

interface ToastViewportProps {
  toasts: ToastRecord[];
  onDismiss: (id: number) => void;
}

function ToastViewport({ toasts, onDismiss }: ToastViewportProps) {
  const [mounted, setMounted] = useState(false);

  /* eslint-disable react-hooks/set-state-in-effect */
  useEffect(() => {
    setMounted(true);
  }, []);
  /* eslint-enable react-hooks/set-state-in-effect */

  if (!mounted) {
    return null;
  }

  const portalTarget = typeof document !== 'undefined' ? document.body : null;
  if (!portalTarget) {
    return null;
  }

  return createPortal(
    <div className="pointer-events-none fixed inset-x-0 top-4 z-50 flex flex-col items-center gap-3 px-4 sm:items-end sm:px-6">
      {toasts.map((toast) => (
        <ToastItem key={toast.id} toast={toast} onDismiss={onDismiss} />
      ))}
    </div>,
    portalTarget,
  );
}

interface ToastItemProps {
  toast: ToastRecord;
  onDismiss: (id: number) => void;
}

function ToastItem({ toast, onDismiss }: ToastItemProps) {
  const variantClass =
    toast.variant === 'destructive'
      ? 'border-destructive/60 bg-destructive/10 text-destructive dark:text-destructive-foreground'
      : toast.variant === 'success'
        ? 'border-emerald-500/50 bg-emerald-500/10 text-emerald-900 dark:text-emerald-100'
        : 'border-border bg-card text-card-foreground';

  const titleClass = cn(
    'text-sm font-semibold',
    toast.variant === 'destructive' && 'text-destructive dark:text-destructive-foreground',
    toast.variant === 'success' && 'text-emerald-900 dark:text-emerald-100',
  );

  const descriptionClass = cn(
    'mt-1 text-sm',
    toast.variant === 'destructive'
      ? 'text-destructive/80 dark:text-destructive-foreground/80'
      : toast.variant === 'success'
        ? 'text-emerald-800 dark:text-emerald-200'
        : 'text-muted-foreground',
  );

  return (
    <div
      className={cn(
        'pointer-events-auto w-full max-w-sm overflow-hidden rounded-md border shadow-lg transition-all duration-200 ease-out',
        toast.isVisible ? 'translate-y-0 opacity-100' : 'translate-y-2 opacity-0',
        variantClass,
      )}
    >
      <div className="flex items-start gap-3 p-4">
        <div className="flex-1">
          {toast.title && <p className={titleClass}>{toast.title}</p>}
          {toast.description && <p className={descriptionClass}>{toast.description}</p>}
        </div>
        <Button
          variant="ghost"
          size="icon"
          onClick={() => onDismiss(toast.id)}
          className="h-6 w-6 shrink-0 rounded-full text-muted-foreground hover:text-foreground"
        >
          <X className="h-4 w-4" />
          <span className="sr-only">Dismiss</span>
        </Button>
      </div>
    </div>
  );
}
