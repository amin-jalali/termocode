// Package containerhost is the workspace Host for a folder inside a
// container (ADR 0018). termocode stays local and reaches the container
// only through the engine CLI (docker or podman, any context):
//
//   - files: `cp` tar streams (no tool needed in the image), plus `sh -c`
//     one-liners for list/mkdir/rename/remove when the image has sh; the
//     paths are always positional arguments ("$1"), never part of the
//     script;
//   - a bind-mounted workspace on a local engine is read and written
//     straight on the local disk (mountfs);
//   - commands: `exec -i -w <dir> [-u user] <c> argv…`.
//
// Nothing is installed, copied or left in the target. The only writes
// are the user's own file edits, which keep the file's mode and owner.
package containerhost
