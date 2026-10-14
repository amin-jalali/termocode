// Package transfer moves files between the workspace (often on a remote
// machine, reached over SSH) and the user's own computer, through the
// terminal itself: no extra port, no second connection.
//
// Locally (no SSH, IsRemote) no protocol is needed: a download is a copy
// into the Downloads folder (DownloadsDir, CopyInto) and an upload a copy
// of the files picked in the desktop's chooser (PickChooser).
//
// Over SSH, protocols, best first, picked per terminal (Detect):
//
//   - kitty file transfer protocol (OSC 5113, the one `kitten transfer`
//     uses): both directions. kitty asks the user to allow every session.
//     The terminal answers on stdin, so a download (KittySend) reads the
//     replies through Input, a filter in front of Bubble Tea's input.
//     An upload (KittyReceive) runs while the app has handed the terminal
//     over (tea.Exec), so it reads stdin itself.
//   - iTerm2 OSC 1337: File=…;inline=0 saves a file in the Downloads
//     folder (ITermFile, one way, no answer); RequestUpload=format=tgz
//     opens a file picker and the terminal types back a base64 tar.gz
//     (ReadITermUpload).
//   - trzsz (`tsz` / `trz` on the server, with trzsz-ssh or a trzsz
//     terminal plugin on the client): the app runs those programs.
//   - Otherwise a ready scp command (ScpDownload / ScpUpload).
//
// Folders travel as one .tar.gz (TarGz / ExtractTarGz). Uploads never
// write outside the target folder and never replace a file: a name that
// is taken gets " (1)" (UniqueName).
package transfer
