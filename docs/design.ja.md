# Toto 設計ドキュメント v2

**Version:** 2.0  
**Date:** 2025-01-28

---

## 1. 概要

Toto は宣言的な開発環境セットアップツール。Kubernetes の Spec/State reconciliation パターンを採用し、ローカル環境のツール、ランタイム、システムパッケージを管理する。

### 設計哲学

- **宣言的管理**: 望む状態を定義し、toto が実現する
- **サンドボックス不使用**: 仮想化やコンテナを使わず、実環境を直接セットアップ
- **CUE による型安全**: スキーマ検証と柔軟な設定
- **シンプルさ**: nix ほど複雑にせず、既存ツール (apt, go install) を活用

---

## 2. インストーラパターン

toto は2つのインストーラパターンをサポートする。

### 2.1 Delegation Pattern

外部コマンドに処理を委譲するパターン。

```
例:
├── apt install <package>
├── brew install <package>
├── go install <package>
├── cargo install <package>
└── npm install -g <package>
```

toto は「何をインストールするか」を指示し、実際の処理は外部ツールが行う。

### 2.2 Download Pattern

toto が直接ダウンロードして配置するパターン。

```
例:
├── GitHub Release からバイナリ取得
├── go.dev から Go tarball 取得
└── Aqua registry 形式のツール
```

チェックサム検証、展開、symlink 作成まで toto が担当。

---

## 3. リソース定義

### 3.1 権限による分類

```
User 権限 (toto apply):
├── Installer  - ユーザー権限インストーラ定義 (aqua, go, cargo, npm, brew)
├── Runtime    - 言語ランタイム (Go, Rust, Node)
├── Tool       - 単体ツール
└── ToolSet    - 複数ツールのセット

System 権限 (sudo toto apply --system):
├── SystemInstaller         - パッケージマネージャ定義 (apt)
├── SystemPackageRepository - サードパーティリポジトリ
└── SystemPackageSet        - パッケージセット
```

### 3.2 各リソースの構造

#### SystemInstaller

パッケージマネージャの定義。apt は toto が builtin として CUE マニフェストを提供。

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "SystemInstaller"
metadata: name: "apt"
spec: {
    pattern: "delegation"
    privileged: true
    commands: {
        install: {command: "apt-get", verb: "install -y"}
        remove: {command: "apt-get", verb: "remove -y"}
        check: {command: "dpkg", verb: "-l"}
        update: "apt-get update"
    }
}
```

#### SystemPackageRepository

サードパーティリポジトリの定義。

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "SystemPackageRepository"
metadata: name: "docker"
spec: {
    installerRef: "apt"
    source: {
        url: "https://download.docker.com/linux/ubuntu"
        keyUrl: "https://download.docker.com/linux/ubuntu/gpg"
        keyHash: "sha256:..."  // optional
        options: {
            distribution: "noble"
            components: "stable"
            arch: "amd64"
        }
    }
}
```

#### SystemPackageSet

パッケージのセット。

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "SystemPackageSet"
metadata: name: "docker"
spec: {
    installerRef: "apt"
    repositoryRef: "docker"  // optional
    packages: ["docker-ce", "docker-ce-cli", "containerd.io"]
}
```

#### Installer

ユーザー権限のインストーラ定義。toto が builtin で aqua, go, cargo, npm, brew を提供。

```cue
// Download Pattern (aqua)
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "aqua"
spec: {
    pattern: "download"
}

// Delegation Pattern (go install)
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "go"
spec: {
    pattern: "delegation"
    runtimeRef: "go"
    commands: {
        install: "go install {{.Package}}@{{.Version}}"
        check: "go version -m {{.BinPath}}"
        remove: "rm {{.BinPath}}"
    }
}

// Delegation Pattern (brew) - Runtime 不要、bootstrap で自己インストール
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "brew"
spec: {
    pattern: "delegation"
    bootstrap: {
        install: "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
        check: "command -v brew"
        remove: "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/uninstall.sh)\""
    }
    commands: {
        install: "brew install {{.Package}}"
        check: "brew list {{.Package}}"
        remove: "brew uninstall {{.Package}}"
    }
}
```

#### Runtime

言語ランタイム。

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
    installerRef: "download"
    version: "1.25.1"
    source: {
        url: "https://go.dev/dl/go{{.Version}}.{{.OS}}-{{.Arch}}.tar.gz"
        checksum: "sha256:..."
        archiveType: "tar.gz"
    }
    binaries: ["go", "gofmt"]
    toolBinPath: "~/go/bin"
    env: {
        GOROOT: "{{.InstallPath}}"
    }
}
```

