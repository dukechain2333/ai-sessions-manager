# Session safety and reliability regression coverage

These regressions cover the full-code audit of `de4744257c41adbe415ef91b51a86297ed86bdb3`.
The IDs map the original findings to a behavior change and an automated check.

| ID | Corrected behavior | Regression coverage |
|---|---|---|
| R01 | Untrusted display text cannot emit terminal controls; native bridges accept only new/resume/attach command shapes. | `ui.TestUntrustedTranscriptCannotEmitTerminalCommands`, `TestSessionLabelsStripTerminalCommands`; `bridge.TestAgentCommandShapes`, `TestServeRejectsMalformedPayloadTypesAndMixedAttach`; Python command/type rejection cases. |
| R02 | Full UUIDs identify tmux sessions; UUIDv7 timestamp prefixes cannot collide. | `tmux.TestCodexUUIDv7NamesDoNotCollide`, `TestShort`. |
| R03 | Delete confirmation remains bound to the captured session across rescans. | `ui.TestDeleteKeepsConfirmedIdentityAcrossRescan`, including a target that disappears. |
| R04 | Enrichment follows its original file path after another session is deleted. | `ui.TestEnrichmentFollowsPathAfterDeletion`; `store.TestEnrichResultsCarrySnapshotPathOnSuccessAndError`. |
| R05 | A launch inside tmux creates detached and switches the client, without nested attachment. | `ui.TestColdLaunchInsideTmuxUsesSilentSwitch`; real tmux client test `tmux.TestDetachedArgsCreatesAndSwitchesWithoutNesting` covers new, cold-resume and existing-resume cases. |
| R06 | HTML/XML user prompts stay visible, counted and searchable. | `store.TestMarkupPromptsRemainVisibleAndSearchable`. |
| R07 | Recognized AGENTS/environment context is removed without dropping following human text. | `store.TestRecognizedContextKeepsFollowingHumanPrompt`, `TestCodexContextInSeparateContentBlocks`. |
| R08 | Codex activity uses the maximum valid record timestamp. | `store.TestCodexLastActivityUsesMaximumRecordTime`. |
| R09 | Linux Ghostty requires command-capable 1.3+ IPC and receives an explicit working directory. | `ghostty.TestLinuxCommandVersionGate`, `TestOpenLinuxUsesIPC`. |
| R10 | macOS Ghostty activates the requested window and reports activation failure. | `ghostty.TestDarwinFocusUsesSupportedWindowCommand`, `TestDarwinFocusFailureDoesNotDuplicateWindow`. |
| R11 | Trash never overwrites an earlier generation, including concurrent collisions. | `store.TestTrashRetainsEveryGeneration`, `TestConcurrentTrashCollisionNeverOverwrites`, `TestTrashCollisionPreservesDestinationSymlink`, `TestTrashFailureKeepsSource`. |
| R12 | Failed config writes preserve the previous file, permissions and symlink. | `config.TestSaveFailedWritePreservesExistingFile`, `TestSavePreservesPermissionsAndSymlink`. |
| R13 | Every new full-text query revalidates file versions; unstable extraction retries finitely and reports failure. Old parser caches are rebuilt. | `ui.TestSearchRevalidatesEachNewQuery`; `store.TestSearchIndexRetriesAnAppendDuringParsing`, `TestSearchIndexRepeatedChangesKeepPreviousCache`, `TestSearchIndexRebuildsLegacyParserCache`. |
| R14 | An explicitly configured Codex root symlink is traversed; nested links remain excluded. | `store.TestCodexScanFollowsOnlyExplicitRootSymlink`. |
| R15 | Slug resolution prunes nonexistent prefixes and shares results within one enrichment pass. | `store.TestResolveSlugPrunesLongNonexistentPrefixes`, `TestSlugResolutionCacheIsSharedAndLimitedToOnePass`. |
| R16 | Rendered list columns match mouse hit zones, including CJK titles. | `ui.TestListPaneWidthMatchesHitZones`, `TestCJKListFitsTerminal`. |
| R17 | Resizing rewraps source transcripts and retains a scroll anchor. | `ui.TestResizeReflowsLoadedTranscript`, `TestHeightResizeKeepsTranscriptScrollPosition`. |
| R18 | Child SSH windows clear inherited RemoteCommand and forwarding options, preserving connection settings. | `bridge.TestWindowSSHDoesNotInheritLoginCommandOrForwardings` uses real `ssh -G`; Python SSH command test. |
| R19 | tmux self-wrapping safely executes a lone path containing spaces/quotes. | Real tmux test `tmux.TestSelfWrapExecutablePathWithSpaces`. |
| R20 | `make install` creates a new destination portably, including paths with spaces. | `scripts/tests/test_install.py::test_make_install_creates_parent_with_portable_install`. |
| R21 | The iTerm installer prints the current nested config schema. | `scripts/tests/test_install.py::test_iterm_installer_prints_current_config_schema`. |
| R22 | Adoption retains the original native-window cache key while using the final tmux name. | `ui.TestAdoptionPreservesNativeWindowKey`, `bridge.TestWindowKeySurvivesSessionAdoption`, Python pending-to-adopted refocus test. |
| R23 | Long filter input scrolls within the available terminal width. | `ui.TestFilterInputFitsTerminal`. |

The parser also excludes structured Claude background-task notifications without
`isMeta`, including their generated output-file hint. Notification regressions
exercise metadata, transcripts, search, adjacent text blocks, genuine trailing
human text, and preservation of ordinary/incomplete XML. Index schema version 3
invalidates version-2 caches that could contain these synthetic messages.

## CI execution

- Linux: exact minimum Go version from `go.mod` and current stable Go.
- macOS: current stable Go, including BSD installation semantics and exclusive rename.
- Every matrix job installs tmux and ShellCheck; `SM_REQUIRE_TMUX_TESTS=1`
  turns missing tmux into a test failure. Each tmux fixture uses a short private
  socket directory and ignores personal tmux configuration.
- Every job runs vet, the complete race-enabled Go suite, Python bridge and
  installer tests, ShellCheck, and shell syntax checks.
- Linux stable also cross-compiles macOS/Linux × amd64/arm64 without CGO.
- Installer tests simulate all four archive mappings and verify that bad or
  absent checksums leave an existing binary unchanged. They use isolated files
  and stub the iTerm install destination; no real HOME or remote host is changed.

## Compatibility and validation limits

Full UUID tmux names intentionally do not auto-adopt ambiguous legacy eight-byte
names. Existing sessions keep running and can be attached to manually. Collision
trash copies live under `duplicate-*/` with their original basename, so manual
restoration preserves the ID. Safe trash fails without moving either file when
the platform/filesystem cannot provide an atomic no-replace rename.

Linux native Ghostty now requires 1.3.0 or later. Ghostty AppleScript/IPC and iTerm
RPC behavior is covered with protocol contracts and test doubles, not a real
graphical desktop. CI does not connect to a user's SSH server or launch real
Claude/Codex conversations. Changes to external desktop APIs still require a
manual smoke test on the installed application.
