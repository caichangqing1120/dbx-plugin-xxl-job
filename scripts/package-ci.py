#!/usr/bin/env python3
"""Build one unsigned candidate from a clean release tag, preserving real Go provenance."""
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tempfile
import tomllib
import zipfile


SDK = "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
TARGETS = ("darwin-arm64", "darwin-x64", "windows-x64", "windows-arm64", "linux-x64", "linux-arm64")


def require(condition, message):
    if not condition:
        raise ValueError(message)


def command(args, cwd=None, env=None):
    result = subprocess.run(args, cwd=cwd, env=env, text=True, capture_output=True, check=True)
    if result.stderr:
        print(result.stderr, end="", file=sys.stderr)
    return result.stdout.strip()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def json_bytes(value):
    return (json.dumps(value, ensure_ascii=True, indent=2, sort_keys=True) + "\n").encode()


def source_identity(root, tag, version):
    git = lambda *args: command(["git", "-C", str(root), *args])
    require(tag and tag == "v" + version, "RELEASE_TAG must match manifest version (v" + version + ")")
    require(Path(git("rev-parse", "--show-toplevel")).resolve() == root, "Expected plugin repository root")
    require(not git("status", "--porcelain", "--untracked-files=all"), "Dirty tracked or untracked source checkout")
    revision = git("rev-parse", "HEAD")
    require(git("rev-parse", "--verify", "refs/tags/" + tag + "^{commit}") == revision,
            "Release tag does not point to HEAD")
    require(not os.environ.get("GITHUB_SHA") or os.environ["GITHUB_SHA"] == revision,
            "GITHUB_SHA does not match tag checkout HEAD")
    return {"tag": tag, "revision": revision, "tree": git("rev-parse", "HEAD^{tree}")}


def verify_binary(binary, revision, target):
    info = command(["go", "version", "-m", str(binary)])
    settings, sdk = {}, {}
    dependency = None
    for line in info.splitlines():
        parts = line.strip().split("\t")
        if parts[0] == "build" and len(parts) == 2 and "=" in parts[1]:
            key, value = parts[1].split("=", 1)
            settings[key] = value
        elif parts[0] == "dep" and len(parts) >= 3:
            dependency = parts[1]
            if dependency == SDK:
                sdk = {"module": SDK, "version": parts[2]}
        elif parts[0] == "=>" and dependency == SDK and len(parts) >= 2:
            sdk["replacement"] = parts[1]
    goos, arch = target.split("-")
    expected = {"vcs": "git", "vcs.revision": revision, "vcs.modified": "false",
                "GOOS": goos, "GOARCH": "amd64" if arch == "x64" else arch,
                "CGO_ENABLED": "0", "-trimpath": "true"}
    for key, value in expected.items():
        require(settings.get(key) == value, "Unexpected or missing Go build setting " + key)
    require(sdk.get("replacement") == "./sdk", "SDK build info must reference checked-in ./sdk, not an absolute replacement")
    return settings, sdk, info.splitlines()[0].rsplit(": ", 1)[-1]


def resource_files(root, includes):
    files = {}
    for directory in includes:
        relative = PurePosixPath(directory)
        require(not relative.is_absolute() and ".." not in relative.parts, "Unsafe package include")
        base = root / directory
        require(base.is_dir() and not base.is_symlink(), "Missing or symlink package directory: " + directory)
        for path in sorted(base.rglob("*")):
            require(not path.is_symlink(), "Symlink package resource: " + str(path))
            if not path.is_dir():
                require(path.is_file(), "Non-regular package resource: " + str(path))
                files[path.relative_to(root).as_posix()] = path.read_bytes()
    require("ui/index.html" in files, "Build UI before CI packaging")
    return files