#### Tool (単体)

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    version: "14.0.0"
    source: {
        url: "https://github.com/BurntSushi/ripgrep/releases/..."
        checksum: "sha256:..."
        archiveType: "tar.gz"
    }
}
```

#### ToolSet

複数ツールのセット。冗長さを解消。

```cue
// Download Pattern
apiVersion: "toto.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "cli-tools"
spec: {
    installerRef: "aqua"
    tools: {
        ripgrep: { version: "14.0.0" }
        fd: { version: "9.0.0" }
        jq: { version: "1.7" }
    }
}

// Runtime Delegation Pattern
apiVersion: "toto.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "go-tools"
spec: {
    installerRef: "go"
    runtimeRef: "go"
    tools: {
        gopls: { package: "golang.org/x/tools/gopls" }
        staticcheck: { package: "honnef.co/go/tools/cmd/staticcheck" }
    }
}
```

---

## 4. コマンド体系

### 4.1 権限の分離

```bash
# User 権限 (Runtime, Tool)
toto apply

# System 権限 (SystemPackage*)
sudo toto apply --system
```

実行順序: `sudo toto apply --system` → `toto apply`

### 4.2 コマンド一覧

```bash
toto validate    # CUE 構文 + 循環参照チェック
toto plan        # validate + 実行計画表示
toto apply       # plan + 実行
toto doctor      # 未管理ツール検知、競合検知
toto adopt       # 未管理ツールを管理下に追加
toto version     # バージョン表示
```

---

## 5. State 管理

### 5.1 ファイル構成

```
User State:
~/.local/share/toto/
├── state.lock  (PID を書き込み、flock 用)
└── state.json  (状態データ)

