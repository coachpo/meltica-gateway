'use client';

const baseLoader = () => import('ace-builds/src-noconflict/ace');

const modeLoaders = {
  javascript: () => import('ace-builds/src-noconflict/mode-javascript'),
  typescript: () => import('ace-builds/src-noconflict/mode-typescript'),
  json: () => import('ace-builds/src-noconflict/mode-json'),
  text: () => import('ace-builds/src-noconflict/mode-text'),
} satisfies Record<string, () => Promise<unknown>>;

const themeLoaders = {
  tomorrow: () => import('ace-builds/src-noconflict/theme-tomorrow'),
  tomorrow_night: () => import('ace-builds/src-noconflict/theme-tomorrow_night'),
  github: () => import('ace-builds/src-noconflict/theme-github'),
  monokai: () => import('ace-builds/src-noconflict/theme-monokai'),
} satisfies Record<string, () => Promise<unknown>>;

const extraLoaders = {
  'language-tools': () => import('ace-builds/src-noconflict/ext-language_tools'),
  'searchbox': () => import('ace-builds/src-noconflict/ext-searchbox'),
} satisfies Record<string, () => Promise<unknown>>;

const loadCache = new Map<string, Promise<void>>();

function cachedLoad(key: string, loader: () => Promise<unknown>) {
  if (!loadCache.has(key)) {
    loadCache.set(
      key,
      loader().then(() => {
        /* no-op */
      }),
    );
  }
  return loadCache.get(key)!;
}

export type AceMode = keyof typeof modeLoaders;
export type AceTheme = keyof typeof themeLoaders;
export type AceExtraModule = keyof typeof extraLoaders;

type LoadAceAssetsParams = {
  mode?: AceMode | null;
  theme?: AceTheme | null;
  extras?: AceExtraModule[] | null;
};

export async function loadAceAssets({ mode, theme, extras }: LoadAceAssetsParams = {}) {
  await cachedLoad('base', baseLoader);

  const loaders: Promise<void>[] = [];

  if (mode && modeLoaders[mode]) {
    loaders.push(cachedLoad(`mode-${mode}`, modeLoaders[mode]));
  }

  if (theme && themeLoaders[theme]) {
    loaders.push(cachedLoad(`theme-${theme}`, themeLoaders[theme]));
  }

  if (Array.isArray(extras)) {
    for (const extra of extras) {
      if (extraLoaders[extra]) {
        loaders.push(cachedLoad(`extra-${extra}`, extraLoaders[extra]));
      }
    }
  }

  if (loaders.length === 0) {
    return;
  }

  await Promise.all(loaders);
}
