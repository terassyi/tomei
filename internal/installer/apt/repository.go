package apt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/resource"
)

// keyringDir is the canonical Debian/Ubuntu location for per-repository
// trust roots used together with `signed-by=`. /etc/apt/trusted.gpg.d/
// would also work but globalizes the trust (any repo can be signed by any
// key there) and is on the deprecation path along with apt-key.
const keyringDir = "/usr/share/keyrings"

// sourcesListDir is the standard location for third-party APT source
// definitions. Files are read in lexical order and must end in `.list`
// (legacy one-line) or `.sources` (deb822). tomei writes `.list`.
const sourcesListDir = "/etc/apt/sources.list.d"

// archOption is the bracket-option key declaring per-architecture
// restrictions in a sources.list entry (e.g. `[arch=amd64]`). It is
// referenced in multiple places so it lives as a named constant to keep
// goconst quiet and to give the key a single source of truth.
const archOption = "arch"

// allowedSourcesListOptions is the whitelist of bracket-option keys
// permitted in SystemPackageRepository.spec.source.options. APT understands
// many more, but the ones below cover all realistic third-party-repository
// needs while excluding security-regression knobs (notably `trusted=yes`,
// which disables signature verification — opt-in for that is tracked as a
// separate ticket via an explicit `AllowUnsigned` flag rather than as a
// freeform option).
var allowedSourcesListOptions = map[string]struct{}{
	"signed-by":                   {},
	archOption:                    {},
	"target":                      {},
	"by-hash":                     {},
	"pdiffs":                      {},
	"check-valid-until":           {},
	"lang":                        {},
	"allow-insecure":              {},
	"allow-weak":                  {},
	"allow-downgrade-to-insecure": {},
}

// disallowedInRepoName covers shell metacharacters, path separators, and
// path-traversal-relevant characters. The CUE layer already constrains
// metadata.name to `^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`; this guard is
// defense-in-depth for non-CUE callers and is also re-checked downstream
// against filepath.Clean to refuse any name that does not survive a
// canonicalization pass.
const disallowedInRepoName = "/\\ \t\n\r;|&`$<>(){}*?[]~#\"'"

// failedToFetchRE matches `W: Failed to fetch <url>` warnings emitted by
// apt-get update when a configured source is unreachable or fails signature
// verification. apt-get returns exit 0 on partial fetch failures (only the
// stderr warning surfaces), so the helper greps for this pattern after
// Install's update step and rolls back the just-placed files if the failure
// is attributable to the repository being installed.
var failedToFetchRE = regexp.MustCompile(`(?m)^W: Failed to fetch (\S+)`)

// PackageRepositoryInstaller adds and removes third-party APT repositories
// by placing a per-repository GPG keyring under /usr/share/keyrings and a
// matching sources.list fragment under /etc/apt/sources.list.d, then
// running `apt-get update`. It satisfies
// executor.Installer[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]
// and is obtained from Client.PackageRepositoryInstaller.
//
// Host requirements: Linux, GNU coreutils `install`, passwordless `sudo -n`,
// and `gpg` (gnupg) in PATH. See Install for the full caller contract,
// trust model, and concurrency notes; the struct itself is just a handle
// that bundles the runner with a download.Downloader for the key fetch.
type PackageRepositoryInstaller struct {
	client     *Client
	downloader download.Downloader
}

// Compile-time assertion that *PackageRepositoryInstaller satisfies the
// executor installer interface for SystemPackageRepository.
var _ executor.Installer[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState] = (*PackageRepositoryInstaller)(nil)

