package image

type OsInfo struct {
	ID         string `json:"id"`
	VersionID  string `json:"versionId"`
	PrettyName string `json:"prettyName"`
	HomeURL    string `json:"homeUrl"`
}
