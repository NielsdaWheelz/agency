package daemon

import "slices"

const (
	InvocationLogKindRaw      = "raw"
	InvocationLogKindStderr   = "stderr"
	InvocationLogKindStream   = "stream"
	InvocationLogKindHooks    = "hooks"
	InvocationLogKindTerminal = "terminal"
)

var invocationLogKinds = []string{
	InvocationLogKindRaw,
	InvocationLogKindStderr,
	InvocationLogKindStream,
	InvocationLogKindHooks,
	InvocationLogKindTerminal,
}

// InvocationLogKinds returns the supported invocation log kinds in stable order.
func InvocationLogKinds() []string {
	return slices.Clone(invocationLogKinds)
}
