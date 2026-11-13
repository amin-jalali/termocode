package dbclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
)

// DBX-51, part two: the new-key form, RedisJSON paths, module info
// (TimeSeries, Bloom / Cuckoo / Top-K / Count-Min, Search), readable
// grids for MEMORY STATS, XINFO, MODULE LIST and CLUSTER NODES, cluster
// MOVED / ASK redirects, Sentinel, key export and a bounded channel
// listen.

func init() {
	for _, c := range []string{
		"TS.INFO", "TS.GET", "TS.RANGE", "TS.REVRANGE", "TS.MGET", "TS.MRANGE", "TS.MREVRANGE", "TS.QUERYINDEX",
		"BF.INFO", "BF.EXISTS", "BF.MEXISTS", "BF.CARD", "CF.INFO", "CF.EXISTS", "CF.MEXISTS", "CF.COUNT",
		"TOPK.INFO", "TOPK.LIST", "TOPK.QUERY", "TOPK.COUNT", "CMS.INFO", "CMS.QUERY",
		"FT._LIST", "FT.INFO", "FT.SEARCH", "FT.AGGREGATE", "FT.EXPLAIN", "FT.TAGVALS", "FT.SPELLCHECK", "FT.SYNDUMP",
		"JSON.RESP", "JSON.ARRINDEX",
	} {
		redisReads[c] = true
	}
}

// RedisNewKeyTypes are the types the new-key form makes.
var RedisNewKeyTypes = []string{"string", "hash", "list", "set", "zset", "stream", "json"}

// RedisNewKeyArgs is the command that makes key of type typ from the
// typed value: a string (SET … NX), "field value …" (hash, stream),
// one item (list, set), "score member" (zset) or a JSON document
// (JSON.SET … NX). SET, ZADD and JSON.SET never replace an existing value.
func RedisNewKeyArgs(typ, key, value string) ([]string, error) {
	if strings.TrimSpace(key) == "" {
		return nil, errors.New("type a key name")
	}
	words := func(min int, even bool) ([]string, error) {
		w, err := ParseRedisCommand(value)
		if err != nil {
			return nil, err
		}
		if len(w) < min || (even && len(w)%2 != 0) {
			return nil, errors.New("type field value pairs, such as: name Ada")
		}
		return w, nil
	}
	switch typ {
	case "string":
		return []string{"SET", key, value, "NX"}, nil
	case "hash":
		w, err := words(2, true)
		if err != nil {
			return nil, err
		}
		return append([]string{"HSET", key}, w...), nil
	case "list", "set":
		if value == "" {
			return nil, errors.New("type the first item")
		}
		if typ == "list" {
			return []string{"RPUSH", key, value}, nil
		}
		return []string{"SADD", key, value}, nil
	case "zset":
		w, err := ParseRedisCommand(value)
		if err != nil {
			return nil, err
		}
		if len(w) != 2 {
			return nil, errors.New("type a score and a member, such as: 1.5 ada")
		}
		if _, err := strconv.ParseFloat(w[0], 64); err != nil {
			return nil, errors.New("the score must be a number")
		}
		return []string{"ZADD", key, "NX", w[0], w[1]}, nil
	case "stream":
		w, err := words(2, true)
		if err != nil {
			return nil, err
		}
		return append([]string{"XADD", key, "*"}, w...), nil
	case "json":
		if !json.Valid([]byte(value)) {
			return nil, errors.New("the value is not valid JSON")
		}
		return []string{"JSON.SET", key, "$", value, "NX"}, nil
	}
	return nil, fmt.Errorf("unknown type %q (use %s)", typ, strings.Join(RedisNewKeyTypes, ", "))
}

// RedisJSONGet is JSON.GET key path ("$", the whole document, when empty).
func RedisJSONGet(key, path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "$"
	}
	return []string{"JSON.GET", key, path}
}

