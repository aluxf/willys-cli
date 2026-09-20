package app

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

func searchOne(ctx context.Context, c *Client, query string, page, limit int) ([]any, error) {
	v, err := c.Get(ctx, "/search/clean", url.Values{"q": {query}, "page": {strconv.Itoa(page)}, "size": {strconv.Itoa(limit)}})
	if err != nil {
		return nil, err
	}
	out := []any{}
	for _, raw := range list(obj(v)["results"]) {
		out = append(out, catalogProductView(obj(raw), false))
	}
	return out, nil
}

func (a *App) Search(ctx context.Context, c *Client, p *Profile, query string, page, limit int) (any, error) {
	terms := strings.Split(query, ",")
	for i, term := range terms {
		terms[i] = strings.TrimSpace(term)
		if terms[i] == "" {
			return nil, errors.New("each comma-separated search term must contain text")
		}
	}
	if len(terms) == 1 {
		return searchOne(ctx, c, terms[0], page, limit)
	}
	results := make([]any, len(terms))
	jobs := make(chan int, len(terms))
	for i := range terms {
		jobs <- i
	}
	close(jobs)
	var workers sync.WaitGroup
	for n := 0; n < min(4, len(terms)); n++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			// Each worker owns its HTTP client and cookie jar.
			worker, err := NewClient(p)
			if err == nil {
				worker.Base = c.Base
			}
			for i := range jobs {
				group := Object{"query": terms[i]}
				if err != nil {
					group["error"] = err.Error()
				} else {
					products, e := searchOne(ctx, worker, terms[i], page, limit)
					if e != nil {
						group["error"] = e.Error()
					} else {
						group["products"] = products
					}
				}
				results[i] = group
			}
		}()
	}
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return results, err
	}
	for _, group := range results {
		if obj(group)["error"] != nil {
			return results, failure("partial_failure", "Some searches failed. Successful groups remain in the output.", 3, nil)
		}
	}
	return results, nil
}
