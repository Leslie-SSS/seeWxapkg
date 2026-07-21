package pkg

type PageIR struct {
	Path     string                 `json:"path"`
	JSONPath string                 `json:"jsonPath,omitempty"`
	Config   map[string]interface{} `json:"config,omitempty"`
	// UsingComponents is deliberately page-scoped. Page-level component
	// declarations must never be promoted to app.json's global declaration.
	UsingComponents map[string]string `json:"usingComponents,omitempty"`
	ScriptPath      string            `json:"scriptPath,omitempty"`
	StylePath       string            `json:"stylePath,omitempty"`
	TemplatePath    string            `json:"templatePath,omitempty"`
	RuntimeChunk    string            `json:"runtimeChunk,omitempty"`
	RuntimeHTML     string            `json:"runtimeHtml,omitempty"`
}

type ComponentIR struct {
	Path string `json:"path"`
}

type TemplateIR struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Path        string `json:"path,omitempty"`
	Content     string `json:"content,omitempty"`
	Source      string `json:"source,omitempty"`
	IsGenerated bool   `json:"isGenerated,omitempty"`
}

type StyleIR struct {
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
	Source      string `json:"source,omitempty"`
	IsGenerated bool   `json:"isGenerated,omitempty"`
}

type ScriptIR struct {
	Path        string `json:"path"`
	Content     string `json:"content,omitempty"`
	Source      string `json:"source,omitempty"`
	EntryKind   string `json:"entryKind,omitempty"`
	IsGenerated bool   `json:"isGenerated,omitempty"`
}

type AssetIR struct {
	Path   string `json:"path"`
	Source string `json:"source,omitempty"`
}

type NormalizedPackage struct {
	Profile     PackageProfile `json:"profile"`
	Manifest    ManifestIR     `json:"manifest"`
	Pages       []PageIR       `json:"pages,omitempty"`
	Components  []ComponentIR  `json:"components,omitempty"`
	Scripts     []ScriptIR     `json:"scripts,omitempty"`
	Styles      []StyleIR      `json:"styles,omitempty"`
	Templates   []TemplateIR   `json:"templates,omitempty"`
	Assets      []AssetIR      `json:"assets,omitempty"`
	Diagnostics []Diagnostic   `json:"diagnostics,omitempty"`
}
