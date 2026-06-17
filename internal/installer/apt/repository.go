package apt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp/armor"
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

// The bracket-option key allowlist is owned by the resource layer as
// resource.IsAllowedAptOption / AllowedAptOptionKeys so that AptSource.Validate (and thus
// `tomei validate`) rejects disallowed keys before the install path
// runs. This file only validates option *values* (line-injection +
// shell-encoding concerns); the *key* allowlist lives in
// internal/resource/system_package.go.

// repoNameRE mirrors the CUE schema's metadata.name constraint
// (`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`) exactly. Using an allowlist
// regex Go-side rather than a hand-curated disallow string closes the
// gap between CUE and Go for non-CUE callers — every character the
// regex rejects (including `=`, `,`, `+`, `:`, uppercase letters, and
// shell metas) is rejected uniformly, with no risk of forgetting a
// metacharacter when the threat model evolves. The derived keyring
// path /usr/share/keyrings/<name>.gpg and the
// signed-by=/usr/share/keyrings/<name>.gpg bracket-option value are
// both safe by construction once name has passed this gate.
var repoNameRE = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)

// failedToFetchRE matches `W: Failed to fetch <url>` warnings emitted by
// apt-get update when a configured source is unreachable or fails signature
// verification. apt-get returns exit 0 on partial fetch failures (only the
// stderr warning surfaces), so the helper greps for this pattern after
// Install's update step and rolls back the just-placed files if the failure
// is attributable to the repository being installed. Install runs apt-get
// with LC_ALL=C LANGUAGE=C so this English-anchored regex matches
// regardless of the host's user locale.
var failedToFetchRE = regexp.MustCompile(`(?m)^W: Failed to fetch (\S+)`)

// defaultRepoInstallTimeout caps the cumulative cost of an Install's
// post-validation work (key download + verify + in-process dearmor + sudo
// install of the keyring and sources.list + apt-get update). Surfaced as a var
// (not const) so tests and a future cross-installer timeout knob can
// override it. 10 minutes is roomy enough for a worst-case apt-get update
// against a healthy-but-distant mirror, while still bounding a hang against a
// dead mirror.
var defaultRepoInstallTimeout = 10 * time.Minute

// destinationSnapshot captures the contents of the destination keyring
// and sources.list at Install entry. If Install fails partway through
// (e.g. apt-get update reports a partial fetch on the new spec), the
// rollback path restores whichever files preexisted instead of rm'ing
// them — without this an upgrade that overwrites a previously-working
// repo and then fails would regress host state by deleting the
// working installation it replaced.
//
// /usr/share/keyrings/<name>.gpg and /etc/apt/sources.list.d/<name>.list
// are root:root 0644 (world-readable), so the snapshot is taken with a
// plain non-root os.ReadFile. Empty []byte slots mean the destination
// did not exist at snapshot time and rollback should rm rather than
// restore.
type destinationSnapshot struct {
	keyringDst  string
	sourcesDst  string
	keyringData []byte // nil if keyringDst did not exist
	sourcesData []byte // nil if sourcesDst did not exist
}

// snapshotInstalledDestinations reads the current contents of the two
// destination files for the given repository name. Errors other than
// ErrNotExist are silently treated as "no snapshot taken" — the
// rollback then falls back to rm, which is the prior behavior and the
// safest default when we cannot confirm what was on disk.
//
// Indirected through a package-level var so unit tests that use names
// colliding with real host installations (e.g. "docker") can stub it
// out — the production fallback runs the real disk-reading
// implementation defined below.
var snapshotInstalledDestinations = snapshotInstalledDestinationsImpl

func snapshotInstalledDestinationsImpl(name string) destinationSnapshot {
	snap := destinationSnapshot{
		keyringDst: keyringPath(name),
		sourcesDst: sourcesListPath(name),
	}
	if data, err := os.ReadFile(snap.keyringDst); err == nil {
		snap.keyringData = data
	}
	if data, err := os.ReadFile(snap.sourcesDst); err == nil {
		snap.sourcesData = data
	}
	return snap
}

