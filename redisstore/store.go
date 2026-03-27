// Package redisstore provides a Redis-backed StateStore for circuitbreaker.
// Use this when running multiple instances and you need shared breaker state.
//
// Each breaker uses two Redis keys:
//   - cb:{prefix}:{name}:state  → integer (0=closed, 1=open, 2=half-open), with TTL when open
//   - cb:{prefix}:{name}:counts → hash with counters
package redisstore

import (
	"context"
	"fmt"
	"strconv"
	"time"

	cb "github.com/aqylsoft/circuitbreaker"
	"github.com/redis/go-redis/v9"
)

const defaultPrefix = "cb"

// Store is a Redis-backed StateStore implementation.
type Store struct {
	client redis.Cmdable
	prefix string

	incrFailureScript *redis.Script
	incrSuccessScript *redis.Script
}

// Option configures a Store.
type Option func(*Store)

// WithPrefix sets a custom key prefix (default: "cb").
func WithPrefix(p string) Option {
	return func(s *Store) { s.prefix = p }
}

// New creates a new Redis-backed Store.
// client can be *redis.Client, *redis.ClusterClient, or any redis.Cmdable.
func New(client redis.Cmdable, opts ...Option) *Store {
	s := &Store{
		client: client,
		prefix: defaultPrefix,
	}
	for _, o := range opts {
		o(s)
	}

	// Lua scripts for atomic counter increments.
	// KEYS[1] = counts hash key
	s.incrFailureScript = redis.NewScript(`
		local key = KEYS[1]
		redis.call('HINCRBY', key, 'requests', 1)
		redis.call('HINCRBY', key, 'total_failures', 1)
		redis.call('HSET', key, 'consec_succ', 0)
		local cf = redis.call('HINCRBY', key, 'consec_fails', 1)
		local req = redis.call('HGET', key, 'requests')
		local tf  = redis.call('HGET', key, 'total_failures')
		local ts  = redis.call('HGET', key, 'total_successes')
		return {req, tf, ts or '0', cf, '0'}
	`)

	s.incrSuccessScript = redis.NewScript(`
		local key = KEYS[1]
		redis.call('HINCRBY', key, 'requests', 1)
		redis.call('HINCRBY', key, 'total_successes', 1)
		redis.call('HSET', key, 'consec_fails', 0)
		local cs = redis.call('HINCRBY', key, 'consec_succ', 1)
		local req = redis.call('HGET', key, 'requests')
		local tf  = redis.call('HGET', key, 'total_failures')
		local ts  = redis.call('HGET', key, 'total_successes')
		return {req, tf or '0', ts, '0', cs}
	`)

	return s
}

func (s *Store) stateKey(name string) string {
	return fmt.Sprintf("%s:%s:state", s.prefix, name)
}

func (s *Store) countsKey(name string) string {
	return fmt.Sprintf("%s:%s:counts", s.prefix, name)
}

func (s *Store) GetState(name string) (cb.State, cb.Counts, error) {
	ctx := context.Background()

	stateStr, err := s.client.Get(ctx, s.stateKey(name)).Result()
	var state cb.State
	if err == redis.Nil {
		state = cb.StateClosed
	} else if err != nil {
		return cb.StateClosed, cb.Counts{}, err
	} else {
		v, _ := strconv.Atoi(stateStr)
		state = cb.State(v)
	}

	fields, err := s.client.HGetAll(ctx, s.countsKey(name)).Result()
	if err != nil && err != redis.Nil {
		return state, cb.Counts{}, err
	}

	counts := cb.Counts{
		Requests:         parseUint32(fields["requests"]),
		TotalFailures:    parseUint32(fields["total_failures"]),
		TotalSuccesses:   parseUint32(fields["total_successes"]),
		ConsecutiveFails: parseUint32(fields["consec_fails"]),
		ConsecutiveSucc:  parseUint32(fields["consec_succ"]),
	}

	return state, counts, nil
}

func (s *Store) SetState(name string, state cb.State, ttl time.Duration) error {
	ctx := context.Background()
	key := s.stateKey(name)
	val := strconv.Itoa(int(state))

	if ttl > 0 {
		return s.client.Set(ctx, key, val, ttl).Err()
	}
	return s.client.Set(ctx, key, val, 0).Err()
}

func (s *Store) IncrFailure(name string) (cb.Counts, error) {
	ctx := context.Background()
	res, err := s.incrFailureScript.Run(ctx, s.client, []string{s.countsKey(name)}).Int64Slice()
	if err != nil {
		return cb.Counts{}, err
	}
	return sliceToCounts(res), nil
}

func (s *Store) IncrSuccess(name string) (cb.Counts, error) {
	ctx := context.Background()
	res, err := s.incrSuccessScript.Run(ctx, s.client, []string{s.countsKey(name)}).Int64Slice()
	if err != nil {
		return cb.Counts{}, err
	}
	return sliceToCounts(res), nil
}

func (s *Store) Reset(name string) error {
	ctx := context.Background()
	return s.client.Del(ctx, s.stateKey(name), s.countsKey(name)).Err()
}

func sliceToCounts(v []int64) cb.Counts {
	if len(v) < 5 {
		return cb.Counts{}
	}
	return cb.Counts{
		Requests:         uint32(v[0]),
		TotalFailures:    uint32(v[1]),
		TotalSuccesses:   uint32(v[2]),
		ConsecutiveFails: uint32(v[3]),
		ConsecutiveSucc:  uint32(v[4]),
	}
}

func parseUint32(s string) uint32 {
	v, _ := strconv.ParseUint(s, 10, 32)
	return uint32(v)
}
