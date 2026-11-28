package legacy

import (
	"log/slog"

	"github.com/z5labs/atomic-go/remote/project"
	"github.com/z5labs/atomic-go/remote/serverurl"
)

// NewWithMaxBody returns a Client like [New]'s, with the default
// User-Agent, that reads at most maxBody bytes of a body, whether sent as
// is or inflated from a zstd Content-Encoding, so a test can reach the
// limit without a 2 GiB body.
func NewWithMaxBody(u serverurl.Project, doer project.Doer, logger *slog.Logger, clientVersion string, maxBody int64) *Client {
	return newClient(u, doer, logger, clientVersion, "atomic-"+clientVersion, maxBody)
}
