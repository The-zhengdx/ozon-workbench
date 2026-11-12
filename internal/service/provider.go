package service

import "github.com/google/wire"

// ProviderSet is the service layer for Google Wire.
var ProviderSet = wire.NewSet(NewShopService, NewCategoryService, NewTaskService)
