'use client';

import { ChangeEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { apiClient } from '@/lib/api-client';
import type { StrategyModuleRevision, StrategyModuleSummary } from '@/lib/types';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Badge } from '@/components/ui/badge';
import { Separator } from '@/components/ui/separator';
import { Checkbox } from '@/components/ui/checkbox';
import { useToast } from '@/components/ui/toast-provider';
import { ConfirmDialog } from '@/components/confirm-dialog';
import {
  ArrowUpCircle,
  Copy,
  Eye,
  FileCode,
  Loader2,
  Pencil,
  RefreshCw,
  Tag,
  Trash2,
  UploadCloud,
} from 'lucide-react';

type ModuleFormMode = 'create' | 'edit';

type LoadOptions = {
  silent?: boolean;
};

type RefreshOptions = {
  silent?: boolean;
  notifySuccess?: boolean;
};

type ModuleFormState = {
  name: string;
  filename: string;
  tag: string;
  aliases: string;
  source: string;
  promoteLatest: boolean;
};

const defaultFormState: ModuleFormState = {
  name: '',
  filename: '',
  tag: '',
  aliases: '',
  source: '',
  promoteLatest: true,
};

function formatBytes(size: number): string {
  if (!Number.isFinite(size) || size <= 0) {
    return '—';
  }
  const units = ['B', 'KB', 'MB', 'GB'];
  let index = 0;
  let value = size;
  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  return `${value % 1 === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[index]}`;
}

function directoryFromPath(path: string | undefined): string | null {
  if (!path) {
    return null;
  }
  const segments = path.split(/[\\/]/).filter(Boolean);
  if (segments.length === 0) {
    return null;
  }
  segments.pop();
  return segments.join('/');
}

const FILE_EXTENSION_HINT = '.js or .mjs';
const PINNED_REVISION_MESSAGE =
  'Revision is pinned by running instances. Stop or redeploy them before deleting.';

function friendlyDeletionMessage(message: string): string {
  const lower = message.toLowerCase();
  if (lower.includes('in use') || lower.includes('pinned')) {
    return PINNED_REVISION_MESSAGE;
  }
  return message;
}

function buildRevisionSelector(module: StrategyModuleSummary, revision: StrategyModuleRevision): string {
  if (revision.hash) {
    return `${module.name}@${revision.hash}`;
  }
  if (revision.tag) {
    return `${module.name}:${revision.tag}`;
  }
  return module.name;
}

