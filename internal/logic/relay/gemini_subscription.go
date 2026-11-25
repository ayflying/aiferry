package relay

import (
	"io"

	"github.com/yunloli/aiferry/internal/logic/geminisub"
)

func buildGeminiSubscriptionBody(body []byte, candidate Candidate) ([]byte, error) {
	return geminisub.BuildRequest(body, candidate.UpstreamName, candidate.ProjectID)
}

// Convert at the wire boundary so the existing relay still handles usage, client
// disconnects, guarded output, and restoration of sensitive data.
func geminiSubscriptionStream(src io.Reader, candidate Candidate) *io.PipeReader {
	reader, writer := io.Pipe()
	go func() {
		_, err := geminisub.ConvertStream(writer, src, candidate.UpstreamName, "chatcmpl-"+newRequestID())
		_ = writer.CloseWithError(err)
	}()
	return reader
}

func aggregateGeminiSubscription(src io.Reader, candidate Candidate) ([]byte, error) {
	body, _, err := geminisub.ConvertResponse(src, candidate.UpstreamName, "chatcmpl-"+newRequestID())
	return body, err
}
