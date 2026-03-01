package vwt

const (
	// WorkspaceRefPrefix stores the current virtual workspace state.
	// The ref points at a commit whose tree is the workspace view.
	WorkspaceRefPrefix = "refs/vwt/workspaces/"
)

func WorkspaceRef(name string) string {
	return WorkspaceRefPrefix + name
}
