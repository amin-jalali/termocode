package app

import "os"

// ReadConfigForTest returns config.json as it is on disk ("" when missing).
func ReadConfigForTest() string {
	p, err := settingsConfigPath()
	if err != nil {
		return ""
	}
	b, _ := os.ReadFile(p)
	return string(b)
}
