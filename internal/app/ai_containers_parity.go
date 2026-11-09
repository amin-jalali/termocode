package app

// Containers, servers and Kubernetes parity: which AI tool does each
// action of the GUI (the palette ids containers-*, kubernetes-* and
// services-deploy-git, the web ops containers.*, kube.* and
// db.add_container). TestAIContainersEveryGUIActionHasAnAITool fails when
// an action is on neither list. A value is "tool", or "tool op a / b"
// (also "action" or "kind"): every named op must be one the tool takes.

// aiContainersParity maps a GUI action to the tool (and op) that does it.
var aiContainersParity = map[string]string{}

// aiContainersNotForAI are the GUI actions the AI does not get, each with why.
var aiContainersNotForAI = map[string]string{}
