package server

import "github.com/google/wire"

// ProviderSet is the server layer for Google Wire.
var ProviderSet = wire.NewSet(NewHTTPServer)
