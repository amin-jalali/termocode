package update

import "testing"

func relWith(names ...string) Release {
	r := Release{Tag: "v0.2.0"}
	for _, n := range names {
		r.Assets = append(r.Assets, Asset{Name: n, Size: 10, URL: "https://example.test/" + n})
	}
	return r
}

func TestPickAsset(t *testing.T) {
	r := relWith("termocode-v0.2.0-linux-arm64.tar.gz", "termocode-v0.2.0-linux-amd64.tar.gz",
		"termocode-v0.2.0-darwin-arm64.tar.gz", "checksums.txt")
	tarball, sums, err := PickAsset(r, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if tarball.Name != "termocode-v0.2.0-linux-amd64.tar.gz" || sums.Name != "checksums.txt" {
		t.Fatalf("got %q %q", tarball.Name, sums.Name)
	}
	if got := BinaryDir(r.Tag, "linux", "amd64"); got != "termocode-v0.2.0-linux-amd64" {
		t.Fatalf("BinaryDir = %q", got)
	}
}

func TestPickAssetMissing(t *testing.T) {
	if _, _, err := PickAsset(relWith("checksums.txt"), "linux", "amd64"); err == nil {
		t.Error("no tarball for this system: want an error")
	}
	if _, _, err := PickAsset(relWith("termocode-v0.2.0-linux-amd64.tar.gz"), "linux", "amd64"); err == nil {
		t.Error("no checksums.txt: want an error")
	}
	if _, _, err := PickAsset(relWith("termocode-v0.2.0-windows-amd64.tar.gz", "checksums.txt"), "windows", "amd64"); err == nil {
		t.Error("windows has no release build: want an error")
	}
}
