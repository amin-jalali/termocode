package webproto

import "strings"

// MOB-55: project attach kinds and ws on sends (plan §3.3, §3.7).
//
// An attach may name the project it comes from: WS is a workspace key
// (16 lower-case hex digits, never a path); "" is the hub's own project.
// A chat.send sent with a ws in its req frame goes through the door to
// that project's hub (MOB-50); the hub resolves only attaches of its own
// project, so the page stamps every attach with the project it was
// picked in, and a send that mixes projects fails as a whole.
//
// An issue or pr attach names a number: "12" or "#12"; a pr may also be
// a GitLab merge request "!12". A project attach names a project of the
// projects topic by its id.

// checkProject is the MOB-55 part of Attach.check: the ws field, and the
// number of an issue or pr.
func (a *Attach) checkProject() error {
	if !validWSField(a.WS) {
		return invalid("bad attach ws")
	}
	switch a.Kind {
	case AttachIssue, AttachPR:
		if _, ok := AttachNumber(a.Kind, a.ID); !ok {
			return invalid("bad %s attach", a.Kind)
		}
	}
	return nil
}

// maxAttachDigits keeps an issue or pr number in an int32.
const maxAttachDigits = 9

// AttachNumber is the number of an issue or pr attach's ID: "12", "#12",
// and for a pr also "!12" (a GitLab merge request). No leading zero.
func AttachNumber(kind, id string) (int, bool) {
	switch {
	case strings.HasPrefix(id, "#"):
		id = id[1:]
	case strings.HasPrefix(id, "!") && kind == AttachPR:
		id = id[1:]
	}
	if id == "" || len(id) > maxAttachDigits || id[0] == '0' {
		return 0, false
	}
	n := 0
	for i := 0; i < len(id); i++ {
		c := id[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

// AttachIsMR reports whether a pr attach's ID names a GitLab merge request.
func AttachIsMR(id string) bool { return strings.HasPrefix(id, "!") }