// RedisModuleInfoArgs is the read-only info command of a module key type
// (TYPE reply), and XINFO GROUPS for a stream; ok=false for other types.
func RedisModuleInfoArgs(typ, key string) ([]string, bool) {
	switch typ {
	case "TSDB-TYPE":
		return []string{"TS.INFO", key}, true
	case "MBbloom--":
		return []string{"BF.INFO", key}, true
	case "MBbloomCF":
		return []string{"CF.INFO", key}, true
	case "TopK-TYPE":
		return []string{"TOPK.INFO", key}, true
	case "CMSk-TYPE":
		return []string{"CMS.INFO", key}, true
	case "ReJSON-RL":
		return RedisJSONGet(key, ""), true
	case "stream":
		return []string{"XINFO", "GROUPS", key}, true
	}
	return nil, false
}

// redisPairs reads a flat field/value array; ok=false when it is not one.
func redisPairs(reply any) ([][2]any, bool) {
	arr, ok := reply.([]any)
	if !ok || len(arr)%2 != 0 {
		return nil, false
	}
	out := make([][2]any, 0, len(arr)/2)
	for i := 0; i < len(arr); i += 2 {
		switch arr[i].(type) {
		case []byte, string:
		default:
			return nil, false
		}
		out = append(out, [2]any{arr[i], arr[i+1]})
	}
	return out, true
}

// redisFlat is a reply value on one line: pairs as k=v, arrays as a, b.
func redisFlat(v any) string {
	arr, ok := v.([]any)
	if !ok {
		if v == nil {
			return "(nil)"
		}
		return redisText(v)
	}
	parts := make([]string, 0, len(arr))
	if pairs, ok := redisPairs(v); ok && len(pairs) > 0 {
		for _, p := range pairs {
			parts = append(parts, redisText(p[0])+"="+redisFlat(p[1]))
		}
		return strings.Join(parts, ", ")
	}
	for _, e := range arr {
		parts = append(parts, redisFlat(e))
	}
	return strings.Join(parts, ", ")
}

// RedisPairsResult shows a field/value reply as field | value rows.
func RedisPairsResult(reply any) (Result, bool) {
	pairs, ok := redisPairs(reply)
	if !ok {
		return Result{}, false
	}
	res := Result{Columns: []Column{{Name: "field"}, {Name: "value"}}}
	for _, p := range pairs {
		res.Rows = append(res.Rows, []Cell{{Text: RedisDisplay(redisText(p[0]))}, {Text: RedisDisplay(redisFlat(p[1]))}})
	}
	if len(res.Rows) == 0 {
		res.Notices = []string{"The reply has no fields"}
	}
	return res, true
}

// RedisRecordsResult shows a list of field/value records as one row each,
// with the field names (in order of first use) as columns.
func RedisRecordsResult(reply any) (Result, bool) {
	arr, ok := reply.([]any)
	if !ok {
		return Result{}, false
	}
	var names []string
	index := map[string]int{}
	recs := make([]map[string]string, 0, len(arr))
	for _, it := range arr {
		pairs, ok := redisPairs(it)
		if !ok {
			return Result{}, false
		}
		rec := map[string]string{}
		for _, p := range pairs {
			n := redisText(p[0])
			if _, seen := index[n]; !seen {
				index[n] = len(names)
				names = append(names, n)
			}
			rec[n] = redisFlat(p[1])
		}
		recs = append(recs, rec)
	}
	res := Result{}
	for _, n := range names {
		res.Columns = append(res.Columns, Column{Name: RedisDisplay(n)})
	}
	for _, rec := range recs {
		row := make([]Cell, len(names))
		for i, n := range names {
			row[i] = Cell{Text: RedisDisplay(rec[n])}
		}
		res.Rows = append(res.Rows, row)
	}
	if len(res.Rows) == 0 {
		res.Notices = []string{"No entries"}
	}
	return res, true
}

