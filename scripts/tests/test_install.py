"""Offline installer regressions; every installed file stays in a temp directory.

The curl/uname stubs use local release fixtures, so tests need no network,
privileges, or changes to the user's configuration or installation directories.
"""
import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="sm-install-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.tools = self.root / "tools"
        self.tools.mkdir()
        self.assets = self.root / "assets"
        self.assets.mkdir()
        checksums = []
        self.binary = b"#!/bin/sh\necho installer-fixture\n"
        self.binaries = {}
        for platform in ("darwin", "linux"):
            for arch in ("amd64", "arm64"):
                name = f"sm_0.6.2_{platform}_{arch}.tar.gz"
                binary = self.binary + f"# {platform}/{arch}\n".encode()
                self.binaries[platform, arch] = binary
                with tarfile.open(self.assets / name, "w:gz") as archive:
                    info = tarfile.TarInfo("sm")
                    info.size = len(binary)
                    info.mode = 0o755
                    archive.addfile(info, io.BytesIO(binary))
                digest = hashlib.sha256((self.assets / name).read_bytes()).hexdigest()
                checksums.append(f"{digest}  {name}")
        (self.assets / "checksums.txt").write_text("\n".join(checksums) + "\n")
        self.tool("curl", '''#!/usr/bin/env python3
import os, pathlib, shutil, sys
args = sys.argv[1:]
if os.environ.get("SM_TEST_NO_DOWNLOAD"):
    sys.exit(0)
if "-w" in args:
    print("https://github.com/dukechain2333/ai-sessions-manager/releases/tag/v0.6.2", end="")
else:
    url = next(arg for arg in args if arg.startswith("https:"))
    output = args[args.index("-o") + 1]
    shutil.copyfile(pathlib.Path(os.environ["SM_TEST_ASSETS"]) / url.rsplit("/", 1)[-1], output)
''')
        self.tool("uname", '''#!/bin/sh
case "$1" in
  -s) echo "$SM_TEST_OS" ;;
  -m) echo "$SM_TEST_ARCH" ;;
  *) exit 1 ;;
esac
''')
        # An unexpected privileged path must fail instead of invoking sudo.
        self.tool("sudo", "#!/bin/sh\nexit 99\n")
        self.env = dict(os.environ, PATH=str(self.tools) + os.pathsep + os.environ["PATH"],
                        SM_TEST_ASSETS=str(self.assets), SM_TEST_OS="Linux", SM_TEST_ARCH="x86_64")
        self.env.pop("VERSION", None)
        self.env.pop("BINDIR", None)

    def tool(self, name, source):
        path = self.tools / name
        path.write_text(source)
        path.chmod(0o755)

    def run_install(self, destination, *options, env=None):
        return subprocess.run(["sh", str(ROOT / "install.sh"), "--bin", str(destination), *options],
                              env=env or self.env, text=True, capture_output=True, check=False)

    def test_supported_architectures_and_new_destination_with_spaces(self):
        for platform, arch in (("Darwin", "arm64"), ("Darwin", "x86_64"),
                               ("Linux", "aarch64"), ("Linux", "x86_64")):
            with self.subTest(platform=platform, arch=arch):
                target = self.root / "new directory" / f"{platform}-{arch}"
                env = dict(self.env, SM_TEST_OS=platform, SM_TEST_ARCH=arch)
                result = self.run_install(target, env=env)
                self.assertEqual(result.returncode, 0, result.stderr)
                expected_arch = "amd64" if arch == "x86_64" else "arm64"
                self.assertEqual((target / "sm").read_bytes(), self.binaries[platform.lower(), expected_arch])
                self.assertTrue(os.access(target / "sm", os.X_OK))

    def test_checksum_failure_preserves_existing_installation(self):
        target = self.root / "existing"
        target.mkdir()
        old = b"previous working binary"
        (target / "sm").write_bytes(old)
        (self.assets / "sm_0.6.2_linux_amd64.tar.gz").write_bytes(b"corrupt download")
        result = self.run_install(target, "--version", "v0.6.2")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("checksum mismatch", result.stderr)
        self.assertEqual((target / "sm").read_bytes(), old)

    def test_missing_checksum_rejects_installation(self):
        (self.assets / "checksums.txt").write_text("")
        target = self.root / "not-installed"
        result = self.run_install(target, "--version", "v0.6.2")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("no checksum listed", result.stderr)
        self.assertFalse((target / "sm").exists())

    def test_make_install_creates_parent_with_portable_install(self):
        source = self.root / "source directory" / "sm"
        source.parent.mkdir()
        source.write_bytes(self.binary)
        source.chmod(0o600)
        target = self.root / "new destination" / "bin"
        # Skip the build dependency: this test exercises the actual Makefile
        # installation recipe with a known fixture, not a real agent binary.
        result = subprocess.run(["make", "-f", str(ROOT / "Makefile"), "-o", "build", "install",
                                 f"BINARY={source}", f"BINDIR={target}"],
                                cwd=self.root, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((target / "sm").read_bytes(), self.binary)
        self.assertEqual((target / "sm").stat().st_mode & 0o777, 0o755)

    def test_iterm_installer_prints_current_config_schema(self):
        # The installer contains a fixed real-user target. Stub both possible
        # filesystem mutations rather than changing HOME or touching it.
        self.tool("mkdir", "#!/bin/sh\nexit 0\n")
        env = dict(self.env, SM_TEST_NO_DOWNLOAD="1")
        result = subprocess.run(["sh", str(ROOT / "scripts/install-iterm2.sh")],
                                env=env, capture_output=True, text=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        line = next(line for line in result.stdout.splitlines() if line.lstrip().startswith("{"))
        cfg = json.loads(line)
        self.assertEqual(cfg["open_in"], {"mode": "window", "iterm2": {"ssh": "myserver"}})
        self.assertNotIn("iterm2", cfg)


if __name__ == "__main__":
    unittest.main()
