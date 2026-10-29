"""Exercise CI provenance with the real Git and Go toolchains, never stamped mocks."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
import zipfile


ROOT = Path(__file__).resolve().parents[1]
SDK = "github.com/t8y2/dbx/plugins/sdk/go/dbx-plugin-sdk"
sys.dont_write_bytecode = True


class CIProvenanceTests(unittest.TestCase):
    def setUp(self):
        helper = ROOT / "scripts/package-ci.py"
        self.assertTrue(helper.is_file(), "Clean-tag CI packaging is not implemented")
        spec = importlib.util.spec_from_file_location("ci_package", helper)
        self.ci = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.ci)
        temp = tempfile.TemporaryDirectory(prefix="dbx-ci-provenance-")
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name)
        for directory in ("backend/sdk", "assets", "ui"):
            (self.root / directory).mkdir(parents=True)
        (self.root / ".gitignore").write_text("dist/\n")
        (self.root / "manifest.json").write_text(json.dumps({
            "id": "io.github.fixture.prometheus", "version": "0.2.2",
            "entrypoints": {"backend": {"executable": "bin/fixture"},
                            "ui": {"root": "ui", "entry": "ui/index.html"}}}))
        (self.root / "package.json").write_text('{"version":"0.2.2"}')
        (self.root / "dbx-plugin.toml").write_text(
            '[backend]\ndirectory = "backend"\nbinary = "fixture"\n'
            '[package]\ninclude = ["assets", "ui"]\n')
        (self.root / "assets/icon.svg").write_text("<svg/>")
        (self.root / "ui/index.html").write_text("<!doctype html><p>fixture</p>")
        (self.root / "backend/go.mod").write_text(
            f"module fixture.example/plugin\n\ngo 1.22\n\nrequire {SDK} v0.0.0\n"
            f"replace {SDK} => ./sdk\n")
        (self.root / "backend/main.go").write_text(
            f'package main\nimport ("fmt"; sdk "{SDK}")\n'
            'func main() { fmt.Println(sdk.Identity()) }\n')
        (self.root / "backend/sdk/go.mod").write_text(f"module {SDK}\n\ngo 1.22\n")
        (self.root / "backend/sdk/sdk.go").write_text(
            'package sdk\nfunc Identity() string { return "fixture" }\n')
        self.git("init", "-q")
        self.git("config", "user.name", "Fixture")
        self.git("config", "user.email", "fixture@example.invalid")
        self.git("add", ".")
        self.git("commit", "-qm", "fixture")
        self.git("tag", "v0.2.2")
        self.revision = self.git("rev-parse", "HEAD")
        host = subprocess.check_output(["go", "env", "GOHOSTOS", "GOHOSTARCH"], text=True).split()
        self.target = host[0] + "-" + {"amd64": "x64", "arm64": "arm64"}[host[1]]

    def git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args],
                                       text=True, stderr=subprocess.STDOUT).strip()

    def package(self):
        with mock.patch.dict(os.environ, {"GITHUB_SHA": self.revision}):
            return self.ci.build_package(self.root, self.target, "v0.2.2")

    def compile(self, vcs=True):
        binary = self.root / "dist/fixture"
        binary.parent.mkdir(exist_ok=True)
        goos, arch = self.target.split("-")
        env = {**os.environ, "GOWORK": "off", "GOFLAGS": "", "CGO_ENABLED": "0",
               "GOOS": goos, "GOARCH": "amd64" if arch == "x64" else arch,
               "GOAMD64": "v1", "GOARM64": "v8.0"}
        subprocess.run(["go", "build", "-trimpath", f"-buildvcs={str(vcs).lower()}",
                        "-o", str(binary), "."], cwd=self.root / "backend", env=env,
                       check=True, capture_output=True)
        return binary

    def test_real_binary_has_clean_tag_revision_and_checked_in_sdk(self):
        artifact = self.package()
        metadata = json.loads(artifact.with_suffix(".artifact.json").read_text())
        provenance = json.loads(artifact.with_suffix(".provenance.json").read_text())
        self.assertEqual(metadata, {"target": self.target, "url": artifact.name,
                                   "sha256": hashlib.sha256(artifact.read_bytes()).hexdigest(),
                                   "size": artifact.stat().st_size})
        self.assertEqual(provenance["source"]["revision"], self.revision)
        self.assertEqual(provenance["source"]["tree"], self.git("rev-parse", "HEAD^{tree}"))
        self.assertEqual(provenance["sdk"]["tree"], self.git("rev-parse", "HEAD:backend/sdk"))
        self.assertEqual(provenance["sdk"]["replacement"], "./sdk")
        self.assertEqual(provenance["buildSettings"]["vcs"], "git")
        self.assertEqual(provenance["buildSettings"]["vcs.revision"], self.revision)
        self.assertEqual(provenance["buildSettings"]["vcs.modified"], "false")
        executable = "bin/" + self.target + "/fixture"
        if self.target.startswith("windows-"):
            executable += ".exe"
        with zipfile.ZipFile(artifact) as archive:
            self.assertEqual(set(archive.namelist()), {
                "manifest.json", "checksums.json", "assets/icon.svg", "ui/index.html", executable})
            self.assertEqual(json.loads(archive.read("manifest.json"))["entrypoints"]["backend"]["executable"], executable)
            self.assertEqual(archive.getinfo(executable).external_attr >> 16 & 0o777, 0o755)
            checksums = json.loads(archive.read("checksums.json"))
            self.assertEqual(set(checksums["files"]), set(archive.namelist()) - {"checksums.json"})
            for name, digest in checksums["files"].items():
                self.assertEqual(hashlib.sha256(archive.read(name)).hexdigest(), digest)
        self.assertEqual(self.git("status", "--porcelain"), "")

    def test_rejects_tracked_or_untracked_dirty_sources_before_packaging(self):
        for name in ("backend/main.go", "unexpected.txt"):
            with self.subTest(name=name):
                path = self.root / name
                original = path.read_bytes() if path.exists() else None
                path.write_text("dirty")
                with self.assertRaisesRegex(ValueError, "[Dd]irty"):
                    self.package()
                self.assertFalse((self.root / "dist").exists())
                if original is None:
                    path.unlink()
                else:
                    path.write_bytes(original)

    def test_rejects_missing_release_tag(self):
        with self.assertRaisesRegex(ValueError, "RELEASE_TAG"):
            self.ci.build_package(self.root, self.target, "")

    def test_rejects_tag_version_mismatch(self):
        with self.assertRaisesRegex(ValueError, "[Tt]ag|version"):
            self.ci.build_package(self.root, self.target, "v0.2.1")

    def test_rejects_tag_pointing_at_another_commit(self):
        (self.root / "assets/icon.svg").write_text("<svg>updated</svg>")
        self.git("add", ".")
        self.git("commit", "-qm", "later source")
        with self.assertRaisesRegex(ValueError, "[Tt]ag|HEAD"):
            self.package()

    def test_rejects_github_sha_not_matching_checkout(self):
        with mock.patch.dict(os.environ, {"GITHUB_SHA": "0" * 40}):
            with self.assertRaisesRegex(ValueError, "GITHUB_SHA"):
                self.ci.build_package(self.root, self.target, "v0.2.2")

    def test_rejects_real_binary_without_vcs_settings(self):
        with self.assertRaisesRegex(ValueError, "vcs"):
            self.ci.verify_binary(self.compile(vcs=False), self.revision, self.target)

    def test_rejects_real_binary_with_another_revision(self):
        with self.assertRaisesRegex(ValueError, "vcs.revision"):
            self.ci.verify_binary(self.compile(), "0" * 40, self.target)

    def test_rejects_real_binary_built_from_dirty_source(self):
        (self.root / "assets/icon.svg").write_text("dirty")
        with self.assertRaisesRegex(ValueError, "vcs.modified"):
            self.ci.verify_binary(self.compile(), self.revision, self.target)

    def test_refuses_to_overwrite_different_existing_artifact(self):
        dist = self.root / "dist"
        dist.mkdir()
        artifact = dist / f"io.github.fixture.prometheus-0.2.2-{self.target}.dbxp"
        artifact.write_bytes(b"immutable-existing-release")
        with self.assertRaisesRegex(ValueError, "overwrite|[Ee]xisting"):
            self.package()
        self.assertEqual(artifact.read_bytes(), b"immutable-existing-release")

    def test_ignores_inherited_go_workspace_and_build_flags(self):
        with mock.patch.dict(os.environ, {"GOWORK": "/nonexistent/go.work",
                                          "GOFLAGS": "-buildvcs=false"}):
            artifact = self.package()
        self.assertEqual(json.loads(artifact.with_suffix(".provenance.json").read_text())
                         ["buildSettings"]["vcs.modified"], "false")


if __name__ == "__main__":
    unittest.main()