// validateRepoName rejects names whose use in shell commands or
// path-joining would be unsafe. The CUE layer enforces a stricter regex
// already; this is defense-in-depth for non-CUE callers and provides a
// localized error message when invariants are violated.
func validateRepoName(name string) error {
	if name == "" {
		return errors.New("apt: empty repository name")
	}
	if strings.ContainsAny(name, disallowedInRepoName) {
		return fmt.Errorf("apt: repository name %q contains disallowed characters", name)
	}
	// Reject NUL explicitly — disallowedInRepoName does not include it
	// because Go string literals cannot embed it cleanly in a const.
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("apt: repository name %q contains NUL byte", name)
	}
	// Refuse any name that does not survive path canonicalization. This
	// catches `..` segments that slipped past the character allowlist via
	// e.g. encoded forms and is the seatbelt against state-file tampering
	// driving Remove towards an unintended target.
	if filepath.Clean(name) != name {
		return fmt.Errorf("apt: repository name %q is not a stable path component", name)
	}
	return nil
}

// validateOptionValue rejects characters that would break APT's
// bracket-option parser (`[key=value key=value]`) or open a line-injection
// attack on the generated sources.list file (CR/LF/NUL/control chars).
// Forward slashes and commas are intentionally permitted so values like
// `signed-by=/usr/share/keyrings/foo.gpg` and `arch=amd64,arm64` work.
func validateOptionValue(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	for _, r := range value {
		switch {
		case r == ' ' || r == '\t':
			return fmt.Errorf("contains whitespace")
		case r == '\n' || r == '\r' || r == 0:
			return fmt.Errorf("contains line-ending or NUL byte")
		case r == ']' || r == '[' || r == '=':
			return fmt.Errorf("contains bracket or equals character")
		case r < 0x20:
			return fmt.Errorf("contains control character")
		}
	}
	return nil
}

// validateSourcesListField rejects characters that would break the
// space-separated `URL Suite Components...` portion of a sources.list
// line. Forward slashes and colons are intentionally permitted (URL
// scheme + path). The strictness on whitespace is necessary because the
// emitted line is single-line and shell-parsed.
func validateSourcesListField(value string) error {
	if value == "" {
		return errors.New("empty value")
	}
	for _, r := range value {
		switch {
		case r == ' ' || r == '\t':
			return fmt.Errorf("contains whitespace")
		case r == '\n' || r == '\r' || r == 0:
			return fmt.Errorf("contains line-ending or NUL byte")
		case r < 0x20:
			return fmt.Errorf("contains control character")
		}
	}
	return nil
}

// keyringPath returns the canonical on-disk path for the per-repository
// GPG keyring.
func keyringPath(name string) string {
	return filepath.Join(keyringDir, name+".gpg")
}

// sourcesListPath returns the canonical on-disk path for the
// per-repository sources.list fragment.
func sourcesListPath(name string) string {
	return filepath.Join(sourcesListDir, name+".list")
}

// buildSourcesListLine renders a single APT one-line sources.list entry
// for the given repository. The returned string includes a trailing
// newline so it can be written verbatim into /etc/apt/sources.list.d/.
//
// Behavior:
//
//   - signed-by: if src.Options["signed-by"] is set, it is honored
//     verbatim (allowing the caller to point at a pre-existing system
//     keyring); otherwise the canonical /usr/share/keyrings/<name>.gpg is
//     emitted. The auto-derived value is guarded against rewriting an
//     existing user override.
//   - Other options: only keys in allowedSourcesListOptions are accepted;
//     unknown keys yield an error so manifest typos do not silently
//     produce broken sources.list lines. Values are checked against
//     validateOptionValue.
//   - Determinism: option keys are emitted in lexical order so unit-test
//     golden assertions are stable regardless of Go's map iteration
//     order.
//
// Returns the rendered line and nil, or an empty string and a validation
// error.
func buildSourcesListLine(name string, src resource.SourceConfig) (string, error) {
	if err := validateRepoName(name); err != nil {
		return "", err
	}
	if err := validateSourcesListField(src.URL); err != nil {
		return "", fmt.Errorf("apt: url: %w", err)
	}
	if err := validateSourcesListField(src.Suite); err != nil {
		return "", fmt.Errorf("apt: suite: %w", err)
	}
	if len(src.Components) == 0 {
		return "", errors.New("apt: components must have at least one entry")
	}
	for i, c := range src.Components {
		if err := validateSourcesListField(c); err != nil {
			return "", fmt.Errorf("apt: components[%d]: %w", i, err)
		}
	}

	opts := make(map[string]string, len(src.Options)+1)
	for k, v := range src.Options {
		if _, ok := allowedSourcesListOptions[k]; !ok {
			return "", fmt.Errorf("apt: option %q is not allowed", k)
		}
		if err := validateOptionValue(v); err != nil {
			return "", fmt.Errorf("apt: option %q value: %w", k, err)
		}
		opts[k] = v
	}
	if _, ok := opts["signed-by"]; !ok {
		opts["signed-by"] = keyringPath(name)
	}

	keys := slices.Sorted(maps.Keys(opts))
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+opts[k])
	}
	line := fmt.Sprintf("deb [%s] %s %s %s\n",
		strings.Join(parts, " "),
		src.URL,
		src.Suite,
		strings.Join(src.Components, " "),
	)
	return line, nil
}

