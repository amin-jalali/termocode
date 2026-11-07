package containers

import "strings"

// normRepo is the repository of an image reference without its tag or
// digest; Docker Hub names lose their docker.io/ and library/ prefixes.
func normRepo(ref string) string {
	if i := strings.IndexByte(ref, '@'); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndexByte(ref, ':'); i > strings.LastIndexByte(ref, '/') {
		ref = ref[:i]
	}
	ref = strings.TrimPrefix(ref, "docker.io/")
	ref = strings.TrimPrefix(ref, "index.docker.io/")
	return strings.TrimPrefix(ref, "library/")
}

// ImageHasUpdate compares the registry digest remote of image ref with
// the local repo digests of that image (CTX-36, a notice only). known is
// false when there is no local digest for the repo or remote is not a
// digest: then no notice is shown.
func ImageHasUpdate(repoDigests []string, ref, remote string) (update, known bool) {
	if !digestRe.MatchString(remote) {
		return false, false
	}
	repo := normRepo(ref)
	for _, rd := range repoDigests {
		name, dig, ok := strings.Cut(rd, "@")
		if !ok || normRepo(name) != repo {
			continue
		}
		if dig == remote {
			return false, true
		}
		known = true
	}
	return known, known
}
