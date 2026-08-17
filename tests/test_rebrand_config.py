"""Regression tests for the "opentelementry" -> "telemetry" project rename.

This PR renames the project (and its misspelled former brand
"opentelementry") to "telemetry" across config, docs, CI workflow, and build
files, and rebrands copyright/ownership references from "Machani Robotics" to
"The Protobuf Project". None of these files contain application source code,
so these tests validate the *content* of the changed configuration/docs
files directly rather than exercising library APIs.

Deliberately dependency-free (stdlib only: json/re/pathlib/unittest) so it
runs in any environment without requiring PyYAML or similar to be installed.
YAML files are therefore validated via targeted text/regex assertions rather
than full structural parsing.
"""

import json
import re
import unittest
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parents[1]

# The former (misspelled) brand name. A real, correctly spelled "opentelemetry"
# (referring to the OpenTelemetry OSS project/standard) is expected to remain
# in many places and must NOT be flagged by this check.
OLD_BRAND_RE = re.compile(r"opentelementry", re.IGNORECASE)

# Old ownership/organization name that was rebranded away.
OLD_OWNER_RE = re.compile(r"Machani Robotics")

# Every non-binary file touched by this PR (paths reflect their post-rename
# location). Used for a blanket regression sweep for the old brand string.
CHANGED_TEXT_FILES = [
    ".devcontainer/devcontainer.json",
    ".devcontainer/docker-compose.yaml",
    ".github/copilot-instructions.md",
    ".github/dependabot.yml",
    ".github/workflows/ci.yaml",
    ".github/workflows/linter.yaml",
    ".github/workflows/release.yaml",
    ".gitignore",
    ".markdown-link-check.json",
    ".pre-commit-config.yaml",
    ".vscode/settings.json",
    ".yamllint.yml",
    "CHANGELOG.md",
    "LICENSE",
    "README.md",
    "buf.gen.example.yaml",
    "buf.gen.yaml",
    "buf.yaml",
    "docker/Dockerfile",
    "docker/README.md",
    "docker/build.sh",
    "docs/configuration.md",
    "telemetry-core/deploy/production/local-webhook/go.mod",
    "telemetry-cpp/examples/bazel/CMakeLists.txt",
]


def read_text(relative_path):
    return (REPO_ROOT / relative_path).read_text(encoding="utf-8")


def read_json(relative_path):
    return json.loads(read_text(relative_path))


class TestNoStaleBrandReferences(unittest.TestCase):
    """Blanket regression sweep: none of the files touched by the rename
    should still reference the old misspelled "opentelementry" brand."""

    def test_no_changed_file_contains_old_brand(self):
        offenders = {}
        for rel_path in CHANGED_TEXT_FILES:
            path = REPO_ROOT / rel_path
            self.assertTrue(path.is_file(), f"expected file missing: {rel_path}")
            content = path.read_text(encoding="utf-8")
            matches = OLD_BRAND_RE.findall(content)
            if matches:
                offenders[rel_path] = matches
        self.assertEqual(
            offenders, {}, f"stale 'opentelementry' references found: {offenders}"
        )

    def test_changed_files_exist_at_new_paths(self):
        for rel_path in CHANGED_TEXT_FILES:
            self.assertTrue(
                (REPO_ROOT / rel_path).exists(),
                f"expected renamed file to exist: {rel_path}",
            )

    def test_old_paths_no_longer_exist(self):
        old_paths = [
            "opentelementry-core",
            "opentelementry-cpp",
            "opentelementry-go",
            "opentelementry-py",
            "opentelementry-rs",
        ]
        for old in old_paths:
            self.assertFalse(
                (REPO_ROOT / old).exists(),
                f"old, pre-rename path should not exist: {old}",
            )


