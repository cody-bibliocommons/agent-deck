package session

import (
	"os"
	"strings"
	"testing"
	"time"
)

// withKiroCommandOverride seeds the user-config cache with a [kiro-cli].command
// override for the duration of the test. The cached mtime is set to the real
// config path's mtime so LoadUserConfig's fast path returns the seeded value.
func withKiroCommandOverride(t *testing.T, command string) {
	t.Helper()

	var mtime time.Time
	if path, err := GetUserConfigPath(); err == nil {
		if st, statErr := os.Stat(path); statErr == nil {
			mtime = st.ModTime()
		}
	}

	userConfigCacheMu.Lock()
	prevCache, prevMtime, prevErr := userConfigCache, userConfigCacheMtime, userConfigCacheErr
	cfg := cloneDefaultUserConfig()
	cfg.Kiro.Command = command
	userConfigCache = &cfg
	userConfigCacheMtime = mtime
	userConfigCacheErr = nil
	userConfigCacheMu.Unlock()

	t.Cleanup(func() {
		userConfigCacheMu.Lock()
		userConfigCache, userConfigCacheMtime, userConfigCacheErr = prevCache, prevMtime, prevErr
		userConfigCacheMu.Unlock()
	})
}

// TestKiroCommandOverrideIsHonored pins the contract that a user's
// [kiro-cli].command override survives the UI's canonical "kiro-cli chat"
// default. The new-session paths in package ui set the command to the canonical
// default; if the adapter treats that as an opaque user passthrough, the
// override is silently dropped and flags like --model never reach the binary.
func TestKiroCommandOverrideIsHonored(t *testing.T) {
	const override = "kiro-cli chat --model haiku"
	withKiroCommandOverride(t, override)

	if got := GetKiroCommand(); got != override {
		t.Fatalf("GetKiroCommand() = %q, want %q", got, override)
	}

	i := &Instance{Tool: "kiro-cli"}
	for _, baseCommand := range []string{"", "kiro-cli", "kiro-cli chat"} {
		got := i.buildKiroCommand(baseCommand, false)
		if !strings.HasSuffix(got, override) {
			t.Errorf("buildKiroCommand(%q) = %q, want suffix %q", baseCommand, got, override)
		}
	}

	// A genuinely custom wrapper must still pass through untouched.
	const wrapper = "my-wrapper kiro-cli chat"
	if got := i.buildKiroCommand(wrapper, false); !strings.HasSuffix(got, wrapper) {
		t.Errorf("buildKiroCommand(%q) = %q, want passthrough", wrapper, got)
	}
}

// TestKiroResumeOnlyForChatInvocations guards the --resume append: it must not
// be added to an invocation that does not target the `chat` subcommand, and a
// path or wrapper name merely containing "chat" must not be mistaken for it.
func TestKiroResumeOnlyForChatInvocations(t *testing.T) {
	i := &Instance{Tool: "kiro-cli"}

	if got := i.buildKiroCommand("kiro-cli chat", true); !strings.HasSuffix(got, "--resume") {
		t.Errorf("chat invocation should resume, got %q", got)
	}
	// "chatty" is not the chat subcommand — a naive substring check false-matches.
	if got := i.buildKiroCommand("/opt/chatty/kiro-cli doctor", true); strings.Contains(got, "--resume") {
		t.Errorf("non-chat invocation must not get --resume, got %q", got)
	}
	// Idempotent: never append a second --resume.
	if got := i.buildKiroCommand("kiro-cli chat --resume", true); strings.Count(got, "--resume") != 1 {
		t.Errorf("--resume must not be duplicated, got %q", got)
	}
}

// TestKiroBuiltin proves the kiro-cli
// built-in is wired into the registry, resolves from realistic command strings,
// and defaults to the `chat` subcommand rather than the bare menu binary.
func TestKiroBuiltin(t *testing.T) {
	r := Init(nil)

	if !r.IsBuiltin("kiro-cli") {
		t.Error("IsBuiltin(kiro-cli) = false, want true")
	}

	if def := r.Get("kiro-cli"); def == nil {
		t.Error("Get(kiro-cli) = nil, want a ToolDef")
	} else if def.Icon != "👻" {
		t.Errorf("icon = %q, want 👻", def.Icon)
	}

	// Command-string resolution, including the aliased and pathed forms.
	for _, tc := range []struct{ cmd, want string }{
		{"kiro-cli", "kiro-cli"},
		{"kiro-cli chat", "kiro-cli"},
		{"kiro", "kiro-cli"},
		{"/root/.local/bin/kiro-cli chat --resume", "kiro-cli"},
		{"KIRO-CLI CHAT", "kiro-cli"},
		// Guard against regressions in neighbouring tools.
		{"claude", "claude"},
		{"cursor agent", "cursor"},
		{"hermes", "hermes"},
	} {
		if got := r.Match(tc.cmd); got != tc.want {
			t.Errorf("Match(%q) = %q, want %q", tc.cmd, got, tc.want)
		}
	}

	// The default launch command must include `chat`; bare kiro-cli is a menu.
	if got := GetKiroCommand(); got != "kiro-cli chat" {
		t.Errorf("GetKiroCommand() = %q, want \"kiro-cli chat\"", got)
	}

	// buildKiroCommand: default, resume, and passthrough behaviour.
	// All tools get a shared env prefix from buildEnvSourceCommand() (e.g.
	// `export COLORFGBG=... && `), so assert on the command suffix.
	i := &Instance{Tool: "kiro-cli"}
	if got := i.buildKiroCommand("kiro-cli", false); !strings.HasSuffix(got, "kiro-cli chat") {
		t.Errorf("build(default) = %q, want suffix \"kiro-cli chat\"", got)
	}
	if got := i.buildKiroCommand("kiro-cli", true); !strings.HasSuffix(got, "kiro-cli chat --resume") {
		t.Errorf("build(resume) = %q, want suffix \"kiro-cli chat --resume\"", got)
	}
	if got := i.buildKiroCommand("my-wrapper kiro-cli chat", false); !strings.HasSuffix(got, "my-wrapper kiro-cli chat") {
		t.Errorf("build(passthrough) = %q, want passthrough suffix", got)
	}
	// A non-kiro instance must be untouched by the adapter.
	other := &Instance{Tool: "claude"}
	if got := other.buildKiroCommand("claude", false); got != "claude" {
		t.Errorf("build(non-kiro) = %q, want \"claude\"", got)
	}
}
