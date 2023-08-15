package proxy

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"strconv"
	"sync"
	"time"

	aa_log "github.com/aaronland/go-log/v2"
	"github.com/aaronland/go-pool/v2"
	"github.com/aaronland/go-uid"
)

const PROXY_SCHEME string = "proxy"

func init() {
	ctx := context.Background()
	uid.RegisterProvider(ctx, PROXY_SCHEME, NewProxyProvider)
}

type ProxyProvider struct {
	uid.Provider
	provider uid.Provider
	logger   *log.Logger
	workers  int
	minimum  int
	pool     pool.Pool
	refill   chan bool
}

func NewProxyProvider(ctx context.Context, uri string) (uid.Provider, error) {

	workers := 10
	minimum := 0

	u, err := url.Parse(uri)

	if err != nil {
		return nil, fmt.Errorf("Failed to parse URI, %w", err)
	}

	q := u.Query()

	source_uri := q.Get("provider")

	if source_uri == "" {
		return nil, fmt.Errorf("Missing ?provider parameter")
	}

	pool_uri := q.Get("pool")

	if pool_uri == "" {
		pool_uri = "memory://"
	}

	str_workers := q.Get("workers")

	if str_workers != "" {

		v, err := strconv.Atoi(str_workers)

		if err != nil {
			return nil, fmt.Errorf("Invalid ?workers parameter")
		}

		workers = v
	}

	str_minimum := q.Get("minimum")

	if str_minimum != "" {

		v, err := strconv.Atoi(str_minimum)

		if err != nil {
			return nil, fmt.Errorf("Invalid ?minimum parameter")
		}

		minimum = v
	}

	source_pr, err := uid.NewProvider(ctx, source_uri)

	if err != nil {
		return nil, fmt.Errorf("Failed to create provider, %w", err)
	}

	pl, err := pool.NewPool(ctx, pool_uri)

	if err != nil {
		return nil, fmt.Errorf("Failed to create pool, %w", err)
	}

	logger := log.New(io.Discard, "", 0)

	refill := make(chan bool)

	pr := &ProxyProvider{
		provider: source_pr,
		pool:     pl,
		logger:   logger,
		workers:  workers,
		minimum:  minimum,
		refill:   refill,
	}

	go pr.refillPool(ctx)
	go pr.status(ctx)
	go pr.monitor(ctx)

	if minimum > 0 {
		refill <- true
	}

	return pr, nil
}

func (pr *ProxyProvider) UID(ctx context.Context, args ...interface{}) (uid.UID, error) {

	if pr.pool.Length(ctx) == 0 {

		aa_log.Warning(pr.logger, "pool length is 0 so fetching integer from source")

		go pr.refillPool(ctx)
		return pr.provider.UID(ctx, args...)
	}

	v, ok := pr.pool.Pop(ctx)

	if !ok {

		aa_log.Info(pr.logger, "failed to pop UID!")

		go pr.refillPool(ctx)
		return pr.provider.UID(ctx, args...)
	}

	return v.(uid.UID), nil	
}

func (pr *ProxyProvider) SetLogger(ctx context.Context, logger *log.Logger) error {
	pr.logger = logger
	return nil
}

func (pr *ProxyProvider) status(ctx context.Context) {

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			aa_log.Debug(pr.logger, "Pool length: %d", pr.pool.Length(ctx))
		}
	}
}

func (pr *ProxyProvider) monitor(ctx context.Context) {

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
			if pr.pool.Length(ctx) < int64(pr.minimum) {
				go pr.refillPool(ctx)
			}
		}

	}
}

func (pr *ProxyProvider) refillPool(ctx context.Context) {

	if pr.minimum == 0 {
		pr.refill <- true
		return
	}

	// Remember there is a fixed size work queue of allowable times to try
	// and refill the pool simultaneously. First, we block until a slot opens
	// up.

	aa_log.Debug(pr.logger, "Refill pool. Waiting for work queue.")

	<-pr.refill

	aa_log.Debug(pr.logger, "Refill pool. Starting.")

	t1 := time.Now()

	// Figure out how many integers we need to get *at this moment* which when
	// the service is under heavy load is a misleading number at best. It might
	// be worth adjusting this by a factor of (n) depending on the current load.
	// But that also means tracking what we think the current load means so we
	// aren't going to do that now...

	todo := int64(pr.minimum) - pr.pool.Length(ctx)

	workers := pr.workers

	if workers == 0 {
		workers = int(pr.minimum / 2)
	}

	if workers == 0 {
		workers = 1
	}

	// Now we're going to set up two simultaneous queues. One (the work group) is
	// just there to keep track of all the requests for new integers we need to
	// make. The second (the throttle) is there to make sure we don't exhaust all
	// the filehandles or network connections.

	th := make(chan bool, workers)

	for i := 0; i < workers; i++ {
		th <- true
	}

	wg := new(sync.WaitGroup)

	aa_log.Info(pr.logger, "refill poll w/ %d integers and %d workers", todo, workers)

	success := 0
	failed := 0

	for j := 0; int64(j) < todo; j++ {

		// Wait for the throttle to open a slot. Also record whether
		// the operation was successful.

		rsp := <-th

		if rsp == true {
			success += 1
		} else {
			failed += 1
		}

		// First check that we still actually need to keep fetching integers

		if pr.pool.Length(ctx) >= int64(pr.minimum) {
			aa_log.Info(pr.logger, "pool is full (%d) stopping after %d iterations", pr.pool.Length(ctx), j)
			break
		}

		// Standard work group stuff

		wg.Add(1)

		// Sudo make me a sandwitch. Note the part where we ping the throttle with
		// the return value at the end both to signal an available slot and to record
		// whether the integer harvesting was successful.

		go func(ctx context.Context, pr *ProxyProvider) {
			defer wg.Done()
			th <- pr.addToPool(ctx)
		}(ctx, pr)
	}

	// More standard work group stuff

	wg.Wait()

	// Again note the way we are freeing a spot in the refill queue

	pr.refill <- true

	t2 := time.Since(t1)
	aa_log.Debug(pr.logger, "Time to refill the pool with %d integers (success: %d failed: %d): %v (pool length is now %d)", todo, success, failed, t2, pr.pool.Length(ctx))

}

func (pr *ProxyProvider) addToPool(ctx context.Context) bool {

	i, err := pr.provider.UID(ctx)

	if err != nil {
		aa_log.Error(pr.logger, "Failed to create new UID to add to pool, %v", err)
		return false
	}

	pr.pool.Push(ctx, i)
	return true
}
