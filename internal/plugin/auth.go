package plugin

import (
	"context"
	"errors"
)

var ErrUnauthorized = errors.New("plugin unauthorized")

type AuthContext struct {
	PluginID string
	Token    string
}

type Authenticator interface {
	Authenticate(ctx context.Context, auth AuthContext) error
}

type StaticTokenAuthenticator struct {
	tokens map[string]string
}

func NewStaticTokenAuthenticator(tokens map[string]string) *StaticTokenAuthenticator {
	cloned := make(map[string]string, len(tokens))
	for pluginID, token := range tokens {
		cloned[pluginID] = token
	}
	return &StaticTokenAuthenticator{tokens: cloned}
}

func (a *StaticTokenAuthenticator) Authenticate(_ context.Context, auth AuthContext) error {
	expected, ok := a.tokens[auth.PluginID]
	if !ok {
		return ErrUnauthorized
	}
	if expected == "" || expected != auth.Token {
		return ErrUnauthorized
	}
	return nil
}
