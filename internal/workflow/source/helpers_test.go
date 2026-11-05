package source

import "encoding/json"

func jsonString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

func jsonStrings(ss []string) (string, error) {
	b, err := json.Marshal(ss)
	return string(b), err
}
