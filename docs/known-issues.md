# Known Issues

## aqua ToolSet のインストール失敗

### hash のみの checksum ファイル形式が未サポート

- **対象パッケージ**: `starship/starship` (v1.24.2)
- **エラー**: `failed to install Tool starship: failed to verify checksum: unknown or unsupported checksum file format`
- **原因**: starship のリリースでは `.sha256` ファイルにハッシュ値のみが記載されている（例: `56b9ff41...`）。一般的な `<hash>  <filename>` 形式ではないため、tomei の checksum 検証が失敗する
- **参考**: `curl -sL https://github.com/starship/starship/releases/download/v1.24.2/starship-aarch64-unknown-linux-musl.tar.gz.sha256` → `56b9ff412bbf374d29b99e5ac09a849124cb37a0a13121e8470df32de53c1ea6`

## Runtime の toolBinPath が必須

- **状況**: Runtime リソースで `toolBinPath` が必須だが、Lua のようにランタイムのみ提供しツールインストーラとしては機能しないケースがある
- **エラー**: `toolBinPath is required`
- **期待**: `commands` が未定義なら `toolBinPath` を省略可能にする
- **回避策**: `binDir` と同じ値を `toolBinPath` に設定

## delegation パターンの resolveVersion が組み込みリゾルバを使わない

- **状況**: delegation パターンの `bootstrap.resolveVersion` で `http-text:` や `github-release:` を指定すると、組み込みリゾルバ（`resolve.Resolver`）を経由せず `ExecuteCapture()` でシェル実行される
- **エラー**: `sh: 1: Syntax error: word unexpected (expecting ")")` — 正規表現の括弧がシェルに解釈される
- **箇所**: `internal/installer/runtime/installer.go` L392-404（`installDelegation()`）
- **原因**: download パターン（L329 `resolveVersionValue()`）では共有リゾルバ経由で正しく処理されるが、delegation パターンでは直接シェル実行される
- **回避策**: `resolveVersion` を使わず `spec.version` にバージョンを直書き

## delegation パターンの bootstrap.check で {{.Version}} が展開されない

- **状況**: `bootstrap.check` コマンドに空の `command.Vars{}` が渡されるため、`{{.Version}}` テンプレートが展開されない
- **箇所**: `internal/installer/runtime/installer.go` L440
- **原因**: `bootstrap.install` では `command.Vars{Version: resolvedVersion}` が正しく渡されるが、check には空の Vars が渡される
- **回避策**: CUE の `\(spec.version)` で静的にバージョンを埋め込む

## delegation パターンで binaries のシンボリックリンクが作成されない

- **状況**: delegation パターンの `installDelegation()` では `binDir` と `binaries` が設定されていても `createSymlinks()` が呼ばれない。download パターン（L142）では呼ばれている
- **箇所**: `internal/installer/runtime/installer.go` L379-476（`installDelegation()`）
- **原因**: delegation パターンは state を返すだけでシンボリックリンク作成をスキップしている
- **期待**: delegation パターンでも `binDir` と `binaries` が設定されていれば `createSymlinks()` を呼ぶ
- **回避策**: bootstrap.install スクリプト内で手動で `ln -sf` する
