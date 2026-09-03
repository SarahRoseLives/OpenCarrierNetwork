package services

import (
	"fmt"
	"log"
	"sync"
)

type Registry struct {
	services map[string]Service
	mu       sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		services: make(map[string]Service),
	}
}

func (r *Registry) Register(s Service) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.services[s.Code()] = s
	log.Printf("Registered service: %s — %s", s.Code(), s.Name())
}

func (r *Registry) Get(code string) (Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.services[code]
	if !ok {
		return nil, fmt.Errorf("service %s not found", code)
	}
	return s, nil
}

func (r *Registry) IsServiceCode(code string) bool {
	return len(code) >= 3 && code[0] == '*'
}