// Install runs the full repository setup flow: download the armored GPG
// key over HTTPS, verify its SHA256 against the manifest, convert to the
// binary keyring format with `gpg --dearmor`, place it under
// /usr/share/keyrings/, write a one-line sources.list entry under
// /etc/apt/sources.list.d/, and refresh the APT index.
//
// Shell commands executed (one per ExecuteCapture call):
//
//   - gpg --dearmor < '<tmp>/<name>.armored' > '<tmp>/<name>.gpg'
//   - sudo -n install -D -m 0644 -o root -g root -- '<tmp>/<name>.gpg' '/usr/share/keyrings/<name>.gpg'
//   - sudo -n install -D -m 0644 -o root -g root -- '<tmp>/<name>.list' '/etc/apt/sources.list.d/<name>.list'
//   - sudo -n env DEBIAN_FRONTEND=noninteractive apt-get update 2>&1
//
// The `-D` flag on `install` ensures the destination directory exists
// (Ubuntu 20.04 minimal images may ship without /usr/share/keyrings/).
// stderr is merged into stdout for the update step so the partial-fetch
// detector can see `W: Failed to fetch` warnings even though apt-get
// returns exit 0 for them.
//
// Return values:
//   - success: (state, nil) where state.InstalledFiles is
//     [keyring path, sources.list path] in install order.
//   - validation error before any host mutation: (nil, wrapped error
//     starting with "apt: repository %q:" — name / spec / options /
//     URL / suite / components / disallowed Options key).
//   - download / verify / dearmor failure: (nil, wrapped error). No
//     files placed.
//   - sudo install failure for the keyring: (nil, wrapped error). No
//     files placed.
//   - sudo install failure for the sources.list: (nil, wrapped error).
//     The keyring is rolled back via best-effort `sudo rm -f`.
//   - apt-get update hard failure (non-zero exit): (nil, wrapped
//     error). Both files are rolled back via best-effort `sudo rm -f`;
//     a follow-up apt-get update is NOT issued because the cache is
//     already in an indeterminate state and re-running will not help.
//   - apt-get update partial fetch failure rooted in spec.Source.URL
//     (exit 0 with `W: Failed to fetch` warnings): (nil, wrapped error
//     including the failing URL). Both files are rolled back AND a
//     follow-up apt-get update is issued so the host's APT index is
//     restored to a consistent state.
//   - ctx cancellation / deadline at any step: (nil, wrapped ctx.Err())
//     so callers can detect cancellation via errors.Is(err,
//     context.Canceled / context.DeadlineExceeded).
//
// Caller contract: when err is non-nil the returned state is always
// nil and MUST NOT be interpreted as data; callers MUST check err
// before consuming the state. On error, any host mutation performed by
// Install has been rolled back on a best-effort basis (rollback
// failures are logged at WARN level but do not affect the returned
// error so the original cause is not masked).
//
// Trust model: name, spec.Source.URL, spec.Source.KeyURL, and
// spec.Source.KeyHash are assumed to come from a trusted source — a
// CUE manifest under the user's control or another in-process caller —
// per command/executor.go's package-level Security Model. The CUE
// layer constrains name (^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$), URL and
// KeyURL (HTTPS only), and KeyHash (sha256:hex); this helper applies
// defense-in-depth validation (validateRepoName, validateOptionValue,
// validateSourcesListField) so non-CUE callers fail closed. KeyHash
// SHA256 verification of the downloaded key is the integrity gate —
// HTTPS alone protects only against passive MITM, not against CDN or
// upstream-mirror compromise.
//
// Concurrency: Install mutates /usr/share/keyrings and
// /etc/apt/sources.list.d (sudo writes), and runs `apt-get update`
// which takes the dpkg-frontend lock. Concurrent Install / Remove
// against the same repository or concurrent apt-get install/remove
// elsewhere will serialize through the apt frontend lock; callers
// should not assume Install is reentrant for the same name.
//
// No special DEBIAN_FRONTEND handling is needed for `install` / `rm`,
// but the apt-get update step prepends env DEBIAN_FRONTEND=noninteractive
// via `sudo -n env ...` (the same pattern as Update — sudo strips most
// env so passing it through sudo's argv is the only reliable path).
func (p *PackageRepositoryInstaller) Install(ctx context.Context, res *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
	if res == nil || res.SystemPackageRepositorySpec == nil {
		return nil, fmt.Errorf("apt: repository %q: nil spec", name)
	}
	if err := validateRepoName(name); err != nil {
		return nil, err
	}
	spec := res.SystemPackageRepositorySpec
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("apt: repository %q: %w", name, err)
	}

	// Build the sources.list line up-front so validation failures abort
	// before any host mutation.
	sourcesLine, err := buildSourcesListLine(name, spec.Source)
	if err != nil {
		return nil, fmt.Errorf("apt: repository %q: build sources line: %w", name, err)
	}

	tmpDir, err := os.MkdirTemp("", "tomei-apt-repo-*")
	if err != nil {
		return nil, fmt.Errorf("apt: repository %q: tmpdir: %w", name, err)
	}
	defer os.RemoveAll(tmpDir)

	armoredPath := filepath.Join(tmpDir, name+".armored")
	if _, err := p.downloader.Download(ctx, spec.Source.KeyURL, armoredPath); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: download key: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: download key: %w", name, err)
	}
	if err := p.downloader.Verify(ctx, armoredPath, &resource.Checksum{Value: spec.Source.KeyHash}); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: verify key: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: verify key: %w", name, err)
	}

	dearmoredPath := filepath.Join(tmpDir, name+".gpg")
	dearmorCmd := fmt.Sprintf("gpg --dearmor < %s > %s",
		shellQuote(armoredPath), shellQuote(dearmoredPath))
	if _, err := p.client.runner.ExecuteCapture(ctx, []string{dearmorCmd}, command.Vars{}, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: dearmor key: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: dearmor key: %w", name, err)
	}

	// Use `install -D` so the destination directory is created if it does
	// not exist. /usr/share/keyrings and /etc/apt/sources.list.d are not
	// guaranteed to be present on minimal Ubuntu 20.04-era images.
	keyringDst := keyringPath(name)
	installKeyringCmd := fmt.Sprintf("sudo -n install -D -m 0644 -o root -g root -- %s %s",
		shellQuote(dearmoredPath), shellQuote(keyringDst))
	if _, err := p.client.runner.ExecuteCapture(ctx, []string{installKeyringCmd}, command.Vars{}, nil); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: install keyring: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: install keyring: %w", name, err)
	}

	sourcesListSrc := filepath.Join(tmpDir, name+".list")
	if err := os.WriteFile(sourcesListSrc, []byte(sourcesLine), 0o644); err != nil {
		// keyring was placed; roll it back since sources.list failed.
		p.bestEffortRemove(ctx, "after sources-line write failure", []string{keyringDst})
		return nil, fmt.Errorf("apt: repository %q: write sources line: %w", name, err)
	}
	sourcesDst := sourcesListPath(name)
	installSourcesCmd := fmt.Sprintf("sudo -n install -D -m 0644 -o root -g root -- %s %s",
		shellQuote(sourcesListSrc), shellQuote(sourcesDst))
	if _, err := p.client.runner.ExecuteCapture(ctx, []string{installSourcesCmd}, command.Vars{}, nil); err != nil {
		p.bestEffortRemove(ctx, "after sources install failure", []string{keyringDst})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: install sources: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: install sources: %w", name, err)
	}

	// Update the APT index and inspect stderr for "W: Failed to fetch"
	// warnings tied to the URL we just added. apt-get update returns
	// exit 0 even when individual mirrors fail, so the warning is the
	// only signal we have to detect a non-functional repo addition.
	updateCmd := "sudo -n " + debianFrontendNoninteractive + " apt-get update 2>&1"
	updateOut, err := p.client.runner.ExecuteCapture(ctx, []string{updateCmd}, command.Vars{}, nil)
	if err != nil {
		p.bestEffortRemove(ctx, "after apt-get update hard failure", []string{sourcesDst, keyringDst})
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: update: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: update: %w", name, err)
	}
	if failed := failedToFetchURLs(updateOut, spec.Source.URL); len(failed) > 0 {
		p.bestEffortRemove(ctx, "after apt-get update partial fetch failure", []string{sourcesDst, keyringDst})
		// Run a follow-up update so the host's APT index is internally
		// consistent again now that the offending sources.list is gone.
		// Errors here are surfaced only via warn logs inside Update —
		// we cannot return them without masking the rollback cause.
		if uerr := p.client.Update(ctx); uerr != nil {
			slog.Warn("apt: repository rollback: follow-up update failed",
				"repository", name, "err", uerr)
		}
		return nil, fmt.Errorf("apt: repository %q: failed to fetch: %s",
			name, strings.Join(failed, ", "))
	}

	return &resource.SystemPackageRepositoryState{
		InstallerRef:   spec.InstallerRef,
		Source:         spec.Source,
		InstalledFiles: []string{keyringDst, sourcesDst},
		UpdatedAt:      time.Now(),
	}, nil
}