class TestDevcontainer(unittest.TestCase):
    def test_devcontainer_json_name(self):
        data = read_json(".devcontainer/devcontainer.json")
        self.assertEqual(data["name"], "telemetry")
        self.assertIn("docker-compose.yaml", data["dockerComposeFile"])

    def test_devcontainer_docker_compose_renamed(self):
        content = read_text(".devcontainer/docker-compose.yaml")
        self.assertIn('name: "telemetry-env"', content)
        self.assertIn("../telemetry-core/compose.yaml", content)
        self.assertIn("telemetry-build-cache:/home/developer/.cache", content)
        # top-level named volume declaration
        self.assertRegex(content, r"(?m)^\s*telemetry-build-cache:\s*$")


class TestDependabot(unittest.TestCase):
    def test_directories_point_at_renamed_packages(self):
        content = read_text(".github/dependabot.yml")
        expected_directories = [
            "/telemetry-go",
            "/telemetry-core/deploy/production/local-webhook",
            "/telemetry-rs",
            "/telemetry-py",
            "/telemetry-core/deploy/production",
        ]
        for directory in expected_directories:
            self.assertIn(
                f"directory: {directory}",
                content,
                f"dependabot.yml missing updated directory {directory}",
            )

    def test_referenced_directories_exist_on_disk(self):
        # A dependabot config pointing at a non-existent directory silently
        # fails to update dependencies there, so make sure every renamed
        # directory is actually present in the repo.
        for directory in [
            "telemetry-go",
            "telemetry-core/deploy/production/local-webhook",
            "telemetry-rs",
            "telemetry-py",
            "telemetry-core/deploy/production",
            ".devcontainer",
        ]:
            self.assertTrue(
                (REPO_ROOT / directory).is_dir(),
                f"dependabot directory does not exist: {directory}",
            )


class TestCIWorkflow(unittest.TestCase):
    def setUp(self):
        self.content = read_text(".github/workflows/ci.yaml")

    def test_checkout_path_renamed(self):
        self.assertIn("path: telemetry", self.content)
        self.assertNotIn("path: opentelementry", self.content)

    def test_go_job_working_directories_renamed(self):
        for wd in [
            "telemetry/telemetry-go",
            "telemetry/telemetry-go/examples/telemetry",
        ]:
            self.assertIn(f"working-directory: {wd}", self.content)

    def test_go_cache_key_uses_renamed_gosum_path(self):
        self.assertIn("hashFiles('telemetry/telemetry-go/go.sum')", self.content)

    def test_proto_job_checks_renamed_stub_path(self):
        self.assertIn("git diff --exit-code -- telemetry-go/protobuf/", self.content)

    def test_rust_job_uses_renamed_directory(self):
        self.assertIn("working-directory: ./telemetry-rs", self.content)
        self.assertIn("telemetry-rs/target/", self.content)

    def test_python_job_uses_renamed_package(self):
        self.assertIn("working-directory: ./telemetry-py", self.content)
        self.assertIn(
            'python -c "import telemetry; print(\'telemetry\', telemetry.__all__)"',
            self.content,
        )

    def test_cpp_job_uses_renamed_directory(self):
        self.assertIn("working-directory: ./telemetry-cpp", self.content)

    def test_docker_compose_job_uses_renamed_directory(self):
        self.assertIn("working-directory: ./telemetry-core", self.content)


class TestLinterWorkflow(unittest.TestCase):
    def setUp(self):
        self.content = read_text(".github/workflows/linter.yaml")

    def test_go_lint_working_directories_renamed(self):
        for wd in [
            "telemetry/telemetry-go",
            "telemetry/telemetry-go/examples/telemetry",
        ]:
            self.assertIn(f"working-directory: {wd}", self.content)

    def test_rust_lint_working_directory_renamed(self):
        self.assertIn("working-directory: ./telemetry-rs", self.content)

    def test_python_lint_working_directory_renamed(self):
        self.assertIn("working-directory: ./telemetry-py", self.content)


