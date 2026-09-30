package benchmark

import "net/url"

// Where reports are shared: a pull request against this repository.
const (
	RepositoryURL = "https://github.com/K0IN/self"
	Branch        = "main"
	Dir           = "benchmarks"

	// maxURL is a safe link length: GitHub refuses much longer addresses.
	maxURL = 7500
)

// UploadPath is where a report belongs in the repository.
func UploadPath(r Report) string { return Dir + "/" + Filename(r) }

// NewFileURL returns a GitHub link that opens the "new file" page with the
// report already filled in. Saving it there creates a fork and a pull request
// for anyone without write access. ok is false when the report is too big for
// a link.
func NewFileURL(r Report) (link string, ok bool) {
	for _, encode := range []func(Report) ([]byte, error){Encode, EncodeCompact} {
		data, err := encode(r)
		if err != nil {
			continue
		}
		u := RepositoryURL + "/new/" + Branch + "?filename=" + url.QueryEscape(UploadPath(r)) + "&value=" + url.QueryEscape(string(data))
		if len(u) <= maxURL {
			return u, true
		}
	}
	return "", false
}

// UploadPageURL opens GitHub's "upload files" page in the reports folder, for
// dragging the report file in. It works for reports of any size.
func UploadPageURL() string { return RepositoryURL + "/upload/" + Branch + "/" + Dir }
