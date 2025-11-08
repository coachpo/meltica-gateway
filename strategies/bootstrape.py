#!/usr/bin/env python3

from __future__ import annotations

import hashlib
import json
import subprocess
import sys
import textwrap
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Dict, List, Optional, Tuple


DEFAULT_ROOT = "."
DEFAULT_VERSION = "v1.0.0"
DEFAULT_USAGE_OUTPUT = "usage.json"
NODE_METADATA_SCRIPT = textwrap.dedent(
    """
    const path = require("path");
    const fs = require("fs");

    function fatal(msg) {
      console.error(`bootstrap: ${msg}`);
      process.exit(1);
    }

    if (process.argv.length < 2) {
      fatal("missing module argument");
    }

    const target = path.resolve(process.argv[1]);
    if (!fs.existsSync(target)) {
      fatal(`module not found: ${target}`);
    }

    let exports;
    try {
      exports = require(target);
    } catch (err) {
      fatal(`failed to execute ${target}: ${err.message}`);
    }

    if (!exports || typeof exports !== "object" || !exports.metadata) {
      fatal(`metadata export missing in ${target}`);
    }

    try {
      console.log(JSON.stringify(exports.metadata));
    } catch (err) {
      fatal(`metadata serialization failed for ${target}: ${err.message}`);
    }
    """
)


@dataclass
class ModuleInfo:
    name: str
    version: str
    path: Path
    source: bytes
    metadata: Dict[str, object]
    sha256: str
    versioned: bool


class BootstrapError(RuntimeError):
    pass


def ensure_dir(path: str) -> Path:
    trimmed = path.strip()
    if not trimmed:
        raise BootstrapError("directory required")
    target = Path(trimmed).expanduser().resolve()
    target.mkdir(parents=True, exist_ok=True)
    return target


def discover_modules(root: Path) -> List[ModuleInfo]:
    modules: List[ModuleInfo] = []
    for js_file in sorted(root.rglob("*.js")):
        if js_file.name == "registry.json":
            continue
        modules.append(load_module(root, js_file))
    return modules


def load_module(root: Path, path: Path) -> ModuleInfo:
    try:
        source = path.read_bytes()
    except OSError as exc:
        raise BootstrapError(f"read {path}: {exc}") from exc

    metadata = extract_metadata(path)
    name = str(metadata.get("name", "")).strip().lower()
    if not name:
        raise BootstrapError(f"{path}: metadata.name required")
    version = str(metadata.get("version", "")).strip() or DEFAULT_VERSION

    sha = hashlib.sha256(source).hexdigest()
    rel = path.relative_to(root)
    versioned = is_versioned_path(rel, name, version)

    return ModuleInfo(
        name=name,
        version=version,
        path=path,
        source=source,
        metadata=metadata,
        sha256=f"sha256:{sha}",
        versioned=versioned,
    )


def is_versioned_path(relative_path: Path, name: str, version: str) -> bool:
    parts = relative_path.parts
    if len(parts) < 3:
        return False
    filename = parts[-1].lower()
    expected = f"{name}.js"
    if filename != expected:
        return False
    version_component = parts[-2]
    return version_component.lower() == version.lower()


def extract_metadata(path: Path) -> Dict[str, object]:
    try:
        result = subprocess.run(
            ["node", "-e", NODE_METADATA_SCRIPT, str(path.resolve())],
            capture_output=True,
            check=False,
            text=True,
        )
    except FileNotFoundError as exc:
        raise BootstrapError("node binary not found (required to load metadata exports)") from exc

    if result.returncode != 0:
        raise BootstrapError(result.stderr.strip() or f"metadata extraction failed for {path}")

    try:
        return json.loads(result.stdout)
    except json.JSONDecodeError as exc:
        raise BootstrapError(f"metadata export invalid for {path}: {exc}") from exc


def materialize_module(root: Path, module: ModuleInfo) -> Path:
    if module.versioned:
        return module.path
    target_dir = root / module.name / module.version
    target_dir.mkdir(parents=True, exist_ok=True)
    target_path = target_dir / f"{module.name}.js"
    try:
        target_path.write_bytes(module.source)
        module.path.unlink()
    except OSError as exc:
        raise BootstrapError(f"move module {module.path} -> {target_path}: {exc}") from exc
    return target_path


def pick_latest_tag(tags: Dict[str, str]) -> str:
    candidates = [tag for tag in tags.keys() if tag != "latest"]
    if not candidates:
        return next(iter(tags.values()), "")
    return tags[sorted(candidates)[-1]]