// Remove deletes the files recorded in state.InstalledFiles in reverse
// install order (sources.list first, then keyring) and refreshes the
// APT index so stale cache entries for the removed repository are
// dropped on the next package operation.
//
// Shell commands executed (one per ExecuteCapture call):
//
//   - sudo -n rm -f -- '<state.InstalledFiles[N]>'   (for each path, reverse order)
//   - sudo -n env DEBIAN_FRONTEND=noninteractive apt-get update         (via Client.Update)
//
// Path safety: each path is re-validated against an allowlist
// (/usr/share/keyrings/ and /etc/apt/sources.list.d/) plus a
// filepath.Clean canonicalisation check before being passed to rm. This
// is the seatbelt against a tampered state file driving Remove towards
// an unintended target. Paths outside the allowlist or with non-canonical
// segments (e.g. `..`) yield an immediate error before any rm fires.
//
// Caller contract: state MUST be non-nil. An empty InstalledFiles is
// treated as a no-op for the rm loop — apt-get update still runs so
// any lingering cache for the (already absent) repository is evicted.
// ctx cancellation surfaces as a wrapped ctx.Err() so callers can
// distinguish cancellation from a genuine rm or update failure.
//
// Concurrency: Remove takes the dpkg-frontend lock via the apt-get
// update step. Concurrent Install / Remove against the same repository
// or concurrent apt-get traffic elsewhere will serialize through that
// lock; callers should not assume Remove is reentrant for the same
// name.
func (p *PackageRepositoryInstaller) Remove(ctx context.Context, state *resource.SystemPackageRepositoryState, name string) error {
	if state == nil {
		return fmt.Errorf("apt: repository %q: nil state", name)
	}
	if err := validateRepoName(name); err != nil {
		return err
	}
	// Remove in reverse install order so the sources.list referencing the
	// keyring is gone before the keyring itself disappears.
	for _, path := range slices.Backward(state.InstalledFiles) {
		if err := validateInstalledPath(path); err != nil {
			return fmt.Errorf("apt: repository %q: %w", name, err)
		}
		rmCmd := fmt.Sprintf("sudo -n rm -f -- %s", shellQuote(path))
		if _, err := p.client.runner.ExecuteCapture(ctx, []string{rmCmd}, command.Vars{}, nil); err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return fmt.Errorf("apt: repository %q: remove %q: %w", name, path, ctxErr)
			}
			return fmt.Errorf("apt: repository %q: remove %q: %w", name, path, err)
		}
	}
	if err := p.client.Update(ctx); err != nil {
		return fmt.Errorf("apt: repository %q: update: %w", name, err)
	}
	return nil
}