def build_package(root, target, tag):
    root = Path(root).resolve()
    manifest = json.loads((root / "manifest.json").read_text())
    require(target in TARGETS and not (manifest["id"].endswith(".apollo") and target == "windows-arm64"),
            "Unsupported DBX_PLUGIN_TARGET: " + target)
    source = source_identity(root, tag, manifest["version"])
    require(json.loads((root / "package.json").read_text())["version"] == manifest["version"], "Package version mismatch")
    config = tomllib.loads((root / "dbx-plugin.toml").read_text())
    backend = root / config["backend"]["directory"]
    require(backend.resolve() == root / "backend" and not backend.is_symlink(), "Expected checked-in backend directory")
    sdk_path = backend / "sdk"
    require(sdk_path.is_dir() and not sdk_path.is_symlink(), "Expected checked-in backend/sdk directory")
    sdk_tree = command(["git", "-C", str(root), "rev-parse", "HEAD:backend/sdk"])
    goos, arch = target.split("-")
    env = {**os.environ, "GOOS": goos, "GOARCH": "amd64" if arch == "x64" else arch,
           "CGO_ENABLED": "0", "GOAMD64": "v1", "GOARM64": "v8.0", "GOWORK": "off",
           "GOFLAGS": "", "GOTOOLCHAIN": "local"}
    module = json.loads(command(["go", "list", "-m", "-json", SDK], cwd=backend, env=env))
    replacement = module.get("Replace", {})
    require(replacement.get("Path") == "./sdk" and Path(replacement.get("Dir", "")).resolve() == sdk_path,
            "go.mod must use the checked-in ./sdk replacement")
    binary_name = config["backend"]["binary"] + (".exe" if goos == "windows" else "")
    require(PurePosixPath(binary_name).name == binary_name and "\\" not in binary_name, "Unsafe backend binary name")
    with tempfile.TemporaryDirectory(prefix="dbx-ci-build-") as temporary:
        binary = Path(temporary) / binary_name
        subprocess.run(["go", "build", "-trimpath", "-buildvcs=true", "-ldflags=-s -w", "-o", str(binary), "."],
                       cwd=backend, env=env, stdout=sys.stderr, stderr=sys.stderr, check=True)
        settings, sdk, compiler = verify_binary(binary, source["revision"], target)
        require(source_identity(root, tag, manifest["version"]) == source, "Source changed during build")
        files = resource_files(root, config["package"]["include"])
        executable = "bin/" + target + "/" + binary_name
        manifest["entrypoints"]["backend"]["executable"] = executable
        binary_data = binary.read_bytes()
    files[executable] = binary_data
    files["manifest.json"] = json_bytes(manifest)
    files["checksums.json"] = json_bytes({"algorithm": "sha256", "files": {
        name: digest(data) for name, data in sorted(files.items())}})
    archive_bytes = io.BytesIO()
    with zipfile.ZipFile(archive_bytes, "w", zipfile.ZIP_DEFLATED, compresslevel=9) as archive:
        for name, data in sorted(files.items()):
            info = zipfile.ZipInfo(name, date_time=(1980, 1, 1, 0, 0, 0))
            info.create_system = 3
            info.external_attr = (0o100755 if name == executable else 0o100644) << 16
            info.compress_type = zipfile.ZIP_DEFLATED
            archive.writestr(info, data, compresslevel=9)
    artifact = root / "dist" / f"{manifest['id']}-{manifest['version']}-{target}.dbxp"
    data = archive_bytes.getvalue()
    metadata = {"target": target, "url": artifact.name, "sha256": digest(data), "size": len(data)}
    provenance = {"schemaVersion": 1, "artifact": metadata, "source": source,
                  "buildSettings": settings, "goVersion": compiler,
                  "sdk": {**sdk, "source": "backend/sdk", "tree": sdk_tree,
                          "goModSHA256": digest((sdk_path / "go.mod").read_bytes())},
                  "backendSHA256": digest(binary_data), "unsigned": True}
    outputs = {artifact: data, artifact.with_suffix(".artifact.json"): json_bytes(metadata),
               artifact.with_suffix(".provenance.json"): json_bytes(provenance)}
    for path, content in outputs.items():
        require(not path.exists() or (path.is_file() and not path.is_symlink() and path.read_bytes() == content),
                "Refusing to overwrite differing existing artifact: " + path.name)
    artifact.parent.mkdir(parents=True, exist_ok=True)
    for path, content in outputs.items():
        if not path.exists():
            with path.open("xb") as output:
                output.write(content)
    return artifact


if __name__ == "__main__":
    try:
        output = build_package(Path(__file__).resolve().parents[1],
                               os.environ.get("DBX_PLUGIN_TARGET", ""), os.environ.get("RELEASE_TAG", ""))
        print("Verified unsigned candidate: " + str(output), file=sys.stderr)
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        print("CI packaging failed: " + str(error), file=sys.stderr)
        sys.exit(1)
