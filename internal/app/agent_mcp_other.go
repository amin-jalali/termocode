//go:build !unix

package app

func agentMCPFilePathOS(string, string) (string, error) { return "", errMCPServeOS }
