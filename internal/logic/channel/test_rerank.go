package channel

// rerankTestRequest keeps the standard rerank JSON body separate from chat-only
// prompt-cache controls and forced-stream adaptations.
type rerankTestRequest struct {
	Model           string   `json:"model"`
	Query           string   `json:"query"`
	Documents       []string `json:"documents"`
	TopN            int      `json:"top_n"`
	ReturnDocuments bool     `json:"return_documents"`
}
