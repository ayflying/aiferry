package relay

import (
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
)

func (s *Service) writeBufferedStreamResponse(writer http.ResponseWriter, result attemptResult) error {
	copyResponseHeaders(writer.Header(), result.headers)
	writer.WriteHeader(result.status)
	flusher, _ := writer.(http.Flusher)
	for _, output := range result.streamOutput {
		if _, err := writer.Write(output); err != nil {
			return gerror.Wrap(err, "write buffered upstream stream")
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
	return nil
}
