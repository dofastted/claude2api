package xai

// Model describes an xAI model in OpenAI-compatible /models shape.
type Model struct {
	ID          string `json:"id"`
	Object      string `json:"object"`
	Created     int64  `json:"created,omitempty"`
	OwnedBy     string `json:"owned_by"`
	DisplayName string `json:"display_name,omitempty"`
}

// defaultModels is the public /v1/models list for Grok chat groups.
// Keep this list small: chat/build/composer only. Image/video models are not
// exposed here because the OpenAI chat/completions gateway cannot serve them.
var defaultModels = []Model{
	{ID: "grok-4.5", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.5"},
	{ID: "grok-4.3", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.3"},
	{ID: "grok-build-0.1", Object: "model", OwnedBy: "xai", DisplayName: "Grok Build 0.1"},
	{ID: "grok-composer-2.5-fast", Object: "model", OwnedBy: "xai", DisplayName: "Grok Composer 2.5 Fast"},
	{ID: "grok-4.20-0309-reasoning", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.20 Reasoning"},
	{ID: "grok-4.20-0309-non-reasoning", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.20 Non Reasoning"},
	{ID: "grok-4.20-multi-agent-0309", Object: "model", OwnedBy: "xai", DisplayName: "Grok 4.20 Multi Agent"},
}

func DefaultModels() []Model {
	out := make([]Model, len(defaultModels))
	copy(out, defaultModels)
	return out
}

func DefaultModelIDs() []string {
	models := DefaultModels()
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	return ids
}

// DefaultModelMapping is used for request routing / IsModelSupported.
// Keep aliases here so clients can still send "grok" / "grok-latest", but
// /v1/models listing should prefer DefaultModelIDs() rather than mapping keys.
func DefaultModelMapping() map[string]string {
	mapping := make(map[string]string, len(defaultModels)+8)
	for _, model := range defaultModels {
		mapping[model.ID] = model.ID
	}
	mapping["grok"] = "grok-4.5"
	mapping["grok-latest"] = "grok-4.5"
	mapping["grok-4.5-latest"] = "grok-4.5"
	mapping["grok-build"] = "grok-build-0.1"
	mapping["grok-build-latest"] = "grok-4.5"
	mapping["grok-composer"] = "grok-composer-2.5-fast"
	mapping["composer-2.5"] = "grok-composer-2.5-fast"
	mapping["grok-4.20-reasoning"] = "grok-4.20-0309-reasoning"
	mapping["grok-4.20-non-reasoning"] = "grok-4.20-0309-non-reasoning"
	return mapping
}
