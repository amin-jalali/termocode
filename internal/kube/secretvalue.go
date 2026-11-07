package kube

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

// secretKeyRE is what Kubernetes allows as a Secret data key; names are
// DNS subdomains. Both are checked before they go into the template, so
// nothing else can be printed.
var (
	secretKeyRE  = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)
	secretNameRE = regexp.MustCompile(`^[a-z0-9]([-.a-z0-9]*[a-z0-9])?$`)
)

// SecretValueCommand is the kubectl call SecretValue makes (for the card).
func SecretValueCommand(name, key string) string {
	return "kubectl get secret " + name + " -o go-template='" + secretValueTemplate(key) + "'"
}

func secretValueTemplate(key string) string {
	return `{{index .data "` + key + `"}}`
}

// SecretValue reads one value of a Secret, on demand only (CTX-40): the
// view shows key names, and only an answered card calls this. The value
// is never cached and never given to the AI.
func (c Client) SecretValue(ctx context.Context, name, key string) (string, error) {
	if !secretNameRE.MatchString(name) || !secretKeyRE.MatchString(key) {
		return "", errors.New("not a secret name and key")
	}
	out, err := c.capture(ctx, append([]string{"get", "secret", name, "-o", "go-template=" + secretValueTemplate(key)}, c.scope(true)...)...)
	if err != nil {
		return "", err
	}
	enc := strings.TrimSpace(out)
	if enc == "<no value>" {
		return "", errors.New("secret " + name + " has no key " + key)
	}
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", errors.New("the value is not base64")
	}
	return string(b), nil
}
