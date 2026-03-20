package main

import (
	"context"
	"log"

	"github.com/Five-Nines-io/terraform-provider-fivenines/internal/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
)

func main() {
	opts := providerserver.ServeOpts{
		Address: "registry.terraform.io/Five-Nines-io/fivenines",
	}

	err := providerserver.Serve(context.Background(), provider.New, opts)
	if err != nil {
		log.Fatal(err)
	}
}
