package data

import "github.com/google/wire"

// ProviderSet is the data layer for Google Wire.
var ProviderSet = wire.NewSet(NewData, NewShopRepo, NewCategoryRepo, NewTaskRepo)
