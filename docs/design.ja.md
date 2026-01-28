# Toto: Technical Design & Implementation Detail

**Version:** 2.0 (Implementation Ready)  
**Target:** Go 1.25+, CUE v0.9+

---

## 1. プロジェクト構造 (Project Layout)

Goの標準的なディレクトリ構成に従い、責務を明確に分離します。

```
toto/
├── cmd/toto/
│   ├── main.go               # エントリポイント
│   ├── root.go               # ルートコマンド
│   ├── version.go            # version サブコマンド
│   ├── apply.go              # apply サブコマンド
│   └── diff.go               # diff サブコマンド
├── internal/
│   ├── config/               # CUE Loader & Schema Validation
│   ├── resource/             # Core Types (ID, Type, Spec, State)
│   ├── engine/               # DAG Scheduler & Execution Logic
│   ├── state/                # ACID State Manager (JSON + Locking)
│   ├── registry/             # Aqua YAML Parser & Checksum
│   └── provider/             # Resource Installers
│       ├── binary/           # Tool Installer (Symlink)
│       ├── runtime/          # Language Runtimes (Go/Node/Rust)
│       └── system/           # Apt/Brew Wrapper (Secure)
├── templates/
│   └── schema.cue            # Embedded CUE Schema
├── Makefile
├── .golangci.yml             # golangci-lint v2 設定
└── go.mod
```

---

## 2. コアデータ構造 (Resource Models)

Kubernetesの **Spec/Status パターン** を採用し、構成情報（Spec）と実状態（State）を厳密に区別します。

### internal/resource/types.go

```go
package resource

// ResourceID uniquely identifies a resource (e.g., "tool:ripgrep", "sys:docker")
type ResourceID string

// ResourceType defines the installer strategy
type ResourceType string

const (
    TypeTool    ResourceType = "tool"     // Aqua-based Binary
    TypeRuntime ResourceType = "runtime"  // Go, Node, Rustup
    TypeSysPkg  ResourceType = "sys_pkg"  // Apt, Brew
    TypeSysRepo ResourceType = "sys_repo" // Apt Repository
)

// Spec: CUEから読み込んだ「あるべき姿」
type Spec struct {
    ID          ResourceID
    Type        ResourceType
    Name        string
    Version     string
    Registry    string            // For tools (default: "standard")
    Deps        []ResourceID      // Dependency Graph (e.g., gopls -> go)
    SysRepoConf *SysRepoConfig    // For TypeSysRepo
}

// SysRepoConfig: Aptリポジトリの詳細設定
type SysRepoConfig struct {
    URL          string
    KeyURL       string
    KeyHash      string   // Optional pinning
    Distribution string
    Components   []string
    SignedBy     bool     // Enforce signed-by option
}

// State: ディスク上の「現在の状態」
type ResourceState struct {
    ID          ResourceID        `json:"id"`
    Version     string            `json:"version"`
    Digest      string            `json:"digest"`       // File SHA256 (Integrity)
    InstallPath string            `json:"install_path"` // Real path
    UpdatedAt   string            `json:"updated_at"`   // ISO8601
    
    // Taint: 依存関係更新による再ビルド要求フラグ
    TaintReason string            `json:"taint_reason,omitempty"` 
    
    // Metadata: ETagやSystem Packageの状態など
    Meta        map[string]string `json:"meta,omitempty"`
}
```

---

## 3. CUE スキーマ詳細 (Schema Definition)

CUEの**強力な検証機能**を活かした `schema.cue` です。これをバイナリに埋め込み (embed)、実行時に読み込みます。

### templates/schema.cue

```cue
package config

// --- Definitions ---

#Tool: {
    name:     string
    version:  string
    registry: string | *"standard"
    // 依存ランタイムの指定（Taint Check用）
    built_with?: "go" | "rust" | "python" | "node"
}

#Runtime: {
    // バージョン番号形式の簡易チェック
    version: string & =~"^[1-9]+\\.[1-9]+\\.[1-9]+$" | "stable" | "latest"
}

#SysRepo: {
    url:          string & =~"^https://" // HTTPS強制
    key_url?:     string & =~"^https://"
    key_hash?:    string & =~"^sha256:[a-f0-9]{64}$" // セキュリティ強化
    distribution: string | *""  // 空文字なら自動検出(lsb_release)
    components:   [...string] | *["main"]
    arch:         string | *"amd64"
}

// --- User Configuration ---

tools: [...#Tool]

runtimes: {
    go?:     #Runtime
    node?:   #Runtime
    rust?:   { channel: "stable" | "nightly" }
    python?: { version: string }
}

sys_repos: [Name=string]: #SysRepo
sys_packages: [...string]
sys_user_groups: [...string]

// バリデーションルール: バージョンが空文字でないこと
#Tool & { version: !="" }
```

