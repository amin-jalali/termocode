package containerhost

// containerFS is the workspace FS inside the container: cp tar streams
// for file contents, sh one-liners for the rest when the image has sh.
type containerFS struct{ h *Host }
