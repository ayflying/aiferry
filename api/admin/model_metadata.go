package admin

import "github.com/yunloli/aiferry/internal/logic/modelmetadata"

// Metadata is replaced as an override document, not patched. Null clears it.
type ModelMetadataInput struct {
	PublicName string                  `json:"publicName"`
	Metadata   *modelmetadata.Metadata `json:"metadata"`
}
