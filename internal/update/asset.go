package update

import "fmt"

// ChecksumsName is the release file with the SHA-256 of every tarball
// (sha256sum format), written by .github/workflows/release.yml.
const ChecksumsName = "checksums.txt"

// releaseTargets are the GOOS/GOARCH pairs with a release build (the
// matrix of .github/workflows/release.yml).
var releaseTargets = map[string]bool{
	"linux/amd64": true, "linux/arm64": true,
	"darwin/amd64": true, "darwin/arm64": true,
	"android/arm64": true,
}

// BinaryDir is the folder inside a release tarball that holds termocode
// (scripts/build-release.sh).
func BinaryDir(tag, goos, goarch string) string {
	return fmt.Sprintf("termocode-%s-%s-%s", tag, goos, goarch)
}

// TarballName is the release tarball of tag for goos/goarch.
func TarballName(tag, goos, goarch string) string {
	return BinaryDir(tag, goos, goarch) + ".tar.gz"
}

// PickAsset finds this system's tarball and checksums.txt in rel.
func PickAsset(rel Release, goos, goarch string) (tarball, sums Asset, err error) {
	if !releaseTargets[goos+"/"+goarch] {
		return Asset{}, Asset{}, fmt.Errorf("no release build for %s/%s", goos, goarch)
	}
	want := TarballName(rel.Tag, goos, goarch)
	var haveTar, haveSums bool
	for _, a := range rel.Assets {
		switch a.Name {
		case want:
			tarball, haveTar = a, true
		case ChecksumsName:
			sums, haveSums = a, true
		}
	}
	switch {
	case !haveTar:
		return Asset{}, Asset{}, fmt.Errorf("release %s has no %s", rel.Tag, want)
	case !haveSums:
		return Asset{}, Asset{}, fmt.Errorf("release %s has no %s", rel.Tag, ChecksumsName)
	}
	return tarball, sums, nil
}
