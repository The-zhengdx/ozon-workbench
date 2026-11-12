package data

import (
	"github.com/google/wire"

	"ozon-workbench/internal/biz"
)

// ProviderSet is the data layer for Google Wire.
var ProviderSet = wire.NewSet(NewData, NewShopRepo, NewCategoryRepo, NewTaskRepo, NewListingRepo, NewListingGateway)

// NewListingGateway adapts the Seller client to the listing port.
func NewListingGateway(c *OzonClient) biz.ListingGateway { return c }
