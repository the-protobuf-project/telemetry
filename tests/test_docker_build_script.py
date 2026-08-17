"""Functional tests for docker/build.sh.

This PR renamed the default `IMAGE_NAME` (and the corresponding usage text)
from the "opentelementry" brand to "ghcr.io/the-protobuf-project/telemetry".
These tests exercise the script's argument handling and output without
requiring a real Docker daemon: a fake `docker` executable is placed first
on PATH so the script's `docker build` / `docker buildx` invocations succeed
trivially, letting us assert on the surrounding shell logic (usage output,
exit codes, image/tag name computation, env var overrides).
"""

import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]
BUILD_SCRIPT = REPO_ROOT / "docker" / "build.sh"

FAKE_DOCKER_SCRIPT = """#!/bin/bash
# Fake docker CLI: just echo what it was called with and succeed.
echo "FAKE_DOCKER_CALLED: $*"
exit 0
"""


class BuildScriptTestCase(unittest.TestCase):
    def setUp(self):
        self.assertTrue(BUILD_SCRIPT.is_file(), "docker/build.sh not found")
        # Create a directory containing a stub `docker` binary and prepend it
        # to PATH so build.sh's docker invocations don't require a real
        # Docker installation/daemon.
        self._tmpdir = tempfile.TemporaryDirectory()
        fake_docker = Path(self._tmpdir.name) / "docker"
        fake_docker.write_text(FAKE_DOCKER_SCRIPT)
        fake_docker.chmod(fake_docker.stat().st_mode | stat.S_IEXEC)

        self.env = os.environ.copy()
        self.env["PATH"] = f"{self._tmpdir.name}:{self.env.get('PATH', '')}"
        # Ensure a clean slate: no stray overrides from the outer environment.
        self.env.pop("IMAGE_NAME", None)
        self.env.pop("IMAGE_TAG", None)

    def tearDown(self):
        self._tmpdir.cleanup()

    def run_script(self, args=(), env=None):
        return subprocess.run(
            ["bash", str(BUILD_SCRIPT), *args],
            cwd=REPO_ROOT,
            env=env if env is not None else self.env,
            capture_output=True,
            text=True,
        )


class TestUsage(BuildScriptTestCase):
    def test_no_arguments_prints_usage_and_exits_nonzero(self):
        result = self.run_script(args=[])
        self.assertEqual(result.returncode, 1)
        self.assertIn("Usage:", result.stdout)
        self.assertIn("<amd64|arm64|manifest|local>", result.stdout)

    def test_invalid_argument_prints_usage_and_exits_nonzero(self):
        result = self.run_script(args=["bogus-arch"])
        self.assertEqual(result.returncode, 1)
        self.assertIn("Usage:", result.stdout)

    def test_usage_documents_renamed_default_image_name(self):
        result = self.run_script(args=[])
        self.assertIn(
            "IMAGE_NAME       Override image name "
            "(default: ghcr.io/the-protobuf-project/telemetry)",
            result.stdout,
        )

    def test_usage_does_not_mention_old_brand(self):
        result = self.run_script(args=[])
        self.assertNotIn("opentelementry", result.stdout.lower())


class TestLocalBuild(BuildScriptTestCase):
    def test_local_build_uses_default_version(self):
        result = self.run_script(args=["local"])
        self.assertEqual(result.returncode, 0)
        self.assertIn("Building opentelemetry-stack:1.0.0 locally", result.stdout)
        self.assertIn("Done! Built opentelemetry-stack:1.0.0", result.stdout)
        self.assertIn("FAKE_DOCKER_CALLED: build", result.stdout)

    def test_local_build_respects_image_tag_override(self):
        env = dict(self.env)
        env["IMAGE_TAG"] = "9.9.9"
        result = self.run_script(args=["local"], env=env)
        self.assertEqual(result.returncode, 0)
        self.assertIn("Building opentelemetry-stack:9.9.9 locally", result.stdout)


class TestArchBuild(BuildScriptTestCase):
    def test_amd64_build_uses_default_renamed_image_name(self):
        result = self.run_script(args=["amd64"])
        self.assertEqual(result.returncode, 0)
        self.assertIn(
            "Building ghcr.io/the-protobuf-project/telemetry:1.0.0-amd64 "
            "for linux/amd64",
            result.stdout,
        )
        self.assertIn(
            "Done! Pushed ghcr.io/the-protobuf-project/telemetry:1.0.0-amd64",
            result.stdout,
        )

    def test_arm64_build_uses_default_renamed_image_name(self):
        result = self.run_script(args=["arm64"])
        self.assertEqual(result.returncode, 0)
        self.assertIn(
            "Building ghcr.io/the-protobuf-project/telemetry:1.0.0-arm64 "
            "for linux/arm64",
            result.stdout,
        )

    def test_image_name_env_override_is_respected(self):
        env = dict(self.env)
        env["IMAGE_NAME"] = "example.com/custom/telemetry"
        result = self.run_script(args=["amd64"], env=env)
        self.assertEqual(result.returncode, 0)
        self.assertIn(
            "Building example.com/custom/telemetry:1.0.0-amd64 for linux/amd64",
            result.stdout,
        )
        self.assertNotIn("the-protobuf-project/telemetry", result.stdout)


class TestManifest(BuildScriptTestCase):
    def test_manifest_uses_default_renamed_image_name(self):
        result = self.run_script(args=["manifest"])
        self.assertEqual(result.returncode, 0)
        self.assertIn(
            "Creating multi-arch manifest for "
            "ghcr.io/the-protobuf-project/telemetry:1.0.0",
            result.stdout,
        )
        self.assertIn("Done! Multi-arch manifest pushed.", result.stdout)


if __name__ == "__main__":
    unittest.main()