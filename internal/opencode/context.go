package opencode

import (
	"context"
	"net/http"
	"time"
)

type providerCatalog struct {
	Providers []struct {
		ID     string `json:"id"`
		Models map[string]struct {
			Limit struct {
				Context int64 `json:"context"`
			} `json:"limit"`
		} `json:"models"`
	} `json:"providers"`
}

const limitRefetchAfter = time.Minute

func (a *Adapter) contextLimit(t *thread, providerID, modelID string) int64 {
	if providerID == "" || modelID == "" || t.api == nil {
		return 0
	}
	key := providerID + "/" + modelID
	a.limitsMu.Lock()
	if limit, ok := a.limits[t.serverKey][key]; ok {
		a.limitsMu.Unlock()
		return limit
	}
	wait := a.limitsFetching[t.serverKey]
	if wait == nil {
		if time.Since(a.limitsAt[t.serverKey]) < limitRefetchAfter {
			a.limitsMu.Unlock()
			return 0
		}
		wait = a.startLimitsFetchLocked(t.serverKey, t.api)
	}
	a.limitsMu.Unlock()

	select {
	case <-wait:
	case <-time.After(2 * time.Second):
	}

	a.limitsMu.Lock()
	limit := a.limits[t.serverKey][key]
	a.limitsMu.Unlock()
	return limit
}

func (a *Adapter) ensureLimits(serverKey string, api *httpClient) {
	if api == nil {
		return
	}
	a.limitsMu.Lock()
	if len(a.limits[serverKey]) > 0 || a.limitsFetching[serverKey] != nil || time.Since(a.limitsAt[serverKey]) < limitRefetchAfter {
		a.limitsMu.Unlock()
		return
	}
	wait := a.startLimitsFetchLocked(serverKey, api)
	a.limitsMu.Unlock()
	<-wait
}

func (a *Adapter) startLimitsFetchLocked(serverKey string, api *httpClient) chan struct{} {
	if a.limitsAt == nil {
		a.limitsAt = make(map[string]time.Time)
	}
	if a.limitsFetching == nil {
		a.limitsFetching = make(map[string]chan struct{})
	}
	a.limitsAt[serverKey] = time.Now()
	done := make(chan struct{})
	a.limitsFetching[serverKey] = done
	go func() {
		defer close(done)
		a.fetchLimits(serverKey, api)
		a.limitsMu.Lock()
		delete(a.limitsFetching, serverKey)
		a.limitsMu.Unlock()
	}()
	return done
}

func (a *Adapter) fetchLimits(serverKey string, api *httpClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var catalog providerCatalog
	if err := api.do(ctx, http.MethodGet, "/config/providers", nil, &catalog); err != nil {
		return
	}
	table := make(map[string]int64)
	for _, p := range catalog.Providers {
		for id, model := range p.Models {
			if model.Limit.Context > 0 {
				table[p.ID+"/"+id] = model.Limit.Context
			}
		}
	}
	a.limitsMu.Lock()
	defer a.limitsMu.Unlock()
	if a.limits == nil {
		a.limits = make(map[string]map[string]int64)
	}
	a.limits[serverKey] = table
}
