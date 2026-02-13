# Module Publishing

The CUE module `tomei.terassyi.net@v0` is published to `ghcr.io/terassyi` as an OCI artifact. This module contains schema definitions and presets that users import in their manifests.

## Versioning

The module version is **independent of the tomei binary version**. Module versions follow semver and are tracked via git tags with the `tomei-cue-` prefix:

```
tomei-cue-v0.0.1   # CUE module version
v0.1.0              # tomei binary version (separate)
```

While in `@v0`, breaking changes are permitted.

### Version in `tomei cue init`

`tomei cue init` queries the OCI registry for the latest published tag and writes it to `deps` in `cue.mod/module.cue`:

```cue
deps: {
    "tomei.terassyi.net@v0": v: "v0.0.1"
}
```

This pins the exact version for reproducibility. To update to a newer module version, run `cue mod tidy`.

## Publish Flow

### Triggers

| Trigger | Action |
|---------|--------|
| `git push` tag `tomei-cue-v*` | Dry run only (`cue fmt --check` + `cue vet`) |
| `workflow_dispatch` with version | Verify tag exists, then publish to ghcr.io |
| `workflow_dispatch` with dry-run | Validate only, do not publish |

### Steps

1. Create and push a tag:
   ```bash
   git tag tomei-cue-v0.0.1
   git push origin tomei-cue-v0.0.1
   ```
2. CI runs dry-run validation automatically (formatting + schema validation)
3. Trigger `workflow_dispatch` on `Publish CUE Module` workflow with `version: v0.0.1` to publish

### Validation

The publish workflow runs the following checks before publishing:

- `cue fmt --check ./...` — formatting consistency
- `cue vet ./...` — schema validation

These checks also run in CI on every PR via the `cue-validate` job.

## Source Layout

```
cuemodule/
├── cue.mod/module.cue          # Module declaration (tomei.terassyi.net@v0)
├── schema/schema.cue           # Resource schema (#Tool, #Runtime, etc.)
├── presets/
│   ├── aqua/aqua.cue           # Aqua toolset preset
│   ├── go/go.cue               # Go runtime preset
│   └── rust/rust.cue           # Rust runtime preset
├── embed.go                    # go:embed for SchemaCUE (production)
├── embed_presets.go            # go:embed for PresetsFS (integration tests only)
└── schema_test.go              # Schema compilation tests
```

## Related Files

- `.github/workflows/publish-module.yaml` — publish workflow
- `.github/actions/cue-validate/` — CUE formatting and validation composite action
- `.github/actions/determine-version/` — version resolution composite action
- `.github/actions/verify-tag/` — git tag existence check composite action
