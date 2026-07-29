package session

import (
	"strings"
)

// Kiro CLI adapter.
//
// Kiro CLI (AWS) is a single binary whose agentic TUI lives behind the `chat`
// subcommand — the bare binary prints a help/menu screen instead of starting an
// agent, so the default invocation here is `kiro-cli chat`:
//
//	kiro-cli chat                    # interactive agent TUI
//	kiro-cli chat --resume           # resume most recent conversation in this dir
//	kiro-cli chat --agent <name>     # start with a specific agent/context profile
//	kiro-cli chat --trust-all-tools  # run tools without confirmation prompts
//	kiro-cli chat --model <model>    # pick the model
//
// agent-deck integrates Kiro CLI at the same level as copilot/crush/hermes:
// launch the TUI in a tmux pane with optional env_file sourcing, an optional
// command override (e.g. for a wrapper script), and optional flags from
// `[kiro-cli]` config.
//
// Naming: the canonical tool name is "kiro-cli" (the real binary), not "kiro".
// See the comment on the registry entry in builtins.go — the installed-tools
// probe resolves built-ins by name via exec.LookPath, which cannot see the
// common `kiro` shell alias.
//
// Status detection: process-alive/dead plus the content patterns in
// internal/tmux/tmux.go.

// kiroDefaultCommand is the default launch invocation. It deliberately includes
// the `chat` subcommand; bare `kiro-cli` is a menu, not an agent.
const kiroDefaultCommand = "kiro-cli chat"

// GetKiroCommand returns the configured Kiro CLI command/alias.
// Mirrors GetCrushCommand: prefer the user config override, fall back to the
// default `kiro-cli chat` invocation.
func GetKiroCommand() string {
	userConfig, _ := LoadUserConfig()
	if userConfig != nil && strings.TrimSpace(userConfig.Kiro.Command) != "" {
		return strings.TrimSpace(userConfig.Kiro.Command)
	}
	return kiroDefaultCommand
}

// isDefaultKiroInvocation reports whether baseCommand represents "the user did
// not choose a specific command" — the empty string, the bare tool name, or the
// canonical default. These defer to [kiro-cli].command; anything else is a
// deliberate custom invocation that passes through untouched.
//
// Recognising the canonical default here is load-bearing: the new-session paths
// in package ui set the command to "kiro-cli chat", and treating that as a
// custom invocation would silently discard the user's config override.
func isDefaultKiroInvocation(baseCommand string) bool {
	cmd := strings.TrimSpace(baseCommand)
	return cmd == "" ||
		strings.EqualFold(cmd, "kiro-cli") ||
		strings.EqualFold(cmd, kiroDefaultCommand)
}

// invocationTargetsChat reports whether cmd invokes the `chat` subcommand, the
// only form that accepts --resume. Matched as a whitespace-delimited token so a
// wrapper or path merely containing the letters (e.g. /opt/chatty/kiro-cli)
// does not false-positive.
func invocationTargetsChat(cmd string) bool {
	for _, field := range strings.Fields(strings.ToLower(cmd)) {
		if field == "chat" {
			return true
		}
	}
	return false
}

// appendKiroConfigFlags adds the optional flags from [kiro-cli]. Applied only to
// the resolved default invocation, never to a custom passthrough command, so a
// user-supplied command is never rewritten behind their back.
func appendKiroConfigFlags(cmd string) string {
	config, _ := LoadUserConfig()
	if config == nil {
		return cmd
	}
	if agent := strings.TrimSpace(config.Kiro.Agent); agent != "" {
		cmd += " --agent " + agent
	}
	if config.Kiro.TrustAllTools {
		cmd += " --trust-all-tools"
	}
	return cmd
}

// buildKiroCommand builds the launch command for Kiro CLI: env sourcing, the
// configured command, optional config flags, and --resume on restart.
func (i *Instance) buildKiroCommand(baseCommand string, continuePrev bool) string {
	if i.Tool != "kiro-cli" {
		return baseCommand
	}

	cmd := strings.TrimSpace(baseCommand)
	if isDefaultKiroInvocation(cmd) {
		cmd = appendKiroConfigFlags(GetKiroCommand())
	}

	// Resume applies to custom commands too — it is a session-lifecycle concern,
	// not a config preference — but only where `chat` can accept it, and never
	// twice (mirrors buildCursorCommand's --continue guard).
	if continuePrev && invocationTargetsChat(cmd) && !strings.Contains(strings.ToLower(cmd), "--resume") {
		cmd += " --resume"
	}

	return i.buildEnvSourceCommand() + cmd
}
