package proxy

import (
	"context"
	"fmt"
	"testing"

	"github.com/aaronland/go-uid"
)

func TestProxyProvider(t *testing.T) {

	ctx := context.Background()

	uri := "proxy://?provider=random://&minimum=5"

	pr, err := uid.NewProvider(ctx, uri)

	if err != nil {
		t.Fatalf("Failed to create new provider for %s, %v", uri, err)
	}

	for i := 0; i < 5; i++ {

		id, err := pr.UID(ctx)

		if err != nil {
			t.Fatalf("Failed to generate UID, %v", err)
		}

		fmt.Printf("%d %s\n", i, id)
	}
}
