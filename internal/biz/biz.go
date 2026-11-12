package biz

import "github.com/google/wire"

// ProviderSet is the biz layer for Google Wire.
var ProviderSet = wire.NewSet(NewShopUsecase, NewCategoryUsecase)
