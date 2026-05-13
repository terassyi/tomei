package schema

// Schema definitions for tomei resources.
// Import: import "tomei.terassyi.net/schema"

// ==========================================================================
// Common definitions
// ==========================================================================

#APIVersion: "tomei.terassyi.net/v1beta1"

#Metadata: {
	name:         string & =~"^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$"
	description?: string
	labels?: {[string]: string}
}

#HTTPSURL: string & =~"^https://"

#Checksum: {
	value?:       string & =~"^sha256:[a-f0-9]{64}$"
	url?:         #HTTPSURL
	filePattern?: string
}

#DownloadSource: {
	url:          #HTTPSURL
	checksum?:    #Checksum
	archiveType?: "tar.gz" | "tar.xz" | "zip" | "raw" | "pkg"
	asset?:       string
}

#CommandSet: {
	install: [...string] & [_, ...]
	check?: [...string]
	remove?: [...string]
}

// RuntimeBootstrap extends CommandSet with update and version resolution support.
#RuntimeBootstrap: {
	install: [...string] & [_, ...]
	update?: [...string]
	check?: [...string]
	remove?: [...string]
	resolveVersion?: [...string]
}

// ToolCommandSet extends CommandSet with update and version resolution for self-managed tools.
#ToolCommandSet: {
	install: [...string] & [_, ...]
	update?: [...string]
	check?: [...string]
	remove?: [...string]
	resolveVersion?: [...string]
}

// Package accepts both string ("owner/repo" or module path) and object form.
#Package: string | {
	owner?: string
	repo?:  string
	name?:  string
}

// ==========================================================================
// Resource definitions
// ==========================================================================

#Runtime: {
	apiVersion: #APIVersion
	kind:       "Runtime"
	metadata:   #Metadata
	platform?: {
		os:   string
		arch: string
	}
	spec: {
		type:         "download" | "delegation"
		version:      string & !=""
		toolBinPath?: string & !=""
		source?:      #DownloadSource
		bootstrap?:   #RuntimeBootstrap
		binaries?: [...string]
		binDir?:   string
		commands?: #CommandSet
		env?: {[string]: string}
		taintOnUpgrade?: bool
		resolveVersion?: [...string]

		// Conditional required fields
		if type == "download" {
			source: #DownloadSource
		}
		if type == "delegation" {
			bootstrap: #RuntimeBootstrap & {
				install: [...string] & [_, ...]
				check: [...string] & [_, ...]
			}
		}
		if commands != _|_ {
			toolBinPath: string & !=""
		}
	}
}

#Installer: {
	apiVersion: #APIVersion
	kind:       "Installer"
	metadata:   #Metadata
	platform?: {
		os:   string
		arch: string
	}
	spec: {
		type:        "download" | "delegation"
		runtimeRef?: string
		// toolRef references the primary tool for PATH injection in delegation commands.
		toolRef?: string
		// dependsOn declares additional tool dependencies for DAG ordering only.
		// Unlike toolRef, these tools are NOT added to PATH.
		dependsOn?: [...string]
		bootstrap?: #CommandSet
		commands?:  #CommandSet

		// Conditional required fields
		if type == "delegation" {
			commands: #CommandSet
			binDir?:  string & =~"^(~/|/)"
		}
	}
}

#InstallerRepository: {
	apiVersion: #APIVersion
	kind:       "InstallerRepository"
	metadata:   #Metadata
	spec: {
		installerRef: string & !=""
		source: {
			type:      "delegation" | "git"
			url?:      #HTTPSURL
			commands?: #CommandSet

			// Conditional required fields
			if type == "delegation" {
				commands: #CommandSet
			}
			if type == "git" {
				url: #HTTPSURL
			}
		}
	}
}

#Tool: {
	apiVersion: #APIVersion
	kind:       "Tool"
	metadata:   #Metadata
	platform?: {
		os:   string
		arch: string
	}
	spec: {
		installerRef?:  string
		runtimeRef?:    string
		repositoryRef?: string
		version?:       string
		enabled?:       bool
		source?:        #DownloadSource
		package?:       #Package
		commands?:      #ToolCommandSet
		binaryName?:    string & =~"^[a-zA-Z0-9][a-zA-Z0-9._-]*$"
		args?: [...string]
		privileged?: bool
	}
}

#ToolSet: {
	apiVersion: #APIVersion
	kind:       "ToolSet"
	metadata:   #Metadata
	spec: {
		installerRef?:  string
		runtimeRef?:    string
		repositoryRef?: string
		tools: {[string]: {
			version?:    string
			enabled?:    bool
			source?:     #DownloadSource
			package?:    #Package
			binaryName?: string & =~"^[a-zA-Z0-9][a-zA-Z0-9._-]*$"
			args?: [...string]
			privileged?: bool
		}}
	}
}

