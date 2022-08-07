package proxy

import (
	"context"
	"fmt"
	"github.com/aaronland/go-uid"
	"testing"
)

func TestProxyProvider(t *testing.T) {

	ctx := context.Background()

	uri := "proxy://?provider=random://"

	pr, err := uid.NewProvider(ctx, uri)

	if err != nil {
		t.Fatalf("Failed to create new provider for %s, %v", uri, err)
	}

	for i := 0; i < 5; i++ {

		u, err := pr.UID(ctx)

		if err != nil {
			t.Fatalf("Failed to generate UID, %v", err)
		}

		fmt.Println(u)
	}
}
