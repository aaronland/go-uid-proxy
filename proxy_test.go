package proxy

import (
	"context"
	"fmt"
	"github.com/aaronland/go-uid"
	"log"
	"os"
	"testing"
)

func TestProxyProvider(t *testing.T) {

	ctx := context.Background()

	uri := "proxy://?provider=random://&minimum=5"

	pr, err := uid.NewProvider(ctx, uri)

	if err != nil {
		t.Fatalf("Failed to create new provider for %s, %v", uri, err)
	}

	logger := log.New(os.Stdout, "", 0)
	pr.SetLogger(ctx, logger)

	for i := 0; i < 5; i++ {

		id, err := pr.UID(ctx)

		if err != nil {
			t.Fatalf("Failed to generate UID, %v", err)
		}

		fmt.Printf("%d %s\n", i, id)
	}
}