// dataFor returns the snapshot bytes for the given destination path,
// or nil if the path does not match a tracked destination or had no
// preexisting content.
func (s destinationSnapshot) dataFor(path string) []byte {
	switch path {
	case s.keyringDst:
		return s.keyringData
	case s.sourcesDst:
		return s.sourcesData
	}
	return nil
}

// PackageRepositoryInstaller adds and removes third-party APT repositories
// by placing a per-repository GPG keyring under /usr/share/keyrings and a
// matching sources.list fragment under /etc/apt/sources.list.d, then
// running `apt-get update`. It satisfies
// executor.Installer[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState]
// and is obtained from Client.PackageRepositoryInstaller.
//
// Host requirements: Linux, GNU coreutils `install`, and passwordless
// `sudo -n`. The signing key is dearmored in-process (no gnupg dependency,
// #283). See Install for the full caller contract, trust model, and
// concurrency notes; the struct itself is just a handle that bundles the
// runner with a download.Downloader for the key fetch.
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
	if !repoNameRE.MatchString(name) {
		return fmt.Errorf("apt: repository name %q does not match required form ^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$", name)
	}
	// repoNameRE already rejects `.` / `..` (it requires the first char
	// to be [a-z0-9]) and any name starting with `.`. The defensive
	// filepath.Clean check is no longer needed because the regex is
	// strictly tighter than what filepath.Clean would normalize.
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

// dearmorKey reads an ASCII-armored OpenPGP key from armoredPath, decodes it to
// raw binary OpenPGP packets, and writes the result to dstPath (0600; the later
// `sudo install` re-chmods to 0644). It returns the number of bytes written.
//
// This is the in-process equivalent of `gpg --dearmor`, and is what lets tomei
// configure third-party repositories on minimal images without gnupg in PATH
// (#283). Like `gpg --dearmor`, it decodes EVERY concatenated armor block and
// appends their packets — a repo may publish multiple public keys in one key
// file during key rotation, and apt must hold all of them to verify a Release
// signed by either the old or the new key. ProtonMail/go-crypto does NOT
// validate the armor CRC24 checksum — integrity is guaranteed upstream by
// keyHash (sha256 of the armored bytes, verified before this call) and by
// apt's own Release-signature check at update time.
//
// The whole (hash-pinned, small) file is read into memory: armor.Decode wraps
// its input in a throwaway bufio that swallows read-ahead, so it cannot be
// called repeatedly on one stream without losing bytes between blocks.
func dearmorKey(armoredPath, dstPath string) (int64, error) {
	data, err := os.ReadFile(armoredPath)
	if err != nil {
		return 0, fmt.Errorf("open armored key: %w", err)
	}

	out, err := os.OpenFile(dstPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, fmt.Errorf("create keyring: %w", err)
	}
	n, decodeErr := decodeAllArmorBlocks(data, out)
	closeErr := out.Close()
	if decodeErr != nil {
		return n, decodeErr
	}
	if closeErr != nil {
		return n, fmt.Errorf("close keyring: %w", closeErr)
	}
	return n, nil
}

// decodeAllArmorBlocks decodes every concatenated ASCII-armor block in data and
// writes each block's raw binary OpenPGP packets to w, returning the total bytes
// written. It errors if data contains no armor block at all (not armored input).
func decodeAllArmorBlocks(data []byte, w io.Writer) (int64, error) {
	var total int64
	rest := data
	for blocks := 0; ; blocks++ {
		block, err := armor.Decode(bytes.NewReader(rest))
		if errors.Is(err, io.EOF) {
			// No (further) armor block. If we never found one, the input is
			// not armored — surface that as a hard error; otherwise we are
			// simply past the last block.
			if blocks == 0 {
				return total, fmt.Errorf("decode armor: %w", err)
			}
			return total, nil
		}
		if err != nil {
			return total, fmt.Errorf("decode armor: %w", err)
		}
		// base64 / structural corruption surfaces while reading block.Body, NOT
		// at armor.Decode — checking copyErr is load-bearing: dropping it would
		// write a truncated/corrupt keyring and let apt fail opaquely later.
		n, copyErr := io.Copy(w, block.Body)
		total += n
		if copyErr != nil {
			return total, fmt.Errorf("copy decoded key: %w", copyErr)
		}
		// Advance past this block's END line so the next iteration can find any
		// subsequent concatenated block. The base64 alphabet has no '-', so the
		// END marker cannot occur inside a block body.
		endMarker := []byte("-----END " + block.Type + "-----")
		idx := bytes.Index(rest, endMarker)
		if idx < 0 {
			// A successful Decode implies a terminated block; defensive.
			return total, nil
		}
		rest = rest[idx+len(endMarker):]
	}
}

