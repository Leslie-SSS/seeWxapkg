package pkg

type PackageProfile struct {
	IsEncrypted      bool   `json:"isEncrypted"`
	IsStandardWxapkg bool   `json:"isStandardWxapkg"`
	IsWeChat4xLike   bool   `json:"isWeChat4xLike"`
	IsSubPackage     bool   `json:"isSubPackage"`
	IsGamePackage    bool   `json:"isGamePackage"`
	HasAppConfigJSON bool   `json:"hasAppConfigJSON"`
	HasAppServiceJS  bool   `json:"hasAppServiceJS"`
	HasWorkersJS     bool   `json:"hasWorkersJS"`
	HasPageFrameHTML bool   `json:"hasPageFrameHTML"`
	HasPageFrameJS   bool   `json:"hasPageFrameJS"`
	HasAppWxssJS     bool   `json:"hasAppWxssJS"`
	IndexFileCount   int    `json:"indexFileCount"`
	SuspectedVariant string `json:"suspectedVariant"`
}

func (p PackageProfile) NeedsAppID() bool {
	return p.IsEncrypted
}

func (p PackageProfile) SupportsNativeRecovery() bool {
	return p.HasAppConfigJSON || p.HasAppServiceJS || p.HasPageFrameHTML || p.HasPageFrameJS || p.HasAppWxssJS
}

func (p PackageProfile) SupportsFallbackRecovery() bool {
	return p.IsStandardWxapkg || p.IsWeChat4xLike
}
