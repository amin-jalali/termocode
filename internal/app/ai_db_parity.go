package app

// DB parity: which AI tool does each database action of the GUI (the
// palette ids db-*, redis-*, containers-open-databases and the web ops
// db.*, redis.*, containers.open_db). TestAIDBEveryGUIActionHasAnAITool
// fails when an action is on neither list.

// aiDBParity maps a GUI action to the tool (and op) that does it.
var aiDBParity = map[string]string{}

// aiDBNotForAI are the GUI actions the AI does not get, each with why.
var aiDBNotForAI = map[string]string{}
