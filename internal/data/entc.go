//go:build ignore

package main

import (
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
)

func main() {
	err := entc.Generate("./schema", &gen.Config{
		Target:  "./ent",
		Package: "ozon-workbench/internal/data/ent",
		Features: []gen.Feature{
			gen.FeatureUpsert,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
