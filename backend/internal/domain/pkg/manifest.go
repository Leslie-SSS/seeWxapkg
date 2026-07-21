package pkg

type SubPackageIR struct {
	Root    string   `json:"root"`
	Pages   []string `json:"pages,omitempty"`
	Plugins []string `json:"plugins,omitempty"`
}

type ManifestIR struct {
	Pages                          []string               `json:"pages,omitempty"`
	SubPackages                    []SubPackageIR         `json:"subPackages,omitempty"`
	Window                         map[string]interface{} `json:"window,omitempty"`
	TabBar                         map[string]interface{} `json:"tabBar,omitempty"`
	NetworkTimeout                 map[string]interface{} `json:"networkTimeout,omitempty"`
	UsingComponents                map[string]string      `json:"usingComponents,omitempty"`
	Plugins                        map[string]interface{} `json:"plugins,omitempty"`
	Permission                     map[string]interface{} `json:"permission,omitempty"`
	Renderer                       *string                `json:"renderer,omitempty"`
	Style                          *string                `json:"style,omitempty"`
	SitemapLocation                *string                `json:"sitemapLocation,omitempty"`
	RequiredBackgroundModes        []string               `json:"requiredBackgroundModes,omitempty"`
	PreloadRule                    map[string]interface{} `json:"preloadRule,omitempty"`
	Workers                        *string                `json:"workers,omitempty"`
	Debug                          *bool                  `json:"debug,omitempty"`
	NavigateToMiniProgramAppIDList []string               `json:"navigateToMiniProgramAppIdList,omitempty"`
	// Original keeps the authoritative app.json document. Recovery starts from
	// this document and overlays normalized fields so fields unknown to this
	// version of the service are not silently discarded.
	Original   map[string]interface{} `json:"original,omitempty"`
	Sources    map[string]string      `json:"sources,omitempty"`
	Confidence map[string]string      `json:"confidence,omitempty"`
}

func NewManifestIR() ManifestIR {
	return ManifestIR{
		Window:          map[string]interface{}{},
		TabBar:          map[string]interface{}{},
		NetworkTimeout:  map[string]interface{}{},
		UsingComponents: map[string]string{},
		Plugins:         map[string]interface{}{},
		Permission:      map[string]interface{}{},
		PreloadRule:     map[string]interface{}{},
		Original:        map[string]interface{}{},
		Sources:         map[string]string{},
		Confidence:      map[string]string{},
	}
}