// validateInstalledPath refuses paths outside the directories Install
// writes to. This is the seatbelt against a tampered state file driving
// Remove towards an unintended target (e.g. /etc/passwd).
func validateInstalledPath(path string) error {
	if path == "" {
		return errors.New("refuse empty installed-file path")
	}
	cleaned := filepath.Clean(path)
	if cleaned != path {
		return fmt.Errorf("refuse non-canonical installed-file path %q", path)
	}
	if !strings.HasPrefix(cleaned, keyringDir+"/") && !strings.HasPrefix(cleaned, sourcesListDir+"/") {
		return fmt.Errorf("refuse installed-file path %q outside %s and %s",
			path, keyringDir, sourcesListDir)
	}
	return nil
}

// bestEffortRemove issues `sudo -n rm -f --` for each path. Errors are
// not returned because callers (Install's rollback branches) have
// already failed and surfacing rollback errors would mask the original
// cause; instead, they are logged at WARN level with the rollback
// reason and offending path so operators can manually clean up if the
// rollback itself did not complete.
func (p *PackageRepositoryInstaller) bestEffortRemove(ctx context.Context, reason string, paths []string) {
	for _, path := range paths {
		rmCmd := fmt.Sprintf("sudo -n rm -f -- %s", shellQuote(path))
		if _, err := p.client.runner.ExecuteCapture(ctx, []string{rmCmd}, command.Vars{}, nil); err != nil {
			slog.Warn("apt: repository rollback rm failed",
				"reason", reason, "path", path, "err", err)
		}
	}
}