class TestReleaseWorkflow(unittest.TestCase):
    def setUp(self):
        self.content = read_text(".github/workflows/release.yaml")

    def test_python_dist_paths_renamed(self):
        self.assertIn("working-directory: ./telemetry-py", self.content)
        self.assertIn("path: telemetry-py/dist/*", self.content)
        self.assertIn("packages-dir: telemetry-py/dist", self.content)

    def test_cpp_build_target_renamed(self):
        self.assertIn("working-directory: ./telemetry-cpp", self.content)
        self.assertIn("bazel build -c opt //src:telemetry", self.content)
        self.assertIn("libtelemetry.a", self.content)
        self.assertIn("telemetry-cpp.tar.gz", self.content)

    def test_rust_package_names_renamed(self):
        self.assertIn("working-directory: ./telemetry-rs", self.content)
        self.assertIn(
            "cargo package -p telemetry-derive -p telemetry --no-verify", self.content
        )
        self.assertIn("cargo publish -p telemetry-derive", self.content)
        self.assertIn("cargo publish -p telemetry", self.content)


class TestGitignore(unittest.TestCase):
    def test_entries_renamed(self):
        lines = read_text(".gitignore").splitlines()
        self.assertIn("telemetry-rs/target", lines)
        self.assertIn("telemetry.toml", lines)
        self.assertNotIn("opentelementry.toml", lines)
        self.assertNotIn("opentelementry-rs/target", lines)


class TestMarkdownLinkCheckConfig(unittest.TestCase):
    def test_release_tag_pattern_renamed(self):
        data = read_json(".markdown-link-check.json")
        patterns = [p["pattern"] for p in data["ignorePatterns"]]
        self.assertIn(
            "github.com/the-protobuf-project/telemetry/releases/tag", patterns
        )

    def test_config_still_well_formed(self):
        data = read_json(".markdown-link-check.json")
        self.assertIn("aliveStatusCodes", data)
        self.assertIsInstance(data["aliveStatusCodes"], list)
        self.assertIn(200, data["aliveStatusCodes"])


class TestPreCommitConfig(unittest.TestCase):
    def setUp(self):
        self.content = read_text(".pre-commit-config.yaml")

    def test_golangci_lint_hook_renamed(self):
        self.assertIn("cd telemetry-go && golangci-lint run", self.content)
        self.assertIn(r"^telemetry-go/.*\.go$", self.content)

    def test_rust_hooks_renamed(self):
        self.assertIn("cd telemetry-rs && cargo fmt --all -- --check", self.content)
        self.assertIn(
            "cd telemetry-rs && cargo clippy --all-targets --all-features -- -D warnings",
            self.content,
        )
        self.assertIn(r"^telemetry-rs/.*\.rs$", self.content)

    def test_python_hooks_renamed(self):
        self.assertIn(r"^telemetry-py/.*\.py$", self.content)


class TestVSCodeSettings(unittest.TestCase):
    def test_cmake_source_directory_renamed(self):
        data = read_json(".vscode/settings.json")
        self.assertTrue(
            data["cmake.sourceDirectory"].endswith("telemetry/telemetry-cpp")
        )


class TestYamllintConfig(unittest.TestCase):
    def test_ignore_paths_renamed(self):
        content = read_text(".yamllint.yml")
        self.assertIn("telemetry-core/deploy/production/envoy/", content)
        self.assertIn("docker/envoy/", content)


class TestBufConfigs(unittest.TestCase):
    def test_buf_yaml_module_name_renamed(self):
        content = read_text("buf.yaml")
        self.assertIn("name: buf.build/the-protobuf-project/telemetry", content)

    def test_buf_gen_yaml_out_and_module_renamed(self):
        content = read_text("buf.gen.yaml")
        self.assertIn("out: telemetry-go/protobuf", content)
        self.assertIn(
            "module=github.com/the-protobuf-project/telemetry/telemetry-go/protobuf",
            content,
        )

    def test_buf_gen_example_yaml_out_and_module_renamed(self):
        content = read_text("buf.gen.example.yaml")
        self.assertIn("out: telemetry-go", content)
        self.assertIn(
            "module=github.com/the-protobuf-project/telemetry/telemetry-go",
            content,
        )
        self.assertIn("local: telemetry-go/bin/protoc-gen-telemetry", content)
        # The managed/override workaround for grafana/telemetry go_package
        # mismatches was removed as part of this PR since it's no longer
        # needed now that go_package paths are correct.
        self.assertNotIn("managed:", content)
        self.assertNotIn("override:", content)


