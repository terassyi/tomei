package download

import "github.com/terassyi/tomei/internal/resource"

// BuiltinInstaller is the builtin "download" installer definition.
var BuiltinInstaller = &resource.Installer{
	BaseResource: resource.BaseResource{
		APIVersion:   resource.GroupVersion,
		ResourceKind: resource.KindInstaller,
		Metadata:     resource.Metadata{Name: "download"},
	},
	InstallerSpec: &resource.InstallerSpec{
		Type: resource.InstallTypeDownload,
	},
}

// BuiltinAquaInstaller is the builtin "aqua" installer definition.
// Aqua uses the download pattern with aqua-registry for package resolution.
var BuiltinAquaInstaller = &resource.Installer{
	BaseResource: resource.BaseResource{
		APIVersion:   resource.GroupVersion,
		ResourceKind: resource.KindInstaller,
		Metadata:     resource.Metadata{Name: "aqua"},
	},
	InstallerSpec: &resource.InstallerSpec{
		Type: resource.InstallTypeDownload,
	},
}
