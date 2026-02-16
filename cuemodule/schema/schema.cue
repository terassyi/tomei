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
	archiveType?: "tar.gz" | "zip" | "raw"
	asset?:       string
}

#CommandSet: {
	install: [...string] & [_, ...]
	check?: [...string]
	remove?: [...string]
}

// RuntimeBootstrap extends CommandSet with version resolution support.
#RuntimeBootstrap: {
	install: [...string] & [_, ...]
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
		type:        "download" | "delegation"
		version:     string & !=""
		toolBinPath: string & !=""
		source?:     #DownloadSource
		bootstrap?:  #RuntimeBootstrap
		binaries?: [...string]
		binDir?:   string
		commands?: #CommandSet
		env?: {[string]: string}

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
	}
}

#Installer: {
	apiVersion: #APIVersion
	kind:       "Installer"
	metadata:   #Metadata
	spec: {
		type:        "download" | "delegation"
		runtimeRef?: string
		toolRef?:    string
		bootstrap?:  #CommandSet
		commands?:   #CommandSet

		// Conditional required fields
		if type == "delegation" {
			commands: #CommandSet
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
	spec: {
		installerRef?:  string
		runtimeRef?:    string
		repositoryRef?: string
		version?:       string
		enabled?:       bool
		source?:        #DownloadSource
		package?:       #Package
		args?: [...string]
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
			version?: string
			enabled?: bool
			source?:  #DownloadSource
			package?: #Package
			args?: [...string]
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

#SystemPackageRepository: {
	apiVersion: #APIVersion
	kind:       "SystemPackageRepository"
	metadata:   #Metadata
	spec: {
		installerRef: string & !=""
		source: {url: #HTTPSURL, ...}
	}
}

#SystemPackageSet: {
	apiVersion: #APIVersion
	kind:       "SystemPackageSet"
	metadata:   #Metadata
	spec: {
		installerRef:   string & !=""
		repositoryRef?: string
		packages: [...string]
	}
}

#Resource: #Runtime | #Installer | #InstallerRepository | #Tool | #ToolSet |
	#SystemInstaller | #SystemPackageRepository | #SystemPackageSet
