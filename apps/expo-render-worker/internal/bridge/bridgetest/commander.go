// Package bridgetest holds the one fake bridge transport shared by the
// bridge, worker and httpapi tests.
package bridgetest

import "context"

// Commander adapts a function to bridge.Commander.
type Commander func(command string) (string, error)

func (f Commander) Execute(_ context.Context, command string) (string, error) { return f(command) }
