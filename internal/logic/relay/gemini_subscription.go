package relay

import (
	"io"
	"net/http"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/yunloli/aiferry/internal/logic/channel"
	"github.com/yunloli/aiferry/internal/logic/geminisub"
)

const geminiSubscriptionURL = channel.GeminiSubscriptionBaseURL + channel.GeminiSubscriptionStreamPath

func applyGeminiSubscriptionToken(req *http.Request, token string) error {
	if req.Method != http.MethodPost || req.URL.String() != geminiSubscriptionURL || token == "" {
		return gerror.New("Gemini 订阅凭据只允许发送到指定文本生成端点")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

func buildGeminiSubscriptionBody(body []byte, model, project string) ([]byte, error) {
	return geminisub.BuildRequest(body, model, project)
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