#SystemInstaller: {
	apiVersion: #APIVersion
	kind:       "SystemInstaller"
	metadata:   #Metadata
	spec: {
		pattern:    string & !=""
		privileged: bool
		commands: {...}
	}
}

// #SystemPackageRepository is the public entry point for declaring a
// third-party system-package repository. Today it is an alias for the
// single implemented arm (#AptPackageRepository, keyed by spec.installerRef
// = "apt") which supplies its own spec.apt source block. When additional
// installer arms (dnf, apk, pacman) land per #213 this will become a real
// CUE disjunction (#AptPackageRepository | #DnfPackageRepository | ...)
// keyed by spec.installerRef — each arm expresses its own configuration
// shape with static CUE validation and existing arms stay unaffected.
// Manifests already written against the "apt" arm round-trip unchanged
// through that future transition.
#SystemPackageRepository: #AptPackageRepository

// #AptPackageRepository declares a third-party APT repository as a
// triple of (1) where to fetch packages from, (2) which GPG key signs
// the repo metadata, and (3) how that line is emitted in
// /etc/apt/sources.list.d. spec.apt maps to the canonical one-line APT
// sources.list format:
//
//	deb [<options>] <url> <suite> <components...>
#AptPackageRepository: {
	apiVersion: #APIVersion
	kind:       "SystemPackageRepository"
	metadata:   #Metadata
	spec: {
		// installerRef binds this repository to a SystemInstaller and
		// selects the matching source-block arm. For #AptPackageRepository
		// it is the literal "apt".
		installerRef: "apt"
		// apt holds the APT-specific source configuration. Required when
		// installerRef is "apt".
		apt: #AptSource
	}
}

// #AptSource holds the source configuration for an APT third-party
// repository. The fields below map directly to the canonical one-line
// sources.list format described on #AptPackageRepository.
#AptSource: {
	// url is the repository base URL (e.g.
	// "https://download.docker.com/linux/ubuntu"). HTTPS only.
	url: #HTTPSURL
	// keyUrl is the HTTPS URL of the armored GPG public key that signs
	// this repository's Release / InRelease files.
	keyUrl: #HTTPSURL
	// keyHash is the SHA256 of the armored key in
	// "sha256:<64-lowercase-hex>" form. Required: HTTPS alone protects
	// only against passive MITM, not against CDN or upstream-mirror
	// compromise.
	keyHash: string & =~"^sha256:[0-9a-f]{64}$"
	// suite is the distribution release identifier (e.g. "jammy",
	// "noble", "bookworm"). Single-suite by design: multi-suite repos
	// (used only by distribution mirrors, never by third-party vendors)
	// are out of scope. Flat repositories — APT's `deb URL ./` syntax
	// where the suite token is the literal `./` — are explicitly
	// unsupported; AptSource.Validate rejects every flat-style marker
	// (`/`, `./`, `.`, `..`) as defense-in-depth alongside this regex.
	suite: string & =~"^[^./]" & !="/"
	// components are the pool components emitted as space-separated
	// trailing tokens (e.g. ["stable"], ["main", "contrib", "non-free"]).
	// At least one required.
	components: [...string & !=""] & [_, ...]
	// options is the bracketed key=value pairs APT understands, restricted
	// to the keys the installer actually needs. signed-by is auto-derived
	// to /usr/share/keyrings/<metadata.name>.gpg and must NOT be set here.
	// trusted=yes / allow-insecure / allow-weak / allow-downgrade-to-insecure
	// are intentionally excluded — they disable or weaken signature
	// verification, which is the protection KeyHash + signed-by together
	// provide. This CUE constraint mirrors resource.AllowedAptOptions; the
	// drift-detector unit test in internal/installer/apt pins them in sync.
	options?: {[=~"^(arch|target|by-hash|pdiffs|check-valid-until|lang)$"]: string}
}

#SystemPackageSet: {
	apiVersion: #APIVersion
	kind:       "SystemPackageSet"
	metadata:   #Metadata
	spec: {
		installerRef:   string & !=""
		repositoryRef?: string & !=""
		packages: [...string & =~"^\\S+$"] & [_, ...]
	}
}

#SystemPackage: {
	apiVersion: #APIVersion
	kind:       "SystemPackage"
	metadata:   #Metadata
	spec: {
		installerRef:   string & !=""
		repositoryRef?: string & !=""
		package:        string & =~"^\\S+$"
	}
}

#Resource: #Runtime | #Installer | #InstallerRepository | #Tool | #ToolSet |
	#SystemInstaller | #SystemPackageRepository | #SystemPackageSet | #SystemPackage
