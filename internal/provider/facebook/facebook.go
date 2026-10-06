package facebook

import (
	"ghostview/internal/model"
	"ghostview/internal/provider"
)

func New() provider.SocialProvider { return provider.Unavailable{Name: model.Facebook} }