// RedisClusterNodesResult shows CLUSTER NODES text: one row per node.
func RedisClusterNodesResult(text string) Result {
	res := Result{Columns: []Column{{Name: "id"}, {Name: "address"}, {Name: "flags"}, {Name: "master"}, {Name: "link"}, {Name: "slots"}}}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 8 {
			continue
		}
		addr, _, _ := strings.Cut(f[1], "@")
		res.Rows = append(res.Rows, []Cell{
			{Text: RedisDisplay(f[0])}, {Text: RedisDisplay(addr)}, {Text: RedisDisplay(f[2])},
			{Text: RedisDisplay(f[3])}, {Text: RedisDisplay(f[7])}, {Text: RedisDisplay(strings.Join(f[8:], " "))},
		})
	}
	if len(res.Rows) == 0 {
		res.Notices = []string{"CLUSTER NODES listed no nodes (is this a cluster?)"}
	}
	return res
}

// redisMoreGrid is RedisAdminGrid for the commands of this file.
func redisMoreGrid(cmd, sub string, reply any) (Result, bool) {
	switch {
	case cmd == "MEMORY" && sub == "STATS", cmd == "XINFO" && sub == "STREAM",
		cmd == "PUBSUB" && (sub == "NUMSUB" || sub == "SHARDNUMSUB"),
		cmd == "TS.INFO", cmd == "BF.INFO", cmd == "CF.INFO", cmd == "TOPK.INFO", cmd == "CMS.INFO", cmd == "FT.INFO":
		return RedisPairsResult(reply)
	case cmd == "XINFO" && (sub == "GROUPS" || sub == "CONSUMERS"), cmd == "MODULE" && sub == "LIST":
		return RedisRecordsResult(reply)
	case cmd == "CLUSTER" && sub == "NODES":
		switch x := reply.(type) {
		case []byte:
			return RedisClusterNodesResult(string(x)), true
		case string:
			return RedisClusterNodesResult(x), true
		}
	}
	return Result{}, false
}

// redisRedirectOf reads a cluster MOVED / ASK error: the node that owns
// the key, and whether it is ASK (send ASKING first).
func redisRedirectOf(err error) (addr string, ask, ok bool) {
	if err == nil {
		return "", false, false
	}
	f := strings.Fields(err.Error())
	if len(f) < 3 || (f[0] != "MOVED" && f[0] != "ASK") {
		return "", false, false
	}
	if _, _, serr := net.SplitHostPort(f[2]); serr != nil {
		return "", false, false
	}
	return f[2], f[0] == "ASK", true
}

// redisSentinelAddr reads SENTINEL GET-MASTER-ADDR-BY-NAME: host, port.
func redisSentinelAddr(reply any) (string, error) {
	arr, ok := reply.([]any)
	if !ok || len(arr) != 2 {
		return "", errors.New("the sentinel does not know this master name")
	}
	host, port := redisText(arr[0]), redisText(arr[1])
	if host == "" || port == "" {
		return "", errors.New("the sentinel sent no master address")
	}
	return net.JoinHostPort(host, port), nil
}

// redisSentinelName is the master name of a Sentinel connection
// ("params": {"sentinel": "mymaster"}, or the older "sentinelMaster"),
// "" for a plain one.
func redisSentinelName(c Conn) string {
	if name := strings.TrimSpace(c.Params["sentinel"]); name != "" {
		return name
	}
	return strings.TrimSpace(c.Params["sentinelMaster"])
}

// dialOpts are the redigo options of every connection of the session.
func (s *RedisSession) dialOpts(db int) ([]redis.DialOption, error) {
	dial := s.dial
	if dial == nil && s.cfg.Dial != nil {
		dial = RedisDialFunc(s.cfg.Dial)
	}
	if dial == nil {
		d := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: -1}
		dial = d.DialContext
	}
	opts := []redis.DialOption{redis.DialContextFunc(dial), redis.DialDatabase(db)}
	if s.password != "" {
		opts = append(opts, redis.DialPassword(s.password))
		if s.target.user != "" {
			opts = append(opts, redis.DialUsername(s.target.user))
		}
	}
	cfg, err := s.tlsConfig()
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		opts = append(opts, redis.DialUseTLS(true), redis.DialTLSConfig(cfg))
	}
	return opts, nil
}