def build_registry(modules: List[ModuleInfo], root: Path, write: bool) -> Dict[str, Dict[str, object]]:
    registry: Dict[str, Dict[str, object]] = {}
    for module in modules:
        current = registry.setdefault(module.name, {"tags": {}, "hashes": {}})
        module_path = module.path
        if write:
            module_path = materialize_module(root, module)
        rel = module_path.relative_to(root)
        current["tags"][module.version] = module.sha256
        current["hashes"][module.sha256] = {
            "tag": module.version,
            "path": rel.as_posix(),
        }

    for entry in registry.values():
        if entry["tags"]:
            entry["tags"]["latest"] = pick_latest_tag(entry["tags"])
    return registry


def write_registry(root: Path, registry: Dict[str, Dict[str, object]]) -> None:
    tmp = root / "registry.json.tmp"
    target = root / "registry.json"
    try:
        tmp.write_text(json.dumps(registry, indent=2) + "\n", encoding="utf-8")
        tmp.replace(target)
    except OSError as exc:
        raise BootstrapError(f"write registry: {exc}") from exc


def read_usage(source: Optional[str], output_path: Optional[str]) -> Optional[List[dict]]:
    if not source:
        return None
    if source.startswith("http://") or source.startswith("https://"):
        try:
            with urllib.request.urlopen(source) as resp:
                payload = resp.read().decode("utf-8")
        except urllib.error.URLError as exc:
            raise BootstrapError(f"fetch usage from {source}: {exc}") from exc
        if output_path:
            Path(output_path).write_text(payload, encoding="utf-8")
        return extract_usage_from_payload(payload, source)
    path = Path(source)
    try:
        payload = path.read_text(encoding="utf-8")
    except OSError as exc:
        raise BootstrapError(f"read usage export {path}: {exc}") from exc
    return extract_usage_from_payload(payload, str(path))


def extract_usage_from_payload(payload: str, source: str) -> Optional[List[dict]]:
    try:
        data = json.loads(payload)
    except json.JSONDecodeError as exc:
        raise BootstrapError(f"usage export {source} invalid JSON: {exc}") from exc
    if isinstance(data, dict) and "usage" in data:
        usage = data.get("usage")
    else:
        usage = data
    if not isinstance(usage, list):
        raise BootstrapError(f"usage export {source} missing usage array")
    return usage


def report_usage(usage: List[dict], registry: Dict[str, Dict[str, object]]) -> None:
    usage_index: Dict[str, Dict[str, dict]] = {}
    for entry in usage:
        name = str(entry.get("strategy", "")).strip().lower()
        hash_value = str(entry.get("hash", "")).strip()
        if not name or not hash_value:
            continue
        usage_index.setdefault(name, {})[hash_value] = entry

    unused: List[Tuple[str, str, str]] = []
    for name, entry in registry.items():
        normalized = name.lower()
        for hash_value, location in entry.get("hashes", {}).items():
            usage_entry = usage_index.get(normalized, {}).get(hash_value)
            count = 0
            if usage_entry is not None:
                try:
                    count = int(usage_entry.get("count", 0))
                except (TypeError, ValueError):
                    count = 0
            if usage_entry is None or count == 0:
                tag = location.get("tag") or "(untagged)"
                unused.append((name, tag, hash_value))

    print()
    if not unused:
        print("usage report: no unused revisions detected (all tracked hashes have usage).")
        return

    print("usage report: revisions with zero running instances:")
    for name, tag, hash_value in sorted(unused):
        print(f"  - {name} {tag} [{hash_value}]")


def rebuild_registry_once(root: Path, write: bool) -> Dict[str, Dict[str, object]]:
    modules = discover_modules(root)
    if not modules:
        raise BootstrapError(f"no JavaScript strategies found under {root}")
    registry = build_registry(modules, root, write)
    write_registry(root, registry)
    print(f"registry.json generated for {len(registry)} strategies under {root}")
    if not write:
        print("filesystem left untouched (pass --write to reorganize)")
    return registry


def prompt_with_default(label: str, default: Optional[str] = None) -> str:
    suffix = f" [{default}]" if default else ""
    try:
        value = input(f"{label}{suffix}: ").strip()
    except EOFError:
        print()
        return default or ""
    return value or (default or "")


def prompt_bool(label: str, default: bool = False) -> bool:
    default_label = "Y/n" if default else "y/N"
    try:
        value = input(f"{label} ({default_label}): ").strip().lower()
    except EOFError:
        print()
        return default
    if not value:
        return default
    return value in {"y", "yes"}


def main() -> int:
    print("=== Onboard Strategies ===")
    root_input = prompt_with_default("Strategies root", DEFAULT_ROOT)
    root = ensure_dir(root_input or DEFAULT_ROOT)
    normalize = prompt_bool("Normalize layout before writing registry?", default=True)
    if not prompt_bool("Proceed with onboarding?", default=True):
        print("Aborted by operator.")
        return 0
    try:
        rebuild_registry_once(root, write=normalize)
    except BootstrapError as exc:
        print(f"bootstrap: {exc}")
        return 1
    if not normalize:
        print("(Layout left untouched; rerun and answer 'y' to reorganize if needed.)")
    print("Done.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
