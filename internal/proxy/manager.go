package proxy

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/D00Movenok/BounceBack/internal/common"
	"github.com/D00Movenok/BounceBack/internal/database"
	"github.com/D00Movenok/BounceBack/internal/filters"
	"github.com/D00Movenok/BounceBack/internal/proxy/http"

	"github.com/rs/zerolog/log"
)

func NewManager(db *database.DB, cfg *common.Config) (*Manager, error) {
	fs, err := filters.NewFilterSet(db, cfg.Filters)
	if err != nil {
		return nil, fmt.Errorf("can't create filters: %w", err)
	}

	proxies := make([]Proxy, 0)
	for _, pc := range cfg.Proxies {
		var p Proxy
		switch pc.Type {
		case http.ProxyType:
			if p, err = http.NewProxy(pc, fs, db); err != nil {
				return nil, fmt.Errorf(
					"can't create proxy \"%s\": %w",
					pc.Name,
					err,
				)
			}
		default:
			return nil, fmt.Errorf("invalid proxy type: %s", pc.Type)
		}
		proxies = append(proxies, p)
	}

	m := &Manager{proxies}
	return m, nil
}

type Manager struct {
	proxies []Proxy
}

func (m *Manager) StartAll() error {
	for i, p := range m.proxies {
		p.GetFullInfoLogger().Info().Msg("Starting proxy")
		if err := p.Start(); err != nil {
			ctx, cancel := context.WithTimeout(
				context.Background(),
				time.Second*5, //nolint:gomnd
			)
			defer cancel()
			for j := 0; j < i; j++ {
				if serr := m.proxies[j].Shutdown(ctx); serr != nil {
					log.Error().Err(serr).Msgf(
						"Error shutting down %s forcefully",
						m.proxies[j],
					)
				}
			}
			return fmt.Errorf("can't start %s: %w", p, err)
		}
	}
	return nil
}

func (m *Manager) Shutdown(ctx context.Context) error {
	wg := sync.WaitGroup{}
	wg.Add(len(m.proxies))
	errCh := make(chan error)
	for _, p := range m.proxies {
		p.GetFullInfoLogger().Info().Msg("Shutting down proxy")
		go func(p Proxy) {
			defer wg.Done()
			if err := p.Shutdown(ctx); err != nil {
				select {
				case errCh <- fmt.Errorf("can't shutdown %s: %w", p, err):
				default:
				}
			}
		}(p)
	}
	wg.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}