// serverAddr is the address to dial: the master the Sentinels name
// (asked on every connect, so a failover is followed; more Sentinels in
// "params": {"sentinels": "s2:26379,s3:26379"}), else the connection's
// own. The Sentinels are asked without the password.
func (s *RedisSession) serverAddr(ctx context.Context, opts []redis.DialOption) (string, error) {
	name := redisSentinelName(s.cfg)
	if name == "" {
		return s.target.addr, nil
	}
	if s.sentinels == nil {
		s.sentinels = redisSentinelAddrs(s.target.addr, s.cfg.Params["sentinels"])
	}
	sopts := []redis.DialOption{opts[0]}
	if cfg, err := s.tlsConfig(); err == nil && cfg != nil {
		sopts = append(sopts, redis.DialUseTLS(true), redis.DialTLSConfig(cfg))
	}
	addr, err := redisSentinelMaster(ctx, sopts, s.sentinels, name)
	if err != nil {
		return "", redisError(err, s.password)
	}
	return addr, nil
}

// doAt runs args once on the cluster node addr (a MOVED / ASK redirect)
// on a short-lived connection.
func (s *RedisSession) doAt(ctx context.Context, addr string, ask bool, args []string) (any, error) {
	opts, err := s.dialOpts(0)
	if err != nil {
		return nil, err
	}
	c, err := redis.DialContext(ctx, "tcp", addr, opts...)
	if err != nil {
		return nil, redisError(err, s.password)
	}
	defer c.Close()
	if ask {
		if _, err := redis.DoContext(c, ctx, "ASKING"); err != nil {
			return nil, redisError(err, s.password)
		}
	}
	rest := make([]any, len(args)-1)
	for i, a := range args[1:] {
		rest[i] = a
	}
	reply, err := redis.DoContext(c, ctx, args[0], rest...)
	return reply, redisError(err, s.password)
}

// RedisExportEntry is one exported key. Value is a string, a map (hash),
// a list (list, set), member/score pairs (zset), entries (stream), the
// JSON document, or null for other module types.
type RedisExportEntry struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	TTLms int64  `json:"ttl_ms,omitempty"`
	Value any    `json:"value"`
}

type redisZEntry struct {
	Member string `json:"member"`
	Score  string `json:"score"`
}

type redisXEntry struct {
	ID     string            `json:"id"`
	Fields map[string]string `json:"fields"`
}

func redisTexts(v any) []string {
	arr, _ := v.([]any)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, redisText(e))
	}
	return out
}

func redisTextMap(v any) map[string]string {
	t := redisTexts(v)
	m := make(map[string]string, len(t)/2)
	for i := 0; i+1 < len(t); i += 2 {
		m[t[i]] = t[i+1]
	}
	return m
}