class TestDockerfile(unittest.TestCase):
    def test_oci_labels_renamed(self):
        content = read_text("docker/Dockerfile")
        self.assertIn(
            'org.opencontainers.image.source="https://github.com/the-protobuf-project/telemetry"',
            content,
        )
        self.assertIn(
            'org.opencontainers.image.title="telemetry-stack"',
            content,
        )


class TestDockerReadme(unittest.TestCase):
    def test_image_references_renamed(self):
        content = read_text("docker/README.md")
        self.assertIn("ghcr.io/the-protobuf-project/telemetry:latest", content)
        self.assertIn("ghcr.io/the-protobuf-project/telemetry", content)

    def test_generic_opentelemetry_mentions_untouched(self):
        # Legitimate (correctly spelled) OpenTelemetry references are
        # unrelated to the brand rename and must be preserved.
        content = read_text("docker/README.md")
        self.assertIn("OpenTelemetry Stack", content)
        self.assertIn("OTel Collector", content)


class TestDocsConfiguration(unittest.TestCase):
    def setUp(self):
        self.content = read_text("docs/configuration.md")

    def test_title_and_config_file_renamed(self):
        self.assertIn("# Telemetry Configuration Guide", self.content)
        self.assertIn("`telemetry.toml`", self.content)

    def test_env_var_prefix_renamed(self):
        self.assertIn("TELEMETRY_CONFIG_PATH", self.content)
        self.assertIn("TELEMETRY_SERVICE_NAME", self.content)
        self.assertNotIn("OPENTELEMENTRY_", self.content)


class TestReadme(unittest.TestCase):
    def setUp(self):
        self.content = read_text("README.md")

    def test_title_renamed(self):
        self.assertIn('<h1 align="center">Telemetry</h1>', self.content)

    def test_logo_image_removed(self):
        # .assets/logo.png was deleted; README should no longer reference it.
        self.assertNotIn(".assets/logo.png", self.content)

    def test_badges_point_at_renamed_repo(self):
        self.assertIn(
            "https://github.com/the-protobuf-project/telemetry/actions/workflows/ci.yaml",
            self.content,
        )
        self.assertIn(
            "https://github.com/the-protobuf-project/telemetry/actions/workflows/linter.yaml",
            self.content,
        )

    def test_sdk_table_paths_renamed(self):
        for sdk_dir in [
            "telemetry-go",
            "telemetry-py",
            "telemetry-rs",
            "telemetry-cpp",
        ]:
            self.assertIn(f"[{sdk_dir}]({sdk_dir}", self.content)

    def test_env_var_prefix_renamed(self):
        self.assertIn("`TELEMETRY_`-prefixed", self.content)
        self.assertIn("TELEMETRY_SERVICE_NAME=my-service", self.content)

    def test_license_owner_renamed(self):
        self.assertIn("Copyright &copy; 2026 The Protobuf Project.", self.content)


class TestChangelog(unittest.TestCase):
    def setUp(self):
        self.content = read_text("CHANGELOG.md")

    def test_header_renamed(self):
        self.assertIn(
            "All notable changes to Telemetry will be documented in this file.",
            self.content,
        )

    def test_rebrand_entry_present(self):
        self.assertIn("Rebranded from Kodo to Telemetry", self.content)
        self.assertIn("Updated all documentation with Telemetry branding", self.content)

    def test_release_link_renamed(self):
        self.assertIn(
            "[1.0.0]: https://github.com/the-protobuf-project/telemetry/releases/tag/v1.0.0",
            self.content,
        )

    def test_owner_renamed(self):
        self.assertIn(
            "This is the initial open source release of Telemetry by The Protobuf Project.",
            self.content,
        )
        self.assertIsNone(OLD_OWNER_RE.search(self.content))