System State:
/var/lib/toto/
├── state.lock
└── state.json
```

### 5.2 ロック機構

- **flock (advisory lock)** を使用
- state.lock に対して flock を取得
- 取得成功時に自身の PID を書き込む
- toto 同士の同時実行を防止
- 手動編集 (vim 等) は防げない (advisory lock の性質)

### 5.3 書き込みフロー

```
1. state.lock を flock (TryLock)
2. 失敗 → PID を読んで「PID 12345 が実行中」エラー
3. 成功 → 自身の PID を書き込む
4. state.json を読む
5. state.json.tmp に書く
6. rename(state.json.tmp, state.json) ← アトミック
7. state.lock を unlock
```

### 5.4 state.json 構造

#### User State

```json
{
  "version": "1",
  "runtimes": {
    "go": {
      "installerRef": "download",
      "version": "1.25.1",
      "digest": "sha256:abc123...",
      "installPath": "~/.local/share/toto/runtimes/go/1.25.1",
      "binaries": ["go", "gofmt"],
      "toolBinPath": "~/go/bin",
      "env": {
        "GOROOT": "~/.local/share/toto/runtimes/go/1.25.1"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  },
  "tools": {
    "ripgrep": {
      "installerRef": "aqua",
      "version": "14.0.0",
      "digest": "sha256:def456...",
      "installPath": "~/.local/share/toto/tools/ripgrep/14.0.0",
      "binPath": "~/.local/bin/rg",
      "source": {
        "url": "https://github.com/BurntSushi/ripgrep/releases/...",
        "archiveType": "tar.gz"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    },
    "gopls": {
      "installerRef": "go",
      "runtimeRef": "go",
      "version": "0.16.0",
      "digest": "sha256:ghi789...",
      "installPath": "~/go/bin/gopls",
      "binPath": "~/.local/bin/gopls",
      "package": "golang.org/x/tools/gopls",
      "taintReason": "",
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  }
}
```

#### System State

```json
{
  "version": "1",
  "systemInstallers": {
    "apt": {
      "version": "1",
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  },
  "systemPackageRepositories": {
    "docker": {
      "installerRef": "apt",
      "source": {
        "url": "https://download.docker.com/linux/ubuntu",
        "keyUrl": "https://download.docker.com/linux/ubuntu/gpg",
        "keyDigest": "sha256:..."
      },
      "installedFiles": [
        "/etc/apt/keyrings/toto-docker.asc",
        "/etc/apt/sources.list.d/toto-docker.list"
      ],
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  },
  "systemPackages": {
    "docker": {
      "installerRef": "apt",
      "repositoryRef": "docker",
      "packages": ["docker-ce", "docker-ce-cli", "containerd.io"],
      "installedVersions": {
        "docker-ce": "24.0.0",
        "docker-ce-cli": "24.0.0",
        "containerd.io": "1.6.0"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  }
}
```

---

## 6. 依存グラフ

### 6.1 依存関係の種類

```
明示的に書く:
├── runtimeRef: Tool → Runtime
├── repositoryRef: SystemPackage → SystemPackageRepository
├── installerRef: 各リソース → Installer
└── deps: Installer → パッケージ名 (best effort)
```

### 6.2 循環参照の検出

**アルゴリズム: DFS + 訪問状態管理**

```
状態:
├── unvisited (未訪問)
├── visiting (訪問中 = 現在のパス上)
└── visited (訪問完了)

手順:
1. 全ノードを unvisited に
2. 各ノードから DFS 開始
3. visiting のノードに再度到達 → 循環検出
4. DFS 完了 → visited に
```

`toto validate` で事前に検出し、エラーメッセージで循環パスを表示。

### 6.3 実行順序の決定

**アルゴリズム: トポロジカルソート**

```
1. 全リソースからグラフ構築
2. 入次数 (依存される数) を計算
3. 入次数 0 のノードをキューに入れる
4. キューから取り出し、レイヤーに追加
5. そのノードに依存していたノードの入次数を減らす
6. 入次数 0 になったらキューに追加
7. 繰り返し
```

**結果: レイヤー分け**

```
Layer 0: [go, rust, ripgrep, fd]
Layer 1: [gopls, staticcheck, rust-analyzer]
```

同一レイヤー内は並列実行可能 (errgroup, max 5)。

### 6.4 実行順序

```
System 権限 (sudo toto apply --system):
  Layer 0: SystemPackageRepository
  Layer 1: SystemPackageSet

User 権限 (toto apply):
  Layer 0: Runtime, Tool (runtimeRef なし)
  Layer 1: Tool (runtimeRef あり)
```

---

## 7. Taint Logic

Runtime 更新時に依存する Tool を再インストールする仕組み。

### 7.1 フロー

```
1. Runtime (go) が 1.25.1 → 1.26.0 に更新
2. runtimeRef: "go" を持つ Tool を検索
3. 該当 Tool に taintReason: "runtime_upgraded" をセット
4. 次の apply で再インストール
```

### 7.2 対象

```
go 更新 → go install でインストールした Tool
rust 更新 → cargo install でインストールした Tool
node 更新 → npm install -g でインストールした Tool
```

---

## 8. toto doctor

未管理ツールの検出と競合検知。

### 8.1 検知対象

```
Runtime 別:
├── go:   ~/go/bin/ (GOBIN)
├── rust: ~/.cargo/bin/
└── node: ~/.npm-global/bin/

共通:
└── ~/.local/bin/ 内の未管理ファイル
```

### 8.2 出力例

```
$ toto doctor

[go] ~/go/bin/
  gopls        unmanaged
  staticcheck  unmanaged

[rust] ~/.cargo/bin/
  cargo-edit   unmanaged

[Conflicts]
  gopls: found in both ~/.local/bin (toto) and ~/go/bin (unmanaged)
         PATH resolves to: ~/go/bin/gopls

[Suggestions]
  toto adopt gopls staticcheck cargo-edit
```

---

## 9. CUE スキーマ設計

### 9.1 基本構造 (K8s スタイル)

```cue
#Resource: {
    apiVersion: "toto.terassyi.net/v1beta1"
    kind: string
    metadata: {
        name: string
        labels?: [string]: string
    }
    spec: {...}
}
```

### 9.2 デフォルト値と enabled フラグ

```cue
#Tool: {
    version: string
    enabled: bool | *true  // デフォルト true
    ...
}
```

### 9.3 環境変数の注入

```cue
// toto が自動注入
_env: {
    os: "linux" | "darwin"
    arch: "amd64" | "arm64"
    headless: bool
}
```

### 9.4 条件分岐 (方式 A)

```cue
tools: {
    ripgrep: { version: "14.0.0" }
    
    if _env.os == "darwin" {
        pbpaste: {}
    }
    
    if _env.headless {
        vscode: enabled: false
    }
}
```

### 9.5 Overlay (方式 B)

```
base/tools.cue
overlays/darwin/tools.cue
overlays/headless/tools.cue
```

CUE の同一パッケージ自動マージ機能を活用。toto が環境に応じてファイルを選択してロード。

### 9.6 除外の表現

```cue
// enabled: false で無効化
tools: vscode: enabled: false
```

---

## 10. 対象環境

```
OS:
├── linux
└── darwin
(Windows は対象外)

Arch:
├── amd64
└── arm64

Mode:
├── headless (サーバー、CI)
└── desktop (GUI あり)
```

---

## 11. 実装フェーズ

### Phase 1: 基盤

```
├── internal/resource/ (types, action)
├── internal/state/ (state.json 読み書き、flock)
├── internal/config/ (CUE ローダー基盤)
└── CLI 骨格 (cobra: apply, validate, version)
```

### Phase 2: User 権限の最小セット

```
├── Tool (Download Pattern, aqua 形式)
├── toto apply でツールインストール
├── ~/.local/bin への symlink
└── state.json の更新
```

### Phase 3: Runtime

```
├── Runtime (Go のみ最初)
├── Tool の Runtime Delegation (go install)
├── Taint Logic
└── toto doctor (未管理ツール検出)
```

### Phase 4: System 権限

```
├── SystemInstaller (apt builtin)
├── SystemPackageRepository
├── SystemPackageSet
└── toto apply --system
```

### Phase 5: 拡張

```
├── ToolSet, overlay
├── toto adopt
├── 他の Runtime (Rust, Node)
├── CUE プリセット
```

---

## 12. ディレクトリ構成

```
~/.config/toto/           # ユーザー設定 (CUE ファイル)
├── base/
│   ├── tools.cue
│   └── runtimes.cue
├── overlays/
│   ├── darwin/
│   ├── linux/
│   ├── headless/
│   └── desktop/
└── system/
    ├── repos.cue
    └── packages.cue

~/.local/share/toto/      # User State & データ
├── state.lock
├── state.json
├── runtimes/
│   └── go/1.25.1/
└── tools/
    └── ripgrep/14.0.0/

~/.local/bin/             # symlink 配置先

/var/lib/toto/            # System State
├── state.lock
└── state.json
```

---

## 13. セキュリティ考慮事項

- ダウンロード時は必ずチェックサム検証
- HTTPS のみ許可 (CUE スキーマで強制)
- apt-key add は使用禁止、/etc/apt/keyrings/ + signed-by を使用
- シェルインジェクション防止: exec.Command で明示的引数
- アトミック書き込み: tmp → rename で破損防止

---

## 13.1 ロギング

`log/slog` を使用した構造化ログで、人間が読みやすい形式で出力する。

### ログレベル

| レベル | 用途 | 例 |
|--------|------|-----|
| Debug | 詳細なデバッグ情報 | HTTP レスポンスステータス、ファイルサイズ |
| Info | 正常な操作の開始/完了 | ダウンロード開始、チェックサム検証完了 |
| Warn | 復旧可能な問題、スキップ | チェックサムファイルが見つからない |
| Error | 機能に影響する失敗 | ダウンロード失敗、検証失敗 |

### 実装例

```go
import "log/slog"

// Debug: 詳細なデバッグ情報
slog.Debug("http response received", "status", resp.StatusCode, "contentLength", resp.ContentLength)
slog.Debug("trying checksum algorithm", "algorithm", alg, "url", checksumURL)

// Info: 操作の開始/完了
slog.Info("downloading file", "url", url, "dest", destPath)
slog.Info("checksum verified", "algorithm", alg)

// Warn: 復旧可能な問題
slog.Warn("checksum file not found, skipping verification", "url", checksumURL)

// Error: 失敗 (通常は error も返す)
slog.Error("failed to download", "url", url, "error", err)
```

### ガイドライン

- 構造化されたキー/バリューペアでコンテキストを提供
- メッセージは簡潔で人間が読める形式に
- Debug: 開発時やトラブルシューティングに有用な詳細情報
- Info: 操作の開始と完了を対で記録
- Warn: 重要な決定やスキップした処理
- Error: 機能に影響する失敗（通常は error も返す）

---

## 14. 将来の設計検討事項

### 14.1 InstallerRepository

aqua registry のように、ツールのメタデータ（URL パターン、アーキテクチャ別ファイル名など）を提供するリポジトリ。SystemPackageRepository と同様の役割。

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "InstallerRepository"
metadata: name: "aqua-registry"
spec: {
    installerRef: "aqua"
    source: {
        type: "git"  // or "local"
        url: "https://github.com/aquaproj/aqua-registry"
        // branch: "main"
        // localPath: "/path/to/local/registry"
    }
}
```

これにより Tool 定義がシンプルになる:

```cue
// InstallerRepository があれば source 不要
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    repositoryRef: "aqua-registry"  // optional, default を使う場合は省略可
    version: "14.1.1"
    // source 不要 - registry から自動解決
}
```

### 14.2 認証・トークン

GitHub API のレートリミット対策、プライベートリポジトリへのアクセス、認証付きレジストリ対応。

**Option A: Installer に持たせる**

```cue
kind: "Installer"
metadata: name: "aqua"
spec: {
    pattern: "download"
    auth: {
        tokenEnvVar: "GITHUB_TOKEN"  // 環境変数から取得
        // or tokenFile: "~/.config/toto/github-token"
    }
}
```

**Option B: 別リソース (Credential)**

```cue
kind: "Credential"
metadata: name: "github"
spec: {
    type: "token"
    envVar: "GITHUB_TOKEN"
    // or file: "~/.config/toto/github-token"
    // or secretRef: "..." (外部シークレット管理との連携)
}

kind: "Installer"
metadata: name: "aqua"
spec: {
    pattern: "download"
    credentialRef: "github"
}
```

**検討ポイント:**
- シンプルさ vs 再利用性
- 複数 Installer で同じ認証を使う場合
- シークレット管理のベストプラクティス
