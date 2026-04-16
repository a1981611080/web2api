package plugin

type Manifest struct {
	ID            string             `json:"id"`
	Name          string             `json:"name"`
	Version       string             `json:"version"`
	Description   string             `json:"description,omitempty"`
	Entry         string             `json:"entry,omitempty"`
	Capabilities  []string           `json:"capabilities,omitempty"`
	Models        []ModelSpec        `json:"models,omitempty"`
	AccountFields []AccountFieldSpec `json:"account_fields,omitempty"`
	Author        string             `json:"author,omitempty"`
}

type ModelSpec struct {
	ID          string   `json:"id"`
	Name        string   `json:"name,omitempty"`
	Modes       []string `json:"modes,omitempty"`
	Description string   `json:"description,omitempty"`
}

type AccountFieldSpec struct {
	Key         string `json:"key"`
	Label       string `json:"label,omitempty"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Secret      bool   `json:"secret,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
}

type Descriptor struct {
	Manifest Manifest `json:"manifest"`
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Error    string   `json:"error,omitempty"`
}