class TestLicense(unittest.TestCase):
    def test_copyright_owner_renamed(self):
        content = read_text("LICENSE")
        self.assertIn("Copyright 2026 The Protobuf Project", content)
        self.assertIsNone(OLD_OWNER_RE.search(content))


class TestCopilotInstructions(unittest.TestCase):
    def setUp(self):
        self.content = read_text(".github/copilot-instructions.md")

    def test_title_renamed(self):
        self.assertIn("# Telemetry Observability Framework", self.content)

    def test_owner_renamed(self):
        self.assertIn("Built by The Protobuf Project", self.content)
        self.assertIsNone(OLD_OWNER_RE.search(self.content))

    def test_struct_tag_and_options_renamed(self):
        self.assertIn('telemetry:"attribute:', self.content)
        self.assertIn("options.TelemetryOptions", self.content)
        self.assertIn("options.OpenTelemetryOptions.OTLP", self.content)

    def test_shared_stack_path_renamed(self):
        self.assertIn("`/telemetry/`", self.content)
        self.assertIn("cd telemetry/", self.content)


class TestLocalWebhookGoMod(unittest.TestCase):
    def test_module_and_go_version_renamed(self):
        content = read_text(
            "telemetry-core/deploy/production/local-webhook/go.mod"
        )
        self.assertIn(
            "module github.com/the-protobuf-project/telemetry/alert-webhook",
            content,
        )
        self.assertIn("go 1.21", content)


class TestCppBazelExampleCMakeLists(unittest.TestCase):
    def setUp(self):
        self.content = read_text("telemetry-cpp/examples/bazel/CMakeLists.txt")

    def test_project_and_option_renamed(self):
        self.assertIn("project(telemetry-example VERSION 1.0.0 LANGUAGES CXX)", self.content)
        self.assertIn("option(USE_LOCAL_TELEMETRY", self.content)

    def test_local_subdirectory_renamed(self):
        self.assertIn(
            "add_subdirectory(${CMAKE_CURRENT_SOURCE_DIR}/../telemetry-cpp "
            "${CMAKE_BINARY_DIR}/telemetry-cpp)",
            self.content,
        )

    def test_fetch_content_uses_renamed_repo(self):
        self.assertIn(
            "GIT_REPOSITORY https://github.com/the-protobuf-project/telemetry.git",
            self.content,
        )
        self.assertIn("SOURCE_SUBDIR telemetry-cpp", self.content)

    def test_target_links_renamed_library(self):
        self.assertIn("target_link_libraries(chat-service PRIVATE telemetry)", self.content)


class TestProtobufGoPackagesUpdated(unittest.TestCase):
    """The buf.gen.example.yaml comment explains that telemetry.v1's
    go_package option was corrected so the managed/override patch is no
    longer needed; verify the underlying proto files actually match."""

    def test_annotations_proto_go_package(self):
        content = read_text("protobuf/telemetry/v1/annotations.proto")
        self.assertIn(
            'option go_package = '
            '"github.com/the-protobuf-project/telemetry/telemetry-go/protobuf/'
            'telemetry/v1/telemetrypbv1;telemetrypbv1";',
            content,
        )

    def test_metrics_and_tracing_proto_go_package(self):
        for rel_path in [
            "protobuf/telemetry/v1/metrics.proto",
            "protobuf/telemetry/v1/tracing.proto",
        ]:
            content = read_text(rel_path)
            self.assertIn(
                'option go_package = '
                '"github.com/the-protobuf-project/telemetry/telemetry-go/protobuf/'
                'telemetry/v1/telemetrypbv1;telemetrypbv1";',
                content,
            )

    def test_example_job_proto_go_package(self):
        content = read_text("protobuf/examples/proto/jobs/v1/job.proto")
        self.assertIn(
            'option go_package = '
            '"github.com/the-protobuf-project/telemetry/telemetry-go/examples/'
            'telemetry/gen/jobs/v1/jobsv1;jobsv1";',
            content,
        )


if __name__ == "__main__":
    unittest.main()