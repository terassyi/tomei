# apt/testdata

## tomei-integration-test.asc

A throwaway ASCII-armored GPG public key used by
`tests/apt_repository_integration_test.go` as the fixture key served from
`httptest.NewServer`. The matching private key is intentionally discarded;
this file is only ever consumed as a public key payload for
`gpg --dearmor` and `apt-get update` signature lookup — the integration
test does not rely on signature verification succeeding (it exercises the
rollback path when `apt-get update` cannot fetch a real `InRelease`).

### Regeneration

If you need to regenerate this key (e.g. for compromised-fixture
hygiene), run the following on a Linux host with `gpg` 2.x installed:

```sh
TMPGNUPG=$(mktemp -d)
cat > "$TMPGNUPG/key.batch" <<'EOF'
%no-protection
Key-Type: RSA
Key-Length: 3072
Subkey-Type: RSA
Subkey-Length: 3072
Name-Real: tomei integration test
Name-Email: tomei-integration-test@example.invalid
Name-Comment: throwaway test key
Expire-Date: 0
%commit
EOF
GNUPGHOME="$TMPGNUPG" gpg --batch --pinentry-mode loopback \
    --gen-key "$TMPGNUPG/key.batch"
GNUPGHOME="$TMPGNUPG" gpg --armor \
    --export tomei-integration-test@example.invalid \
    > internal/installer/apt/testdata/tomei-integration-test.asc
rm -rf "$TMPGNUPG"
sha256sum internal/installer/apt/testdata/tomei-integration-test.asc
```

After regenerating, update the `keyHashSHA256` constant in
`tests/apt_repository_integration_test.go` to match the new SHA256.

### Why a real key (rather than random bytes)?

`gpg --dearmor` rejects input that is not a valid ASCII-armored OpenPGP
packet stream. The integration test exercises the real `gpg --dearmor`
subprocess, so the fixture must be a real (if throwaway) public key.
