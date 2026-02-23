package vwt

const (
	PatchRefPrefix    = "refs/vwt/patches/"
	SnapshotRefPrefix = "refs/vwt/snapshots/"
)

func PatchRef(id string) string {
	return PatchRefPrefix + id
}

func SnapshotRef(id string) string {
	return SnapshotRefPrefix + id
}
