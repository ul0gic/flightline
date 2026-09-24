package config

func hydratePreviewChecksums(previews *PreviewsSpec, stateDir string) {
	if previews == nil {
		return
	}
	for _, types := range previews.Locales {
		hydratePreviewTypeChecksums(types, stateDir)
	}
}

func hydratePreviewTypeChecksums(types map[string][]PreviewFile, stateDir string) {
	for kind, files := range types {
		for i := range files {
			if checksum := assetChecksum(stateDir, files[i].Path); checksum != "" {
				files[i].SourceFileChecksum = checksum
			}
		}
		types[kind] = files
	}
}
