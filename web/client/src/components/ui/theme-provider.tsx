'use client';

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';

type Theme = 'light' | 'dark';

interface ThemeContextValue {
  theme: Theme;
  toggleTheme: () => void;
  setTheme: (theme: Theme) => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);
const STORAGE_KEY = 'meltica-theme';

function getPreferredTheme(): Theme {
  if (typeof window === 'undefined') {
    return 'light';
  }
  const stored = window.localStorage.getItem(STORAGE_KEY);
  if (stored === 'light' || stored === 'dark') {
    return stored;
  }
  const prefersDark = window.matchMedia?.('(prefers-color-scheme: dark)').matches;
  return prefersDark ? 'dark' : 'light';
}

function applyThemeClass(theme: Theme) {
  if (typeof document === 'undefined') {
    return;
  }
  const root = document.documentElement;
  root.classList.toggle('dark', theme === 'dark');
  root.dataset.theme = theme;
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<Theme>('light');

  useEffect(() => {
    const initial = getPreferredTheme();
    setThemeState(initial);
    applyThemeClass(initial);
  }, []);

  const updateTheme = useCallback((value: Theme) => {
    setThemeState(value);
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, value);
    }
    applyThemeClass(value);
  }, []);

  const toggleTheme = useCallback(() => {
    updateTheme(theme === 'dark' ? 'light' : 'dark');
  }, [theme, updateTheme]);

  const setTheme = useCallback(
    (value: Theme) => {
      updateTheme(value);
    },
    [updateTheme],
  );

  const value = useMemo(
    () => ({
      theme,
      toggleTheme,
      setTheme,
    }),
    [theme, toggleTheme, setTheme],
  );

  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  const context = useContext(ThemeContext);
  if (!context) {
    throw new Error('useTheme must be used within a ThemeProvider');
  }
  return context;
}