// RedisExportKeys reads keys with the command of each type (TYPE, PTTL,
// then GET / HGETALL / LRANGE / SMEMBERS / ZRANGE / XRANGE / JSON.GET).
// A key that is gone by then is left out.
func RedisExportKeys(ctx context.Context, do func(ctx context.Context, args []string) (any, error), keys []string) ([]RedisExportEntry, error) {
	var out []RedisExportEntry
	for _, k := range keys {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		t, err := do(ctx, []string{"TYPE", k})
		if err != nil {
			return out, err
		}
		typ := redisText(t)
		if typ == "none" || typ == "" {
			continue
		}
		e := RedisExportEntry{Key: k, Type: typ}
		ttl, err := do(ctx, []string{"PTTL", k})
		if err != nil {
			return out, err
		}
		if n, ok := ttl.(int64); ok && n > 0 {
			e.TTLms = n
		}
		var cmd []string
		switch typ {
		case "string":
			cmd = []string{"GET", k}
		case "hash":
			cmd = []string{"HGETALL", k}
		case "list":
			cmd = []string{"LRANGE", k, "0", "-1"}
		case "set":
			cmd = []string{"SMEMBERS", k}
		case "zset":
			cmd = []string{"ZRANGE", k, "0", "-1", "WITHSCORES"}
		case "stream":
			cmd = []string{"XRANGE", k, "-", "+"}
		case "ReJSON-RL":
			cmd = []string{"JSON.GET", k}
		}
		if cmd != nil {
			v, err := do(ctx, cmd)
			if err != nil {
				return out, fmt.Errorf("%s: %w", RedisDisplay(k), err)
			}
			switch typ {
			case "string":
				e.Value = redisText(v)
			case "hash":
				e.Value = redisTextMap(v)
			case "list":
				e.Value = redisTexts(v)
			case "set":
				m := redisTexts(v)
				sort.Strings(m)
				e.Value = m
			case "zset":
				t := redisTexts(v)
				z := make([]redisZEntry, 0, len(t)/2)
				for i := 0; i+1 < len(t); i += 2 {
					z = append(z, redisZEntry{Member: t[i], Score: t[i+1]})
				}
				e.Value = z
			case "stream":
				arr, _ := v.([]any)
				xs := make([]redisXEntry, 0, len(arr))
				for _, it := range arr {
					f, _ := it.([]any)
					if len(f) == 2 {
						xs = append(xs, redisXEntry{ID: redisText(f[0]), Fields: redisTextMap(f[1])})
					}
				}
				e.Value = xs
			case "ReJSON-RL":
				if doc := redisText(v); json.Valid([]byte(doc)) {
					e.Value = json.RawMessage(doc)
				} else {
					e.Value = doc
				}
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// RedisMessage is one message a channel listen received.
type RedisMessage struct {
	Channel, Pattern, Data string
}

// Listen subscribes to the channel pattern on its own connection and
// collects messages until ctx ends or max arrived (more=true). The end of
// ctx is the normal end, not an error.
func (s *RedisSession) Listen(ctx context.Context, pattern string, max int) (msgs []RedisMessage, more bool, err error) {
	opts, err := s.dialOpts(0)
	if err != nil {
		return nil, false, err
	}
	addr, err := s.serverAddr(ctx, opts)
	if err != nil {
		return nil, false, err
	}
	c, err := redis.DialContext(ctx, "tcp", addr, opts...)
	if err != nil {
		return nil, false, redisError(err, s.password)
	}
	defer c.Close()
	psc := redis.PubSubConn{Conn: c}
	if err := psc.PSubscribe(pattern); err != nil {
		return nil, false, redisError(err, s.password)
	}
	for {
		switch v := psc.ReceiveContext(ctx).(type) {
		case redis.Message:
			msgs = append(msgs, RedisMessage{Channel: v.Channel, Pattern: v.Pattern, Data: string(v.Data)})
			if len(msgs) >= max {
				return msgs, true, nil
			}
		case error:
			if ctx.Err() != nil {
				return msgs, false, nil
			}
			// The read deadline comes from the context and may fire a
			// moment before the context reports it: still the normal end.
			var ne net.Error
			if dl, ok := ctx.Deadline(); ok && errors.As(v, &ne) && ne.Timeout() && !time.Now().Before(dl.Add(-100*time.Millisecond)) {
				return msgs, false, nil
			}
			return msgs, false, redisError(v, s.password)
		}
	}
}

// RedisMessagesResult shows the messages of a listen.
func RedisMessagesResult(msgs []RedisMessage, more bool) Result {
	res := Result{Columns: []Column{{Name: "#"}, {Name: "channel"}, {Name: "pattern"}, {Name: "message"}}, Truncated: more}
	for i, m := range msgs {
		res.Rows = append(res.Rows, []Cell{{Text: strconv.Itoa(i + 1), Kind: "number"}, {Text: RedisDisplay(m.Channel)}, {Text: RedisDisplay(m.Pattern)}, {Text: RedisDisplay(m.Data)}})
	}
	if len(res.Rows) == 0 {
		res.Notices = []string{"No message arrived while listening"}
	}
	return res
}
