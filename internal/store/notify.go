package store

import "context"

// Seq increases every time stored data changes.
func (s *Store) Seq() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seq
}

func (s *Store) bump(seq int64) {
	s.mu.Lock()
	if seq > s.seq {
		s.seq = seq
	}
	close(s.waiters)
	s.waiters = make(chan struct{})
	s.mu.Unlock()
}

// Wait blocks until Seq is past after or ctx ends, and returns Seq.
func (s *Store) Wait(ctx context.Context, after int64) int64 {
	for {
		s.mu.Lock()
		seq, ch := s.seq, s.waiters
		s.mu.Unlock()
		if seq > after {
			return seq
		}
		select {
		case <-ch:
		case <-ctx.Done():
			return s.Seq()
		}
	}
}