// failedToFetchURLs scans apt-get update output for `W: Failed to fetch
// <url>` lines whose URL is rooted under the provided base. The base
// match is a prefix check so that fetch failures against the Release /
// InRelease / Packages files (each containing the base URL as a prefix)
// are all attributable to the repo being installed. A trailing slash on
// base is normalised away so callers may pass either
// `https://example.com/ubuntu` or `https://example.com/ubuntu/`. If base
// is empty no failures are returned, matching the early-return contract
// that Install relies on when the caller has already validated the
// non-emptiness of spec.Source.URL upstream.
//
// Capture group 1 of failedToFetchRE yields the failing URL; anything
// after the URL (apt typically writes `  <code> <reason>` after the URL)
// is excluded by `\S+`.
func failedToFetchURLs(output, base string) []string {
	if base == "" {
		return nil
	}
	base = strings.TrimRight(base, "/")
	matches := failedToFetchRE.FindAllStringSubmatch(output, -1)
	var hits []string
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if strings.HasPrefix(m[1], base) {
			hits = append(hits, m[1])
		}
	}
	return hits
}

// shellQuote single-quotes a string for safe interpolation into a
// `sh -c` command, escaping any embedded single quotes via the standard
// `'\”` idiom. Required because filepath.Join can produce paths
// containing characters that are sensitive to shell parsing (though
// none in practice here — tmp dirs come from os.MkdirTemp and installed
// paths are derived from validateRepoName-sanitized inputs); POSIX
// single-quoting keeps the helpers correct against future path
// expansions.
//
// NUL bytes in s would silently terminate the C string passed to
// execve and are NOT handled here — every caller in this package
// derives s from inputs that have been NUL-checked upstream by
// validateRepoName / validateSourcesListField / validateOptionValue.
// Callers reaching this helper through a new code path must apply
// the same upstream guards.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