---

## 4. エンジンロジック (Engine Logic)

Totoの中核となる **DAG（有向非巡回グラフ）スケジューラー** の設計です。

### 実行フェーズ (Pipeline)

1. **Load Phase:**
   - CUE Config をロード → `resource.Spec` のリストに変換
   - `state.json` をロード → `resource.State` マップに変換

2. **Diff Phase:**
   - Spec と State を比較し、必要なアクションを決定します
   - `toto diff` コマンドで差分を確認可能

3. **Graph Phase:**
   - 依存関係に基づき、インストール順序を決定します
   - **Layer 0:** Runtimes (Go, Rust), System Repos
   - **Layer 1:** Tools (gopls, cargo-tools), System Packages

4. **Apply Phase (Parallel):**
   - `errgroup` を使用し、同レイヤー内のタスクを並列実行します（最大5並列）
   - **重要:** 各タスク完了ごとに State を更新し、アトミックにファイル保存します

---

## 5. インストーラー詳細実装 (Providers)

### A. System Repository (Secure Apt)

Ubuntuの最新仕様に準拠した `providers/system/apt_repo.go` のロジックです。

- **非推奨:** `apt-key add` コマンドは一切実行しません

**実装フロー:**

1. **Key Fetch:** KeyURL から鍵をダウンロード
   - `http.Head` で ETag を確認し、Stateと比較（変更がなければスキップ）
   - ダウンロードした内容のハッシュを計算し、KeyHash (あれば) と検証

2. **Key Placement:** `/etc/apt/keyrings/toto-<name>.asc` に保存

3. **Source List Gen:** `/etc/apt/sources.list.d/toto-<name>.list` を作成

4. **Update Trigger:** ファイルに変更があった場合のみ、最後に `apt-get update` フラグを立てる

### B. Hybrid Runtimes

公式ツールへの委譲と、バイナリ直接配置を使い分けます。

#### Go (`providers/runtime/go.go`)

- **URL:** `https://go.dev/dl/go1.22.0.linux-amd64.tar.gz`
- **Action:** `~/.local/share/toto/runtimes/go` に展開
- **No Shim:** 環境変数 `GOROOT` の設定をユーザーのシェル設定 (`env.fish`等) に追記するよう促すのみ

#### Rust (`providers/runtime/rust.go`)

- **Check:** `rustup` コマンドが `~/.local/bin` にあるか確認。なければ `rustup-init` をダウンロードして実行
- **Action:** `rustup toolchain install stable` を `exec.Command` で叩く
- **State:** Rustのバージョンは `rustc --version` の出力をパースして State に記録

### C. Adopter (`toto scan`)

既存環境を取り込むための `internal/scanner` のロジックです。

- **Target:** `GOPATH/bin` 内のバイナリ
- **Algorithm:** バイナリの検出と照合
- **Mapping:** 取得したパスを Aqua Registry のデータベース（YAML）と照合し、一致すれば `tools` 設定のCUEコードを生成して標準出力します

---

## 6. 安全性担保の仕組み (Safety Mechanisms)

### Integrity Check (完全性)

- **Checksum Database:** Aqua Registryはチェックサムを持っています。ダウンロード時に必ず SHA256 を計算し、一致しなければ即座にエラーとし、ファイルシステムには書き込みません

### Atomic Swap (アトミック性)

ファイルの破損を防ぐため、すべての書き込み操作は以下の手順で行います：

1. 一時ディレクトリ (`.tmp/`) にダウンロード・展開
2. Symlink や Rename を使って一瞬で切り替え

### Taint Logic (汚染チェック)

ランタイム更新時のロジック詳細：

1. `go` ランタイムが `1.21` → `1.22` に更新される
2. State内の全ツールをスキャン
3. Specで `built_with: "go"` と定義されているツールの State に `TaintReason: "runtime_upgraded"` をセット
4. 次のループで、Taintされたツールが「再インストール対象」として検出される