// buildSourcesListLine renders a single APT one-line sources.list entry
// for the given repository. The returned string includes a trailing
// newline so it can be written verbatim into /etc/apt/sources.list.d/.
//
// Behavior:
//
//   - signed-by: always /usr/share/keyrings/<name>.gpg, auto-derived from
//     name. Manifests cannot override the keyring location through Options
//     — AptSource.Validate rejects spec.apt.options["signed-by"] outright,
//     so by the time this helper runs the override path cannot exist.
//   - Other options: AptSource.Validate has already confirmed each key is
//     per resource.IsAllowedAptOption. This helper only re-validates option
//     *values* against validateOptionValue (line-injection and
//     shell-encoding concerns for the rendered sources.list line).
//   - Determinism: option keys are emitted in lexical order so unit-test
//     golden assertions are stable regardless of Go's map iteration
//     order.
//
// Returns the rendered line and nil, or an empty string and a validation
// error. src must be non-nil; Install's caller passes spec.Apt after
// SystemPackageRepositorySpec.Validate has confirmed presence.
func buildSourcesListLine(name string, src *resource.AptSource) (string, error) {
	if err := validateRepoName(name); err != nil {
		return "", err
	}
	if src == nil {
		return "", errors.New("apt: nil apt source")
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
		if err := validateOptionValue(v); err != nil {
			return "", fmt.Errorf("apt: option %q value: %w", k, err)
		}
		opts[k] = v
	}
	opts["signed-by"] = keyringPath(name)

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
// key from spec.Apt.KeyURL (HTTPS-only — the CUE schema, AptSource.Validate
// via spec.Validate() called at the top of this function, and
// download.Downloader's own validateDownloadURL all enforce the same
// rule, with a localhost http:// escape hatch for integration tests),
// verify its SHA256 against spec.Apt.KeyHash, decode the ASCII-armored
// key to binary OpenPGP packet form in-process (no gnupg dependency,
// #283), place it under /usr/share/keyrings/, write a one-line
// sources.list entry under /etc/apt/sources.list.d/, and refresh the APT index.
//
// Shell commands executed (the first two via ExecuteCapture; the
// apt-get update step via ExecuteWithOutput with a per-line callback
// that scans both stdout and stderr for partial-fetch warnings):
//
//   - sudo -n install -D -m 0644 -o root -g root -- '<tmp>/<name>.gpg' '/usr/share/keyrings/<name>.gpg'
//   - sudo -n install -D -m 0644 -o root -g root -- '<tmp>/<name>.list' '/etc/apt/sources.list.d/<name>.list'
//   - sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update
//
// The `-D` flag on `install` ensures the destination directory exists
// (Ubuntu 20.04 minimal images may ship without /usr/share/keyrings/).
// `LC_ALL=C LANGUAGE=C` on apt-get update ensures the `W: Failed to fetch`
// partial-fetch detector matches against the canonical English warning
// regardless of the host's locale. The update step's stdout and stderr
// are read concurrently (no shell `2>&1` redirection) and only the
// matching `W: Failed to fetch` lines are buffered, so the apt-get
// transcript is never held in memory in full.
//
// Return values:
//   - success: (state, nil) where state.InstalledFiles is
//     [keyring path, sources.list path] in install order.
//   - validation error before any host mutation: (nil, error). Two
//     prefix shapes occur — `apt: repository name %q ...` from
//     validateRepoName (returned directly, no per-call wrap because the
//     name is already embedded in the message) and `apt: repository %q:
//     <reason>` from spec.Validate / buildSourcesListLine wraps (spec /
//     options / URL / suite / components / disallowed Options key).
//     Callers wanting the name in a uniform position should use
//     errors.Is / errors.As on sentinel errors rather than the prefix.
//   - download / verify / decode failure: (nil, wrapped error). No
//     files placed.
//   - sudo install failure for the keyring: (nil, wrapped error). No
//     files placed.
//   - sudo install failure for the sources.list: (nil, wrapped error).
//     The keyring is rolled back via best-effort `sudo rm -f`.
//   - apt-get update hard failure (non-zero exit): (nil, wrapped
//     error). Both files are rolled back via best-effort `sudo rm -f`;
//     a follow-up apt-get update is NOT issued because the cache is
//     already in an indeterminate state and re-running will not help.
//   - apt-get update partial fetch failure rooted in spec.Apt.URL
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
// Trust model: every field that reaches the shell-emitted sources.list
// line (name, spec.Apt.URL, spec.Apt.KeyURL, spec.Apt.KeyHash,
// spec.Apt.Suite, spec.Apt.Components, spec.Apt.Options) is assumed to
// come from a trusted source — a CUE manifest under the user's control
// or another in-process caller — per command/executor.go's package-level
// Security Model. Shell command strings are library-emitted (not
// template-expanded user input); the only user-controlled values that
// reach `sh -c` are shellQuote-wrapped operands derived from the
// validated spec. The CUE layer constrains name
// (^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$), URL and KeyURL (HTTPS only —
// keyUrl may legitimately be served from a different host than URL,
// e.g. kubernetes's pkgs.k8s.io vs Google's packages.cloud.google.com),
// KeyHash (sha256:hex), and Options keys (resource.IsAllowedAptOption);
// this helper applies defense-in-depth validation (validateRepoName,
// validateOptionValue, validateSourcesListField) so non-CUE callers
// fail closed. KeyHash SHA256 verification of the downloaded key is
// the integrity gate — HTTPS alone protects only against passive MITM,
// not against CDN or upstream-mirror compromise.
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

	// Cap the cumulative cost of the post-validation steps so a slow or
	// dead mirror (DNS hang, half-routed CDN) cannot block tomei apply
	// indefinitely. ctx.WithTimeout ensures the spawned subprocesses
	// (sudo install / apt-get update) receive SIGTERM via
	// command.Executor's CommandContext path on deadline expiry, which
	// is the only way to release the dpkg-frontend lock cleanly;
	// wall-clock kill would leave orphan apt holding the lock.
	ctx, cancel := context.WithTimeout(ctx, defaultRepoInstallTimeout)
	defer cancel()

	// Build the sources.list line up-front so validation failures abort
	// before any host mutation. spec.Validate above guarantees spec.Apt
	// is non-nil when installerRef is "apt".
	sourcesLine, err := buildSourcesListLine(name, spec.Apt)
	if err != nil {
		return nil, fmt.Errorf("apt: repository %q: build sources line: %w", name, err)
	}

	tmpDir, err := os.MkdirTemp("", "tomei-apt-repo-*")
	if err != nil {
		return nil, fmt.Errorf("apt: repository %q: tmpdir: %w", name, err)
	}
	defer os.RemoveAll(tmpDir)

	// Snapshot the destination files (if any preexist) so a rollback
	// after this point can restore the pre-Install state rather than
	// rm'ing the working installation this Install is replacing. The
	// snapshot is taken with non-root os.ReadFile (files are 0644);
	// missing files leave nil slots that mean "rm on rollback."
	snap := snapshotInstalledDestinations(name)

	armoredPath := filepath.Join(tmpDir, name+".armored")
	if _, err := p.downloader.Download(ctx, spec.Apt.KeyURL, armoredPath); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: download key: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: download key: %w", name, err)
	}
	if err := p.downloader.Verify(ctx, armoredPath, &resource.Checksum{Value: spec.Apt.KeyHash}); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: verify key: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: verify key: %w", name, err)
	}

	// Decode the armored key to binary keyring form in-process (no gnupg
	// dependency, #283). keyHash was already verified above, so the armored
	// bytes are trusted at this point.
	dearmoredPath := filepath.Join(tmpDir, name+".gpg")
	n, err := dearmorKey(armoredPath, dearmoredPath)
	if err != nil {
		return nil, fmt.Errorf("apt: repository %q: dearmor key: %w", name, err)
	}
	// Defense-in-depth: a well-formed but empty armor envelope decodes to a
	// 0-byte keyring, which would otherwise sail through to apt-get update and
	// fail opaquely with "the repository is not signed".
	if n == 0 {
		return nil, fmt.Errorf("apt: repository %q: dearmor produced empty keyring", name)
	}

	// Use `install -D` so the destination directory is created if it does
	// not exist. /usr/share/keyrings and /etc/apt/sources.list.d are not
	// guaranteed to be present on minimal Ubuntu 20.04-era images.
	keyringDst := keyringPath(name)
	installKeyringCmd := fmt.Sprintf("sudo -n install -D -m 0644 -o root -g root -- %s %s",
		shellQuote(dearmoredPath), shellQuote(keyringDst))
	if _, err := p.client.runner.ExecuteCapture(ctx, []string{installKeyringCmd}, command.Vars{}, nil); err != nil {
		// `install` can fail after partially creating the destination
		// (disk full mid-copy, interrupted write, sudo timeout between
		// chmod and chown phases, etc.) so a best-effort rm is
		// necessary even when the higher-level command "failed" —
		// otherwise a stray /usr/share/keyrings/<name>.gpg can linger
		// despite Install returning an error.
		p.bestEffortRollback("after keyring install failure", []string{keyringDst}, snap)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: install keyring: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: install keyring: %w", name, err)
	}

	sourcesListSrc := filepath.Join(tmpDir, name+".list")
	if err := os.WriteFile(sourcesListSrc, []byte(sourcesLine), 0o644); err != nil {
		// keyring was placed; roll it back since sources.list failed.
		p.bestEffortRollback("after sources-line write failure", []string{keyringDst}, snap)
		return nil, fmt.Errorf("apt: repository %q: write sources line: %w", name, err)
	}
	sourcesDst := sourcesListPath(name)
	installSourcesCmd := fmt.Sprintf("sudo -n install -D -m 0644 -o root -g root -- %s %s",
		shellQuote(sourcesListSrc), shellQuote(sourcesDst))
	if _, err := p.client.runner.ExecuteCapture(ctx, []string{installSourcesCmd}, command.Vars{}, nil); err != nil {
		// `install` can fail after partially creating or overwriting
		// sourcesDst (e.g. it copied to a temp path under the
		// destination directory, then renamed and the rename failed,
		// or it began an in-place write that errored midway). rm -f
		// is safe even if sourcesDst does not exist, so remove it
		// alongside the keyring to keep rollback symmetric and prevent
		// a stranded sources.list pointing at a removed keyring from
		// breaking subsequent apt operations.
		p.bestEffortRollback("after sources install failure", []string{sourcesDst, keyringDst}, snap)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: install sources: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: install sources: %w", name, err)
	}

	// Update the APT index and scan stdout+stderr for `W: Failed to
	// fetch` warnings tied to the URL we just added. apt-get update
	// returns exit 0 even when individual mirrors fail, so the warning
	// is the only signal we have to detect a non-functional repo
	// addition. ExecuteWithOutput streams both streams line-by-line via
	// the callback (no `2>&1`, no full buffer), which avoids coupling
	// correctness to shell redirection and keeps memory bounded for
	// hosts with verbose apt-get output. The callback runs from two
	// goroutines (stdout + stderr), so collectFailedFetches synchronizes
	// internally.
	updateCmd := "sudo -n " + aptGetEnvPrefix + " apt-get update"
	failedFetches := newFailedFetchCollector(spec.Apt.URL)
	updateErr := p.client.runner.ExecuteWithOutput(ctx, []string{updateCmd}, command.Vars{}, nil, failedFetches.scanLine)
	if updateErr != nil {
		p.bestEffortRollback("after apt-get update hard failure", []string{sourcesDst, keyringDst}, snap)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("apt: repository %q: update: %w", name, ctxErr)
		}
		return nil, fmt.Errorf("apt: repository %q: update: %w", name, updateErr)
	}
	if failed := failedFetches.urls(); len(failed) > 0 {
		p.bestEffortRollback("after apt-get update partial fetch failure", []string{sourcesDst, keyringDst}, snap)
		// Run a follow-up update on a fresh Background-rooted context
		// (with its own short deadline) so the host's APT index is
		// restored to a consistent state even when Install's caller
		// ctx is already past its deadline — without the detached
		// context the follow-up Update would be refused by
		// CommandContext, defeating the rollback contract this branch
		// is meant to satisfy. Errors here are surfaced only via warn
		// logs; we cannot return them without masking the rollback
		// cause.
		//
		// Wrapped in a tiny inline closure so `defer cancelUpdate()`
		// runs even if a future change inside the block adds an early
		// return — the manual cancel() that used to live after the
		// Update call was fragile in that respect.
		func() {
			updateCleanupCtx, cancelUpdate := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancelUpdate()
			if uerr := p.client.Update(updateCleanupCtx); uerr != nil {
				slog.Warn("apt: repository rollback: follow-up update failed",
					"repository", name, "err", uerr)
			}
		}()
		return nil, fmt.Errorf("apt: repository %q: failed to fetch: %s",
			name, strings.Join(failed, ", "))
	}

	// Deep-copy spec.Apt into state. State must outlive Install's stack
	// frame and is later consumed by the reconciler comparator and the
	// Remove path; aliasing spec.Apt would leak post-Install spec
	// mutations into persisted state. Matches the precedent set by
	// PackageSetInstaller in apt.go (Packages slice cloned on
	// hand-off to state).
	return &resource.SystemPackageRepositoryState{
		InstallerRef: spec.InstallerRef,
		Apt: &resource.AptSource{
			URL:        spec.Apt.URL,
			KeyURL:     spec.Apt.KeyURL,
			KeyHash:    spec.Apt.KeyHash,
			Suite:      spec.Apt.Suite,
			Components: slices.Clone(spec.Apt.Components),
			Options:    maps.Clone(spec.Apt.Options),
		},
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
//   - sudo -n rm -f -- '/etc/apt/sources.list.d/<name>.list'
//   - sudo -n rm -f -- '/usr/share/keyrings/<name>.gpg'
//   - sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update         (via Client.Update)
//
// The rm order is fixed (sources.list first, then keyring) regardless
// of state.InstalledFiles order so a reordered state file cannot
// induce a window where apt would consult a still-present sources.list
// pointing at a now-removed keyring.
//
// Path safety: each path is re-validated against the deterministic
// per-name destinations Install writes (keyringPath(name) and
// sourcesListPath(name)) plus a filepath.Clean canonicalisation check
// before being passed to rm. This is the seatbelt against a tampered
// state file driving Remove towards an unintended target. Any path
// other than the exact pair this name produces — outside the allowed
// directories entirely, or inside them but for a different repository
// — yields an immediate error before any rm fires.
//
// Caller contract: state MUST be non-nil. InstalledFiles is consulted
// only as a membership set for the path validator — Remove always
// issues the two canonical rm calls (sources.list then keyring)
// regardless of slice contents, including when InstalledFiles is
// empty. `rm -f` against a non-existent path is a no-op which matches
// the "Remove makes the host quiet on this repo" contract, then
// apt-get update runs so any lingering cache for the (already absent)
// repository is evicted. ctx cancellation surfaces as a wrapped
// ctx.Err() so callers can distinguish cancellation from a genuine rm
// or update failure.
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
	// state.InstalledFiles is re-validated against the exact paths this
	// installer writes for the given name — directory-prefix-only
	// matching would let a tampered state file enumerate other
	// repositories' files under the same allowed directories and trick
	// Remove into deleting them. Once validated, we IGNORE the recorded
	// order and remove in the canonical sequence (sources.list first,
	// then keyring) so a reordered state file cannot induce a window
	// where apt would consult a sources.list pointing at a removed
	// keyring — which would break concurrent apt activity on the host.
	for _, path := range state.InstalledFiles {
		if err := validateInstalledPath(name, path); err != nil {
			return fmt.Errorf("apt: repository %q: %w", name, err)
		}
	}
	// Validation above guarantees every path in state.InstalledFiles is
	// either keyringPath(name) or sourcesListPath(name) (and any
	// duplicates collapse harmlessly — rm -f is idempotent). The fixed
	// order below means an empty InstalledFiles still results in two rm
	// calls, but rm -f against non-existent paths is a no-op which
	// matches the "Remove makes the host quiet on this repo" contract.
	orderedPaths := []string{sourcesListPath(name), keyringPath(name)}
	for _, path := range orderedPaths {
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

// validateInstalledPath refuses any path that does not exactly match
// one of the two paths this installer writes for the given repository
// name (the keyring at /usr/share/keyrings/<name>.gpg or the
// sources.list fragment at /etc/apt/sources.list.d/<name>.list). This
// is the seatbelt against a tampered state file driving Remove towards
// an unintended target — either outside the allowlist directories
// entirely (/etc/passwd) or inside them but for a different repository
// (e.g. a malicious state pointing at /usr/share/keyrings/<other>.gpg).
// The exact-match policy is sound because Install's paths are
// deterministic functions of name; a state file that has anything else
// in InstalledFiles is by definition corrupt.
func validateInstalledPath(name, path string) error {
	if path == "" {
		return errors.New("refuse empty installed-file path")
	}
	cleaned := filepath.Clean(path)
	if cleaned != path {
		return fmt.Errorf("refuse non-canonical installed-file path %q", path)
	}
	wantKeyring := keyringPath(name)
	wantSources := sourcesListPath(name)
	if cleaned != wantKeyring && cleaned != wantSources {
		return fmt.Errorf("refuse installed-file path %q: expected %q or %q for repository %q",
			path, wantKeyring, wantSources, name)
	}
	return nil
}

// bestEffortRollback removes or restores each path. If snap has
// preexisting content for the path (i.e. this Install overwrote an
// already-installed repo), the path is restored to its pre-Install
// bytes via `sudo install -m 0644 -o root -g root` from a per-call
// tmp file. Otherwise the path is rm'd. Errors are not returned
// because callers (Install's rollback branches) have already failed
// and surfacing rollback errors would mask the original cause; they
// are logged at WARN level with the rollback reason and offending
// path so operators can manually clean up if the rollback itself did
// not complete.
//
// Cleanup runs on a fresh context.Background-rooted context with its
// own 30s deadline, fully decoupled from the caller's ctx. The typical
// path into rollback is "Install timed out during apt-get update," at
// which point the caller's ctx is already past its deadline and any
// context derived from it (even via WithoutCancel, which strips
// cancellation and deadline but is brittle to reason about) risks
// refusing to spawn rm/install at all. Starting from Background() is
// the unambiguously-correct pattern: cleanup gets a guaranteed fresh
// budget regardless of what the caller's ctx is doing.
func (p *PackageRepositoryInstaller) bestEffortRollback(reason string, paths []string, snap destinationSnapshot) {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Lazily create a per-call tmp dir only if we need to restore at
	// least one file. The dir is mode 0700 (from MkdirTemp) and removed
	// at the end.
	var restoreTmpDir string
	defer func() {
		if restoreTmpDir != "" {
			_ = os.RemoveAll(restoreTmpDir)
		}
	}()
	for _, path := range paths {
		preexisting := snap.dataFor(path)
		if preexisting == nil {
			rmCmd := fmt.Sprintf("sudo -n rm -f -- %s", shellQuote(path))
			if _, err := p.client.runner.ExecuteCapture(cleanupCtx, []string{rmCmd}, command.Vars{}, nil); err != nil {
				slog.Warn("apt: repository rollback rm failed",
					"reason", reason, "path", path, "err", err)
			}
			continue
		}
		// Restore the preexisting contents. Write to tmpDir first
		// (user-owned, no sudo), then `sudo install` it back with
		// root:root 0644 — matching how Install placed the file
		// originally.
		if restoreTmpDir == "" {
			d, err := os.MkdirTemp("", "tomei-apt-rollback-*")
			if err != nil {
				slog.Warn("apt: repository rollback restore tmpdir failed",
					"reason", reason, "path", path, "err", err)
				continue
			}
			restoreTmpDir = d
		}
		bakPath := filepath.Join(restoreTmpDir, filepath.Base(path))
		if err := os.WriteFile(bakPath, preexisting, 0o600); err != nil {
			slog.Warn("apt: repository rollback restore write failed",
				"reason", reason, "path", path, "err", err)
			continue
		}
		restoreCmd := fmt.Sprintf("sudo -n install -D -m 0644 -o root -g root -- %s %s",
			shellQuote(bakPath), shellQuote(path))
		if _, err := p.client.runner.ExecuteCapture(cleanupCtx, []string{restoreCmd}, command.Vars{}, nil); err != nil {
			slog.Warn("apt: repository rollback restore install failed",
				"reason", reason, "path", path, "err", err)
		}
	}
}

// failedFetchCollector receives apt-get update output one line at a
// time (via ExecuteWithOutput's callback) and accumulates the failing
// URLs whose URL is rooted under the configured base. The base match is
// a path-boundary prefix check so that fetch failures against the
// Release / InRelease / Packages files (each containing the base URL as
// a prefix) are all attributable to the repo being installed without
// falsely matching sibling repos: `https://example.com/repo` must not
// match a failure URL of `https://example.com/repo-staging/...`. The
// match is exact-equal OR prefix-with-`/`-boundary.
//
// Capture group 1 of failedToFetchRE yields the failing URL; anything
// after the URL (apt typically writes `  <code> <reason>` after the
// URL) is excluded by `\S+`. The collector is safe for concurrent
// scanLine calls because ExecuteWithOutput spawns one goroutine per
// stream (stdout + stderr) and invokes the callback from both.
type failedFetchCollector struct {
	base   string
	prefix string
	mu     sync.Mutex
	hits   []string
}

func newFailedFetchCollector(base string) *failedFetchCollector {
	base = strings.TrimRight(base, "/")
	return &failedFetchCollector{base: base, prefix: base + "/"}
}

func (f *failedFetchCollector) scanLine(line string) {
	if f.base == "" {
		return
	}
	m := failedToFetchRE.FindStringSubmatch(line)
	if len(m) < 2 {
		return
	}
	url := m[1]
	if url == f.base || strings.HasPrefix(url, f.prefix) {
		f.mu.Lock()
		f.hits = append(f.hits, url)
		f.mu.Unlock()
	}
}

func (f *failedFetchCollector) urls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	// scanLine is invoked from two goroutines (stdout + stderr); raw
	// append order would therefore be a race-dependent interleave, and
	// apt can report the same URL on multiple lines (one per fetch
	// stage). Dedupe + sort so the user-facing error string is stable
	// across runs and free of duplicates.
	if len(f.hits) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(f.hits))
	out := make([]string, 0, len(f.hits))
	for _, u := range f.hits {
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// failedToFetchURLs is a thin wrapper over failedFetchCollector that
// keeps the original single-shot string-input API for unit tests. The
// streaming Install path uses the collector directly; this helper
// exists only so the partial-fetch-detection table tests can stay
// readable (pass a full apt-get update transcript, get back hits).
func failedToFetchURLs(output, base string) []string {
	c := newFailedFetchCollector(base)
	for line := range strings.SplitSeq(output, "\n") {
		c.scanLine(line)
	}
	return c.urls()
}

// shellQuote single-quotes a string for safe interpolation into a
// `sh -c` command, escaping any embedded single quotes via the standard
// POSIX idiom: close the open quote with a quote, emit an
// escaped-then-literal quote pair (backslash + quote), then reopen
// with another quote — written literally as four characters: quote,
// backslash, quote, quote. Required because filepath.Join can produce paths
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