export default function StrategyModulesPage() {
  const [modules, setModules] = useState<StrategyModuleSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [refreshing, setRefreshing] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [formMode, setFormMode] = useState<ModuleFormMode>('create');
  const [formData, setFormData] = useState(defaultFormState);
  const [formError, setFormError] = useState<string | null>(null);
  const [formProcessing, setFormProcessing] = useState(false);
  const [formPrefillLoading, setFormPrefillLoading] = useState(false);
  const [formTarget, setFormTarget] = useState<StrategyModuleSummary | null>(null);
  const [detailModule, setDetailModule] = useState<StrategyModuleSummary | null>(null);
  const [sourceModule, setSourceModule] = useState<StrategyModuleSummary | null>(null);
  const [sourceContent, setSourceContent] = useState('');
  const [sourceLoading, setSourceLoading] = useState(false);
  const [sourceError, setSourceError] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<StrategyModuleSummary | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [revisionToDelete, setRevisionToDelete] = useState<{
    module: StrategyModuleSummary;
    revision: StrategyModuleRevision;
  } | null>(null);
  const [revisionActionBusy, setRevisionActionBusy] = useState<string | null>(null);
  const [aliasDialogTarget, setAliasDialogTarget] = useState<{
    module: StrategyModuleSummary;
    revision: StrategyModuleRevision;
  } | null>(null);
  const [aliasValue, setAliasValue] = useState('');
  const [aliasPromoteLatest, setAliasPromoteLatest] = useState(false);
  const [aliasError, setAliasError] = useState<string | null>(null);
  const [aliasProcessing, setAliasProcessing] = useState(false);
  const [promoteTarget, setPromoteTarget] = useState<{
    module: StrategyModuleSummary;
    revision: StrategyModuleRevision;
  } | null>(null);

  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const { show: showToast } = useToast();

  const sortedModules = useMemo(
    () => [...modules].sort((a, b) => a.name.localeCompare(b.name)),
    [modules],
  );

  const strategyDirectory = useMemo(() => {
    const candidate = modules.find((module) => module.path);
    return directoryFromPath(candidate?.path ?? undefined);
  }, [modules]);

  const loadModules = useCallback(
    async ({ silent = false }: LoadOptions = {}) => {
      if (!silent) {
        setLoading(true);
        setError(null);
      }
      try {
        const response = await apiClient.getStrategyModules();
        const entries = Array.isArray(response.modules) ? response.modules : [];
        setModules(entries);
        if (!silent) {
          setError(null);
        }
      } catch (err) {
        const message =
          err instanceof Error ? err.message : 'Failed to load strategy modules';
        if (silent) {
          showToast({
            title: 'Reload failed',
            description: message,
            variant: 'destructive',
          });
        } else {
          setError(message);
        }
      } finally {
        if (!silent) {
          setLoading(false);
        }
      }
    },
    [showToast],
  );

  useEffect(() => {
    void loadModules();
  }, [loadModules]);

  const refreshCatalog = useCallback(
    async ({ silent = false, notifySuccess = !silent }: RefreshOptions = {}) => {
      if (!silent) {
        setRefreshing(true);
      }
      try {
        const result = await apiClient.refreshStrategies();
        await loadModules({ silent });
        if (notifySuccess) {
          showToast({
            title: 'Strategy catalog refreshed',
            description:
              result.status?.toLowerCase() === 'refreshed'
                ? 'Latest JavaScript modules loaded into the runtime.'
                : 'Strategy runtime acknowledged refresh command.',
            variant: 'success',
          });
        }
        return result;
      } catch (err) {
        const message =
          err instanceof Error ? err.message : 'Failed to refresh strategy modules';
        if (!silent) {
          showToast({
            title: 'Refresh failed',
            description: message,
            variant: 'destructive',
          });
        }
        throw err;
      } finally {
        if (!silent) {
          setRefreshing(false);
        }
      }
    },
    [loadModules, showToast],
  );

  const openCreateDialog = () => {
    setFormMode('create');
    setFormTarget(null);
    setFormData(defaultFormState);
    setFormError(null);
    setFormPrefillLoading(false);
    setFormProcessing(false);
    setFormOpen(true);
  };

  const openEditDialog = async (module: StrategyModuleSummary) => {
    setFormMode('edit');
    setFormTarget(module);
    setFormError(null);
    setFormProcessing(false);
    setFormPrefillLoading(true);
    const aliasKeys = Object.keys(module.tagAliases ?? {}).filter((tag) => {
      if (tag === 'latest') {
        return false;
      }
      if (module.version && tag === module.version) {
        return false;
      }
      return true;
    });
    setFormData({
      name: module.name,
      filename: module.file,
      tag: module.version ?? '',
      aliases: aliasKeys.join(', '),
      source: '',
      promoteLatest:
        (module.tagAliases?.latest ?? module.hash) === module.hash || !module.tagAliases?.latest,
    });
    setFormOpen(true);
    try {
      const identifier = module.file || module.name;
      const source = await apiClient.getStrategyModuleSource(identifier);
      setFormData((prev) => ({
        ...prev,
        source,
      }));
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to load strategy source';
      setFormError(message);
      showToast({
        title: 'Load failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setFormPrefillLoading(false);
    }
  };

  const handleFilePickerClick = () => {
    fileInputRef.current?.click();
  };

  const handleFileSelected = async (event: ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0];
    if (!file) {
      return;
    }
    try {
      const text = await file.text();
      setFormData((prev) => ({
        ...prev,
        filename: prev.filename || file.name,
        source: text,
      }));
      setFormError(null);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to read selected file';
      setFormError(message);
    } finally {
      // reset input so same file can be reselected
      event.target.value = '';
    }
  };

  const validateForm = () => {
    const trimmedName = formData.name.trim();
    if (!trimmedName) {
      setFormError('Strategy name is required.');
      return false;
    }
    const trimmedFilename = formData.filename.trim();
    if (trimmedFilename) {
      const lower = trimmedFilename.toLowerCase();
      if (!lower.endsWith('.js') && !lower.endsWith('.mjs')) {
        setFormError(`Filename must end with ${FILE_EXTENSION_HINT}.`);
        return false;
      }
    }
    if (!formData.source || formData.source.trim().length === 0) {
      setFormError('Strategy source code cannot be empty.');
      return false;
    }
    setFormError(null);
    return true;
  };

  const handleFormSubmit = async () => {
    if (!validateForm()) {
      return;
    }
    const trimmedName = formData.name.trim();
    const trimmedFilename = formData.filename.trim();
    const trimmedTag = formData.tag.trim();
    const aliases = formData.aliases
      .split(',')
      .map((alias) => alias.trim())
      .filter((alias) => alias.length > 0);
    const payload = {
      source: formData.source,
      promoteLatest: formData.promoteLatest,
      ...(trimmedName ? { name: trimmedName } : {}),
      ...(trimmedFilename ? { filename: trimmedFilename } : {}),
      ...(trimmedTag ? { tag: trimmedTag } : {}),
      ...(aliases.length > 0 ? { aliases } : {}),
    };
    setFormProcessing(true);
    try {
      if (formMode === 'create') {
        const response = await apiClient.createStrategyModule(payload);
        const identifier = response.module?.name ?? trimmedName ?? response.filename ?? 'module';
        showToast({
          title: 'Strategy module saved',
          description: `Saved ${identifier}. Refreshing catalog…`,
          variant: 'success',
        });
      } else if (formTarget) {
        const targetIdentifier = formTarget.file || formTarget.name;
        const response = await apiClient.updateStrategyModule(targetIdentifier, payload);
        const identifier = response.module?.name ?? trimmedName ?? response.filename ?? targetIdentifier;
        showToast({
          title: 'Strategy module updated',
          description: `Updated ${identifier}. Refreshing catalog…`,
          variant: 'success',
        });
      }
      await refreshCatalog({ silent: true, notifySuccess: false });
      showToast({
        title: 'Runtime refreshed',
        description: 'JavaScript strategy catalog now reflects the latest source.',
        variant: 'success',
      });
      setFormOpen(false);
      setFormTarget(null);
      setFormData(defaultFormState);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to save strategy module';
      setFormError(message);
      showToast({
        title: 'Save failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setFormProcessing(false);
    }
  };

  const handleDelete = async () => {
    if (!deleteTarget) {
      return;
    }
    const identifier = deleteTarget.file || deleteTarget.name;
    setDeleting(true);
    try {
      await apiClient.deleteStrategyModule(identifier);
      showToast({
        title: 'Module removed',
        description: `${deleteTarget.name} deleted successfully.`,
        variant: 'success',
      });
      await refreshCatalog({ silent: true, notifySuccess: false });
      setDeleteTarget(null);
    } catch (err) {
      const messageRaw =
        err instanceof Error ? err.message : 'Failed to delete strategy module';
      const message = friendlyDeletionMessage(messageRaw);
      showToast({
        title: 'Delete failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setDeleting(false);
    }
  };

  const openSourceDialog = async (module: StrategyModuleSummary) => {
    setSourceModule(module);
    setSourceContent('');
    setSourceError(null);
    setSourceLoading(true);
    try {
      const source = await apiClient.getStrategyModuleSource(module.file || module.name);
      setSourceContent(source);
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to load strategy source';
      setSourceError(message);
      showToast({
        title: 'Load failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setSourceLoading(false);
    }
  };

  const copyHash = async (hash: string) => {
    try {
      if (typeof navigator === 'undefined' || !navigator.clipboard) {
        throw new Error('Clipboard API unavailable in this environment');
      }
      await navigator.clipboard.writeText(hash);
      showToast({
        title: 'Hash copied',
        description: 'SHA-256 hash copied to clipboard.',
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to copy hash';
      showToast({
        title: 'Copy failed',
        description: message,
        variant: 'destructive',
      });
    }
  };

  const copySource = async () => {
    if (!sourceContent) {
      return;
    }
    try {
      if (typeof navigator === 'undefined' || !navigator.clipboard) {
        throw new Error('Clipboard API unavailable in this environment');
      }
      await navigator.clipboard.writeText(sourceContent);
      showToast({
        title: 'Source copied',
        description: 'Strategy JavaScript copied to clipboard.',
        variant: 'success',
      });
    } catch (err) {
      const message =
        err instanceof Error ? err.message : 'Failed to copy strategy source';
      showToast({
        title: 'Copy failed',
        description: message,
        variant: 'destructive',
      });
    }
  };

  const revisionKey = (
    module: StrategyModuleSummary,
    revision: StrategyModuleRevision,
    action: string,
  ) => `${module.name}:${revision.hash || revision.tag || 'latest'}:${action}`;

  const revisionLabel = (revision: StrategyModuleRevision) => {
    if (revision.tag) {
      return revision.tag;
    }
    if (revision.version) {
      return revision.version;
    }
    if (revision.hash) {
      return `${revision.hash.slice(0, 12)}…`;
    }
    return 'revision';
  };

  const handlePromoteRevision = async (
    module: StrategyModuleSummary,
    revision: StrategyModuleRevision,
  ) => {
    const key = revisionKey(module, revision, 'promote');
    setRevisionActionBusy(key);
    try {
      const selector = buildRevisionSelector(module, revision);
      const source = await apiClient.getStrategyModuleSource(selector);
      const payload = {
        source,
        name: module.name,
        promoteLatest: true,
        ...(revision.tag ? { tag: revision.tag } : {}),
      };
      await apiClient.updateStrategyModule(selector, payload);
      await refreshCatalog({ silent: true, notifySuccess: false });
      const description = `Revision ${revisionLabel(revision)} promoted to latest.`;
      showToast({
        title: 'Tag promoted',
        description,
        variant: 'success',
      });
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to promote revision';
      showToast({
        title: 'Promotion failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setRevisionActionBusy(null);
      setPromoteTarget(null);
    }
  };

  const handleDeleteRevision = async (
    module: StrategyModuleSummary,
    revision: StrategyModuleRevision,
  ) => {
    const key = revisionKey(module, revision, 'delete');
    setRevisionActionBusy(key);
    try {
      const selector = buildRevisionSelector(module, revision);
      await apiClient.deleteStrategyModule(selector);
      await refreshCatalog({ silent: true, notifySuccess: false });
      showToast({
        title: 'Revision removed',
        description: `${module.name} revision ${revisionLabel(revision)} deleted.`,
        variant: 'success',
      });
    } catch (err) {
      const messageRaw =
        err instanceof Error ? err.message : 'Failed to delete revision';
      const message = friendlyDeletionMessage(messageRaw);
      showToast({
        title: 'Deletion failed',
        description: message,
        variant: 'destructive',
      });
    } finally {
      setRevisionActionBusy(null);
      setRevisionToDelete(null);
    }
  };

  const handleAliasSubmit = async () => {
    if (!aliasDialogTarget) {
      return;
    }
    const alias = aliasValue.trim();
    if (!alias) {
      setAliasError('Alias name is required.');
      return;
    }
    if (alias.toLowerCase() === 'latest') {
      setAliasError('Use “Promote latest” to update the latest alias.');
      return;
    }
    setAliasProcessing(true);
    setAliasError(null);
    const { module, revision } = aliasDialogTarget;
    try {
      const selector = buildRevisionSelector(module, revision);
      const source = await apiClient.getStrategyModuleSource(selector);
      const payload = {
        source,
        name: module.name,
        aliases: [alias],
        promoteLatest: aliasPromoteLatest,
        ...(revision.tag ? { tag: revision.tag } : {}),
      };
      await apiClient.updateStrategyModule(selector, payload);
      await refreshCatalog({ silent: true, notifySuccess: false });
      showToast({
        title: 'Alias added',
        description: `Alias ${alias} now points to ${revisionLabel(revision)}.`,
        variant: 'success',
      });
      setAliasDialogTarget(null);
      setAliasValue('');
      setAliasPromoteLatest(false);
      setAliasError(null);
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Failed to add alias';
      setAliasError(message);
    } finally {
      setAliasProcessing(false);
    }
  };

  const moduleCount = modules.length;

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-4 lg:flex-row lg:items-center lg:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight">Strategy Modules</h1>
          <p className="text-muted-foreground">
            Upload, edit, and refresh JavaScript trading strategies available to the runtime.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button onClick={openCreateDialog} variant="default">
            <UploadCloud className="mr-2 h-4 w-4" />
            New module
          </Button>
          <Button
            onClick={() => void refreshCatalog()}
            variant="outline"
            disabled={refreshing}
          >
            {refreshing ? (
              <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="mr-2 h-4 w-4" />
            )}
            Refresh catalog
          </Button>
        </div>
      </div>

      {strategyDirectory ? (
        <Alert>
          <AlertTitle>Strategy directory</AlertTitle>
          <AlertDescription className="mt-1 text-xs sm:text-sm">
            Sources are persisted under <span className="font-mono">{strategyDirectory}</span>. Uploading or editing
            modules will write to this location before triggering a runtime refresh.
          </AlertDescription>
        </Alert>
      ) : null}

      {error ? (
        <Alert variant="destructive">
          <AlertTitle>Unable to load strategy modules</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {loading ? (
        <Card>
          <CardContent className="py-12 text-center text-muted-foreground">
            Loading strategy modules…
          </CardContent>
        </Card>
      ) : moduleCount === 0 ? (
        <Card>
          <CardHeader>
            <CardTitle>No JavaScript strategies detected</CardTitle>
            <CardDescription>
              Upload a JavaScript module to bootstrap the runtime catalog.
            </CardDescription>
          </CardHeader>
          <CardContent>
            <Button onClick={openCreateDialog}>
              <UploadCloud className="mr-2 h-4 w-4" />
              Upload your first module
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <CardTitle>Loaded modules</CardTitle>
              <CardDescription>{moduleCount} module{moduleCount === 1 ? '' : 's'} loaded in runtime.</CardDescription>
            </div>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Display Name</TableHead>
                  <TableHead>Version</TableHead>
                  <TableHead>Aliases</TableHead>
                  <TableHead>Latest hash</TableHead>
                  <TableHead>Size</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {sortedModules.map((module) => (
                  <TableRow key={module.name}>
                    <TableCell>
                      <span className="font-mono text-xs sm:text-sm">{module.name}</span>
                    </TableCell>
                    <TableCell>{module.metadata.displayName || '—'}</TableCell>
                    <TableCell>
                      {module.version ? (
                        <Badge variant="outline">{module.version}</Badge>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="flex flex-wrap items-center gap-1">
                        {Object.entries(module.tagAliases ?? {})
                          .filter(([tag]) => tag !== 'latest')
                          .map(([tag]) => (
                            <Badge key={tag} variant="secondary" className="text-xs">
                              {tag}
                            </Badge>
                          ))}
                        {Object.keys(module.tagAliases ?? {}).filter((tag) => tag !== 'latest')
                          .length === 0 ? (
                            <span className="text-xs text-muted-foreground">No aliases</span>
                          ) : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="space-y-1 text-xs">
                        <div className="flex items-center gap-2">
                          <Badge variant="outline">latest</Badge>
                          <button
                            type="button"
                            className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground"
                            onClick={() => copyHash(module.tagAliases?.latest ?? module.hash)}
                          >
                            <span className="font-mono">
                              {(module.tagAliases?.latest ?? module.hash).slice(0, 12)}…
                            </span>
                            <Copy className="h-3 w-3" />
                          </button>
                        </div>
                        <div className="flex items-center gap-2">
                          <Badge variant="outline">pinned</Badge>
                          <button
                            type="button"
                            className="inline-flex items-center gap-1 text-muted-foreground hover:text-foreground"
                            onClick={() => copyHash(module.hash)}
                          >
                            <span className="font-mono">{module.hash.slice(0, 12)}…</span>
                            <Copy className="h-3 w-3" />
                          </button>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>{formatBytes(module.size)}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => setDetailModule(module)}
                          title="View metadata"
                        >
                          <Eye className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => void openSourceDialog(module)}
                          title="View source"
                        >
                          <FileCode className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => void openEditDialog(module)}
                          title="Edit source"
                        >
                          <Pencil className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => setDeleteTarget(module)}
                          title="Delete module"
                        >
                          <Trash2 className="h-4 w-4 text-destructive" />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Dialog
        open={formOpen}
        onOpenChange={(open) => {
          setFormOpen(open);
          if (!open) {
            setFormTarget(null);
            setFormError(null);
            setFormProcessing(false);
            setFormPrefillLoading(false);
            setFormData(defaultFormState);
          }
        }}
      >
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>
              {formMode === 'create' ? 'Upload strategy module' : `Edit ${formTarget?.name ?? ''}`}
            </DialogTitle>
            <DialogDescription>
              {formMode === 'create'
                ? 'Provide a JavaScript file that exports strategy metadata and factory functions.'
                : 'Update the JavaScript source for this strategy module.'}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="grid gap-2">
              <Label htmlFor="strategy-name">Strategy name</Label>
              <Input
                id="strategy-name"
                placeholder="grid"
                value={formData.name}
                disabled={formMode === 'edit'}
                onChange={(event) =>
                  setFormData((prev) => ({ ...prev, name: event.target.value }))
                }
              />
              <p className="text-xs text-muted-foreground">
                Provide the canonical strategy identifier. This cannot be changed after creation.
              </p>
            </div>
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="strategy-tag">Tag (optional)</Label>
                <Input
                  id="strategy-tag"
                  placeholder="v1.2.0"
                  value={formData.tag}
                  onChange={(event) =>
                    setFormData((prev) => ({ ...prev, tag: event.target.value }))
                  }
                  disabled={formProcessing}
                />
                <p className="text-xs text-muted-foreground">
                  Supply a semantic version or release tag for this revision.
                </p>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="strategy-aliases">Aliases</Label>
                <Input
                  id="strategy-aliases"
                  placeholder="stable, canary"
                  value={formData.aliases}
                  onChange={(event) =>
                    setFormData((prev) => ({ ...prev, aliases: event.target.value }))
                  }
                  disabled={formProcessing}
                />
                <p className="text-xs text-muted-foreground">
                  Comma-separated alias tags that should resolve to this revision.
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Checkbox
                id="promote-latest"
                checked={formData.promoteLatest}
                disabled={formProcessing}
                onChange={(event) =>
                  setFormData((prev) => ({ ...prev, promoteLatest: event.target.checked }))
                }
              />
              <Label htmlFor="promote-latest" className="text-sm font-normal">
                Promote this revision to the <span className="font-semibold">latest</span> tag after save
              </Label>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="strategy-filename">Filename (optional)</Label>
              <Input
                id="strategy-filename"
                placeholder={`example${FILE_EXTENSION_HINT}`}
                value={formData.filename}
                disabled={formMode === 'edit'}
                onChange={(event) =>
                  setFormData((prev) => ({ ...prev, filename: event.target.value }))
                }
              />
              <p className="text-xs text-muted-foreground">
                Leave blank to derive a versioned filename from the strategy name and tag. Manual filenames must end with {FILE_EXTENSION_HINT}.
              </p>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="strategy-source">Source</Label>
              <Textarea
                id="strategy-source"
                className="font-mono text-sm h-[50vh] min-h-[280px]"
                value={formData.source}
                onChange={(event) =>
                  setFormData((prev) => ({ ...prev, source: event.target.value }))
                }
                spellCheck={false}
                disabled={formPrefillLoading || formProcessing}
              />
              <div className="flex flex-wrap items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleFilePickerClick}
                  disabled={formProcessing}
                >
                  <UploadCloud className="mr-2 h-4 w-4" />
                  Load from file
                </Button>
                <input
                  type="file"
                  accept=".js,.mjs,application/javascript"
                  ref={fileInputRef}
                  className="hidden"
                  onChange={handleFileSelected}
                />
                {formPrefillLoading ? (
                  <span className="flex items-center text-xs text-muted-foreground">
                    <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                    Loading current source…
                  </span>
                ) : null}
              </div>
            </div>
            {formError ? (
              <Alert variant="destructive">
                <AlertDescription>{formError}</AlertDescription>
              </Alert>
            ) : null}
          </div>
          <DialogFooter className="gap-2 sm:gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => setFormOpen(false)}
              disabled={formProcessing}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => void handleFormSubmit()}
              disabled={formProcessing || formPrefillLoading}
            >
              {formProcessing ? 'Saving…' : formMode === 'create' ? 'Save & refresh' : 'Update & refresh'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(detailModule)}
        onOpenChange={(open) => {
          if (!open) {
            setDetailModule(null);
            setRevisionToDelete(null);
            setPromoteTarget(null);
            setAliasDialogTarget(null);
            setAliasValue('');
            setAliasError(null);
            setAliasPromoteLatest(false);
            setAliasProcessing(false);
          }
        }}
      >
        <DialogContent className="max-w-3xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{detailModule?.metadata.displayName ?? detailModule?.name ?? 'Strategy'}</DialogTitle>
            <DialogDescription>
              Runtime metadata exported by the JavaScript module.
            </DialogDescription>
          </DialogHeader>
          {detailModule ? (
            <div className="space-y-6">
              <div>
                <h4 className="text-sm font-semibold">Identifiers</h4>
                <div className="mt-2 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                  <div>
                    <p className="text-xs text-muted-foreground uppercase">Strategy name</p>
                    <p className="font-mono text-sm">{detailModule.name}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground uppercase">Module file</p>
                    <p className="font-mono text-sm">{detailModule.file}</p>
                  </div>
                  <div>
                    <p className="text-xs text-muted-foreground uppercase">Version</p>
                    <p className="font-mono text-sm">{detailModule.version || '—'}</p>
                  </div>
                </div>
              </div>
              <div>
                <h4 className="text-sm font-semibold">Hash & size</h4>
                <div className="mt-2 flex flex-wrap items-center gap-4">
                  <button
                    type="button"
                    className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                    onClick={() => copyHash(detailModule.hash)}
                  >
                    <span className="font-mono">{detailModule.hash}</span>
                    <Copy className="h-3 w-3" />
                  </button>
                  <span className="text-xs text-muted-foreground">
                    {formatBytes(detailModule.size)}
                  </span>
                </div>
              </div>
              <div>
                <h4 className="text-sm font-semibold">Tag aliases</h4>
                <div className="mt-2 space-y-2">
                  {Object.entries(detailModule.tagAliases ?? {}).length === 0 ? (
                    <p className="text-sm text-muted-foreground">No tag aliases defined.</p>
                  ) : (
                    Object.entries(detailModule.tagAliases ?? {}).map(([tag, hash]) => (
                      <div
                        key={tag}
                        className="flex flex-wrap items-center justify-between gap-2 rounded-md border p-2 text-xs"
                      >
                        <div className="flex items-center gap-2">
                          <Badge variant={tag === 'latest' ? 'default' : 'secondary'}>{tag}</Badge>
                          <span className="font-mono">{hash}</span>
                        </div>
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          className="h-7 px-2"
                          onClick={() => copyHash(hash)}
                        >
                          <Copy className="mr-1 h-3 w-3" /> Copy
                        </Button>
                      </div>
                    ))
                  )}
                </div>
              </div>
              <div>
                <h4 className="text-sm font-semibold">Revision history</h4>
                {detailModule.revisions && detailModule.revisions.length > 0 ? (
                  <div className="mt-2 overflow-x-auto rounded-md border">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Tag / version</TableHead>
                          <TableHead>Hash</TableHead>
                          <TableHead>Size</TableHead>
                          <TableHead className="text-right">Actions</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {detailModule.revisions.map((revision) => {
                          const promoteBusy =
                            revisionActionBusy === revisionKey(detailModule, revision, 'promote');
                          const deleteBusy =
                            revisionActionBusy === revisionKey(detailModule, revision, 'delete');
                          return (
                            <TableRow key={`${revision.hash}-${revision.tag ?? 'untagged'}`}>
                              <TableCell>
                                <div className="flex flex-col gap-1">
                                  <div className="flex flex-wrap items-center gap-2">
                                    {revision.tag ? (
                                      <Badge variant="secondary">{revision.tag}</Badge>
                                    ) : (
                                      <span className="text-xs text-muted-foreground">—</span>
                                    )}
                                    {revision.version && revision.version !== revision.tag ? (
                                      <Badge variant="outline">{revision.version}</Badge>
                                    ) : null}
                                  </div>
                                  <p className="text-xs text-muted-foreground">
                                    {revision.path || '—'}
                                  </p>
                                </div>
                              </TableCell>
                              <TableCell>
                                <button
                                  type="button"
                                  className="inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                                  onClick={() => copyHash(revision.hash)}
                                >
                                  <span className="font-mono">{revision.hash.slice(0, 18)}…</span>
                                  <Copy className="h-3 w-3" />
                                </button>
                              </TableCell>
                              <TableCell>{formatBytes(revision.size)}</TableCell>
                              <TableCell>
                                <div className="flex justify-end gap-2">
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="sm"
                                    onClick={() =>
                                      setPromoteTarget({ module: detailModule, revision })
                                    }
                                    disabled={promoteBusy || deleteBusy}
                                    className="h-8 px-2"
                                  >
                                    {promoteBusy ? (
                                      <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                                    ) : (
                                      <ArrowUpCircle className="mr-1 h-3 w-3" />
                                    )}
                                    Promote
                                  </Button>
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="sm"
                                    className="h-8 px-2"
                                    onClick={() => {
                                      setAliasDialogTarget({ module: detailModule, revision });
                                      setAliasValue('');
                                      setAliasPromoteLatest(false);
                                      setAliasError(null);
                                    }}
                                    disabled={deleteBusy}
                                  >
                                    <Tag className="mr-1 h-3 w-3" /> Alias
                                  </Button>
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="sm"
                                    className="h-8 px-2 text-destructive"
                                    onClick={() => setRevisionToDelete({ module: detailModule, revision })}
                                    disabled={deleteBusy || promoteBusy}
                                  >
                                    {deleteBusy ? (
                                      <Loader2 className="mr-1 h-3 w-3 animate-spin" />
                                    ) : (
                                      <Trash2 className="mr-1 h-3 w-3" />
                                    )}
                                    Delete
                                  </Button>
                                </div>
                              </TableCell>
                            </TableRow>
                          );
                        })}
                      </TableBody>
                    </Table>
                  </div>
                ) : (
                  <p className="mt-2 text-sm text-muted-foreground">
                    No revision history available for this strategy yet.
                  </p>
                )}
              </div>
              {detailModule.metadata.description ? (
                <div>
                  <h4 className="text-sm font-semibold">Description</h4>
                  <p className="mt-2 text-sm text-muted-foreground">
                    {detailModule.metadata.description}
                  </p>
                </div>
              ) : null}
              <div>
                <h4 className="text-sm font-semibold">Events</h4>
                <div className="mt-2 flex flex-wrap gap-2">
                  {detailModule.metadata.events.map((event) => (
                    <Badge key={event} variant="secondary">
                      {event}
                    </Badge>
                  ))}
                  {detailModule.metadata.events.length === 0 ? (
                    <span className="text-sm text-muted-foreground">None declared</span>
                  ) : null}
                </div>
              </div>
              <div>
                <h4 className="text-sm font-semibold">Configuration fields</h4>
                <div className="mt-2 space-y-3">
                  {detailModule.metadata.config.length === 0 ? (
                    <p className="text-sm text-muted-foreground">No configurable fields exported.</p>
                  ) : (
                    detailModule.metadata.config.map((field) => (
                      <div key={field.name} className="rounded-md border p-3">
                        <div className="flex items-center justify-between">
                          <span className="font-mono text-sm">{field.name}</span>
                          <Badge variant="outline">{field.type}</Badge>
                        </div>
                        {field.description ? (
                          <p className="mt-1 text-sm text-muted-foreground">{field.description}</p>
                        ) : null}
                        <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-muted-foreground">
                          <span>{field.required ? 'Required' : 'Optional'}</span>
                          {field.default !== undefined ? (
                            <>
                              <Separator orientation="vertical" className="h-4" />
                              <span>
                                Default:{' '}
                                <span className="font-mono">
                                  {typeof field.default === 'string'
                                    ? field.default
                                    : JSON.stringify(field.default)}
                                </span>
                              </span>
                            </>
                          ) : null}
                        </div>
                      </div>
                    ))
                  )}
                </div>
              </div>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(sourceModule)}
        onOpenChange={(open) => {
          if (!open) {
            setSourceModule(null);
            setSourceContent('');
            setSourceError(null);
          }
        }}
      >
        <DialogContent className="max-w-4xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>
              {sourceModule ? `Source: ${sourceModule.file || sourceModule.name}` : 'Source'}
            </DialogTitle>
            <DialogDescription>Read-only view of the on-disk JavaScript module.</DialogDescription>
          </DialogHeader>
          {sourceLoading ? (
            <div className="flex items-center gap-2 py-8 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              Loading strategy source…
            </div>
          ) : sourceError ? (
            <Alert variant="destructive">
              <AlertDescription>{sourceError}</AlertDescription>
            </Alert>
          ) : (
            <Textarea
              value={sourceContent}
              readOnly
              spellCheck={false}
              className="font-mono text-sm h-[55vh] min-h-[300px]"
            />
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => copySource()}
              disabled={!sourceContent}
            >
              <Copy className="mr-2 h-4 w-4" />
              Copy source
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={Boolean(deleteTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setDeleteTarget(null);
          }
        }}
        title="Delete strategy module"
        description={
          <span>
            Are you sure you want to delete{' '}
            <span className="font-semibold">{deleteTarget?.name}</span>? This action removes the JavaScript file from disk.
          </span>
        }
        confirmLabel="Delete"
        confirmVariant="destructive"
        loading={deleting}
        onConfirm={() => void handleDelete()}
      />
      <ConfirmDialog
        open={Boolean(revisionToDelete)}
        onOpenChange={(open) => {
          if (!open) {
            setRevisionToDelete(null);
          }
        }}
        title="Delete revision"
        description={
          revisionToDelete ? (
            <span>
              Are you sure you want to delete revision{' '}
              <span className="font-semibold">
                {revisionLabel(revisionToDelete.revision)}
              </span>{' '}
              for <span className="font-semibold">{revisionToDelete.module.name}</span>?
            </span>
          ) : undefined
        }
        confirmLabel="Delete"
        confirmVariant="destructive"
        loading={Boolean(
          revisionToDelete &&
            revisionActionBusy ===
              revisionKey(revisionToDelete.module, revisionToDelete.revision, 'delete'),
        )}
        onConfirm={() =>
          revisionToDelete
            ? void handleDeleteRevision(revisionToDelete.module, revisionToDelete.revision)
            : undefined
        }
      />
      <ConfirmDialog
        open={Boolean(promoteTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setPromoteTarget(null);
          }
        }}
        title="Promote revision to latest"
        description={
          promoteTarget ? (
            <span>
              Move the <span className="font-semibold">latest</span> tag to revision{' '}
              <span className="font-semibold">{revisionLabel(promoteTarget.revision)}</span>?
            </span>
          ) : undefined
        }
        confirmLabel="Promote"
        confirmVariant="default"
        loading={Boolean(
          promoteTarget &&
            revisionActionBusy ===
              revisionKey(promoteTarget.module, promoteTarget.revision, 'promote'),
        )}
        onConfirm={() =>
          promoteTarget
            ? void handlePromoteRevision(promoteTarget.module, promoteTarget.revision)
            : undefined
        }
      />
      <Dialog
        open={Boolean(aliasDialogTarget)}
        onOpenChange={(open) => {
          if (!open) {
            setAliasDialogTarget(null);
            setAliasValue('');
            setAliasPromoteLatest(false);
            setAliasError(null);
            setAliasProcessing(false);
          }
        }}
      >
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>Add alias</DialogTitle>
            <DialogDescription>
              Point an additional tag to revision{' '}
              {aliasDialogTarget ? revisionLabel(aliasDialogTarget.revision) : ''}.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid gap-2">
              <Label htmlFor="revision-alias">Alias name</Label>
              <Input
                id="revision-alias"
                value={aliasValue}
                onChange={(event) => {
                  setAliasValue(event.target.value);
                  setAliasError(null);
                }}
                placeholder="stable"
                disabled={aliasProcessing}
              />
            </div>
            <div className="flex items-center gap-2">
              <Checkbox
                id="alias-promote-latest"
                checked={aliasPromoteLatest}
                onChange={(event) => setAliasPromoteLatest(event.target.checked)}
                disabled={aliasProcessing}
              />
              <Label htmlFor="alias-promote-latest" className="text-sm font-normal">
                Promote to <span className="font-semibold">latest</span> after assigning this alias
              </Label>
            </div>
            {aliasError ? (
              <p className="text-sm text-destructive">{aliasError}</p>
            ) : null}
          </div>
          <DialogFooter className="gap-2 sm:gap-2">
            <Button
              type="button"
              variant="outline"
              onClick={() => {
                setAliasDialogTarget(null);
                setAliasValue('');
                setAliasPromoteLatest(false);
                setAliasError(null);
              }}
              disabled={aliasProcessing}
            >
              Cancel
            </Button>
            <Button
              type="button"
              onClick={() => void handleAliasSubmit()}
              disabled={aliasProcessing}
            >
              {aliasProcessing ? 'Saving…' : 'Save alias'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
