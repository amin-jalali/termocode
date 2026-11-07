package session

// EnvDoorIdle tells a UI started headless by the door after how many
// minutes with no use it stops by itself. The UI reads it once at start
// and removes it, so nothing it starts (terminals, agents) inherits it.
const EnvDoorIdle = "TERMOCODE_DOOR_IDLE_MINUTES"

// The door's idle range in minutes (web_project_idle_minutes).
const (
	DoorIdleMin = 5
	DoorIdleMax = 240
)
